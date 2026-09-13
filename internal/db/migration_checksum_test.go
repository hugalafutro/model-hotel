package db

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// TestRunMigration_Checksum covers the ledger's checksum: applying a migration
// records the hash of its text; a re-run with the same text is silent; a
// re-run with different text is skipped and warned about, on that start and
// every later one, with the stored hash left as what was applied; a row
// recorded before checksums existed (NULL) adopts the current text without a
// warning.
func TestRunMigration_Checksum(t *testing.T) {
	ctx := context.Background()
	const name = "zz_checksum_probe.sql"
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM schema_migrations WHERE name = $1`, name); err != nil {
			t.Errorf("cleanup ledger row: %v", err)
		}
	})

	var logged bytes.Buffer
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { debuglog.SetHandler(debuglog.StdoutHandler()) })

	stored := func() *string {
		t.Helper()
		var v *string
		if err := testPool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name = $1`, name).Scan(&v); err != nil {
			t.Fatalf("read ledger row: %v", err)
		}
		return v
	}
	run := func(sql string, wantApplied bool) {
		t.Helper()
		applied, err := testDB.runMigration(ctx, name, sql)
		if err != nil {
			t.Fatalf("runMigration(%q): %v", sql, err)
		}
		if applied != wantApplied {
			t.Fatalf("runMigration(%q) applied = %v, want %v", sql, applied, wantApplied)
		}
	}
	warned := func() bool {
		defer logged.Reset()
		return strings.Contains(logged.String(), "migration file changed after it was applied")
	}

	run("SELECT 1", true)
	if got := stored(); got == nil || *got != migrationChecksum("SELECT 1") {
		t.Fatalf("checksum after apply = %v, want the hash of the applied text", got)
	}
	if warned() {
		t.Fatal("a first apply must not warn")
	}

	run("SELECT 1", false)
	if warned() {
		t.Fatal("a re-run with the same text must not warn")
	}

	// The edit is named, never executed: this text divides by zero, so a run
	// would fail rather than return applied=false.
	const edited = "SELECT 1/0"
	run(edited, false)
	if !warned() {
		t.Fatal("a re-run with changed text must warn")
	}
	if got := stored(); got == nil || *got != migrationChecksum("SELECT 1") {
		t.Fatalf("checksum after drift = %v, want the hash of the applied text, untouched", got)
	}
	run(edited, false)
	if !warned() {
		t.Fatal("the drift must be named on every start until the file is restored")
	}
	run("SELECT 1", false)
	if warned() {
		t.Fatal("restoring the file must end the warning")
	}

	if _, err := testPool.Exec(ctx, `UPDATE schema_migrations SET checksum = NULL WHERE name = $1`, name); err != nil {
		t.Fatalf("null the checksum: %v", err)
	}
	run("SELECT 1", false)
	if warned() {
		t.Fatal("a row recorded before checksums existed must adopt the file silently")
	}
	if got := stored(); got == nil || *got != migrationChecksum("SELECT 1") {
		t.Fatalf("checksum after adoption = %v, want the hash of the current text", got)
	}
}

// TestRunMigration_ChecksumErrors reaches the three failure exits of the
// verify path without a production seam: the ledger read times out behind an
// ACCESS EXCLUSIVE lock held by another transaction, and the adoption write
// for a NULL checksum, then its commit, fail on a trigger that raises for the
// probe row only.
func TestRunMigration_ChecksumErrors(t *testing.T) {
	ctx := context.Background()
	const name = "zz_checksum_error_probe.sql"
	if _, err := testPool.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DROP TRIGGER IF EXISTS zz_checksum_probe_update ON schema_migrations`,
			`DROP TRIGGER IF EXISTS zz_checksum_probe_commit ON schema_migrations`,
			`DROP FUNCTION IF EXISTS zz_checksum_probe_fail()`,
			`DELETE FROM schema_migrations WHERE name = 'zz_checksum_error_probe.sql'`,
		} {
			if _, err := testPool.Exec(context.Background(), stmt); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})
	expect := func(step string, err error, want string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want one containing %q", step, err, want)
		}
	}

	// The ledger read: a one-connection pool with a statement timeout, built
	// before the lock so its own startup migrations pass (the timeout governs
	// those too, hence seconds rather than milliseconds), then blocked on the
	// SELECT by an exclusive lock another transaction holds.
	u, err := url.Parse(testDBURL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	q := u.Query()
	q.Set("statement_timeout", "5000")
	u.RawQuery = q.Encode()
	timed, err := New(ctx, u.String(), 1, 1)
	if err != nil {
		t.Fatalf("open timed pool: %v", err)
	}
	defer timed.Close()
	lock, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := lock.Exec(ctx, `LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock ledger: %v", err)
	}
	_, err = timed.runMigration(ctx, name, "SELECT 1")
	if rbErr := lock.Rollback(ctx); rbErr != nil {
		t.Fatalf("release lock: %v", rbErr)
	}
	expect("read behind a lock", err, "failed to check migration status")

	// The checksum write: a row trigger that refuses the probe row's UPDATE.
	if _, err := testPool.Exec(ctx, `
		CREATE FUNCTION zz_checksum_probe_fail() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'probe refuses'; END $$`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		CREATE TRIGGER zz_checksum_probe_update BEFORE UPDATE ON schema_migrations
		FOR EACH ROW WHEN (NEW.name = 'zz_checksum_error_probe.sql')
		EXECUTE FUNCTION zz_checksum_probe_fail()`); err != nil {
		t.Fatalf("create update trigger: %v", err)
	}
	_, err = testDB.runMigration(ctx, name, "SELECT 1")
	expect("refused update", err, "failed to record migration checksum")
	if _, err := testPool.Exec(ctx, `DROP TRIGGER zz_checksum_probe_update ON schema_migrations`); err != nil {
		t.Fatalf("drop update trigger: %v", err)
	}

	// The commit: the same refusal deferred to commit time, so the UPDATE
	// itself succeeds and the transaction fails when it closes.
	if _, err := testPool.Exec(ctx, `
		CREATE CONSTRAINT TRIGGER zz_checksum_probe_commit AFTER UPDATE ON schema_migrations
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.name = 'zz_checksum_error_probe.sql')
		EXECUTE FUNCTION zz_checksum_probe_fail()`); err != nil {
		t.Fatalf("create commit trigger: %v", err)
	}
	_, err = testDB.runMigration(ctx, name, "SELECT 1")
	expect("refused commit", err, "failed to commit migration checksum")
}
