// Package db provides database connection and migration management.
package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// migrationsFS is the filesystem used for reading migration files.
// It can be overridden in tests to inject errors.
var migrationsFS fs.FS

// migrationNames lists the migration filenames embedded in the binary, in the
// order fs.ReadDir returns them (lexical). Directories, non-regular entries and
// dotfiles are not migrations.
func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !e.Type().IsRegular() || e.Name()[0] == '.' {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// KnownMigrations returns the list of migration filenames embedded
// in the binary. Used by the backup restore validation to compare a dump's
// schema_migrations against the app's expected set.
func KnownMigrations() []string {
	names, err := migrationNames()
	if err != nil {
		return nil
	}
	return names
}

func init() {
	migrationsFS = embeddedMigrations
}

// DB manages the PostgreSQL connection pool and migrations.
type DB struct {
	pool *pgxpool.Pool
}

// ErrMigrations marks the one migration failure that is a deliberate refusal:
// a migration that raised its own exception rather than a statement the
// database would not take. Migration 082 refuses an install whose provider
// names collide once normalized, and that has to read as a schema the operator
// has to repair, not as anything that went wrong on its own.
//
// Reserved for exactly that. A pool that never connected, a statement timeout,
// a migration file that would not read and an ordinary SQL error are
// ErrMigrationFailed instead, because labelling them a refusal sends the
// operator looking for a decision nobody made.
var ErrMigrations = errors.New("migration refused")

// ErrMigrationFailed marks every other failure to apply the schema, so the
// caller can say "migration failed" for it rather than reporting a database it
// reached as one it could not connect to.
var ErrMigrationFailed = errors.New("failed to run migrations")

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
		// A migration that raised its own exception decided to refuse; anything
		// else merely failed. Only the first is the operator's to resolve.
		if IsRaisedException(err) {
			return nil, fmt.Errorf("%w: %w", ErrMigrations, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrMigrationFailed, err)
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
	names, err := migrationNames()
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// The ledger is created once, outside the per-migration transactions:
	// CREATE TABLE IF NOT EXISTS is idempotent, and asking every migration
	// whether it exists cost one round trip per file at every startup.
	if _, err := db.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMPTZ DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	// The checksum column is the ledger's own and lives here rather than in a
	// numbered migration: the ledger has to exist before any migration runs,
	// and a restored dump from a binary that predates the column gets it back
	// on the next start the same way. Probed before it is added, because ALTER
	// TABLE takes an exclusive lock before it finds the column already there,
	// and a start during a running backup would wait on the dump.
	var hasChecksum bool
	if err := db.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'schema_migrations' AND column_name = 'checksum'
		)
	`).Scan(&hasChecksum); err != nil {
		return fmt.Errorf("failed to inspect schema_migrations: %w", err)
	}
	if !hasChecksum {
		if _, err := db.pool.Exec(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT`); err != nil {
			return fmt.Errorf("failed to add schema_migrations.checksum: %w", err)
		}
	}

	var applied, skipped int

	for _, filename := range names {
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

// migrationChecksum is the hex SHA-256 of a migration file's text, the form
// schema_migrations.checksum stores.
func migrationChecksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

func (db *DB) runMigration(ctx context.Context, name, sql string) (bool, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	checksum := migrationChecksum(sql)

	// A row means applied; its checksum says which text was applied. NULL is a
	// row written before checksums were recorded and adopts the current file
	// silently, since there is nothing to compare it against.
	var stored *string
	err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name = $1`, name).Scan(&stored)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, fmt.Errorf("failed to check migration status: %w", err)
	default:
		return false, verifyAppliedMigration(ctx, tx, name, stored, checksum)
	}

	debuglog.Info("db: Applying migration", "name", name)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return false, fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)
	`, name, checksum)
	if err != nil {
		return false, fmt.Errorf("failed to record migration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit migration: %w", err)
	}

	debuglog.Info("db: Successfully applied migration", "name", name)
	return true, nil
}

// verifyAppliedMigration handles a migration the ledger already holds. When
// the file's text no longer matches what was applied, it records the new
// checksum and warns, so an edit to a shipped migration is named once per
// database rather than failing startup: the database holds the old text's
// effect and only a new migration can change that. Two binaries carrying
// different texts and sharing one database name it on every alternation. A
// NULL checksum, recorded before checksums existed, is adopted without a
// warning.
func verifyAppliedMigration(ctx context.Context, tx pgx.Tx, name string, stored *string, checksum string) error {
	if stored != nil && *stored == checksum {
		debuglog.Debug("db: Migration already applied, skipping", "name", name)
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE schema_migrations SET checksum = $2 WHERE name = $1`, name, checksum); err != nil {
		return fmt.Errorf("failed to record migration checksum: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit migration checksum: %w", err)
	}
	// Named after the record is durable: a start that fails to record it
	// fails outright and names the edit again next time.
	if stored == nil {
		debuglog.Debug("db: Migration already applied, checksum recorded", "name", name)
	} else {
		debuglog.Warn("db: migration file changed after it was applied; the database holds the effect of the old text and this start does not re-run it", "name", name, "applied_checksum", *stored, "file_checksum", checksum)
	}
	return nil
}

// waitForReadyInterval is the time between readiness check attempts.
// Can be reduced in tests for faster execution.
var waitForReadyInterval = 2 * time.Second

// WaitForReady polls the database until it responds or maxAttempts is reached.
func (db *DB) WaitForReady(ctx context.Context, maxAttempts int) error {
	for i := range maxAttempts {
		err := db.pool.Ping(ctx)
		if err == nil {
			return nil
		}

		debuglog.Info("db: Database not ready", "attempt", i+1, "max", maxAttempts, "error", err)
		if err := util.SleepContext(ctx, waitForReadyInterval); err != nil {
			return err
		}
	}

	debuglog.Error("db: database not ready, giving up", "attempts", maxAttempts)
	return fmt.Errorf("database not ready after %d attempts", maxAttempts)
}
