// Package testdb provides a shared test database connector for integration tests.
// It isolates tests from the developer's local database by defaulting to a
// separate `sunred_test` database (overridable via SUNRED_DATABASE_URL), creating
// it if needed, and running migrations so the schema stays current.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/fuegoio/sunred/go/api/internal/migrations"
)

// DefaultDSN is the test database used when SUNRED_DATABASE_URL is not set.
// It targets a separate `sunred_test` database so the developer's `sunred`
// database (with real subscriptions and data) is never touched by tests.
const DefaultDSN = "postgres://sunred:sunred@localhost:5432/sunred_test?sslmode=disable"

// Connect returns a *sql.DB for the test database, creating it if it doesn't
// exist and applying any pending migrations. Tests are skipped (not failed)
// when Postgres is unreachable so `go test ./...` works without a running DB.
func Connect(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("SUNRED_DATABASE_URL")
	if dsn == "" {
		dsn = DefaultDSN
	}

	// Auto-create the target database if it doesn't exist yet. We connect
	// to the `postgres` maintenance database (always present) with the same
	// credentials and issue CREATE DATABASE IF NOT EXISTS equivalent.
	if err := ensureDatabase(dsn); err != nil {
		t.Skipf("could not ensure test database: %v", err)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("could not open database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database not reachable, skipping integration test: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

// ensureDatabase connects to the maintenance database and creates the target
// database named in the DSN if it doesn't already exist.
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil // nothing to create (e.g. connecting to maintenance db)
	}

	// Connect to the maintenance database to issue CREATE DATABASE.
	maintDSN := strings.Replace(dsn, "/"+dbName, "/postgres", 1)
	maint, err := sql.Open("postgres", maintDSN)
	if err != nil {
		return fmt.Errorf("open maintenance db: %w", err)
	}
	defer maint.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := maint.PingContext(ctx); err != nil {
		return fmt.Errorf("ping maintenance db: %w", err)
	}

	var exists bool
	err = maint.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := maint.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)); err != nil {
		return fmt.Errorf("create database %q: %w", dbName, err)
	}
	return nil
}
