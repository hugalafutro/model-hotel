package db

import (
	"context"
	"io/fs"
	"strings"
	"testing"
)

const rateLimitBoundsMigration = "migrations/064_rate_limit_bounds.sql"

// readRateLimitBoundsMigration returns the embedded migration's SQL. The
// migration is exercised directly rather than via runMigrations, which has
// already applied it to the shared test database: the legacy rows it exists to
// clean cannot be seeded once its CHECK constraints are in place.
func readRateLimitBoundsMigration(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(embeddedMigrations, rateLimitBoundsMigration)
	if err != nil {
		t.Fatalf("read %s: %v", rateLimitBoundsMigration, err)
	}
	return string(b)
}

// dropRateLimitBoundsConstraints puts the schema back into its pre-064 shape so
// a pre-#226 row can be seeded, and restores it afterwards by replaying the
// migration.
func dropRateLimitBoundsConstraints(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`ALTER TABLE virtual_keys DROP CONSTRAINT IF EXISTS virtual_keys_rate_limit_bounds`,
		`ALTER TABLE users DROP CONSTRAINT IF EXISTS users_rate_limit_bounds`,
	} {
		if _, err := testPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("drop constraint: %v", err)
		}
	}
}

// TestRateLimitBoundsMigrationNormalizesLegacyRows covers the reason the
// migration exists: an install from before PR #226 can hold a virtual key with
// rate_limit_burst = 0, and config sync now refuses the whole envelope over it,
// so one stale row on the primary stops the entire fleet converging.
func TestRateLimitBoundsMigrationNormalizesLegacyRows(t *testing.T) {
	ctx := context.Background()
	sql := readRateLimitBoundsMigration(t)
	dropRateLimitBoundsConstraints(t)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM virtual_keys WHERE key_hash IN ('legacy-bounds-vk', 'fresh-bounds-vk')`)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM users WHERE username IN ('legacy-bounds-user', 'fresh-bounds-user')`)
		// Leave the shared database with the constraints the migration installs,
		// whatever this test did to them.
		if _, err := testPool.Exec(context.Background(), sql); err != nil {
			t.Errorf("restore migration: %v", err)
		}
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm)
		VALUES ('legacy bounds', 'legacy-bounds-vk', 'sk-...vk', -1, 0, -5)`); err != nil {
		t.Fatalf("seed legacy virtual key: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO users (username, password_hash, rate_limit_rps, rate_limit_burst, rate_limit_tpm)
		VALUES ('legacy-bounds-user', 'x', -2, -1, 0)`); err != nil {
		t.Fatalf("seed legacy user: %v", err)
	}

	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	for _, tc := range []struct{ table, where string }{
		{"virtual_keys", `key_hash = 'legacy-bounds-vk'`},
		{"users", `username = 'legacy-bounds-user'`},
	} {
		var rps *float64
		var burst, tpm *int
		if err := testPool.QueryRow(ctx,
			`SELECT rate_limit_rps, rate_limit_burst, rate_limit_tpm FROM `+tc.table+` WHERE `+tc.where).
			Scan(&rps, &burst, &tpm); err != nil {
			t.Fatalf("read %s: %v", tc.table, err)
		}
		if rps != nil || burst != nil || tpm != nil {
			t.Fatalf("%s out-of-bounds limits not normalized to NULL: rps=%v burst=%v tpm=%v",
				tc.table, rps, burst, tpm)
		}
	}
}

// A migration that only cleaned up would leave the next writer free to
// reintroduce the row, so the bounds have to hold in the schema too. Every
// in-tree writer already validates; this is the backstop for the one that does
// not, and for a restored pre-#226 dump.
func TestRateLimitBoundsMigrationRejectsNewOutOfBoundsRows(t *testing.T) {
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM virtual_keys WHERE key_hash = 'reject-bounds-vk'`)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM users WHERE username = 'reject-bounds-user'`)
	})

	tests := []struct {
		name string
		stmt string
		args []any
	}{
		{
			name: "virtual key zero burst",
			stmt: `INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_burst)
			       VALUES ('reject', 'reject-bounds-vk', 'sk-...vk', 0)`,
		},
		{
			name: "virtual key non-positive tpm",
			stmt: `INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_tpm)
			       VALUES ('reject', 'reject-bounds-vk', 'sk-...vk', -1)`,
		},
		{
			name: "virtual key negative rps",
			stmt: `INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps)
			       VALUES ('reject', 'reject-bounds-vk', 'sk-...vk', -0.5)`,
		},
		{
			name: "user negative burst",
			stmt: `INSERT INTO users (username, password_hash, rate_limit_burst)
			       VALUES ('reject-bounds-user', 'x', -1)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testPool.Exec(ctx, tt.stmt, tt.args...)
			if err == nil {
				t.Fatalf("insert succeeded, want a check-constraint violation")
			}
			if !strings.Contains(err.Error(), "rate_limit_bounds") {
				t.Fatalf("error %v is not the rate-limit bounds constraint", err)
			}
		})
	}

	// The bounds must not reject what the API legitimately writes: NULL means
	// "fall back to the global setting", and the minimums themselves are legal.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm)
		VALUES ('accept', 'reject-bounds-vk', 'sk-...vk', 0, 1, 1)`); err != nil {
		t.Fatalf("legal virtual key rejected: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO users (username, password_hash) VALUES ('reject-bounds-user', 'x')`); err != nil {
		t.Fatalf("user with NULL limits rejected: %v", err)
	}
}
