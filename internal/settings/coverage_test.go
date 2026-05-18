package settings

import (
	"context"
	"testing"
	"time"
)

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
