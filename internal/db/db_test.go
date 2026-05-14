package db

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool
var testDB *DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testURL, err := SetupTestDB("db")
	if err != nil {
		log.Printf("failed to setup test DB: %v", err)
		os.Exit(1)
	}
	defer CleanupTestDB("db")

	testDB, err = New(ctx, testURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	testPool = testDB.Pool()
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
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
	testURL, err := SetupTestDB("db_close")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_close")

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
	testURL, err := SetupTestDB("db_migrations")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_migrations")

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

// TestNew_PoolCreationError tests the error path when pgxpool.NewWithConfig fails.
// Uses a valid URL format but unreachable port to trigger pool creation failure.
func TestNew_PoolCreationError(t *testing.T) {
	ctx := context.Background()
	// Port 1 is typically unreachable, causing pool creation to fail
	// while still passing ParseConfig validation
	_, err := New(ctx, "postgres://user:pass@localhost:1/testdb?sslmode=disable", 25, 5)
	if err == nil {
		t.Error("expected error for unreachable database port")
	}
}

// TestRunMigration_BeginError tests the error path when tx.Begin fails in runMigration.
// This is triggered by calling runMigration on a closed pool.
func TestRunMigration_BeginError(t *testing.T) {
	ctx := context.Background()
	testURL, err := SetupTestDB("db_begin")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_begin")

	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}

	// Close the pool to trigger Begin error
	d.Close()

	// runMigration should fail when trying to begin a transaction on closed pool
	err = d.runMigration(ctx, "test.sql", "SELECT 1")
	if err == nil {
		t.Error("expected error when running migration on closed pool")
	}
}

// TestRunMigration_ExecError tests the error path when migration SQL execution fails.
// Uses invalid SQL to trigger the exec error path.
func TestRunMigration_ExecError(t *testing.T) {
	ctx := context.Background()
	testURL, err := SetupTestDB("db_exec")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_exec")

	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Use invalid SQL to trigger exec error
	err = d.runMigration(ctx, "bad.sql", "INVALID SQL SYNTAX !!!")
	if err == nil {
		t.Error("expected error for invalid SQL in migration")
	}
}

// TestWaitForReady_MaxAttemptsExhausted tests the error path when max attempts are exhausted.
// Uses a closed pool to ensure Ping always fails.
func TestWaitForReady_MaxAttemptsExhausted(t *testing.T) {
	ctx := context.Background()
	testURL, err := SetupTestDB("db_ready")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_ready")

	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}

	// Close the pool so Ping always fails
	d.Close()

	// With maxAttempts=1, should fail immediately with "not ready after 1 attempts"
	err = d.WaitForReady(ctx, 1)
	if err == nil {
		t.Error("expected error when max attempts exhausted")
	}
}

// TestWaitForReady_ContextCancelledDuringWait tests the ctx.Done() branch.
// Cancels the context during the wait sleep to trigger context cancellation.
func TestWaitForReady_ContextCancelledDuringWait(t *testing.T) {
	testURL, err := SetupTestDB("db_cancel")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_cancel")

	ctx, cancel := context.WithCancel(context.Background())

	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}

	// Close the pool so Ping always fails
	d.Close()

	// Cancel context after a short delay to trigger ctx.Done() during sleep
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = d.WaitForReady(ctx, 5)
	if err == nil {
		t.Error("expected error from context cancellation")
	}
}

// TestBuildTestDBURL_EnvOverride tests that TEST_DATABASE_URL env var takes precedence.
func TestBuildTestDBURL_EnvOverride(t *testing.T) {
	const expectedURL = "postgres://test:test@localhost:5432/customdb?sslmode=disable"
	t.Setenv("TEST_DATABASE_URL", expectedURL)

	result := buildTestDBURL()
	if result != expectedURL {
		t.Errorf("buildTestDBURL() = %q, want %q", result, expectedURL)
	}
}

// TestSetupTestDB_InvalidURL tests the url.Parse error path.
func TestSetupTestDB_InvalidURL(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "://invalid-url")

	_, err := SetupTestDB("invalid")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// TestSetupTestDB_ConnectionError tests the pgxpool.New error path.
// Uses an unreachable host to trigger connection failure.
func TestSetupTestDB_ConnectionError(t *testing.T) {
	// Use a valid URL format but unreachable host/port
	t.Setenv("TEST_DATABASE_URL", "postgres://user:pass@localhost:1/testdb?sslmode=disable")

	_, err := SetupTestDB("unreachable")
	if err == nil {
		t.Error("expected error for unreachable database")
	}
}

// TestCleanupTestDB_InvalidURL tests the url.Parse error path in CleanupTestDB.
func TestCleanupTestDB_InvalidURL(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "://invalid-url")

	// CleanupTestDB should not panic, just return silently on error
	CleanupTestDB("invalid")
}

// TestCleanupTestDB_ConnectionError tests the pgxpool.New error path in CleanupTestDB.
// Uses an unreachable host to trigger connection failure.
func TestCleanupTestDB_ConnectionError(t *testing.T) {
	// Use a valid URL format but unreachable host/port
	t.Setenv("TEST_DATABASE_URL", "postgres://user:pass@localhost:1/testdb?sslmode=disable")

	// CleanupTestDB should not panic, just return silently on error
	CleanupTestDB("unreachable")
}
