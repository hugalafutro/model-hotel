package settings

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
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

func TestCacheTTL(t *testing.T) {
	r := NewRepository(testPool)
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

	time.Sleep(r.cacheTTL + time.Second)

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

	for i := 0; i < subCount; i++ {
		subs[i] = r.Subscribe()
		dones[i] = make(chan ChangeEvent, 1)
	}

	defer func() {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}()

	for i := 0; i < subCount; i++ {
		i := i
		go func() {
			change := <-subs[i].Events()
			dones[i] <- change
		}()
	}

	if err := r.Set(ctx, key, "broadcast"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	for i := 0; i < subCount; i++ {
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
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = r.Set(ctx, fmt.Sprintf("race_key_%d", n), fmt.Sprintf("val_%d", n))
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.GetWithDefault(ctx, fmt.Sprintf("race_key_%d", n), "default")
		}(i)
	}

	// Concurrent subscriber drain.
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(100 * time.Millisecond)
		for {
			select {
			case <-sub.Events():
			case <-timeout:
				return
			}
		}
	}()

	wg.Wait()
}
