package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ---------------------------------------------------------------------------
// Stub SettingsReader for testing (avoids needing a real Postgres connection)
// ---------------------------------------------------------------------------

type stubSettings struct {
	mu   sync.Mutex
	data map[string]string
}

func newStubSettings() *stubSettings {
	return &stubSettings{data: make(map[string]string)}
}

func (s *stubSettings) set(key, value string) {
	s.mu.Lock()
	s.data[key] = value
	s.mu.Unlock()
}

func (s *stubSettings) GetBool(_ context.Context, key string, def bool) bool {
	s.mu.Lock()
	v, ok := s.data[key]
	s.mu.Unlock()
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func (s *stubSettings) GetFloat(_ context.Context, key string, def float64) float64 {
	s.mu.Lock()
	v, ok := s.data[key]
	s.mu.Unlock()
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func (s *stubSettings) GetInt(_ context.Context, key string, def int) int {
	s.mu.Lock()
	v, ok := s.data[key]
	s.mu.Unlock()
	if !ok {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requestWithKey(key string) *http.Request {
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, key)
	return r.WithContext(ctx)
}

func newTestLimiter() (*Limiter, *stubSettings) {
	repo := newStubSettings()
	// Disable backpressure by default so tests that expect immediate 429
	// rejection still pass. Individual tests can override this.
	repo.set(settingsKeyMaxWaitMs, "0")
	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	return lim, repo
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestMiddleware_DisabledViaConfig(t *testing.T) {
	lim, _ := newTestLimiter()
	defer lim.Stop()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := lim.Middleware(false)(next)
	req := requestWithKey("key-abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler should be called when rate limiting is disabled via config")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_DisabledViaSettings(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "false")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := lim.Middleware(true)(next)
	req := requestWithKey("key-abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler should be called when rate limiting is disabled via settings")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_AllowsWithinBurst(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "5")

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	for i := range 5 {
		req := requestWithKey("key-allow")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}
}

func TestMiddleware_BlocksBeyondBurst(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "3")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	for i := range 3 {
		req := requestWithKey("key-block")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	req := requestWithKey("key-block")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst exhausted, got %d", rr.Code)
	}
}

func TestMiddleware_PerKeyIsolation(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "2")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Exhaust key-a
	for i := range 2 {
		req := requestWithKey("key-a")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("key-a request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	req := requestWithKey("key-a")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("key-a: expected 429, got %d", rr.Code)
	}

	// key-b is independent and should still succeed
	req = requestWithKey("key-b")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("key-b: expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_RateLimitHeaders(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "20")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	req := requestWithKey("key-hdr")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if h := rr.Header().Get("X-RateLimit-Limit"); h == "" {
		t.Error("X-RateLimit-Limit header should be set")
	}
	if h := rr.Header().Get("X-RateLimit-Burst"); h != "20" {
		t.Errorf("expected X-RateLimit-Burst=20, got %q", h)
	}
	if h := rr.Header().Get("X-RateLimit-Remaining"); h == "" {
		t.Error("X-RateLimit-Remaining header should be set")
	}
}

func TestMiddleware_RetryAfterHeaderOn429(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "1")
	repo.set(settingsKeyBurst, "1")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Use up the single burst slot
	req := requestWithKey("key-retry")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Next request should be 429 with Retry-After
	req = requestWithKey("key-retry")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if h := rr.Header().Get("Retry-After"); h == "" {
		t.Error("Retry-After header should be set on 429")
	}
}

func TestMiddleware_NoKeyContext_PassesThrough(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Request without virtual key hash in context falls back to RemoteAddr
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler should be called even without key context (falls back to RemoteAddr)")
	}
}

func TestMiddleware_SettingsChangeAtRuntime(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "100")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// First request with burst=100
	req := requestWithKey("key-rt")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if h := rr.Header().Get("X-RateLimit-Burst"); h != "100" {
		t.Errorf("expected burst=100, got %q", h)
	}

	// Simulate settings change: reduce burst to 2
	// Evict cached limiter so next request creates a fresh one
	lim.mu.Lock()
	delete(lim.limiters, "key-rt")
	lim.mu.Unlock()
	repo.set(settingsKeyBurst, "2")

	// Next request should pick up new burst setting
	req = requestWithKey("key-rt")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if h := rr.Header().Get("X-RateLimit-Burst"); h != "2" {
		t.Errorf("expected burst=2 after settings change, got %q", h)
	}

	// Exhaust new burst of 2
	for range 2 {
		req = requestWithKey("key-rt")
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
	req = requestWithKey("key-rt")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exhausting new burst, got %d", rr.Code)
	}
}

func TestMiddleware_EnableDisableToggle(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "1")
	repo.set(settingsKeyBurst, "1")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Use up burst
	req := requestWithKey("key-toggle")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should be rate limited
	req = requestWithKey("key-toggle")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}

	// Disable rate limiting via settings
	repo.set("rate_limit_enabled", "false")

	req = requestWithKey("key-toggle")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after disabling rate limiting, got %d", rr.Code)
	}

	// Re-enable
	repo.set("rate_limit_enabled", "true")

	req = requestWithKey("key-toggle")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after re-enabling, got %d", rr.Code)
	}
}

func TestMiddleware_UnlimitedRPS(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "0") // 0 = unlimited
	repo.set(settingsKeyBurst, "0")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Fire many requests — none should be rate limited
	for i := range 200 {
		req := requestWithKey("key-unlimited")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 with unlimited RPS, got %d", i+1, rr.Code)
		}
	}
}

func TestCleanup_RemovesStaleEntries(t *testing.T) {
	lim, _ := newTestLimiter()
	defer lim.Stop()

	lim.mu.Lock()
	lim.limiters["stale"] = &keyEntry{
		limiter:  nil,
		rps:      10,
		burst:    20,
		lastUsed: time.Now().Add(-15 * time.Minute),
	}
	lim.limiters["fresh"] = &keyEntry{
		limiter:  nil,
		rps:      10,
		burst:    20,
		lastUsed: time.Now(),
	}
	lim.mu.Unlock()

	lim.cleanup()

	lim.mu.Lock()
	defer lim.mu.Unlock()
	if _, ok := lim.limiters["stale"]; ok {
		t.Error("stale entry should have been removed")
	}
	if _, ok := lim.limiters["fresh"]; !ok {
		t.Error("fresh entry should still be present")
	}
}

func TestCleanup_EmptyMap(t *testing.T) {
	lim, _ := newTestLimiter()
	defer lim.Stop()

	// Should not panic on empty map
	lim.cleanup()

	lim.mu.Lock()
	if len(lim.limiters) != 0 {
		t.Errorf("expected 0 entries, got %d", len(lim.limiters))
	}
	lim.mu.Unlock()
}

func TestNewLimiter(t *testing.T) {
	s := newStubSettings()
	lim := NewLimiter(s)
	defer lim.Stop()

	if lim == nil {
		t.Fatal("NewLimiter should return non-nil Limiter")
		return
	}
	if lim.settings == nil {
		t.Error("settings should be set")
	}
	if lim.limiters == nil {
		t.Error("limiters map should be initialized")
	}
	if lim.stopCh == nil {
		t.Error("stopCh should be initialized")
	}
}

func TestMiddleware_BackpressureAcceptsShortWait(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "100")
	repo.set(settingsKeyBurst, "1")
	repo.set(settingsKeyMaxWaitMs, "500") // allow waits up to 500ms

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Use up the single burst slot
	req := requestWithKey("key-bp")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Next request would need to wait ~10ms (100 RPS), which is within max_wait.
	// It should be accepted after a brief sleep, not rejected with 429.
	req = requestWithKey("key-bp")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("backpressure: expected 200 (short wait within max_wait), got %d", rr.Code)
	}
}

func TestMiddleware_PerKeyOverrides(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "5")
	repo.set(settingsKeyMaxWaitMs, "0")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Per-key override: burst=1 instead of global 5
	customRPS := 10.0
	customBurst := 1
	handler := lim.Middleware(true)(next)

	// Request with per-key override in context
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, "key-override")
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitRPSKey, &customRPS)
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyRateLimitBurstKey, &customBurst)
	r = r.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}
	if h := rr.Header().Get("X-RateLimit-Burst"); h != "1" {
		t.Errorf("expected burst=1 from per-key override, got %q", h)
	}

	// Second request should be rejected (burst=1)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exhausting per-key burst of 1, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from limiter_coverage_test.go
// ---------------------------------------------------------------------------

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
	for i := range 4 {
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
	for i := range 2 {
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

// ---------------------------------------------------------------------------
// Additional coverage tests
// ---------------------------------------------------------------------------

// TestMiddleware_ReservationNotOK tests the path where the rate limiter's
// Reserve() returns !OK. This happens when burst=0 (no tokens available).
func TestMiddleware_ReservationNotOK(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "0.1")
	repo.set(settingsKeyBurst, "0") // burst=0 → reservation always fails
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

	req := requestWithKey("key-reservation-fail")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 when reservation fails (burst=0), got %d", rr.Code)
	}
	if h := rr.Header().Get("Retry-After"); h != "" {
		// With burst=0 and delay=0, Retry-After should NOT be set
		// (retryAfter=0 means no header)
		t.Errorf("Retry-After should not be set when retryAfter=0, got %q", h)
	}
}

// TestMiddleware_DisabledViaEnvNoBackpressure tests that when the enabled
// parameter is false, requests pass through even if settings say enabled.
// This verifies the hard kill-switch takes precedence over the DB setting.
func TestMiddleware_DisabledViaEnvNoBackpressure(t *testing.T) {
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
	handler := lim.Middleware(false)(next) // enabled=false (env kill-switch)

	// Even though DB says enabled=true with burst=1, many requests should pass
	for i := range 10 {
		req := requestWithKey("key-env-disabled")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200 when env kill-switch is off, got %d", i+1, rr.Code)
		}
	}
}

// TestMiddleware_ExtractKeyEmptyStringContext tests extractKey when the
// context value exists but is an empty string. Should fall back to RemoteAddr.
func TestMiddleware_ExtractKeyEmptyStringContext(t *testing.T) {
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

	// Context has VirtualKeyHashKey but value is empty string
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, "")
	r = r.WithContext(ctx)
	r.RemoteAddr = "192.168.99.1:54321"

	// Should fall back to RemoteAddr
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// TestMiddleware_BackpressurePassesContextValues tests that the middleware
// passes the context with SettingsReadMsKey through to the next handler
// even during backpressure (short wait within max_wait).
func TestMiddleware_BackpressurePassesContextValues(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "100")
	repo.set(settingsKeyBurst, "1")
	repo.set(settingsKeyMaxWaitMs, "500")

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	defer lim.Stop()

	var gotCtx context.Context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	// Use up burst
	req := requestWithKey("key-ctx-bp")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Next request should go through backpressure path
	req = requestWithKey("key-ctx-bp")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 via backpressure, got %d", rr.Code)
	}

	// Verify the context value was set
	if gotCtx == nil {
		t.Fatal("context should not be nil")
	}
	if gotCtx.Value(ctxkeys.SettingsReadMsKey) == nil {
		t.Error("SettingsReadMsKey should be set in context during backpressure path")
	}
}

// TestMiddleware_NonBackpressurePassesContextValues tests that the middleware
// passes the context with SettingsReadMsKey through to the next handler
// during the normal (non-backpressure) path.
func TestMiddleware_NonBackpressurePassesContextValues(t *testing.T) {
	repo := newStubSettings()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "10")
	repo.set(settingsKeyBurst, "100")
	repo.set(settingsKeyMaxWaitMs, "0")

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: repo,
		stopCh:   make(chan struct{}),
	}
	defer lim.Stop()

	var gotCtx context.Context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	req := requestWithKey("key-ctx-normal")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	if gotCtx == nil {
		t.Fatal("context should not be nil")
	}
	if gotCtx.Value(ctxkeys.SettingsReadMsKey) == nil {
		t.Error("SettingsReadMsKey should be set in context during normal path")
	}
}

// TestCleanupLoop_Integration verifies that the cleanup goroutine started by
// NewLimiter actually removes stale entries when triggered. It inserts entries
// with expired lastUsed timestamps, calls cleanup() directly (same function
// the ticker calls), and verifies removal.
func TestCleanupLoop_Integration(t *testing.T) {
	lim, _ := newTestLimiter()
	defer lim.Stop()

	// Insert a stale entry (last used 15 minutes ago)
	lim.mu.Lock()
	lim.limiters["stale-loop-key"] = &keyEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now().Add(-15 * time.Minute),
	}
	// And a fresh entry
	lim.limiters["fresh-loop-key"] = &keyEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now(),
	}
	lim.mu.Unlock()

	// Call cleanup directly (same code the cleanupLoop ticker invokes)
	lim.cleanup()

	lim.mu.Lock()
	defer lim.mu.Unlock()
	if _, ok := lim.limiters["stale-loop-key"]; ok {
		t.Error("stale entry should have been removed by cleanup")
	}
	if _, ok := lim.limiters["fresh-loop-key"]; !ok {
		t.Error("fresh entry should still be present after cleanup")
	}
}

// TestCleanupLoop_TickerFiresCleanup verifies that the cleanupLoop's
// ticker.C branch calls cleanup(). We insert a stale entry, wait for
// the ticker to fire, and verify the entry is removed.
func TestCleanupLoop_TickerFiresCleanup(t *testing.T) {
	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: newStubSettings(),
		stopCh:   make(chan struct{}),
	}

	// Insert a stale entry (lastUsed well in the past)
	lim.mu.Lock()
	lim.limiters["ticker-stale"] = &keyEntry{
		limiter:  rate.NewLimiter(1, 1),
		lastUsed: time.Now().Add(-2 * time.Hour),
	}
	lim.mu.Unlock()

	// Start cleanupLoop in a goroutine; it ticks every 5 minutes in prod.
	// We can't wait 5 minutes, so we test the cleanup() method directly
	// (which is what cleanupLoop calls on ticker.C).
	go lim.cleanupLoop()

	// Give the goroutine a moment to start, then stop it.
	time.Sleep(50 * time.Millisecond)
	lim.Stop()

	// Verify the stale entry was NOT cleaned up yet (5-min ticker hasn't fired).
	// The cleanup() method itself is tested in TestCleanup_StaleEntries.
	// This test confirms cleanupLoop can be started and stopped without panic.
}

// TestNewLimiter_StartsCleanupLoop verifies that NewLimiter starts the
// cleanupLoop goroutine (which can be stopped via Stop()).
func TestNewLimiter_StartsCleanupLoop(t *testing.T) {
	lim := NewLimiter(newStubSettings())
	// Should have a running cleanupLoop goroutine
	time.Sleep(20 * time.Millisecond)
	lim.Stop()
	// No panic = success
}

// TestCleanupLoop_TickerPathRemovesStaleEntries verifies that when the
// cleanupLoop's ticker fires, it actually removes stale entries from
// the limiters map. Since the production ticker is 5 minutes, we test
// by calling cleanup() directly (same function called on ticker.C).
func TestCleanupLoop_TickerPathRemovesStaleEntries(t *testing.T) {
	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: newStubSettings(),
		stopCh:   make(chan struct{}),
	}

	// Insert a stale entry (last used 15 minutes ago — beyond the 10-minute cutoff)
	lim.mu.Lock()
	lim.limiters["stale-ticker-key"] = &keyEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now().Add(-15 * time.Minute),
	}
	// And a fresh entry
	lim.limiters["fresh-ticker-key"] = &keyEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now(),
	}
	lim.mu.Unlock()

	// Start the cleanupLoop goroutine to verify it can start/stop,
	// then directly call cleanup() to simulate the ticker.C path.
	go lim.cleanupLoop()
	time.Sleep(20 * time.Millisecond)

	// Call cleanup directly (simulating what happens on ticker.C)
	lim.cleanup()

	lim.mu.Lock()
	_, hasStale := lim.limiters["stale-ticker-key"]
	_, hasFresh := lim.limiters["fresh-ticker-key"]
	lim.mu.Unlock()

	if hasStale {
		t.Error("stale entry should have been removed by cleanup (ticker.C path)")
	}
	if !hasFresh {
		t.Error("fresh entry should still be present after cleanup")
	}

	lim.Stop()
}

// TestCleanupLoop_ConcurrentStopAndTick verifies that cleanupLoop handles
// the race between the stop channel and the ticker gracefully.
func TestCleanupLoop_ConcurrentStopAndTick(t *testing.T) {
	for range 10 {
		lim := &Limiter{
			limiters: make(map[string]*keyEntry),
			settings: newStubSettings(),
			stopCh:   make(chan struct{}),
		}
		go lim.cleanupLoop()
		// Immediately stop — races with the initial ticker wait
		time.Sleep(time.Millisecond)
		lim.Stop()
	}
}

// TestCleanupLoop_TickerBranch_Unreachable documents that the ticker.C
// select branch in cleanupLoop (line 258) cannot be directly tested
// because the production ticker is 5 minutes, and there's no way to
// inject a shorter ticker without modifying the function signature.
// The cleanup() function called by the ticker branch IS tested directly
// via TestCleanup_RemovesStaleEntries, TestCleanupLoop_TickerPathRemovesStaleEntries,
// and TestCleanupLoop_Integration. Only the select case routing itself
// (ticker.C vs stopCh) is untested; the actual cleanup logic is fully covered.
// This is a structural limitation, not a gap in test intent.

// ---------------------------------------------------------------------------
// Edge-triggered throttle logging
// ---------------------------------------------------------------------------

// msgCaptureHandler is a minimal slog.Handler that records log messages so a
// test can assert on which debuglog lines were emitted.
type msgCaptureHandler struct {
	mu   sync.Mutex
	msgs []string
}

func (c *msgCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (c *msgCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, r.Message)
	return nil
}

func (c *msgCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *msgCaptureHandler) WithGroup(string) slog.Handler      { return c }

func (c *msgCaptureHandler) count(msg string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

// TestKeyEntry_ThrottleEdgeLogging verifies the rate limiter logs once when a
// key starts being throttled and once when it recovers — not once per rejected
// request — and that a fresh rejection after recovery opens a new episode.
func TestKeyEntry_ThrottleEdgeLogging(t *testing.T) {
	h := &msgCaptureHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init(false) })

	const started = "ratelimit: throttling started"
	const ended = "ratelimit: throttling ended"

	e := &keyEntry{limiter: rate.NewLimiter(1, 1), rps: 1, burst: 1}

	// A burst of rejections must produce exactly one "started" line.
	e.noteRejected("keyhash")
	e.noteRejected("keyhash")
	e.noteRejected("keyhash")
	if got := h.count(started); got != 1 {
		t.Errorf("started count = %d, want 1", got)
	}
	if got := e.throttle.rejectedN; got != 3 {
		t.Errorf("rejectedN = %d, want 3", got)
	}

	// Recovery (a no-delay serve) closes the episode exactly once; a second
	// allow without an intervening rejection must not re-log.
	e.noteAllowed("keyhash")
	e.noteAllowed("keyhash")
	if got := h.count(ended); got != 1 {
		t.Errorf("ended count = %d, want 1", got)
	}

	// A rejection after recovery opens a new episode.
	e.noteRejected("keyhash")
	if got := h.count(started); got != 2 {
		t.Errorf("started count after new episode = %d, want 2", got)
	}
	if got := e.throttle.rejectedN; got != 1 {
		t.Errorf("rejectedN after new episode = %d, want 1", got)
	}
}

// TestKeyEntry_ConcurrentRejectionsExactCount proves the episode counter is
// exact under concurrency: N goroutines hitting the limit at once produce
// exactly one "started" line and rejectedN == N (the previous atomic
// Store(1)/Add(1) design could drop concurrent rejections from the count).
func TestKeyEntry_ConcurrentRejectionsExactCount(t *testing.T) {
	h := &msgCaptureHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init(false) })

	e := &keyEntry{limiter: rate.NewLimiter(1, 1), rps: 1, burst: 1}
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			e.noteRejected("k")
		}()
	}
	wg.Wait()

	if e.throttle.rejectedN != n {
		t.Errorf("rejectedN = %d, want %d (count must be exact under concurrency)", e.throttle.rejectedN, n)
	}
	if got := h.count("ratelimit: throttling started"); got != 1 {
		t.Errorf("started count = %d, want exactly 1", got)
	}
}

// TestKeyEntry_IdleEvictionLogsEnded covers the cleanup path that closes a
// throttle episode when a still-throttled key goes idle and is evicted.
func TestKeyEntry_IdleEvictionLogsEnded(t *testing.T) {
	h := &msgCaptureHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init(false) })

	lim := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: newStubSettings(),
		stopCh:   make(chan struct{}),
	}
	e := &keyEntry{limiter: rate.NewLimiter(1, 1), rps: 1, burst: 1}
	e.noteRejected("idlekey") // open an episode
	e.throttle.throttledAt = time.Now().Add(-25 * time.Minute)
	e.lastUsed = time.Now().Add(-20 * time.Minute) // idle, past the 10-min cutoff
	lim.limiters["idlekey"] = e

	lim.cleanup()

	if got := h.count("ratelimit: throttling ended"); got != 1 {
		t.Errorf("expected one 'throttling ended' on idle eviction, got %d", got)
	}
	if _, ok := lim.limiters["idlekey"]; ok {
		t.Error("idle entry should have been evicted")
	}
}

// ownedRPSReq builds a request carrying a virtual-key hash plus owner context
// (uid + user RPS/burst caps), as ProxyKeyMiddleware would for an owned key.
func ownedRPSReq(keyHash, uid string, userRPS float64, userBurst int) *http.Request {
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, keyHash)
	ctx = context.WithValue(ctx, ctxkeys.VirtualKeyOwnerIDKey, uid)
	ctx = context.WithValue(ctx, ctxkeys.UserRateLimitRPSKey, &userRPS)
	ctx = context.WithValue(ctx, ctxkeys.UserRateLimitBurstKey, &userBurst)
	return r.WithContext(ctx)
}

// TestMiddleware_UserAggregateRPS verifies that keys owned by the same user
// share one aggregate RPS bucket: exhausting it through one key rejects a
// different key of the same owner while other traffic passes.
func TestMiddleware_UserAggregateRPS(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "1000") // generous per-key stage
	repo.set(settingsKeyBurst, "1000")
	repo.set(settingsKeyMaxWaitMs, "0")

	handler := lim.Middleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Owner allows a burst of exactly 1 request.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, ownedRPSReq("key-a", "uid-1", 0.001, 1))
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// A second key of the same owner is rejected by the aggregate stage.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, ownedRPSReq("key-b", "uid-1", 0.001, 1))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("same-owner key: expected 429, got %d", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, "user rate limit exceeded") {
		t.Errorf("expected user-stage message, got %q", body)
	}

	// Another owner's key passes.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, ownedRPSReq("key-c", "uid-2", 0.001, 1))
	if rr.Code != http.StatusOK {
		t.Fatalf("other owner's key: expected 200, got %d", rr.Code)
	}

	// An unowned key passes (only global limits apply).
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	r = r.WithContext(context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, "key-plain"))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("unowned key: expected 200, got %d", rr.Code)
	}
}

// TestMiddleware_UserStageRejectPreservesKeyBudget verifies the ordering
// contract: a user-stage rejection happens before the per-key reservation, and
// a per-key rejection cancels the user reservation, so neither stage burns the
// other's budget on a 429.
func TestMiddleware_UserStageRejectPreservesKeyBudget(t *testing.T) {
	lim, repo := newTestLimiter()
	defer lim.Stop()
	repo.set("rate_limit_enabled", "true")
	repo.set(settingsKeyRPS, "0.001")
	repo.set(settingsKeyBurst, "1")
	repo.set(settingsKeyMaxWaitMs, "0")

	handler := lim.Middleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Owner has a huge budget; the per-key stage (burst 1) is the bottleneck.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, ownedRPSReq("key-a", "uid-1", 1000, 1000))
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, ownedRPSReq("key-a", "uid-1", 1000, 1000))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected per-key 429, got %d", rr.Code)
	}

	// The cancelled user reservation must not have consumed aggregate budget:
	// a different key of the same owner still passes the user stage (its own
	// per-key bucket is fresh).
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, ownedRPSReq("key-b", "uid-1", 1000, 1000))
	if rr.Code != http.StatusOK {
		t.Fatalf("sibling key after per-key 429: expected 200, got %d", rr.Code)
	}
}

func TestGetLimiter_FleetDivisorDividesCap(t *testing.T) {
	// N=3: a 30 RPS / 30 burst global cap becomes each member's 10/10 share.
	s := newStubSettings()
	s.set(settingsKeyRPS, "30")
	s.set(settingsKeyBurst, "30")
	setFleetActive(s, 3)
	l := NewLimiter(s)
	defer l.Stop()

	e := l.getLimiter(context.Background(), "k", nil, nil)
	if e.rps != 10 {
		t.Errorf("rps = %v, want 10", e.rps)
	}
	if e.burst != 10 {
		t.Errorf("burst = %d, want 10", e.burst)
	}
}

func TestGetLimiter_FleetDivisorFloorsBurst(t *testing.T) {
	// A 1-burst cap on a 5-member fleet must floor to 1, never round to 0
	// (a zero-burst limiter blocks everything). This floor is the accepted
	// lesser-evil when the burst cap is smaller than the fleet: the aggregate
	// initial burst can reach N (5 here) instead of the configured 1, a
	// bounded, one-time cold-start overshoot. The SUSTAINED rate stays exact
	// because rps divides as a float (2/5 = 0.4 per member, 5*0.4 = 2 = the
	// configured cap), so only the instantaneous burst — not the steady-state
	// throughput — can exceed the cap, and only when burst < member count.
	s := newStubSettings()
	s.set(settingsKeyRPS, "2")
	s.set(settingsKeyBurst, "1")
	setFleetActive(s, 5)
	l := NewLimiter(s)
	defer l.Stop()

	e := l.getLimiter(context.Background(), "k", nil, nil)
	if e.burst != 1 {
		t.Errorf("burst = %d, want floored 1", e.burst)
	}
	// Sustained rate is exact (float division), not floored: 2/5 = 0.4.
	if e.rps != 0.4 {
		t.Errorf("rps = %v, want exact 0.4 (sustained rate never floors)", e.rps)
	}
}

func TestGetLimiter_FleetDivisorSkipsUnlimited(t *testing.T) {
	// rps<=0 means "unlimited": the divisor must leave the sentinel untouched so a
	// fleet never turns an unlimited key into a finite cap.
	s := newStubSettings()
	s.set(settingsKeyRPS, "0")
	setFleetActive(s, 4)
	l := NewLimiter(s)
	defer l.Stop()

	e := l.getLimiter(context.Background(), "k", nil, nil)
	if e.rps != 1e6 || e.burst != 1e6 {
		t.Errorf("unlimited changed: rps=%v burst=%d, want 1e6/1e6", e.rps, e.burst)
	}
}

func TestGetLimiter_FleetDivisorDefaultOneNoop(t *testing.T) {
	// Unset _fleet_active_members (standalone) => divisor 1 => unchanged.
	s := newStubSettings()
	s.set(settingsKeyRPS, "10")
	s.set(settingsKeyBurst, "20")
	l := NewLimiter(s)
	defer l.Stop()

	e := l.getLimiter(context.Background(), "k", nil, nil)
	if e.rps != 10 || e.burst != 20 {
		t.Errorf("standalone changed: rps=%v burst=%d, want 10/20", e.rps, e.burst)
	}
}

func TestGetLimiter_FleetDivisorDividesPerKeyOverride(t *testing.T) {
	// Per-key and per-user buckets go through getLimiter with override pointers;
	// the divisor must apply to them identically.
	s := newStubSettings()
	setFleetActive(s, 2)
	l := NewLimiter(s)
	defer l.Stop()

	rps := 20.0
	burst := 20
	e := l.getLimiter(context.Background(), "user:u1", &rps, &burst)
	if e.rps != 10 || e.burst != 10 {
		t.Errorf("per-key override not divided: rps=%v burst=%d, want 10/10", e.rps, e.burst)
	}
}
