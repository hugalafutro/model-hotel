// Package dbtest holds database helpers shared by tests in several packages.
// It is imported only by _test.go files, never by production code.
package dbtest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// onQueryStart is a pgx tracer that runs fn as each query starts, so a test can
// land a cache eviction while a read-through's SELECT is in flight.
type onQueryStart struct{ fn func() }

func (q onQueryStart) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.fn()
	return ctx
}

func (onQueryStart) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TracedPool opens a pool on dbURL that calls fn as every query starts, and
// closes it when the test ends.
func TracedPool(t *testing.T, dbURL string, fn func()) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse test DB URL: %v", err)
	}
	cfg.ConnConfig.Tracer = onQueryStart{fn: fn}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// InsertTestProvider inserts an enabled provider row with only the columns a
// keyless provider needs (encrypted_key, key_nonce and key_salt are nullable)
// and returns its ID.
func InsertTestProvider(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, true, now(), now())
	`, id, name, "https://test.example.com"); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}

// KeyAllowListQuery and UserAllowListQuery read, for ReadAllowList, a virtual
// key's (by key_hash) or a user's (by username) allowed_providers as three
// facts no codec can collapse: is the column SQL NULL, how many members it has
// (-1 standing in for NULL, which has no cardinality), and what they are.
const (
	KeyAllowListQuery = `
	SELECT allowed_providers IS NULL,
	       coalesce(cardinality(allowed_providers), -1),
	       coalesce(allowed_providers, '{}'::text[])
	  FROM virtual_keys WHERE key_hash = $1`

	UserAllowListQuery = `
	SELECT allowed_providers IS NULL,
	       coalesce(cardinality(allowed_providers), -1),
	       coalesce(allowed_providers, '{}'::text[])
	  FROM users WHERE username = $1`
)

// ReadAllowList runs query (one parameter, subject) and returns three facts
// about an allowed_providers column, read in SQL: whether it is SQL NULL
// (unrestricted), its cardinality, and its members. The column is never scanned
// straight into a []string, because pgx yields a nil slice for both NULL and
// '{}' and that is the distinction callers assert on. A query failure fails the
// test: treating it as "nothing to assert" would pass every check on a missing
// row.
func ReadAllowList(t *testing.T, pool *pgxpool.Pool, query, subject string) (isNull bool, card int, members []string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), query, subject).Scan(&isNull, &card, &members); err != nil {
		t.Fatalf("reading allowed_providers for %q: %v", subject, err)
	}
	return isNull, card, members
}
