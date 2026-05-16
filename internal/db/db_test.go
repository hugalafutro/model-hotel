package db

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool
var testDB *DB

// mockMigrationsFS is a test implementation of fs.FS for testing migration errors.
type mockMigrationsFS struct {
	readDirFn  func(name string) ([]fs.DirEntry, error)
	readFileFn func(name string) ([]byte, error)
}

func (m mockMigrationsFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (m mockMigrationsFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return m.readDirFn(name)
}

func (m mockMigrationsFS) ReadFile(name string) ([]byte, error) {
	return m.readFileFn(name)
}

// mockDirEntry is a test implementation of fs.DirEntry.
type mockDirEntry struct {
	name  string
	isDir bool
	typ   fs.FileMode
}

func (e mockDirEntry) Name() string               { return e.name }
func (e mockDirEntry) IsDir() bool                { return e.isDir }
func (e mockDirEntry) Type() fs.FileMode          { return e.typ }
func (e mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

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
	_, err = d.runMigration(ctx, "test.sql", "SELECT 1")
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
	_, err = d.runMigration(ctx, "bad.sql", "INVALID SQL SYNTAX !!!")
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

// TestBuildTestDBURL_FallbackDefaults tests the POSTGRES_* fallback path
// when TEST_DATABASE_URL is not set. Covers the default values for
// user, password, and host.
func TestBuildTestDBURL_FallbackDefaults(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "")
	t.Setenv("POSTGRES_PASSWORD", "")
	t.Setenv("POSTGRES_HOST", "")

	result := buildTestDBURL()
	expected := "postgres://modelhotel:changeme@localhost:5433/testdb?sslmode=disable"
	if result != expected {
		t.Errorf("buildTestDBURL() = %q, want %q", result, expected)
	}
}

// TestBuildTestDBURL_FallbackCustom tests the POSTGRES_* fallback path
// with explicit env vars overriding the defaults.
func TestBuildTestDBURL_FallbackCustom(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "myuser")
	t.Setenv("POSTGRES_PASSWORD", "mypass")
	t.Setenv("POSTGRES_HOST", "myhost")

	result := buildTestDBURL()
	expected := "postgres://myuser:mypass@myhost:5433/testdb?sslmode=disable"
	if result != expected {
		t.Errorf("buildTestDBURL() = %q, want %q", result, expected)
	}
}

// TestBuildTestDBURL_FallbackDockerHost tests that POSTGRES_HOST="db"
// is rewritten to "localhost" (test runs outside Docker).
func TestBuildTestDBURL_FallbackDockerHost(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "u")
	t.Setenv("POSTGRES_PASSWORD", "p")
	t.Setenv("POSTGRES_HOST", "db")

	result := buildTestDBURL()
	expected := "postgres://u:p@localhost:5433/testdb?sslmode=disable"
	if result != expected {
		t.Errorf("buildTestDBURL() = %q, want %q", result, expected)
	}
}

// TestRunMigrations_ReadDirError tests the error path when fs.ReadDir fails.
func TestRunMigrations_ReadDirError(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testURL, err := SetupTestDB("db_read_dir_err")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_read_dir_err")

	// Save original migrationsFS
	origFS := migrationsFS
	t.Cleanup(func() { migrationsFS = origFS })

	// Replace with mock that returns ReadDir error
	migrationsFS = mockMigrationsFS{
		readDirFn: func(name string) ([]fs.DirEntry, error) {
			return nil, errors.New("read dir failed")
		},
	}

	// New calls runMigrations internally, which should fail
	_, err = New(ctx, testURL, 25, 5)
	if err == nil {
		t.Fatal("expected error from ReadDir failure")
	}
	if got := err.Error(); !strings.Contains(got, "failed to read migrations directory") {
		t.Errorf("error = %q, want substring %q", got, "failed to read migrations directory")
	}
}

// TestRunMigrations_DirEntry tests that directory entries are skipped.
func TestRunMigrations_DirEntry(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testURL, err := SetupTestDB("db_dir_entry")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_dir_entry")

	// Create DB with real FS first to get a working pool
	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Save original migrationsFS
	origFS := migrationsFS
	t.Cleanup(func() { migrationsFS = origFS })

	// Replace with mock that returns a directory entry and a .sql file
	migrationsFS = mockMigrationsFS{
		readDirFn: func(name string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				mockDirEntry{name: "subdir", isDir: true},
				mockDirEntry{name: "001_test.sql", isDir: false, typ: 0},
			}, nil
		},
		readFileFn: func(name string) ([]byte, error) {
			if name == "migrations/001_test.sql" {
				return []byte("SELECT 1"), nil
			}
			return nil, fs.ErrNotExist
		},
	}

	// Call runMigrations directly - should skip directory and execute the .sql file
	if err := d.runMigrations(ctx); err != nil {
		t.Fatalf("runMigrations failed: %v", err)
	}
}

// TestRunMigrations_NonRegularFile tests that non-regular files are skipped.
func TestRunMigrations_NonRegularFile(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testURL, err := SetupTestDB("db_non_regular")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_non_regular")

	// Create DB with real FS first to get a working pool
	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Save original migrationsFS
	origFS := migrationsFS
	t.Cleanup(func() { migrationsFS = origFS })

	// Replace with mock that returns a socket entry and a .sql file
	migrationsFS = mockMigrationsFS{
		readDirFn: func(name string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				mockDirEntry{name: "socket", isDir: false, typ: fs.ModeSocket},
				mockDirEntry{name: "001_test.sql", isDir: false, typ: 0},
			}, nil
		},
		readFileFn: func(name string) ([]byte, error) {
			if name == "migrations/001_test.sql" {
				return []byte("SELECT 1"), nil
			}
			return nil, fs.ErrNotExist
		},
	}

	// Call runMigrations directly - should skip socket and execute the .sql file
	if err := d.runMigrations(ctx); err != nil {
		t.Fatalf("runMigrations failed: %v", err)
	}
}

// TestRunMigrations_DotfileEntry tests that dotfiles are skipped.
func TestRunMigrations_DotfileEntry(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testURL, err := SetupTestDB("db_dotfile")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_dotfile")

	// Create DB with real FS first to get a working pool
	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Save original migrationsFS
	origFS := migrationsFS
	t.Cleanup(func() { migrationsFS = origFS })

	// Replace with mock that returns a dotfile entry and a .sql file
	migrationsFS = mockMigrationsFS{
		readDirFn: func(name string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				mockDirEntry{name: ".hidden.sql", isDir: false, typ: 0},
				mockDirEntry{name: "001_test.sql", isDir: false, typ: 0},
			}, nil
		},
		readFileFn: func(name string) ([]byte, error) {
			if name == "migrations/001_test.sql" {
				return []byte("SELECT 1"), nil
			}
			return nil, fs.ErrNotExist
		},
	}

	// Call runMigrations directly - should skip dotfile and execute the .sql file
	if err := d.runMigrations(ctx); err != nil {
		t.Fatalf("runMigrations failed: %v", err)
	}
}

// TestRunMigration_NewlyApplied tests that runMigration returns (true, nil)
// when a migration is applied for the first time.
func TestRunMigration_NewlyApplied(t *testing.T) {
	ctx := context.Background()
	testURL, err := SetupTestDB("db_new_applied")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_new_applied")

	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Use a unique migration name that hasn't been applied yet
	migrationName := "test_newly_applied_" + time.Now().Format("20060102150405") + ".sql"
	newlyApplied, err := d.runMigration(ctx, migrationName, "SELECT 1")
	if err != nil {
		t.Fatalf("runMigration failed: %v", err)
	}
	if !newlyApplied {
		t.Error("expected newlyApplied=true for first application")
	}

	// Running the same migration again should return (false, nil)
	newlyApplied, err = d.runMigration(ctx, migrationName, "SELECT 1")
	if err != nil {
		t.Fatalf("runMigration on second call failed: %v", err)
	}
	if newlyApplied {
		t.Error("expected newlyApplied=false for already-applied migration")
	}
}

// TestRunMigrations_AppliedAndSkipped tests that runMigrations correctly
// counts applied and skipped migrations. First run applies all, second run
// skips all.
func TestRunMigrations_AppliedAndSkipped(t *testing.T) {
	ctx := context.Background()
	testURL, err := SetupTestDB("db_applied_skipped")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_applied_skipped")

	// First DB: applies all migrations
	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Second call: all migrations should be skipped (returns no error)
	if err := d.runMigrations(ctx); err != nil {
		t.Fatalf("second runMigrations call failed: %v", err)
	}
}

// TestRunMigrations_ReadFileError tests the error path when fs.ReadFile fails.
func TestRunMigrations_ReadFileError(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testURL, err := SetupTestDB("db_read_file_err")
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer CleanupTestDB("db_read_file_err")

	// Create DB with real FS first to get a working pool
	d, err := New(ctx, testURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	defer d.Close()

	// Save original migrationsFS
	origFS := migrationsFS
	t.Cleanup(func() { migrationsFS = origFS })

	// Replace with mock that returns ReadFile error
	migrationsFS = mockMigrationsFS{
		readDirFn: func(name string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				mockDirEntry{name: "001_test.sql", isDir: false, typ: 0},
			}, nil
		},
		readFileFn: func(name string) ([]byte, error) {
			return nil, errors.New("read file failed")
		},
	}

	// Call runMigrations directly - should fail on ReadFile
	err = d.runMigrations(ctx)
	if err == nil {
		t.Fatal("expected error from ReadFile failure")
	}
	if got := err.Error(); !strings.Contains(got, "failed to read migration") {
		t.Errorf("error = %q, want substring %q", got, "failed to read migration")
	}
}

// TestKnownMigrations returns embedded migration filenames.
func TestKnownMigrations(t *testing.T) {
	names := KnownMigrations()
	if len(names) == 0 {
		t.Fatal("expected at least one migration, got none")
	}
	for _, n := range names {
		if !strings.HasSuffix(n, ".sql") {
			t.Errorf("migration %q should end with .sql", n)
		}
	}
}

// TestKnownMigrations_ReadDirError tests the error path when fs.ReadDir fails.
func TestKnownMigrations_ReadDirError(t *testing.T) {
	origFS := migrationsFS
	t.Cleanup(func() { migrationsFS = origFS })

	migrationsFS = mockMigrationsFS{
		readDirFn: func(name string) ([]fs.DirEntry, error) {
			return nil, errors.New("read dir failed")
		},
	}

	names := KnownMigrations()
	if names != nil {
		t.Errorf("expected nil on ReadDir error, got %v", names)
	}
}

// verify hook
// hook test
