package db

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// fatalRecorder stands in for *testing.T where a helper is expected to fail
// the test: it records the Fatalf instead of ending the goroutine, so the
// failure path itself can be asserted.
type fatalRecorder struct{ failed bool }

func (*fatalRecorder) Helper()                 {}
func (r *fatalRecorder) Fatalf(string, ...any) { r.failed = true }
func (*fatalRecorder) Cleanup(func())          {}

func TestTracedPool_CallsFnAsEachQueryStarts(t *testing.T) {
	var calls atomic.Int32
	pool := TracedPool(t, testDBURL, func() { calls.Add(1) })
	for range 2 {
		if _, err := pool.Exec(context.Background(), "SELECT 1"); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("fn ran %d times for 2 queries, want 2", got)
	}
}

func TestTracedPool_UnparseableURLFailsTheTest(t *testing.T) {
	rec := &fatalRecorder{}
	if pool := TracedPool(rec, "postgres://%zz", func() {}); pool != nil || !rec.failed {
		t.Fatalf("pool = %v, failed = %v; want nil pool and a failed test", pool, rec.failed)
	}
}

func TestInsertTestProvider_InsertsAnEnabledProvider(t *testing.T) {
	ctx := context.Background()
	name := "testdb-provider-" + uuid.NewString()
	id := InsertTestProvider(ctx, t, testPool, name)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, id) })

	var gotName string
	var enabled bool
	if err := testPool.QueryRow(ctx, `SELECT name, enabled FROM providers WHERE id = $1`, id).Scan(&gotName, &enabled); err != nil {
		t.Fatalf("read back provider: %v", err)
	}
	if gotName != name || !enabled {
		t.Fatalf("provider = (%q, enabled %v), want (%q, enabled true)", gotName, enabled, name)
	}
}

func TestInsertTestProvider_FailedInsertFailsTheTest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := &fatalRecorder{}
	InsertTestProvider(ctx, rec, testPool, "testdb-provider-cancelled")
	if !rec.failed {
		t.Fatal("an insert on a cancelled context did not fail the test")
	}
}

// ReadAllowList must keep SQL NULL (unrestricted) and '{}' (restricted to
// nothing) apart, which a plain []string scan cannot.
func TestReadAllowList_DistinguishesNullFromEmpty(t *testing.T) {
	ctx := context.Background()
	suffix := uuid.NewString()
	open, empty, listed := "open-"+suffix, "empty-"+suffix, "listed-"+suffix
	if _, err := testPool.Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers) VALUES
			($1, $1, 'sk-...op', NULL),
			($2, $2, 'sk-...em', '{}'),
			($3, $3, 'sk-...li', ARRAY['a','b'])`, open, empty, listed); err != nil {
		t.Fatalf("seed virtual keys: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM virtual_keys WHERE key_hash = ANY($1)`, []string{open, empty, listed})
	})

	if isNull, card, _ := ReadAllowList(t, testPool, KeyAllowListQuery, open); !isNull || card != -1 {
		t.Errorf("NULL column: isNull %v card %d, want true -1", isNull, card)
	}
	if isNull, card, _ := ReadAllowList(t, testPool, KeyAllowListQuery, empty); isNull || card != 0 {
		t.Errorf("'{}' column: isNull %v card %d, want false 0", isNull, card)
	}
	if isNull, card, members := ReadAllowList(t, testPool, KeyAllowListQuery, listed); isNull || card != 2 || len(members) != 2 {
		t.Errorf("listed column: isNull %v card %d members %v, want false 2 [a b]", isNull, card, members)
	}
}

func TestReadAllowList_MissingRowFailsTheTest(t *testing.T) {
	rec := &fatalRecorder{}
	ReadAllowList(rec, testPool, UserAllowListQuery, "no-such-user-"+uuid.NewString())
	if !rec.failed {
		t.Fatal("reading a missing row did not fail the test")
	}
}
