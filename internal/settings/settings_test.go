package settings

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/db"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	testURL, setupErr := db.SetupTestDB("settings")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("settings")

	testDB, err := db.New(ctx, testURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	testPool = testDB.Pool()
	defer testDB.Close()
	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

func clearSettings(t *testing.T) {
	ctx := context.Background()
	_, err := testPool.Exec(ctx, "DELETE FROM settings")
	if err != nil {
		t.Fatalf("failed to clear settings: %v", err)
	}
}

func TestGetWithDefault(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "test_key"
	defaultVal := "default"

	val := r.GetWithDefault(ctx, key, defaultVal)
	if val != defaultVal {
		t.Errorf("expected %q, got %q", defaultVal, val)
	}

	err := r.Set(ctx, key, "newval")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val = r.GetWithDefault(ctx, key, defaultVal)
	if val != "newval" {
		t.Errorf("expected %q, got %q", "newval", val)
	}

	// Clear cache
	r.mu.Lock()
	r.cache = make(map[string]cacheEntry)
	r.mu.Unlock()

	val = r.GetWithDefault(ctx, key, defaultVal)
	if val != "newval" {
		t.Errorf("expected %q, got %q", "newval", val)
	}

	// Update directly and check cache
	_, err = testPool.Exec(ctx, "UPDATE settings SET value = 'cached' WHERE key = $1", key)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	val = r.GetWithDefault(ctx, key, defaultVal)
	if val != "newval" {
		t.Errorf("expected cached %q, got %q", "newval", val)
	}

	r.InvalidateCache(key)

	val = r.GetWithDefault(ctx, key, defaultVal)
	if val != "cached" {
		t.Errorf("expected %q, got %q", "cached", val)
	}
}

func TestGetChecked(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "checked_key"

	// Absent key: found=false, no error, no value.
	val, found, err := r.GetChecked(ctx, key)
	if err != nil || found || val != "" {
		t.Fatalf("absent key: got (%q, %v, %v), want (\"\", false, nil)", val, found, err)
	}

	if err := r.Set(ctx, key, "present"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Present key read from DB (cache was evicted by Set): found=true.
	val, found, err = r.GetChecked(ctx, key)
	if err != nil || !found || val != "present" {
		t.Fatalf("present key: got (%q, %v, %v), want (\"present\", true, nil)", val, found, err)
	}

	// Cache hit: change the row via raw SQL; within TTL the cached value wins.
	if _, err := testPool.Exec(ctx,
		"UPDATE settings SET value = 'changed' WHERE key = $1", key); err != nil {
		t.Fatalf("raw update failed: %v", err)
	}
	val, found, err = r.GetChecked(ctx, key)
	if err != nil || !found || val != "present" {
		t.Fatalf("cache hit: got (%q, %v, %v), want (\"present\", true, nil)", val, found, err)
	}

	// After invalidation the fresh DB value is read.
	r.InvalidateCache(key)
	val, found, err = r.GetChecked(ctx, key)
	if err != nil || !found || val != "changed" {
		t.Fatalf("post-invalidate: got (%q, %v, %v), want (\"changed\", true, nil)", val, found, err)
	}

	// A canceled context returns a non-nil error and must not cache anything.
	// Use a fresh key that was never read (so the read must hit the DB, where
	// the canceled context surfaces); Set evicts it from the cache without
	// populating it, and unlike InvalidateCache does not best-effort re-read it.
	coldKey := "checked_cold_key"
	if err := r.Set(ctx, coldKey, "x"); err != nil {
		t.Fatalf("Set cold key failed: %v", err)
	}
	r.InvalidateCache(coldKey) // may repopulate; drop it again just before the read
	r.mu.Lock()
	delete(r.cache, coldKey)
	r.mu.Unlock()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	val, found, err = r.GetChecked(canceled, coldKey)
	if err == nil {
		t.Fatalf("canceled context: want non-nil error, got (%q, %v, nil)", val, found)
	}
	if found || val != "" {
		t.Errorf("canceled context: got (%q, %v), want (\"\", false)", val, found)
	}
	if r.IsCached(coldKey) {
		t.Errorf("canceled read must not populate the cache")
	}
}

func TestSetMany(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Seed one key so the batch exercises both INSERT and ON CONFLICT UPDATE.
	if err := r.Set(ctx, "k1", "old"); err != nil {
		t.Fatalf("seed Set failed: %v", err)
	}

	if err := r.SetMany(ctx, [][2]string{
		{"k1", "new"},
		{"k2", "two"},
		{"k3", "three"},
	}); err != nil {
		t.Fatalf("SetMany failed: %v", err)
	}

	// Read straight from the DB (cache was evicted) to confirm every row landed.
	want := map[string]string{"k1": "new", "k2": "two", "k3": "three"}
	for k, v := range want {
		var got string
		if err := testPool.QueryRow(ctx,
			"SELECT value FROM settings WHERE key = $1", k).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", k, err)
		}
		if got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	// Empty input is a no-op, not an error or a malformed statement.
	if err := r.SetMany(ctx, nil); err != nil {
		t.Errorf("SetMany(nil) = %v, want nil", err)
	}
}

func TestSetManyNotifiesSubscribers(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	sub := r.Subscribe()
	defer sub.Unsubscribe()

	if err := r.SetMany(ctx, [][2]string{{"a", "1"}, {"b", "2"}}); err != nil {
		t.Fatalf("SetMany failed: %v", err)
	}

	got := map[string]string{}
	for range 2 {
		select {
		case c := <-sub.Events():
			got[c.Key] = c.Value
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for change events, got %v", got)
		}
	}
	if got["a"] != "1" || got["b"] != "2" {
		t.Errorf("change events = %v, want a=1 b=2", got)
	}
}

func TestSubscribe(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	sub := r.Subscribe()
	defer sub.Unsubscribe()

	done := make(chan bool)

	go func() {
		change := <-sub.Events()
		if change.Key != "test_change" || change.Value != "value" {
			t.Errorf("unexpected change: %+v", change)
		}
		done <- true
	}()

	err := r.Set(ctx, "test_change", "value")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for change event")
	}
}

func TestSetTxAndInvalidate(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "discovery_interval"

	sub := r.Subscribe()
	defer sub.Unsubscribe()

	done := make(chan bool)

	go func() {
		change := <-sub.Events()
		if change.Key != key || change.Value != "tx_value" {
			t.Errorf("unexpected change: %+v", change)
		}
		done <- true
	}()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	err = r.SetTx(ctx, tx, key, "tx_value")
	if err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}

	r.InvalidateCache(key)

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for change event")
	}
}

func TestSetTxAllowed(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	err = r.SetTx(ctx, tx, "not_allowed", "val")
	if err == nil {
		t.Error("expected error for not allowed key")
	}

	err = r.SetTx(ctx, tx, "discovery_interval", "val")
	if err != nil {
		t.Errorf("expected no error for allowed key, got %v", err)
	}
}

func TestGetInt(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "test_int"
	err := r.Set(ctx, key, "42")
	if err != nil {
		t.Fatal(err)
	}

	i := r.GetInt(ctx, key, 0)
	if i != 42 {
		t.Errorf("got %d, want 42", i)
	}

	err = r.Set(ctx, key, "invalid")
	if err != nil {
		t.Fatal(err)
	}

	i = r.GetInt(ctx, key, 10)
	if i != 10 {
		t.Errorf("got %d, want 10", i)
	}
}

func TestGetBool(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "test_bool"
	err := r.Set(ctx, key, "true")
	if err != nil {
		t.Fatal(err)
	}

	b := r.GetBool(ctx, key, false)
	if !b {
		t.Error("got false, want true")
	}

	err = r.Set(ctx, key, "invalid")
	if err != nil {
		t.Fatal(err)
	}

	b = r.GetBool(ctx, key, false)
	if b {
		t.Error("got true, want false")
	}
}

func TestGetDuration(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "test_duration"
	err := r.Set(ctx, key, "5m")
	if err != nil {
		t.Fatal(err)
	}

	d := r.GetDuration(ctx, key, time.Minute)
	if d != 5*time.Minute {
		t.Errorf("got %v, want 5m", d)
	}

	err = r.Set(ctx, key, "invalid")
	if err != nil {
		t.Fatal(err)
	}

	d = r.GetDuration(ctx, key, time.Minute)
	if d != time.Minute {
		t.Errorf("got %v, want 1m", d)
	}
}

func TestGetDurationDaySuffix(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "test_duration"
	err := r.Set(ctx, key, "1d")
	if err != nil {
		t.Fatal(err)
	}

	d := r.GetDuration(ctx, key, 0)
	if d != 24*time.Hour {
		t.Errorf("got %v, want 24h0m0s", d)
	}

	err = r.Set(ctx, key, "7d")
	if err != nil {
		t.Fatal(err)
	}

	d = r.GetDuration(ctx, key, 0)
	if d != 7*24*time.Hour {
		t.Errorf("got %v, want 168h0m0s", d)
	}

	err = r.Set(ctx, key, "2d12h30m")
	if err != nil {
		t.Fatal(err)
	}

	d = r.GetDuration(ctx, key, 0)
	want := 2*24*time.Hour + 12*time.Hour + 30*time.Minute
	if d != want {
		t.Errorf("got %v, want %v", d, want)
	}
}

func TestGetFloat(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "test_float"
	err := r.Set(ctx, key, "3.14")
	if err != nil {
		t.Fatal(err)
	}

	f := r.GetFloat(ctx, key, 0.0)
	if f != 3.14 {
		t.Errorf("got %f, want 3.14", f)
	}

	err = r.Set(ctx, key, "invalid")
	if err != nil {
		t.Fatal(err)
	}

	f = r.GetFloat(ctx, key, 2.718)
	if f != 2.718 {
		t.Errorf("got %f, want 2.718", f)
	}
}

// A cached value is served until its entry expires, then re-read. The
// "still cached" read used to race a 100ms TTL against the two queries
// before it, which the race detector's slowdown lost on CI, so the TTL is
// generous and expiry is forced on the entry rather than waited for.
func TestCacheTTL(t *testing.T) {
	r := NewRepository(testPool)
	r.cacheTTL = time.Minute
	ctx := context.Background()
	clearSettings(t)

	key := "ttl_key"
	err := r.Set(ctx, key, "initial")
	if err != nil {
		t.Fatal(err)
	}

	val := r.GetWithDefault(ctx, key, "default")
	if val != "initial" {
		t.Errorf("got %q, want initial", val)
	}

	// Update DB
	_, err = testPool.Exec(ctx, "UPDATE settings SET value = 'updated' WHERE key = $1", key)
	if err != nil {
		t.Fatal(err)
	}

	// Still cached
	val = r.GetWithDefault(ctx, key, "default")
	if val != "initial" {
		t.Errorf("got %q, want initial (cached)", val)
	}

	// Expire the entry in place: the same state the TTL elapsing leaves.
	// cacheGen is left alone on purpose: expiry is not eviction, and only
	// evictLocked bumps the generation.
	r.mu.Lock()
	entry, ok := r.cache[key]
	if !ok {
		r.mu.Unlock()
		t.Fatal("the read did not populate the cache")
	}
	entry.expiresAt = time.Now().Add(-time.Second)
	r.cache[key] = entry
	r.mu.Unlock()

	val = r.GetWithDefault(ctx, key, "default")
	if val != "updated" {
		t.Errorf("got %q, want updated", val)
	}
}

// TestGet tests the raw Get method which returns an error for missing keys.
func TestGet(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "get_test_key"
	val := "get_test_value"

	// Missing key should return an error.
	_, err := r.Get(ctx, key)
	if err == nil {
		t.Error("expected error for missing key")
	}

	// Set a value and Get should return it.
	if err := r.Set(ctx, key, val); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := r.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != val {
		t.Errorf("Get = %q, want %q", got, val)
	}
}

// TestGetAll tests that GetAll returns all key-value pairs.
func TestGetAll(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Empty result.
	result, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll on empty table failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}

	// Insert several settings.
	settings := map[string]string{
		"discovery_interval": "30s",
		"theme":              "dark",
		"rate_limit_enabled": "true",
	}
	for k, v := range settings {
		if err := r.Set(ctx, k, v); err != nil {
			t.Fatalf("Set %s failed: %v", k, err)
		}
	}

	result, err = r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(result) < len(settings) {
		t.Errorf("GetAll returned %d entries, want at least %d", len(result), len(settings))
	}
	for k, want := range settings {
		if got := result[k]; got != want {
			t.Errorf("GetAll[%s] = %q, want %q", k, got, want)
		}
	}
}

// TestSetCacheInvalidation tests that Set clears the cached entry so that
// a subsequent GetWithDefault fetches the fresh value from the database.
func TestSetCacheInvalidation(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "cache_inval_key"

	// Write a value through Set so it is in the DB, then prime the cache.
	if err := r.Set(ctx, key, "original"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	val := r.GetWithDefault(ctx, key, "default")
	if val != "original" {
		t.Fatalf("expected 'original' after Set, got %q", val)
	}

	// Bypass Set and update the value directly in the database.
	_, err := testPool.Exec(ctx,
		"UPDATE settings SET value = $1, updated_at = now() WHERE key = $2",
		"bypass", key)
	if err != nil {
		t.Fatalf("direct update failed: %v", err)
	}

	// Cache is still holding "original" because we didn't invalidate yet.
	val = r.GetWithDefault(ctx, key, "default")
	if val != "original" {
		t.Errorf("expected cached 'original', got %q", val)
	}

	// Set should delete the cache entry and write the new value.
	if err := r.Set(ctx, key, "new"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val = r.GetWithDefault(ctx, key, "default")
	if val != "new" {
		t.Errorf("expected 'new' after Set, got %q", val)
	}
}

// TestRegisterOnChange tests that registered callbacks are invoked
// when settings change via Set.
func TestRegisterOnChange(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "onchange_key"
	done := make(chan ChangeEvent, 1)

	r.RegisterOnChange(func(k, v string) {
		done <- ChangeEvent{Key: k, Value: v}
	})

	if err := r.Set(ctx, key, "fired"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	select {
	case event := <-done:
		if event.Key != key || event.Value != "fired" {
			t.Errorf("callback got key=%q value=%q, want key=%q value=%q",
				event.Key, event.Value, key, "fired")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for onChange callback")
	}
}

// TestSubscribeMultiSubscriber tests that multiple subscribers each receive
// change events for every write.
func TestSubscribeMultiSubscriber(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "multi_sub_key"
	subCount := 3
	subs := make([]*Subscription, subCount)
	dones := make([]chan ChangeEvent, subCount)

	for i := range subCount {
		subs[i] = r.Subscribe()
		dones[i] = make(chan ChangeEvent, 1)
	}

	defer func() {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}()

	for i := range subCount {
		go func() {
			change := <-subs[i].Events()
			dones[i] <- change
		}()
	}

	if err := r.Set(ctx, key, "broadcast"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	for i := range subCount {
		select {
		case change := <-dones[i]:
			if change.Key != key || change.Value != "broadcast" {
				t.Errorf("subscriber %d got key=%q value=%q, want key=%q value=%q",
					i, change.Key, change.Value, key, "broadcast")
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d timed out waiting for event", i)
		}
	}
}

// TestSubscriptionDoubleUnsubscribe tests that calling Unsubscribe twice
// (or more) does not panic.
func TestSubscriptionDoubleUnsubscribe(t *testing.T) {
	r := NewRepository(testPool)

	sub := r.Subscribe()

	// First unsubscribe should clean up.
	sub.Unsubscribe()

	// Second unsubscribe must be safe (no-op via sync.Once).
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("double Unsubscribe panicked: %v", r)
			}
		}()
		sub.Unsubscribe()
	}()
}

// TestSetEmptyValue tests that setting a key to an empty string works
// and is retrievable.
func TestSetEmptyValue(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "empty_val_key"

	if err := r.Set(ctx, key, ""); err != nil {
		t.Fatalf("Set empty value failed: %v", err)
	}

	got, err := r.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after Set empty failed: %v", err)
	}
	if got != "" {
		t.Errorf("Get = %q, want empty string", got)
	}
}

func TestConcurrentSetGetSubscribe(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	sub := r.Subscribe()
	defer sub.Unsubscribe()

	var wg sync.WaitGroup

	// Concurrent writers.
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = r.Set(ctx, fmt.Sprintf("race_key_%d", n), fmt.Sprintf("val_%d", n))
		}(i)
	}

	// Concurrent readers.
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.GetWithDefault(ctx, fmt.Sprintf("race_key_%d", n), "default")
		}(i)
	}

	// Concurrent subscriber drain.
	wg.Go(func() {
		timeout := time.After(100 * time.Millisecond)
		for {
			select {
			case <-sub.Events():
			case <-timeout:
				return
			}
		}
	})

	wg.Wait()
}

// ---------------------------------------------------------------------------
// TestGetAll edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetAll_Empty(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// GetAll when no settings exist - should return empty map
	result, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestRepository_SetAndGetAll(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Set a setting
	err := r.Set(ctx, "test_setting", "test_value")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// GetAll should include it
	result, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(result) < 1 {
		t.Errorf("expected at least 1 setting, got %d", len(result))
	}
	if val, ok := result["test_setting"]; !ok || val != "test_value" {
		t.Errorf("expected test_setting=test_value, got %q", result["test_setting"])
	}
}

// ---------------------------------------------------------------------------
// TestGetWithDefault edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetWithDefault_Missing(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Get non-existent key with default - should return default
	defaultValue := "my_default_value"
	result := r.GetWithDefault(ctx, "non_existent_key", defaultValue)
	if result != defaultValue {
		t.Errorf("expected default %q, got %q", defaultValue, result)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_test.go
// ---------------------------------------------------------------------------

// TestRepository_GetAll_DBError tests that GetAll returns an error when the
// database query fails. Uses a canceled context to trigger the error.
func TestRepository_GetAll_DBError(t *testing.T) {
	r := NewRepository(testPool)
	// Use a canceled context to trigger a DB error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.GetAll(ctx)
	if err == nil {
		t.Error("expected error from GetAll with canceled context, got nil")
	}
}

// TestRepository_Set_DBError tests that Set returns an error when the
// database operation fails. Uses a canceled context to trigger the error.
func TestRepository_Set_DBError(t *testing.T) {
	r := NewRepository(testPool)
	// Use a canceled context to trigger a DB error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Set(ctx, "test_key", "test_value")
	if err == nil {
		t.Error("expected error from Set with canceled context, got nil")
	}
}

// TestRepository_Set_InvalidatesCache tests that Set invalidates the cache
// entry for the key, so a subsequent GetWithDefault fetches the fresh value.
func TestRepository_Set_InvalidatesCache(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	key := "cache_test_key"

	// Set initial value
	if err := r.Set(ctx, key, "initial"); err != nil {
		t.Fatalf("Set initial failed: %v", err)
	}

	// Prime the cache
	val := r.GetWithDefault(ctx, key, "default")
	if val != "initial" {
		t.Fatalf("expected 'initial' after Set, got %q", val)
	}

	// Update value via Set - this should invalidate the cache
	if err := r.Set(ctx, key, "updated"); err != nil {
		t.Fatalf("Set updated failed: %v", err)
	}

	// GetWithDefault should now return the new value (cache was invalidated)
	val = r.GetWithDefault(ctx, key, "default")
	if val != "updated" {
		t.Errorf("expected 'updated' after cache invalidation, got %q", val)
	}
}

// TestUnsubscribe_NotSubscribed tests that calling unsubscribe with a
// subscription ID that doesn't exist does not panic.
func TestUnsubscribe_NotSubscribed(t *testing.T) {
	r := NewRepository(testPool)

	// Create a subscription and manually call unsubscribe with wrong ID
	sub := r.Subscribe()
	validID := sub.id
	sub.Unsubscribe()

	// Now try to unsubscribe with an invalid ID - should not panic
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("unsubscribe with invalid ID panicked: %v", rec)
			}
		}()
		// Try to unsubscribe with a non-existent ID
		r.unsubscribe(validID + 1)
	}()
}

// TestUnsubscribe_EmptySubscriptions tests that unsubscribe handles the case
// where there are no subscriptions registered.
func TestUnsubscribe_EmptySubscriptions(t *testing.T) {
	r := NewRepository(testPool)

	// No subscriptions exist, calling unsubscribe should not panic
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("unsubscribe on empty list panicked: %v", rec)
			}
		}()
		r.unsubscribe(999)
	}()
}

// TestNotifyChange_NoSubscribers tests that notifyChange handles the case
// where there are no subscribers registered without panicking.
func TestNotifyChange_NoSubscribers(t *testing.T) {
	r := NewRepository(testPool)

	// No subscribers registered, calling notifyChange should not panic
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("notifyChange with no subscribers panicked: %v", rec)
			}
		}()
		r.notifyChange("test_key", "test_value")
	}()
}

// TestNotifyChange_WithSubscribers tests that notifyChange delivers events
// to all registered subscribers.
func TestNotifyChange_WithSubscribers(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	sub := r.Subscribe()
	defer sub.Unsubscribe()

	received := make(chan ChangeEvent, 1)
	go func() {
		event := <-sub.Events()
		received <- event
	}()

	// Trigger notifyChange via Set
	err := r.Set(ctx, "notify_key", "notify_value")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	select {
	case event := <-received:
		if event.Key != "notify_key" || event.Value != "notify_value" {
			t.Errorf("got event %+v, want key=notify_key value=notify_value", event)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for change event")
	}
}

// TestRepository_GetAll_SingleEntry tests GetAll with exactly one entry.
func TestRepository_GetAll_SingleEntry(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Insert a single setting directly via SQL to avoid cache effects
	_, err := testPool.Exec(ctx,
		"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())",
		"single_key", "single_value")
	if err != nil {
		t.Fatalf("failed to insert test setting: %v", err)
	}

	result, err := r.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 setting, got %d", len(result))
	}
	if val, ok := result["single_key"]; !ok || val != "single_value" {
		t.Errorf("expected single_key=single_value, got %q", result["single_key"])
	}
}

// ---------------------------------------------------------------------------
// Unsubscribe edge cases
// ---------------------------------------------------------------------------

// TestUnsubscribe_DoubleUnsubscribeViaSubscriptionMethod tests calling
// Unsubscribe() on a Subscription that was never actually used (no events
// received). This exercises the clean func / drain goroutine path when the
// channel is empty — confirming the drain goroutine completes and closes
// the channel without hanging.
func TestUnsubscribe_UnusedSubscription(t *testing.T) {
	r := NewRepository(testPool)
	sub := r.Subscribe()

	// Never read from sub.Events() — channel is empty.
	// Unsubscribe should drain and close it without blocking.
	done := make(chan struct{})
	go func() {
		sub.Unsubscribe()
		close(done)
	}()

	select {
	case <-done:
		// Success — unsubscribe completed.
	case <-time.After(2 * time.Second):
		t.Fatal("Unsubscribe on unused subscription hung — drain goroutine may be stuck")
	}

	// Double-unsubscribe must also be safe (sync.Once no-op).
	sub.Unsubscribe()
}

// TestUnsubscribe_BufferedEventsInChannel tests that Unsubscribe correctly
// drains buffered events from the channel before closing it, preventing
// publishers from blocking on a full channel.
func TestUnsubscribe_BufferedEventsInChannel(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	sub := r.Subscribe()

	// Send multiple events to buffer them in the channel
	for i := range 5 {
		err := r.Set(ctx, "buffer_test_key", fmt.Sprintf("val_%d", i))
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}
	}

	// Now unsubscribe — the drain goroutine should drain all buffered events
	done := make(chan struct{})
	go func() {
		sub.Unsubscribe()
		close(done)
	}()

	select {
	case <-done:
		// Success — unsubscribe with buffered events completed
	case <-time.After(2 * time.Second):
		t.Fatal("Unsubscribe hung with buffered events — drain goroutine may be stuck")
	}
}

// ---------------------------------------------------------------------------
// DeleteKey
// ---------------------------------------------------------------------------

// TestDeleteKey_RemovesInternalKeyOutsideTheAllowlist is the whole reason
// DeleteKey exists. The internal `_`-prefixed keys (_fleet_*, _quota_schema_*)
// are deliberately off the operator allowlist, which is exactly what makes
// DeleteKeysTx refuse them — so a per-provider baseline had no removal path at
// all and its row outlived the provider forever.
func TestDeleteKey_RemovesInternalKeyOutsideTheAllowlist(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	const key = "_quota_schema_11111111-1111-1111-1111-111111111111"
	if AllowedSettings[key] {
		t.Fatal("setup: this test is meaningless if the key is on the allowlist")
	}
	if err := r.Set(ctx, key, `["a","b"]`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := r.DeleteKey(ctx, key); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}

	var count int
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM settings WHERE key = $1", key).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("got %d row(s) for the deleted key, want 0", count)
	}
}

// TestDeleteKey_EvictsTheCachedValue guards the half that a raw DELETE against
// the pool would miss: the value was read (and cached) moments earlier by the
// drift watch, and without eviction the next read would keep serving the
// deleted baseline for the rest of the cache TTL.
func TestDeleteKey_EvictsTheCachedValue(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	const key = "_quota_schema_22222222-2222-2222-2222-222222222222"
	if err := r.Set(ctx, key, "cached"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := r.GetWithDefault(ctx, key, ""); got != "cached" {
		t.Fatalf("setup: got %q, want the seeded value cached", got)
	}
	if !r.IsCached(key) {
		t.Fatal("setup: the read should have cached the value")
	}

	if err := r.DeleteKey(ctx, key); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}

	if got := r.GetWithDefault(ctx, key, "gone"); got != "gone" {
		t.Errorf("got %q after deletion, want the caller's default — the cache was not evicted", got)
	}
}

// TestDeleteKey_MissingKeyIsNotAnError keeps the caller's error handling honest:
// a provider deleted before it was ever polled has no baseline, and that must
// not read as a cleanup failure.
func TestDeleteKey_MissingKeyIsNotAnError(t *testing.T) {
	r := NewRepository(testPool)
	clearSettings(t)

	if err := r.DeleteKey(context.Background(), "_quota_schema_never-written"); err != nil {
		t.Errorf("deleting an absent key must be a no-op, got %v", err)
	}
}

// TestDeleteKey_NotifiesSubscribers matches Set's contract: a subscriber that
// caches a setting locally has to learn the value is gone, not keep the last
// one it saw.
func TestDeleteKey_NotifiesSubscribers(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	const key = "_quota_schema_33333333-3333-3333-3333-333333333333"
	if err := r.Set(ctx, key, "before"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sub := r.Subscribe()
	defer sub.Unsubscribe()

	if err := r.DeleteKey(ctx, key); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}

	select {
	case ev := <-sub.Events():
		if ev.Key != key {
			t.Errorf("got change event for %q, want %q", ev.Key, key)
		}
		if ev.Value != "" {
			t.Errorf("got value %q, want empty — the key is gone", ev.Value)
		}
	case <-time.After(2 * time.Second):
		t.Error("DeleteKey must notify subscribers, like Set does")
	}
}

// ---------------------------------------------------------------------------
// DeleteKeysTx
// ---------------------------------------------------------------------------

func TestDeleteKeysTx_DeletesSpecifiedKeys(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Insert settings directly.
	for _, kv := range []struct{ k, v string }{
		{"discovery_interval", "1h"},
		{"discovery_on_startup", "true"},
		{"circuit_breaker_enabled", "false"},
	} {
		_, err := testPool.Exec(ctx,
			"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now()) ON CONFLICT (key) DO UPDATE SET value = $2",
			kv.k, kv.v)
		if err != nil {
			t.Fatalf("insert %s: %v", kv.k, err)
		}
	}

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	if err := r.DeleteKeysTx(ctx, tx, []string{"discovery_interval", "circuit_breaker_enabled"}); err != nil {
		t.Fatalf("DeleteKeysTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Verify deleted keys are gone.
	var count int
	err = testPool.QueryRow(ctx, "SELECT count(*) FROM settings WHERE key = ANY($1)", []string{"discovery_interval", "circuit_breaker_enabled"}).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deleted settings, got %d", count)
	}

	// Verify remaining key still exists.
	val, err := r.Get(ctx, "discovery_on_startup")
	if err != nil {
		t.Fatalf("Get remaining: %v", err)
	}
	if val != "true" {
		t.Errorf("remaining key = %q, want %q", val, "true")
	}
}

func TestDeleteKeysTx_EmptyKeys(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	if err := r.DeleteKeysTx(ctx, tx, []string{}); err != nil {
		t.Errorf("DeleteKeysTx with empty keys should not error, got: %v", err)
	}
}

func TestDeleteKeysTx_InvalidKey(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	err = r.DeleteKeysTx(ctx, tx, []string{"not_a_real_setting"})
	if err == nil {
		t.Error("DeleteKeysTx should reject keys not in allowlist")
	}
}

// TestDeleteKeysTx_NilKeys tests that passing a nil slice returns nil
// (same as empty slice — the early return at len(keys) == 0).
func TestDeleteKeysTx_NilKeys(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	if err := r.DeleteKeysTx(ctx, tx, nil); err != nil {
		t.Errorf("DeleteKeysTx with nil keys should not error, got: %v", err)
	}
}

// TestDeleteKeysTx_CancelledContext tests that DeleteKeysTx returns an error
// when the transaction executes against a cancelled context (DB error path).
func TestDeleteKeysTx_CancelledContext(t *testing.T) {
	r := NewRepository(testPool)

	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(context.Background())

	// Cancelled context should cause the SQL DELETE to fail.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = r.DeleteKeysTx(ctx, tx, []string{"discovery_interval"})
	if err == nil {
		t.Error("expected error from DeleteKeysTx with cancelled context")
	}
}

// ---------------------------------------------------------------------------
// NotifyDeleted
// ---------------------------------------------------------------------------

func TestNotifyDeleted_EvictsCacheAndNotifies(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Insert and cache a setting.
	_, err := testPool.Exec(ctx,
		"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now()) ON CONFLICT (key) DO UPDATE SET value = $2",
		"discovery_interval", "5m")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Read to populate cache.
	_ = r.GetWithDefault(ctx, "discovery_interval", "2h")

	// NotifyDeleted should evict cache and publish SSE event.
	r.NotifyDeleted("discovery_interval")

	// Verify cache was evicted — the next read should come from DB (not cached).
	r.mu.RLock()
	_, inCache := r.cache["discovery_interval"]
	r.mu.RUnlock()
	if inCache {
		t.Error("NotifyDeleted should have evicted cache entry")
	}
}

// ---------------------------------------------------------------------------
// IsCached
// ---------------------------------------------------------------------------

func TestIsCached_AfterRead(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Before population, cache hit is false.
	if r.IsCached("discovery_interval") {
		t.Error("IsCached should return false before any read")
	}

	// Insert and read to populate cache.
	_, err := testPool.Exec(ctx,
		"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now()) ON CONFLICT (key) DO UPDATE SET value = $2",
		"discovery_interval", "3h")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	_ = r.GetWithDefault(ctx, "discovery_interval", "2h")

	if !r.IsCached("discovery_interval") {
		t.Error("IsCached should return true after read populates cache")
	}
}

// ---------------------------------------------------------------------------
// WarmCache
// ---------------------------------------------------------------------------

func TestWarmCache_PopulatesAllSettings(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Insert multiple settings.
	for _, kv := range []struct{ k, v string }{
		{"discovery_interval", "1h"},
		{"discovery_on_startup", "true"},
		{"circuit_breaker_enabled", "false"},
	} {
		_, err := testPool.Exec(ctx,
			"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now()) ON CONFLICT (key) DO UPDATE SET value = $2",
			kv.k, kv.v)
		if err != nil {
			t.Fatalf("insert %s: %v", kv.k, err)
		}
	}

	r.WarmCache(ctx)

	if !r.IsCached("discovery_interval") {
		t.Error("WarmCache should populate discovery_interval")
	}
	if !r.IsCached("discovery_on_startup") {
		t.Error("WarmCache should populate discovery_on_startup")
	}
	if !r.IsCached("circuit_breaker_enabled") {
		t.Error("WarmCache should populate circuit_breaker_enabled")
	}
}

// TestWarmCache_DBError tests that WarmCache gracefully handles a database
// error by returning early without populating the cache. Uses a cancelled
// context to make GetAll fail.
func TestWarmCache_DBError(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// Insert a setting and verify it's not cached before WarmCache
	_, err := testPool.Exec(ctx,
		"INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now()) ON CONFLICT (key) DO UPDATE SET value = $2",
		"discovery_interval", "30m")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Use a cancelled context so GetAll fails
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// WarmCache should not panic; it logs a warning and returns
	r.WarmCache(cancelledCtx)

	// Cache should remain empty since WarmCache failed
	if r.IsCached("discovery_interval") {
		t.Error("WarmCache should not populate cache when GetAll fails")
	}
}

// TestWarmCache_EmptyDB tests that WarmCache handles an empty settings table
// without error — the cache remains empty but the function succeeds.
func TestWarmCache_EmptyDB(t *testing.T) {
	r := NewRepository(testPool)
	ctx := context.Background()
	clearSettings(t)

	// No settings in DB — WarmCache should succeed with nothing to cache
	r.WarmCache(ctx)

	// Cache should remain empty
	r.mu.RLock()
	cacheLen := len(r.cache)
	r.mu.RUnlock()
	if cacheLen != 0 {
		t.Errorf("expected empty cache after WarmCache on empty DB, got %d entries", cacheLen)
	}
}

// readerRacingWrite runs a reader hammering the key while write flips it from
// "20" to "0", then reports whether the post-write value is visible once the
// write returns. It is the shared body of the Set and SetMany regression
// tests below.
//
// The hazard being covered: the cache is evicted before the write, so a reader
// that misses the cache mid-write reads the *pre-write* row (the UPSERT has
// not committed, so it is invisible) and installs it. Without the post-write
// eviction that stale entry is served for the full cacheTTL (30s), which is
// far longer than any subscriber that re-reads on the change notification is
// prepared to wait.
func readerRacingWrite(t *testing.T, r *Repository, key string, write func() error) int {
	t.Helper()
	ctx := context.Background()

	violations := 0
	const rounds = 50
	deadline := time.Now().Add(90 * time.Second)

	for i := range rounds {
		if time.Now().After(deadline) {
			t.Fatalf("test budget exhausted after %d rounds", i)
		}
		if err := r.Set(ctx, key, "20"); err != nil {
			t.Fatalf("seed set failed: %v", err)
		}
		_ = r.GetInt(ctx, key, 5) // warm the cache with the pre-write value

		var stop atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			for !stop.Load() {
				_ = r.GetInt(ctx, key, 5)
			}
		}()

		err := write()
		stop.Store(true)
		<-done
		if err != nil {
			t.Fatalf("write failed: %v", err)
		}

		if got := r.GetInt(ctx, key, 5); got != 0 {
			violations++
		}
	}
	return violations
}

// TestSetVisibleToReaderRacingTheWrite pins the fix for a cache race in Set: a
// read concurrent with the write must not be able to pin the pre-write value
// for the rest of the cache TTL. Against the pre-fix code this reproduced on
// every round.
func TestSetVisibleToReaderRacingTheWrite(t *testing.T) {
	clearSettings(t)
	r := NewRepository(testPool)
	key := "race_set_key"

	violations := readerRacingWrite(t, r, key, func() error {
		return r.Set(context.Background(), key, "0")
	})
	if violations != 0 {
		t.Errorf("Set: reader observed the pre-write value after the write returned in %d rounds", violations)
	}
}

// TestSetManyVisibleToReaderRacingTheWrite is TestSetVisibleToReaderRacingTheWrite
// for SetMany, which had the identical evict-before-write-only bug.
func TestSetManyVisibleToReaderRacingTheWrite(t *testing.T) {
	clearSettings(t)
	r := NewRepository(testPool)
	key := "race_setmany_key"

	violations := readerRacingWrite(t, r, key, func() error {
		return r.SetMany(context.Background(), [][2]string{{key, "0"}})
	})
	if violations != 0 {
		t.Errorf("SetMany: reader observed the pre-write value after the write returned in %d rounds", violations)
	}
}

// waitForBlockedSettingsRead blocks until a query against the settings table is
// waiting on a lock, so the caller knows a reader has captured its cache
// generation and is parked inside its query.
func waitForBlockedSettingsRead(t *testing.T) {
	t.Helper()
	// Bound the probe itself by the same deadline, not just the loop: a probe
	// that blocks would otherwise never get back to the deadline check.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%FROM settings WHERE key%'
		`).Scan(&n)
		if err != nil {
			t.Fatalf("pg_stat_activity probe failed: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("reader never blocked on the settings table lock")
}

// TestCacheGenerationGuardBlocksLateReader covers the residual the double
// eviction alone cannot close: a reader whose query has already returned, but
// which has not yet taken the write lock, could still install the value it
// read after an eviction happened. The per-key generation counter stops it.
//
// The interleaving is made deterministic by holding an ACCESS EXCLUSIVE lock on
// the settings table, which blocks the reader's plain SELECT. That parks the
// reader precisely between capturing the generation and installing its result,
// with no test-only hook in the production code.
func TestCacheGenerationGuardBlocksLateReader(t *testing.T) {
	ctx := context.Background()
	clearSettings(t)
	r := NewRepository(testPool)
	key := "gen_guard_key"

	if err := r.Set(ctx, key, "old"); err != nil {
		t.Fatalf("seed set failed: %v", err)
	}
	if r.IsCached(key) {
		t.Fatal("Set must leave the cache empty")
	}

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin failed: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "LOCK TABLE settings IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock table failed: %v", err)
	}

	// Bound the read so a lock we somehow fail to release cannot hang the suite.
	readCtx, cancelRead := context.WithTimeout(ctx, 60*time.Second)
	defer cancelRead()
	got := make(chan string, 1)
	go func() { got <- r.GetWithDefault(readCtx, key, "default") }()

	waitForBlockedSettingsRead(t)

	// An invalidation lands while the reader is parked inside its query. This
	// only touches the cache, so it does not contend for the table lock.
	r.NotifyDeleted(key)

	// Release the lock; the reader's SELECT now returns the row it was waiting
	// on, which is the value that predates the invalidation.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	select {
	case v := <-got:
		// The reader still returns what it read — the guard suppresses caching,
		// not the result.
		if v != "old" {
			t.Errorf("expected the reader to return the value it read, got %q", v)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("blocked read never completed")
	}

	if r.IsCached(key) {
		t.Error("a reader whose generation moved mid-query must not populate the cache")
	}
}

// TestCacheGenerationGuardStillCachesAfterWritesStop guards against the guard
// itself wedging the hot path: once writes stop, reads must cache normally
// again rather than degrading into a query per read.
func TestCacheGenerationGuardStillCachesAfterWritesStop(t *testing.T) {
	ctx := context.Background()
	clearSettings(t)
	r := NewRepository(testPool)
	key := "gen_settle_key"

	// Wait for the readers rather than leaking them: a reader still parked in
	// its SELECT would be matched by another test's pg_stat_activity probe.
	var wg sync.WaitGroup
	for range 20 {
		if err := r.Set(ctx, key, "1"); err != nil {
			t.Fatalf("set failed: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.GetWithDefault(ctx, key, "d")
		}()
	}
	if err := r.Set(ctx, key, "final"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	wg.Wait()

	// With writes finished, the next read must install a cache entry.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if v := r.GetWithDefault(ctx, key, "d"); v != "final" {
			t.Fatalf("expected %q, got %q", "final", v)
		}
		if r.IsCached(key) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("reads never resumed caching after writes stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestFailedWriteStillEvictsOnBothSides covers the error path of Set and
// SetMany. pgx reports a failure for a statement the server actually applied
// when the connection drops or the context is cancelled in the
// commit-acknowledgement window, and both functions are called with
// cancellable request contexts (quota drift, config-sync apply). The
// post-write eviction must therefore run even when Exec returns an error,
// otherwise a reader that installed the pre-write value mid-write keeps it for
// the full cacheTTL against a write that took effect. Only the change
// notification is gated on success.
//
// The discriminating observable is the generation counter: a write must move
// it twice, once on each side of Exec, regardless of the outcome. A full
// end-to-end repro would need a reader whose entire query completes inside the
// write window of a write that then fails, which cannot be scheduled without a
// fault-injection hook in production code. That hook is deliberately not
// added, so the invariant is asserted directly instead.
func TestFailedWriteStillEvictsOnBothSides(t *testing.T) {
	clearSettings(t)
	r := NewRepository(testPool)

	generation := func(key string) uint64 {
		r.mu.RLock()
		defer r.mu.RUnlock()
		return r.cacheGen[key]
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("Set", func(t *testing.T) {
		key := "failed_set_key"
		before := generation(key)
		if err := r.Set(cancelled, key, "new"); err == nil {
			t.Fatal("expected Set to fail on a cancelled context")
		}
		if moved := generation(key) - before; moved != 2 {
			t.Errorf("failed Set moved the generation %d time(s), want 2 (one eviction on each side of the write)", moved)
		}
	})

	t.Run("SetMany", func(t *testing.T) {
		key := "failed_setmany_key"
		before := generation(key)
		if err := r.SetMany(cancelled, [][2]string{{key, "new"}}); err == nil {
			t.Fatal("expected SetMany to fail on a cancelled context")
		}
		if moved := generation(key) - before; moved != 2 {
			t.Errorf("failed SetMany moved the generation %d time(s), want 2 (one eviction on each side of the write)", moved)
		}
	})

	t.Run("a failed write does not notify subscribers", func(t *testing.T) {
		key := "failed_notify_key"
		sub := r.Subscribe()
		defer sub.Unsubscribe()

		if err := r.Set(cancelled, key, "new"); err == nil {
			t.Fatal("expected Set to fail on a cancelled context")
		}
		select {
		case ev := <-sub.Events():
			t.Errorf("a failed write must not announce a change, got %+v", ev)
		case <-time.After(100 * time.Millisecond):
		}
	})
}
