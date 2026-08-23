// Package main implements the Sunred API server entry point.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/joho/godotenv"

	"github.com/fuegoio/sunred/go/api/internal/api"
	"github.com/fuegoio/sunred/go/api/internal/atproto"
	"github.com/fuegoio/sunred/go/api/internal/auth"
	"github.com/fuegoio/sunred/go/api/internal/config"
	"github.com/fuegoio/sunred/go/api/internal/cors"
	"github.com/fuegoio/sunred/go/api/internal/httplog"
	"github.com/fuegoio/sunred/go/api/internal/logging"
	"github.com/fuegoio/sunred/go/api/internal/migrations"
	"github.com/fuegoio/sunred/go/api/internal/reader/fetcher"
	"github.com/fuegoio/sunred/go/api/internal/reader/processor"
	"github.com/fuegoio/sunred/go/api/internal/scheduler"
	"github.com/fuegoio/sunred/go/api/internal/store"
	"github.com/fuegoio/sunred/go/api/internal/worker"

	_ "github.com/lib/pq"
)

func main() {
	code, err := run()
	if err != nil {
		slog.Error("api: fatal", "err", err)
		os.Exit(1)
	}
	if code != 0 {
		os.Exit(code)
	}
}

func run() (int, error) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	migrateOnly := flag.Bool("migrate", false, "Run migrations and exit")
	dumpOpenAPI := flag.Bool("openapi", false, "Print OpenAPI spec as JSON and exit")
	flag.Parse()

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("api: load .env", "err", err)
	}

	// The OpenAPI spec is derived from huma operations + struct tags alone.
	// Short-circuit before any DB or auth dependency so --openapi works without
	// a running Postgres or a LIMEN_SECRET.
	if *dumpOpenAPI {
		humaMux := http.NewServeMux()
		humaConfig := huma.DefaultConfig("Sunred API", "1.0.0")
		humaConfig.Servers = []*huma.Server{{URL: ""}}
		humaConfig.Tags = api.OpenAPITags()
		humaRouter := humago.New(humaMux, humaConfig)

		apiHandler := api.New(humaRouter, nil, nil, nil, nil)
		apiHandler.RegisterRoutes()

		b, err := humaRouter.OpenAPI().MarshalJSON()
		if err != nil {
			return 0, fmt.Errorf("marshal openapi: %w", err)
		}
		fmt.Println(string(b))
		return 0, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return 0, fmt.Errorf("config: %w", err)
	}

	if _, err := logging.Init(cfg.LogFormat, cfg.LogLevel, os.Stderr); err != nil {
		return 0, fmt.Errorf("logging: %w", err)
	}
	slog.Info("api: starting", "format", cfg.LogFormat, "level", cfg.LogLevel, "addr", cfg.HTTPAddr, "relay", cfg.RelayURL)

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("api: connecting to database")
	if err := db.Ping(); err != nil {
		slog.Error("api: database ping failed", "err", err)
		return 0, fmt.Errorf("ping db: %w", err)
	}
	slog.Info("api: database connected")

	if err := migrations.Run(db); err != nil {
		return 0, fmt.Errorf("migrate: %w", err)
	}
	if *migrateOnly {
		slog.Info("api: migrations complete")
		return 0, nil
	}

	st := store.New(db)
	authInst, err := auth.New(cfg, db, st)
	if err != nil {
		return 0, fmt.Errorf("auth: %w", err)
	}

	// AT Proto OAuth client (indigo). Backed by the same Postgres for
	// auth-request and session persistence.
	oauthApp, err := atproto.NewOAuthApp(db, cfg.OAuthClientID, cfg.OAuthCallbackURL)
	if err != nil {
		return 0, fmt.Errorf("oauth: %w", err)
	}

	humaMux := http.NewServeMux()
	humaConfig := huma.DefaultConfig("Sunred API", "1.0.0")
	humaConfig.Servers = []*huma.Server{{URL: ""}}
	humaConfig.Tags = api.OpenAPITags()
	humaRouter := humago.New(humaMux, humaConfig)

	f := fetcher.New(cfg.HTTPTimeout, cfg.HTTPMaxBody, "Sunred")
	apiHandler := api.New(humaRouter, st, authInst, cfg, f)
	apiHandler.SetOAuthApp(atproto.NewOAuthAppAdapter(oauthApp))
	apiHandler.RegisterRoutes()

	oauthHandlers := api.NewOAuthHandlers(oauthApp, st, authInst, cfg)

	mux := http.NewServeMux()

	// OAuth flow (public): login start, callback, client metadata, signout.
	for path, handler := range oauthHandlers.Routes() {
		mux.Handle(path, handler)
	}
	// Public device-flow endpoints (issue + poll) must be reachable without
	// a session; the confirm + status endpoints sit behind the middleware
	// because they require an authenticated user to approve the grant.
	for _, p := range api.PublicDevicePaths {
		mux.Handle(p, humaMux)
	}
	mux.Handle("/auth/device/confirm", authInst.Middleware(humaMux))
	mux.Handle("/auth/device/status", authInst.Middleware(humaMux))
	mux.Handle("/v1/health", humaMux)
	mux.Handle("/", authInst.Middleware(humaMux))
	mux.Handle("/.well-known/atproto-did", apiHandler.WellKnownATProtoDIDHandler())
	mux.Handle("/docs", humaRouter.Adapter())
	mux.Handle("/openapi.json", humaRouter.Adapter())

	if !cfg.DisableSched {
		proc := processor.New(st, f)
		pool := worker.New(proc, cfg.WorkerPool)
		sched := scheduler.New(st, pool, cfg.PollingFreq, cfg.BatchSize)
		go sched.Start(ctx)
	} else {
		slog.Info("api: scheduler disabled")
	}

	// Start the relay consumer so the API receives push events (backfill +
	// live) from the relay instead of pulling per-collection on each login.
	if cfg.RelayURL != "" {
		consumer := api.NewRelayConsumer(st, cfg.RelayURL, cfg.BaseURL)
		go consumer.Start(ctx)
	}

	slog.Info("api: listening", "addr", cfg.HTTPAddr)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httplog.Middleware(cors.Middleware(cfg.TrustedOrigins)(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		slog.Info("api: shutting down http server")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("api: http shutdown", "err", err)
		} else {
			slog.Info("api: http server shut down cleanly")
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return 0, fmt.Errorf("server: %w", err)
	}
	slog.Info("api: stopped")
	return 0, nil
}
