package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
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
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, key)
	return r.WithContext(ctx)
}

func newTestLimiter() (*Limiter, *stubSettings) {
	repo := newStubSettings()
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
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(true)(next)

	for i := 0; i < 5; i++ {
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

	for i := 0; i < 3; i++ {
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
	for i := 0; i < 2; i++ {
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
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	for i := 0; i < 2; i++ {
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
	for i := 0; i < 200; i++ {
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
