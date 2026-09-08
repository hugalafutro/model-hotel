package db

import (
	"context"
	"errors"
	"io/fs"
	"testing"
)

// withFakeMigrations points the embedded migration set at a single file holding
// sql, restoring the real set afterwards, and returns a URL for a fresh database
// so the fake never touches the one the rest of the package shares.
func withFakeMigrations(t *testing.T, name, sql string) string {
	t.Helper()
	dbName := "db_" + name
	testURL, err := SetupTestDB(dbName)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	t.Cleanup(func() { CleanupTestDB(dbName) })

	orig := migrationsFS
	t.Cleanup(func() { migrationsFS = orig })
	migrationsFS = mockMigrationsFS{
		readDirFn: func(string) ([]fs.DirEntry, error) {
			return []fs.DirEntry{mockDirEntry{name: "001_fake.sql"}}, nil
		},
		readFileFn: func(string) ([]byte, error) { return []byte(sql), nil },
	}
	return testURL
}

// A migration that raises its own exception has decided not to upgrade this
// install, which is what migration 082 does to an install whose provider names
// collide once normalized. Startup calls that a refusal and prints the
// migration's own remedy, so the sentinel is reserved for it.
func TestNew_MigrationRefusalCarriesTheRefusalSentinel(t *testing.T) {
	testURL := withFakeMigrations(t, "migration_refusal", `
		DO $$ BEGIN
			RAISE EXCEPTION 'rename one provider in each group, then restart';
		END $$;`)

	_, err := New(context.Background(), testURL, 25, 5)
	if err == nil {
		t.Fatal("a migration that raised an exception applied anyway")
	}
	if !errors.Is(err, ErrMigrations) {
		t.Errorf("error %v is not an ErrMigrations: a deliberate refusal has to read as one", err)
	}
	if errors.Is(err, ErrMigrationFailed) {
		t.Errorf("error %v is labelled a failure as well as a refusal", err)
	}
}

// Everything else is a failure, not a decision: the operator has nothing to
// rename, and telling them a migration "refused" sends them looking for one.
func TestNew_MigrationErrorCarriesTheFailureSentinel(t *testing.T) {
	testURL := withFakeMigrations(t, "migration_failure", `SELECT * FROM a_table_that_is_not_there`)

	_, err := New(context.Background(), testURL, 25, 5)
	if err == nil {
		t.Fatal("a migration referencing a missing table applied anyway")
	}
	if !errors.Is(err, ErrMigrationFailed) {
		t.Errorf("error %v is not an ErrMigrationFailed", err)
	}
	if errors.Is(err, ErrMigrations) {
		t.Errorf("error %v is labelled a refusal: an undefined table is not a decision anyone made", err)
	}
}
