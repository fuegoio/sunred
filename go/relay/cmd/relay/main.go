// Package main is the Sunred relay server entrypoint.
//
// The relay:
//  1. Accepts announcements from Sunred instances when users connect AT Proto identities.
//  2. Maintains persistent WebSocket subscriptions to each announced DID's PDS repo stream.
//  3. Aggregates io.sunred.* record events into global counts (followers, shares, feed subs).
//  4. Streams events back to subscribed instances via the subscribeEvents WebSocket endpoint.
//
// Run: relay [--migrate]
//
// Environment variables (see internal/config/config.go):
//
//	RELAY_HTTP_ADDR        default :9090
//	RELAY_DATABASE_URL     required
//	RELAY_LOG_FORMAT       default pretty (pretty|json)
//	RELAY_LOG_LEVEL        default info (debug|info|warn|error)
//	RELAY_FANOUT_WORKERS   default 50
//	RELAY_RECONNECT_DELAY default 5s
//	RELAY_EVENT_RETENTION  default 168h (7 days)
package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/fuegoio/sunred/go/relay/internal/config"
	"github.com/fuegoio/sunred/go/relay/internal/fanout"
	"github.com/fuegoio/sunred/go/relay/internal/migrations"
	"github.com/fuegoio/sunred/go/relay/internal/server"
	"github.com/fuegoio/sunred/go/relay/internal/store"
)

// setupLogger configures the package-level slog default logger from cfg.
func setupLogger(cfg *config.Config) error {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "info", "":
	default:
		slog.Warn("relay: unknown log level, defaulting to info", "level", cfg.LogLevel)
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("relay: fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	migrateOnly := flag.Bool("migrate", false, "run migrations and exit")
	flag.Parse()

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("relay: load .env", "err", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := setupLogger(cfg); err != nil {
		return err
	}
	slog.Info("relay: starting", "format", cfg.LogFormat, "level", cfg.LogLevel, "addr", cfg.HTTPAddr)

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("relay: connecting to database")
	if err := db.Ping(); err != nil {
		slog.Error("relay: database ping failed", "err", err)
		return err
	}
	slog.Info("relay: database connected")
	if err := migrations.Run(db); err != nil {
		slog.Error("relay: migrations failed", "err", err)
		return err
	}
	if *migrateOnly {
		slog.Info("relay: migrations complete")
		return nil
	}

	st := store.New(db)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fan := fanout.New(st, cfg.ReconnectDelay)
	go func() {
		fan.Start(ctx)
		slog.Info("relay: fanout stopped")
	}()

	// Background: purge old relay events periodically.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		slog.Info("relay: purge loop started", "retention", cfg.EventRetention)
		for {
			select {
			case <-ctx.Done():
				slog.Info("relay: purge loop stopped")
				return
			case <-ticker.C:
				n, err := st.PurgeOldEvents(ctx, cfg.EventRetention)
				if err != nil {
					slog.Warn("relay: purge events", "err", err)
				} else if n > 0 {
					slog.Info("relay: purged old events", "count", n)
				}
			}
		}
	}()

	srv := server.New(st, fan)
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		slog.Info("relay: shutting down http server")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			slog.Error("relay: http shutdown", "err", err)
		} else {
			slog.Info("relay: http server shut down cleanly")
		}
	}()

	slog.Info("relay: listening", "addr", cfg.HTTPAddr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
