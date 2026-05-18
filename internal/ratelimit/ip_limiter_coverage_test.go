package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIPLimiter_CleanupLoopStop verifies that Stop() closes the channel
// properly so the cleanup goroutine exits.
func TestIPLimiter_CleanupLoopStop(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, ipSettingsNoBackpressure())

	// Stop() should close the channel without panicking
	lim.Stop()
}

// TestIPLimiter_UnlimitedRPS tests that when settings provide RPS=0,
// the getLimiter uses extremely high RPS (1e6). Many requests should all succeed.
func TestIPLimiter_UnlimitedRPS(t *testing.T) {
	settings := &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPRPS:       "0", // 0 = unlimited
		settingsKeyIPBurst:     "0",
		settingsKeyIPMaxWaitMs: "200",
	}}
	lim := NewIPLimiter(1, 1, nil, settings)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// Fire many requests — none should be rate limited
	for i := 0; i < 200; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "10.10.10.10:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 with unlimited RPS, got %d", i+1, rr.Code)
		}
	}
}

// TestIPLimiter_SettingsChangeRPS tests that with default RPS=1, burst=1,
// then providing settings that override to high RPS allows more requests.
func TestIPLimiter_SettingsChangeRPS(t *testing.T) {
	// Start with settings that limit to RPS=1, burst=1
	settings := &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPRPS:       "1",
		settingsKeyIPBurst:     "1",
		settingsKeyIPMaxWaitMs: "0",
	}}
	lim := NewIPLimiter(1, 1, nil, settings)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// First request succeeds
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "172.16.0.1:5000"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Second request should fail (burst exhausted, RPS=1)
	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "172.16.0.1:5000"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second request with RPS=1: expected 429, got %d", rr.Code)
	}

	// Now update settings to high RPS
	settings.values[settingsKeyIPRPS] = "1000"
	settings.values[settingsKeyIPBurst] = "1000"

	// Evict the cached limiter so next request picks up new settings
	lim.mu.Lock()
	delete(lim.limiters, "172.16.0.1")
	lim.mu.Unlock()

	// With new settings, many more requests should succeed
	for i := 0; i < 50; i++ {
		req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "172.16.0.1:5000"
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d after settings change: expected 200, got %d", i+3, rr.Code)
		}
	}
}

// TestIPLimiter_ExistingEntryReuse tests that when the same IP makes a
// second request and RPS/burst haven't changed, the existing limiter
// is reused (lastUsed updated).
func TestIPLimiter_ExistingEntryReuse(t *testing.T) {
	settings := &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPRPS:       "10",
		settingsKeyIPBurst:     "5",
		settingsKeyIPMaxWaitMs: "0",
	}}
	lim := NewIPLimiter(10, 5, nil, settings)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	ip := "192.168.50.50:9999"

	// First request - creates limiter entry
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = ip
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Get the initial lastUsed time
	lim.mu.Lock()
	entry1, ok1 := lim.limiters["192.168.50.50"]
	lim.mu.Unlock()
	if !ok1 {
		t.Fatal("limiter entry should exist after first request")
	}
	initialLastUsed := entry1.lastUsed

	// Small delay to ensure time difference is detectable
	// (time.Now() may have limited resolution)

	// Second request - should reuse existing limiter (update lastUsed)
	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = ip
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rr.Code)
	}

	// Verify lastUsed was updated (limiter was reused, not recreated)
	lim.mu.Lock()
	entry2, ok2 := lim.limiters["192.168.50.50"]
	lim.mu.Unlock()
	if !ok2 {
		t.Fatal("limiter entry should still exist")
	}
	if !entry2.lastUsed.After(initialLastUsed) {
		t.Error("lastUsed should be updated on reuse")
	}
	// Verify it's the same limiter instance (same pointer)
	if entry1 != entry2 {
		t.Error("limiter should be reused (same entry pointer)")
	}
}

// TestIPLimiter_CleanupEmptyMap tests that cleanup on an empty map
// does not panic.
func TestIPLimiter_CleanupEmptyMap(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, ipSettingsNoBackpressure())
	defer lim.Stop()

	// Ensure map is empty
	lim.mu.Lock()
	lim.limiters = make(map[string]*ipEntry)
	lim.mu.Unlock()

	// Should not panic on empty map
	lim.cleanup()

	// Verify still empty
	lim.mu.Lock()
	if len(lim.limiters) != 0 {
		t.Errorf("expected 0 entries, got %d", len(lim.limiters))
	}
	lim.mu.Unlock()
}
