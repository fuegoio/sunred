// Package config loads relay configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds relay runtime configuration.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	LogFormat   string
	LogLevel    string
	// FanoutWorkers is the number of concurrent PDS subscription goroutines.
	FanoutWorkers int
	// ReconnectDelay is how long to wait before reconnecting a failed PDS subscription.
	ReconnectDelay time.Duration
	// EventRetention is how long to keep relay_events rows (for cursor replay).
	EventRetention time.Duration
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:       env("RELAY_HTTP_ADDR", ":9090"),
		DatabaseURL:    env("RELAY_DATABASE_URL", "postgres://sunred:sunred@localhost:5433/sunred_relay?sslmode=disable"),
		LogFormat:      env("RELAY_LOG_FORMAT", "pretty"),
		LogLevel:       env("RELAY_LOG_LEVEL", "info"),
		FanoutWorkers:  envInt("RELAY_FANOUT_WORKERS", 50),
		ReconnectDelay: envDuration("RELAY_RECONNECT_DELAY", 5*time.Second),
		EventRetention: envDuration("RELAY_EVENT_RETENTION", 7*24*time.Hour),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("RELAY_DATABASE_URL must be set")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
