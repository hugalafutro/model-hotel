package provider

import (
	"context"
	"net/url"
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
	u, err := url.Parse(testDBURL)
	if err != nil {
		t.Fatalf("parse test DB URL: %v", err)
	}
	q := u.Query()
	q.Set("statement_timeout", "250")
	u.RawQuery = q.Encode()
	slow, err := db.New(ctx, u.String(), 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer slow.Close()
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
