package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool
var testDB *DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testURL := os.Getenv("TEST_DATABASE_URL")
		if testURL == "" {
			testURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
		}

	var err error
	testDB, err = New(ctx, testURL, 25, 5)
	if err != nil {
		fmt.Printf("failed to initialize test DB: %v\n", err)
		os.Exit(1)
	}
	testPool = testDB.Pool()
	defer testDB.Close()

	os.Exit(m.Run())
}

func TestNewInvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := New(ctx, "invalid://url", 25, 5)
	if err == nil {
		t.Error("expected error for invalid database URL")
	}
}

func TestNewCreatesPool(t *testing.T) {
	if testDB == nil {
		t.Fatal("testDB is nil, TestMain must have failed")
	}

	pool := testDB.Pool()
	if pool == nil {
		t.Fatal("expected non-nil pool from New")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool.Ping failed: %v", err)
	}
}

func TestPoolConfiguration(t *testing.T) {
	pool := testDB.Pool()
	config := pool.Config()

	if config.MaxConns != 25 {
		t.Errorf("MaxConns = %d, want 25", config.MaxConns)
	}
	if config.MinConns != 5 {
		t.Errorf("MinConns = %d, want 5", config.MinConns)
	}
	if config.MaxConnLifetime != 4*time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 4h", config.MaxConnLifetime)
	}
	if config.MaxConnIdleTime != 30*time.Minute {
		t.Errorf("MaxConnIdleTime = %v, want 30m", config.MaxConnIdleTime)
	}
	if config.HealthCheckPeriod != 1*time.Minute {
		t.Errorf("HealthCheckPeriod = %v, want 1m", config.HealthCheckPeriod)
	}
}

func TestClose(t *testing.T) {
	// Create a fresh DB to close without affecting the shared testDB.
	ctx := context.Background()
	testURL := os.Getenv("TEST_DATABASE_URL")
		if testURL == "" {
			testURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
		}

	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB for close test: %v", err)
	}

	// Close should not panic. After close, the pool is closed.
	d.Close()

	// A second close should be safe (no-op).
	d.Close()
}

func TestBegin(t *testing.T) {
	ctx := context.Background()

	tx, err := testDB.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	if tx == nil {
		t.Fatal("expected non-nil transaction")
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
}

func TestWaitForReady(t *testing.T) {
	ctx := context.Background()

	// The pool should already be ready.
	if err := testDB.WaitForReady(ctx, 3); err != nil {
		t.Fatalf("WaitForReady failed on connected pool: %v", err)
	}
}

func TestWaitForReadyTimeout(t *testing.T) {
	// Use a cancelled context to trigger immediate timeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := testDB.WaitForReady(ctx, 2)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestRunMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	testURL := os.Getenv("TEST_DATABASE_URL")
		if testURL == "" {
			testURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
		}

	// Create a new DB; this calls runMigrations internally via New.
	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB for migration test: %v", err)
	}
	defer d.Close()

	// Run migrations a second time directly; they should all be skipped.
	if err := d.runMigrations(ctx); err != nil {
		t.Fatalf("second runMigrations call failed: %v", err)
	}
}

func TestMigrationSchemaTableExists(t *testing.T) {
	ctx := context.Background()

	var exists bool
	err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_tables
			WHERE schemaname = 'public'
			AND tablename = 'schema_migrations'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to check schema_migrations table: %v", err)
	}
	if !exists {
		t.Error("schema_migrations table should exist after migrations")
	}
}

func TestPool(t *testing.T) {
	pool := testDB.Pool()
	if pool != testPool {
		t.Error("Pool() should return the same pool instance")
	}
}
