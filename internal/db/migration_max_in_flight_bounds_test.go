package db

import (
	"context"
	"io/fs"
	"testing"
)

const maxInFlightBoundsMigration = "migrations/079_provider_max_in_flight_bounds.sql"

// TestMaxInFlightBoundsMigrationNormalizesLegacyRows covers the reason the
// migration exists: an install that took an envelope through the unvalidated
// config-sync import can hold a provider with max_in_flight = -5 (which the
// runtime read as "no ceiling"), and the import now refuses the whole envelope
// over it, so one stale row on the primary would stop the fleet converging.
// The migration must null exactly the out-of-range rows, leave the rest, and
// be safe to replay against a database that already carries the constraint.
func TestMaxInFlightBoundsMigrationNormalizesLegacyRows(t *testing.T) {
	ctx := context.Background()
	b, err := fs.ReadFile(embeddedMigrations, maxInFlightBoundsMigration)
	if err != nil {
		t.Fatalf("read %s: %v", maxInFlightBoundsMigration, err)
	}
	sql := string(b)
	// Pre-079 shape, so the legacy rows can be seeded at all.
	if _, err := testPool.Exec(ctx, `ALTER TABLE providers DROP CONSTRAINT IF EXISTS providers_max_in_flight_bounds`); err != nil {
		t.Fatalf("drop constraint: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM providers WHERE name LIKE 'mif-bounds-%'`)
		// Leave the shared database with the constraint the migration
		// installs, and prove the replay is harmless while doing so.
		if _, err := testPool.Exec(context.Background(), sql); err != nil {
			t.Errorf("restore migration: %v", err)
		}
	})

	rows := map[string]*int{
		"mif-bounds-negative": new(-5),
		"mif-bounds-zero":     new(0),
		"mif-bounds-absurd":   new(999999999),
		"mif-bounds-floor":    new(1),
		"mif-bounds-ceiling":  new(10000),
		"mif-bounds-null":     nil,
	}
	for name, v := range rows {
		if _, err := testPool.Exec(ctx, `INSERT INTO providers (name, base_url, provider_type, max_in_flight) VALUES ($1, 'https://mif.example.test/v1', 'custom', $2)`, name, v); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("run migration: %v", err)
	}
	want := map[string]*int{
		"mif-bounds-negative": nil, "mif-bounds-zero": nil, "mif-bounds-absurd": nil,
		"mif-bounds-floor": new(1), "mif-bounds-ceiling": new(10000), "mif-bounds-null": nil,
	}
	for name, w := range want {
		var got *int
		if err := testPool.QueryRow(ctx, `SELECT max_in_flight FROM providers WHERE name = $1`, name).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if (got == nil) != (w == nil) || (got != nil && *got != *w) {
			t.Errorf("%s: max_in_flight = %v, want %v", name, got, w)
		}
	}
	// The constraint is in place and replaying the migration is a no-op.
	if _, err := testPool.Exec(ctx, `UPDATE providers SET max_in_flight = -1 WHERE name = 'mif-bounds-floor'`); err == nil {
		t.Fatal("the constraint did not come back with the migration")
	}
	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("replaying the migration failed: %v", err)
	}
}
