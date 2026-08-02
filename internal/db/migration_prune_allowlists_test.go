package db

import (
	"context"
	"io/fs"
	"testing"
)

const pruneAllowlistsMigration = "migrations/066_prune_provider_allowlists.sql"

// readPruneAllowlistsMigration returns the embedded migration's SQL. The
// migration is exercised directly rather than via runMigrations, which has
// already applied it to the shared test database: once its trigger is in place
// the dangling ids it exists to clean can no longer be created.
func readPruneAllowlistsMigration(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(embeddedMigrations, pruneAllowlistsMigration)
	if err != nil {
		t.Fatalf("read %s: %v", pruneAllowlistsMigration, err)
	}
	return string(b)
}

// The half of migration 066 that a fresh database never exercises: the one-time
// UPDATE that cleans ids left dangling by every provider deleted before the
// trigger existed. The trigger covers deletions from here on; only this arm
// repairs the rows already in an upgrading install.
//
// Both stored meanings are asserted separately, because they are opposites:
// NULL is "unrestricted" and '{}' is "restricted to nothing". A row whose ids
// are all dangling must land on '{}', since that is what it already did at
// runtime (a dangling id matches no provider); turning it into NULL would hand
// the key or account every provider instead.
func TestPruneAllowlistsMigrationCleansDanglingIDs(t *testing.T) {
	ctx := context.Background()
	sql := readPruneAllowlistsMigration(t)

	// Pre-066 shape, so a dangling id can be seeded at all.
	if _, err := testPool.Exec(ctx, `DROP TRIGGER IF EXISTS providers_prune_allowlists ON providers`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM virtual_keys WHERE key_hash IN ('prune-mixed-vk', 'prune-dangling-vk', 'prune-open-vk')`)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM users WHERE username IN ('prune-mixed-user', 'prune-dangling-user', 'prune-open-user')`)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM providers WHERE name = 'prune-live-provider'`)
		// Leave the shared database with the trigger the migration installs,
		// whatever this test did to it.
		if _, err := testPool.Exec(context.Background(), sql); err != nil {
			t.Errorf("restore migration: %v", err)
		}
	})

	var live string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO providers (name, base_url) VALUES ('prune-live-provider', 'https://live.example')
		 RETURNING id::text`).Scan(&live); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	const dangling = "99999999-9999-9999-9999-999999999999"

	if _, err := testPool.Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers) VALUES
			('prune mixed',    'prune-mixed-vk',    'sk-...mx', $1),
			('prune dangling', 'prune-dangling-vk', 'sk-...dg', $2),
			('prune open',     'prune-open-vk',     'sk-...op', NULL)`,
		[]string{live, dangling}, []string{dangling}); err != nil {
		t.Fatalf("seed virtual keys: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO users (username, password_hash, allowed_providers) VALUES
			('prune-mixed-user',    'x', $1),
			('prune-dangling-user', 'x', $2),
			('prune-open-user',     'x', NULL)`,
		[]string{live, dangling}, []string{dangling}); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	// Read the columns in SQL rather than scanning them into a []string: pgx
	// yields a nil slice for both NULL and '{}', which would hide the exact
	// distinction under test.
	for _, tc := range []struct {
		what       string
		query      string
		subject    string
		wantNull   bool
		wantCard   int
		wantMember string
	}{
		{"virtual key with one live id", `virtual_keys`, "prune-mixed-vk", false, 1, live},
		{"virtual key with only dangling ids", `virtual_keys`, "prune-dangling-vk", false, 0, ""},
		{"unrestricted virtual key", `virtual_keys`, "prune-open-vk", true, -1, ""},
		{"user with one live id", `users`, "prune-mixed-user", false, 1, live},
		{"user with only dangling ids", `users`, "prune-dangling-user", false, 0, ""},
		{"uncapped user", `users`, "prune-open-user", true, -1, ""},
	} {
		t.Run(tc.what, func(t *testing.T) {
			var query string
			switch tc.query {
			case "virtual_keys":
				query = `SELECT allowed_providers IS NULL, coalesce(cardinality(allowed_providers), -1),
				                coalesce(allowed_providers, '{}'::text[])
				           FROM virtual_keys WHERE key_hash = $1`
			default:
				query = `SELECT allowed_providers IS NULL, coalesce(cardinality(allowed_providers), -1),
				                coalesce(allowed_providers, '{}'::text[])
				           FROM users WHERE username = $1`
			}
			var isNull bool
			var card int
			var members []string
			if err := testPool.QueryRow(ctx, query, tc.subject).Scan(&isNull, &card, &members); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if isNull != tc.wantNull {
				t.Fatalf("allowed_providers IS NULL = %v, want %v", isNull, tc.wantNull)
			}
			if card != tc.wantCard {
				t.Fatalf("cardinality = %d, want %d", card, tc.wantCard)
			}
			if tc.wantMember != "" && (len(members) != 1 || members[0] != tc.wantMember) {
				t.Fatalf("members = %v, want [%s]", members, tc.wantMember)
			}
		})
	}
}
