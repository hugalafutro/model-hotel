package webauthn

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CreateLoginState + ConsumeLoginState back the OIDC SSO login handshake. They
// run over the same webauthn_sessions table as auth tokens, so they're exercised
// here against the real test database.

func TestLoginStateRoundTripAndSingleUse(t *testing.T) {
	sm := NewSessionManager(newTestRepo(t))
	ctx := context.Background()

	id, err := sm.CreateLoginState(ctx, []byte("state-blob"), 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateLoginState: %v", err)
	}
	t.Cleanup(func() { _ = newTestRepo(t).DeleteSession(ctx, id) })

	data, err := sm.ConsumeLoginState(ctx, id)
	if err != nil {
		t.Fatalf("ConsumeLoginState: %v", err)
	}
	if string(data) != "state-blob" {
		t.Fatalf("data = %q, want state-blob", data)
	}

	// Single-use: the record is gone after the first consume.
	if _, err := sm.ConsumeLoginState(ctx, id); err == nil {
		t.Fatal("second ConsumeLoginState should fail (single use)")
	}
}

func TestConsumeLoginStateMissing(t *testing.T) {
	sm := NewSessionManager(newTestRepo(t))
	if _, err := sm.ConsumeLoginState(context.Background(), uuid.New()); err == nil {
		t.Fatal("ConsumeLoginState on a missing id should fail")
	}
}

func TestConsumeLoginStateWrongType(t *testing.T) {
	repo := newTestRepo(t)
	sm := NewSessionManager(repo)
	ctx := context.Background()

	// A session of a different type must never be consumable as login state.
	id := uuid.New()
	rec := &SessionRecord{
		ID:          id,
		Challenge:   "c",
		SessionData: []byte("not-login-state"),
		Type:        "auth_token",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	if err := repo.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteSession(ctx, id) })

	if _, err := sm.ConsumeLoginState(ctx, id); err == nil {
		t.Fatal("ConsumeLoginState should reject a non-oidc_login record")
	}
}

func TestConsumeLoginStateExpired(t *testing.T) {
	sm := NewSessionManager(newTestRepo(t))
	ctx := context.Background()

	// Negative TTL -> already expired. GetSession returns it (no expiry filter),
	// so the expiry check inside ConsumeLoginState is what rejects it.
	id, err := sm.CreateLoginState(ctx, []byte("blob"), -time.Minute)
	if err != nil {
		t.Fatalf("CreateLoginState: %v", err)
	}
	t.Cleanup(func() { _ = newTestRepo(t).DeleteSession(ctx, id) })

	if _, err := sm.ConsumeLoginState(ctx, id); err == nil {
		t.Fatal("ConsumeLoginState should reject an expired record")
	}
}

// TestConsumeLoginStateConcurrent is the real single-use guard: many goroutines
// race to consume the same record and exactly one must win, because the DELETE
// (not the read) is the atomic claim.
func TestConsumeLoginStateConcurrent(t *testing.T) {
	sm := NewSessionManager(newTestRepo(t))
	ctx := context.Background()

	id, err := sm.CreateLoginState(ctx, []byte("blob"), 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateLoginState: %v", err)
	}
	t.Cleanup(func() { _ = newTestRepo(t).DeleteSession(ctx, id) })

	const racers = 8
	var success int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Go(func() {
			<-start
			if data, err := sm.ConsumeLoginState(ctx, id); err == nil && string(data) == "blob" {
				atomic.AddInt64(&success, 1)
			}
		})
	}
	close(start)
	wg.Wait()

	if success != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", success)
	}
}
