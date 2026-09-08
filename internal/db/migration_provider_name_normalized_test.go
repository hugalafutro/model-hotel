package db

import (
	"context"
	"io/fs"
	"strings"
	"testing"
)

const providerNameNormalizedMigration = "migrations/082_provider_name_normalized_unique.sql"

// Migration 082 makes provider names unique in the form routing actually uses,
// where a space and a hyphen are the same character. Its three behaviours are
// asserted in one test because they share a seeded state: the migration refuses
// an install that already holds a colliding pair (naming both rows so the
// operator knows which to rename), applies once the collision is gone, and
// leaves behind an index that stops the next twin at the database.
//
// The migration is replayed directly, because runMigrations has already applied
// it to the shared test database; the index it installs has to come off first or
// the colliding pair could not be seeded at all. That drop is on the database
// every test in this package shares, so this test must not run in parallel with
// one that writes providers: it holds the index off until its cleanup restores
// it.
func TestProviderNameNormalizedMigrationRefusesCollisions(t *testing.T) {
	ctx := context.Background()
	b, err := fs.ReadFile(embeddedMigrations, providerNameNormalizedMigration)
	if err != nil {
		t.Fatalf("read %s: %v", providerNameNormalizedMigration, err)
	}
	sql := string(b)

	if _, err := testPool.Exec(ctx, `DROP INDEX IF EXISTS providers_name_normalized_unique`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM providers WHERE name LIKE 'twin%'`)
		// Leave the shared database with the index the migration installs, and
		// prove the replay is harmless while doing so.
		if _, err := testPool.Exec(context.Background(), sql); err != nil {
			t.Errorf("restore migration: %v", err)
		}
	})

	for _, name := range []string{"twin a", "twin-a"} {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO providers (name, base_url, provider_type) VALUES ($1, 'https://twin.example.test/v1', 'custom')`,
			name); err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
	}

	_, err = testPool.Exec(ctx, sql)
	if err == nil {
		t.Fatal("the migration applied over a colliding pair; it must refuse")
	}
	// The operator has to pick which name survives, so the message names both,
	// and it has to carry the remedy itself: the Go driver renders a server
	// error as severity, message and SQLSTATE only, so the HINT never reaches
	// the startup log.
	for _, want := range []string{"twin a", "twin-a", "rename one provider in each group"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not carry %q", err.Error(), want)
		}
	}

	if _, err := testPool.Exec(ctx, `DELETE FROM providers WHERE name = 'twin a'`); err != nil {
		t.Fatalf("resolve the collision: %v", err)
	}
	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("the migration must apply once the collision is gone: %v", err)
	}

	_, err = testPool.Exec(ctx,
		`INSERT INTO providers (name, base_url, provider_type) VALUES ('twin a', 'https://twin.example.test/v1', 'custom')`)
	if !IsUniqueViolationOn(err, "providers_name_normalized_unique") {
		t.Fatalf("inserting a normalized twin returned %v, want a 23505", err)
	}
}
