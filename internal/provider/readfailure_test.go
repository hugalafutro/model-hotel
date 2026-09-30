package provider

import (
	"context"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// TestList_ReadFailure covers the iteration-error branch of List with a
// deterministic seam: a repository over a one-connection pool with a short
// statement_timeout, and an ACCESS EXCLUSIVE lock on providers held by another
// transaction. The first (unlocked) List prepares the statement; under the lock
// the execute phase times out and surfaces from rows.Err().
func TestList_ReadFailure(t *testing.T) {
	ctx := context.Background()
	// The timeout is set after connecting, not in the URL: there it also bound
	// the migration check db.New runs, which lost the race under -race in CI.
	// It holds for the pool's one connection, which a statement timeout does
	// not replace; the check below fails fast if it did not take.
	slow, err := db.New(ctx, testDBURL, 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer slow.Close()
	if _, err := slow.Pool().Exec(ctx, "SET statement_timeout = '250ms'"); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	var timeout string
	if err := slow.Pool().QueryRow(ctx, "SELECT current_setting('statement_timeout')").Scan(&timeout); err != nil || timeout != "250ms" {
		t.Fatalf("statement_timeout = %q (%v), want 250ms", timeout, err)
	}
	repo := NewRepository(slow.Pool())

	if _, err := repo.List(ctx); err != nil {
		t.Fatalf("warm-up List: %v", err)
	}

	holder, err := testDB.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, "LOCK TABLE providers IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock: %v", err)
	}

	if _, err := repo.List(ctx); err == nil {
		t.Fatal("List under lock: want read error, got nil")
	}
}
