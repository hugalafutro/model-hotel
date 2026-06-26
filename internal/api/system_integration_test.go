package api

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// getTestPool creates a pgxpool.Pool for testing, skipping if unavailable
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// TestCollect_InvalidSince tests that collect returns an error for invalid since parameter
func TestCollect_InvalidSince(t *testing.T) {
	pool := getTestPool(t)
	sysHandler := NewSystemHandler(pool, nil)
	ctx := context.Background()

	// Invalid since format should return an error
	stats, err := sysHandler.collect(ctx, "not-a-date")
	if err == nil {
		t.Error("Expected error for invalid since format, got nil")
	}
	if stats != nil {
		t.Error("Expected nil stats on error")
	}

	// Partial date format should also return an error
	stats, err = sysHandler.collect(ctx, "2024-01-01")
	if err == nil {
		t.Error("Expected error for partial date format, got nil")
	}
	if stats != nil {
		t.Error("Expected nil stats on error")
	}
}

// TestCollect_EmptySinceWithTimeout tests that collect with empty since returns
// stats within a reasonable time. Uses a context timeout to avoid Docker hangs.
func TestCollect_EmptySinceWithTimeout(t *testing.T) {
	pool := getTestPool(t)
	sysHandler := NewSystemHandler(pool, nil)
	// Mock Docker stats to avoid real Docker API calls that spawn persistent
	// HTTP transport goroutines and hang the test process.
	sysHandler.dockerStatsCollector = func(filter util.ContainerFilter) util.AggregatedDockerStats {
		return util.AggregatedDockerStats{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := sysHandler.collect(ctx, "")
	if err != nil {
		t.Fatalf("collect with empty since failed: %v", err)
	}
	if stats == nil { //nolint:staticcheck // SA5011
		t.Fatal("Expected non-nil stats")
	}

	// Verify AppStats structure
	if stats.App.Goroutines <= 0 { //nolint:staticcheck // SA5011
		t.Error("Expected goroutines > 0")
	}
	if stats.App.UptimeSeconds < 0 { //nolint:staticcheck // SA5011
		t.Error("Expected uptime_seconds >= 0")
	}
	if stats.App.Procs <= 0 { //nolint:staticcheck // SA5011
		t.Error("Expected procs > 0")
	}
}
