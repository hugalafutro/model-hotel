package settings

import (
	"context"
	"testing"
	"time"
)

// A key with no row is the common case for a runtime setting: nearly every
// one is read on the proxy's per-request path with a default the operator
// never overrode. These tests pin that an absence is cached like a value, that
// every write path sees through it at once, and that it expires like a value.
//
// The observable is a row inserted with raw SQL, which bypasses the eviction a
// Set performs: a reader still answering "absent" afterwards is a reader that
// did not go to the database.

// rawInsert writes a row behind the repository's back.
func rawInsert(t *testing.T, key, value string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())", key, value); err != nil {
		t.Fatalf("raw insert failed: %v", err)
	}
}

func TestGetWithDefaultCachesAnAbsentKey(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)
	key := "absent_key"

	if got := r.GetWithDefault(ctx, key, "default"); got != "default" {
		t.Fatalf("setup: got %q, want the default", got)
	}
	if !r.IsCached(key) {
		t.Fatal("an absent key was read and not cached: every read of an unset setting is a SELECT")
	}

	// A row that appears behind the repository's back is invisible for the
	// TTL, exactly as a changed value would be: the absence was cached.
	rawInsert(t, key, "written-behind")
	if got := r.GetWithDefault(ctx, key, "default"); got != "default" {
		t.Errorf("got %q, want the cached absence to hold for the TTL", got)
	}

	// Eviction reads the row.
	r.InvalidateCache(key)
	if got := r.GetWithDefault(ctx, key, "default"); got != "written-behind" {
		t.Errorf("after invalidation got %q, want the row", got)
	}
}

func TestGetCheckedCachesAnAbsentKey(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)
	key := "absent_checked_key"

	if v, found, err := r.GetChecked(ctx, key); err != nil || found || v != "" {
		t.Fatalf("setup: got (%q, %v, %v), want absent", v, found, err)
	}
	if !r.IsCached(key) {
		t.Fatal("an absent key was read and not cached")
	}

	rawInsert(t, key, "written-behind")
	if v, found, err := r.GetChecked(ctx, key); err != nil || found || v != "" {
		t.Errorf("got (%q, %v, %v), want the cached absence to hold for the TTL", v, found, err)
	}

	// The two readers share the cache: an absence GetChecked cached answers
	// GetWithDefault too, and the other way round.
	if got := r.GetWithDefault(ctx, key, "default"); got != "default" {
		t.Errorf("GetWithDefault got %q, want the absence GetChecked cached", got)
	}

	r.InvalidateCache(key)
	if v, found, err := r.GetChecked(ctx, key); err != nil || !found || v != "written-behind" {
		t.Errorf("after invalidation got (%q, %v, %v), want the row", v, found, err)
	}
}

// Every write path evicts, so the first write after a miss is seen at once.
// This is the invariant that makes negative caching safe to add: an operator
// saving a setting for the first time must not wait out a TTL of "unset".
func TestWritesSeeThroughACachedAbsence(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	set, many, deleted := "absent_then_set", "absent_then_setmany", "set_then_deleted"

	if got := r.GetWithDefault(ctx, set, "d"); got != "d" {
		t.Fatalf("setup: got %q", got)
	}
	if err := r.Set(ctx, set, "v"); err != nil {
		t.Fatal(err)
	}
	if got := r.GetWithDefault(ctx, set, "d"); got != "v" {
		t.Errorf("after Set got %q, want the write: Set must evict a cached absence", got)
	}

	if got := r.GetWithDefault(ctx, many, "d"); got != "d" {
		t.Fatalf("setup: got %q", got)
	}
	if err := r.SetMany(ctx, [][2]string{{many, "v"}}); err != nil {
		t.Fatal(err)
	}
	if got := r.GetWithDefault(ctx, many, "d"); got != "v" {
		t.Errorf("after SetMany got %q, want the write: SetMany must evict a cached absence", got)
	}

	if err := r.Set(ctx, deleted, "v"); err != nil {
		t.Fatal(err)
	}
	if got := r.GetWithDefault(ctx, deleted, "d"); got != "v" {
		t.Fatalf("setup: got %q", got)
	}
	if err := r.DeleteKey(ctx, deleted); err != nil {
		t.Fatal(err)
	}
	if got := r.GetWithDefault(ctx, deleted, "d"); got != "d" {
		t.Errorf("after DeleteKey got %q, want the default", got)
	}
	// And the absence a delete leaves behind is cached in its turn.
	rawInsert(t, deleted, "written-behind")
	if got := r.GetWithDefault(ctx, deleted, "d"); got != "d" {
		t.Errorf("got %q, want the absence left by the delete to be cached", got)
	}
}

func TestACachedAbsenceExpiresWithTheTTL(t *testing.T) {
	r := NewRepository(testPool)
	r.cacheTTL = 100 * time.Millisecond
	ctx := context.Background()
	clearSettings(t)
	key := "absent_ttl_key"

	if got := r.GetWithDefault(ctx, key, "d"); got != "d" {
		t.Fatalf("setup: got %q", got)
	}
	rawInsert(t, key, "written-behind")
	if got := r.GetWithDefault(ctx, key, "d"); got != "d" {
		t.Fatalf("got %q, want the cached absence inside the TTL", got)
	}

	time.Sleep(r.cacheTTL + 50*time.Millisecond)

	if got := r.GetWithDefault(ctx, key, "d"); got != "written-behind" {
		t.Errorf("after the TTL got %q, want the row", got)
	}
}

// The generation guard applies to an absence the way it applies to a value,
// and this is the case where it matters most: a reader that observed no row
// while a Set was landing must not cache "absent" over the write, or the
// setting the operator just saved is invisible for a full TTL. The
// interleaving is made deterministic the way TestCacheGenerationGuardBlocksLateReader
// does it, by parking the reader's SELECT behind a table lock.
func TestCacheGenerationGuardBlocksALateAbsence(t *testing.T) {
	ctx := context.Background()
	clearSettings(t)
	r := NewRepository(testPool)
	key := "gen_guard_absent_key"

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin failed: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "LOCK TABLE settings IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock table failed: %v", err)
	}

	readCtx, cancelRead := context.WithTimeout(ctx, 60*time.Second)
	defer cancelRead()
	got := make(chan string, 1)
	go func() { got <- r.GetWithDefault(readCtx, key, "default") }()

	waitForBlockedSettingsRead(t)

	// A write's evictions land while the reader is parked inside its query.
	// Only the cache side is simulated, because the write itself would queue
	// behind the same table lock; NotifyDeleted moves the generation the way
	// Set's evictions do.
	r.NotifyDeleted(key)

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	select {
	case v := <-got:
		if v != "default" {
			t.Errorf("the reader returned %q, want the default it observed", v)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("blocked read never completed")
	}

	if r.IsCached(key) {
		t.Error("a reader whose generation moved mid-query must not cache the absence it observed")
	}
}
