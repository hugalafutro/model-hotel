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

func TestCacheTTL(t *testing.T) {
	r := NewRepository(testPool)
	r.cacheTTL = 100 * time.Millisecond
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

	time.Sleep(r.cacheTTL + 50*time.Millisecond)

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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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

// ---------------------------------------------------------------------------
// DeleteKeysTx
// ---------------------------------------------------------------------------

func TestDeleteKeysTx_DeletesSpecifiedKeys(t *testing.T) {
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
	t.Helper()
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
