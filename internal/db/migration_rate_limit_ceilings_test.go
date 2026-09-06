package db

import (
	"context"
	"io/fs"
	"testing"
)

// TestRateLimitCeilingsMigrationClampsLegacyRows covers the reason migration
// 081 exists: a row whose limit was set above the interactive ceiling back when
// only floors were validated would now answer 400 to every edit, because the
// edit modals resubmit every field. The migration clamps such rows to the
// ceiling and leaves in-range values alone. The migration runs directly rather
// than via runMigrations, which has already applied it to the shared database;
// it installs no constraint, so the seed rows can be written afterwards.
func TestRateLimitCeilingsMigrationClampsLegacyRows(t *testing.T) {
	ctx := context.Background()
	b, err := fs.ReadFile(embeddedMigrations, "migrations/081_rate_limit_ceilings.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(b)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM virtual_keys WHERE key_hash IN ('above-ceiling-vk', 'in-range-vk')`)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM users WHERE username IN ('above-ceiling-user', 'in-range-user')`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm) VALUES
		('above ceiling', 'above-ceiling-vk', 'sk-...vk', 10000.5, 10001, 1000000000),
		('in range',      'in-range-vk',      'sk-...vk', 10000,   10000, 100000000)`); err != nil {
		t.Fatalf("seed virtual keys: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO users (username, password_hash, rate_limit_rps, rate_limit_burst, rate_limit_tpm) VALUES
		('above-ceiling-user', 'x', 20000, 999999, 2147483647),
		('in-range-user',      'x', 0.5,   1,      1)`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	for _, tc := range []struct {
		table, where string
		rps          float64
		burst, tpm   int
	}{
		{"virtual_keys", `key_hash = 'above-ceiling-vk'`, 10000, 10000, 100000000},
		{"virtual_keys", `key_hash = 'in-range-vk'`, 10000, 10000, 100000000},
		{"users", `username = 'above-ceiling-user'`, 10000, 10000, 100000000},
		{"users", `username = 'in-range-user'`, 0.5, 1, 1},
	} {
		var rps float64
		var burst, tpm int
		if err := testPool.QueryRow(ctx,
			`SELECT rate_limit_rps, rate_limit_burst, rate_limit_tpm FROM `+tc.table+` WHERE `+tc.where).
			Scan(&rps, &burst, &tpm); err != nil {
			t.Fatalf("read %s %s: %v", tc.table, tc.where, err)
		}
		if rps != tc.rps || burst != tc.burst || tpm != tc.tpm {
			t.Fatalf("%s %s: got rps=%v burst=%d tpm=%d, want rps=%v burst=%d tpm=%d",
				tc.table, tc.where, rps, burst, tpm, tc.rps, tc.burst, tc.tpm)
		}
	}
}
