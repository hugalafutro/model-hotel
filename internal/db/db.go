// Package db provides database connection and migration management.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// migrationsFS is the filesystem used for reading migration files.
// It can be overridden in tests to inject errors.
var migrationsFS fs.FS

func init() {
	migrationsFS = embeddedMigrations
}

// DB manages the PostgreSQL connection pool and migrations.
type DB struct {
	pool *pgxpool.Pool
}

// New creates a new DB instance, runs migrations, and returns the database connection.
func New(ctx context.Context, databaseURL string, maxConns, minConns int32) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	if maxConns > 0 {
		config.MaxConns = maxConns
	}
	if minConns > 0 {
		config.MinConns = minConns
	}
	config.MaxConnLifetime = 4 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute
	config.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	db := &DB{pool: pool}

	if err := db.runMigrations(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection pool.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Pool returns the underlying pgx connection pool.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Begin starts a new database transaction.
func (db *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.pool.Begin(ctx)
}

func (db *DB) runMigrations(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var applied, skipped int

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !entry.Type().IsRegular() {
			continue
		}

		filename := entry.Name()
		if filename[0] == '.' {
			continue
		}

		migrationPath := "migrations/" + filename
		content, err := fs.ReadFile(migrationsFS, migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		newlyApplied, err := db.runMigration(ctx, filename, string(content))
		if err != nil {
			return fmt.Errorf("failed to run migration %s: %w", filename, err)
		}
		if newlyApplied {
			applied++
		} else {
			skipped++
		}
	}

	switch {
	case applied > 0 && skipped > 0:
		debuglog.Info("db: Migrations complete", "applied", applied, "skipped", skipped)
	case applied > 0:
		debuglog.Info("db: Migrations complete", "applied", applied)
	case skipped > 0:
		debuglog.Info("db: Migrations already applied", "count", skipped)
	}

	return nil
}

func (db *DB) runMigration(ctx context.Context, name, sql string) (bool, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_tables
			WHERE schemaname = 'public'
			AND tablename = 'schema_migrations'
		)
	`).Scan(&exists)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("failed to check schema_migrations table: %w", err)
	}

	if !exists {
		_, err = tx.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS schema_migrations (
				id SERIAL PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				applied_at TIMESTAMPTZ DEFAULT now()
			)
		`)
		if err != nil {
			return false, fmt.Errorf("failed to create schema_migrations table: %w", err)
		}
	}

	var applied bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM schema_migrations WHERE name = $1
		)
	`, name).Scan(&applied)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	if applied {
		debuglog.Debug("db: Migration already applied, skipping", "name", name)
		return false, nil
	}

	debuglog.Info("db: Applying migration", "name", name)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return false, fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO schema_migrations (name) VALUES ($1)
	`, name)
	if err != nil {
		return false, fmt.Errorf("failed to record migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit migration: %w", err)
	}

	debuglog.Info("db: Successfully applied migration", "name", name)
	return true, nil
}

// WaitForReady polls the database until it responds or maxAttempts is reached.
func (db *DB) WaitForReady(ctx context.Context, maxAttempts int) error {
	for i := 0; i < maxAttempts; i++ {
		err := db.pool.Ping(ctx)
		if err == nil {
			return nil
		}

		debuglog.Info("db: Database not ready", "attempt", i+1, "max", maxAttempts, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return fmt.Errorf("database not ready after %d attempts", maxAttempts)
}
