package db

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetupTestDB creates an isolated test database for a specific package.
// It parses the base TEST_DATABASE_URL, appends the package name to create a
// unique database (e.g., "testdb_model"), drops any existing version, creates
// it fresh, and returns the full connection URL for the new database.
//
// This eliminates deadlocks and state pollution when multiple test packages
// run concurrently against the same PostgreSQL instance.
//
// The caller should defer a call to CleanupTestDB to drop the database after
// tests complete, though the next test run will DROP+CREATE anyway.
func SetupTestDB(pkgName string) (string, error) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		baseURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse TEST_DATABASE_URL: %w", err)
	}

	// Derive the per-package database name from the original DB name.
	origDBName := parsed.Path
	if len(origDBName) > 0 && origDBName[0] == '/' {
		origDBName = origDBName[1:]
	}
	newDBName := origDBName + "_" + pkgName

	// Connect to the "maintenance" database (the original one) to CREATE/DROP.
	ctx := context.Background()
	maintPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to maintenance DB: %w", err)
	}
	defer maintPool.Close()

	// Terminate any existing connections to the target database.
	_, _ = maintPool.Exec(ctx, fmt.Sprintf(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`,
		newDBName,
	))

	// Drop if exists, then create fresh.
	_, err = maintPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", newDBName))
	if err != nil {
		return "", fmt.Errorf("failed to drop test database %s: %w", newDBName, err)
	}

	_, err = maintPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", newDBName))
	if err != nil {
		return "", fmt.Errorf("failed to create test database %s: %w", newDBName, err)
	}

	// Build the new URL pointing to the per-package database.
	parsed.Path = "/" + newDBName
	return parsed.String(), nil
}

// CleanupTestDB drops the per-package test database. Call this in a defer
// from TestMain after tests finish.
func CleanupTestDB(pkgName string) {
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		baseURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return
	}

	origDBName := parsed.Path
	if len(origDBName) > 0 && origDBName[0] == '/' {
		origDBName = origDBName[1:]
	}
	newDBName := origDBName + "_" + pkgName

	ctx := context.Background()
	maintPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		return
	}
	defer maintPool.Close()

	_, _ = maintPool.Exec(ctx, fmt.Sprintf(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`,
		newDBName,
	))
	_, _ = maintPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", newDBName))
}
