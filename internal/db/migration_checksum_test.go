package db

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// TestRunMigration_Checksum covers the ledger's checksum: applying a migration
// records the hash of its text; a re-run with the same text is silent; a
// re-run with different text is skipped, warned about once, and the new hash
// recorded so the next start is silent again; a row recorded before checksums
// existed (NULL) adopts the current text without a warning.
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

	// The edit is not executed (a run would fail on the syntax), only named.
	run("SELECT 1 -- edited after shipping", false)
	if !warned() {
		t.Fatal("a re-run with changed text must warn")
	}
	if got := stored(); got == nil || *got != migrationChecksum("SELECT 1 -- edited after shipping") {
		t.Fatalf("checksum after drift = %v, want the hash of the current text", got)
	}
	run("SELECT 1 -- edited after shipping", false)
	if warned() {
		t.Fatal("the drift must be named once, not on every start")
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
