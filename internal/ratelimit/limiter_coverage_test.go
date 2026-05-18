package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// TestCleanupLoop_StopsOnChannelClose verifies that Stop() properly closes
// the stop channel so the cleanup goroutine exits.
func TestCleanupLoop_StopsOnChannelClose(t *testing.T) {
	lim, _ := newTestLimiter()

	// Stop() should close the channel without panicking
	lim.Stop()
}

// TestMiddleware_BackpressureRejectsLongWait tests the Middleware's
// backpressure path where delay exceeds max_wait. With RPS=0.1, burst=1,
// max_wait_ms=5, the first request succeeds. The second request should
// get 429 because the wait (~10s) exceeds max_wait (5ms).
func TestMiddleware_BackpressureRejectsLongWait(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "0.1") // 1 request per 10s
	repo.set(settingsKeyBurst, "1")
	repo.set(settingsKeyMaxWaitMs, "5") // 5ms max wait

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// First request succeeds (consumes the single burst token)
	req := requestWithKey("bp-long-wait")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Second request should get 429 because wait (~10s) exceeds max_wait (5ms)
	req = requestWithKey("bp-long-wait")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 (wait exceeds max_wait), got %d", rr.Code)
	}
}

// TestMiddleware_WasDisabledReenableEviction tests that when rate limiting
// is disabled then re-enabled, old limiters are evicted.
func TestMiddleware_WasDisabledReenableEviction(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "1")
	repo.set(settingsKeyBurst, "1")
	repo.set(settingsKeyMaxWaitMs, "0")

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Make a request to "evict-test" (consumes burst)
	req := requestWithKey("evict-test")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Disable rate limiting
	repo.set("rate_limit_enabled", "false")

	// Make another request (passes through, sets wasDisabled=true)
	req = requestWithKey("evict-test")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("request while disabled: expected 200, got %d", rr.Code)
	}

	// Re-enable rate limiting
	repo.set("rate_limit_enabled", "true")

	// Make request to "evict-test" - should succeed because old limiter was evicted
	req = requestWithKey("evict-test")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after re-enable (limiter evicted), got %d", rr.Code)
	}
}

// TestMiddleware_PerKeyOverrideNil tests that when per-key override context
// values are nil (wrong type), the global settings are used.
func TestMiddleware_PerKeyOverrideNil(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "5")
	repo.set(settingsKeyMaxWaitMs, "0")

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Request with wrong types in context (string instead of *float64/*int)
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, "key-wrong-type")
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitRPSKey, "not-a-float")
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitBurstKey, "not-an-int")
	r = r.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Should use global burst=5, so we can make 4 more requests (5 total)
	for i := 0; i < 4; i++ {
		r = r.WithContext(ctx)
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, r)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 (global burst=5), got %d", i+2, rr.Code)
		}
	}

	// 6th request should fail (burst exhausted)
	r = r.WithContext(ctx)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exhausting global burst of 5, got %d", rr.Code)
	}
}

// TestMiddleware_ExtractKeyFallbackToRemoteAddr tests that when no virtual
// key hash is in context, the middleware uses RemoteAddr as the key.
func TestMiddleware_ExtractKeyFallbackToRemoteAddr(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "2")
	repo.set(settingsKeyMaxWaitMs, "0")

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Make requests without virtual key hash in context - should use RemoteAddr
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "192.168.1.100:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd request should be rate limited (burst exhausted for this IP)
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "192.168.1.100:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 (fallback to RemoteAddr rate limiting), got %d", rr.Code)
	}
}
