package ratelimit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// stubIPSettings is a minimal SettingsReader for IP limiter tests.
type stubIPSettings struct {
	values map[string]string
}

func (s *stubIPSettings) GetBool(_ context.Context, key string, def bool) bool {
	if v, ok := s.values[key]; ok {
		return v == "true"
	}
	return def
}

func (s *stubIPSettings) GetFloat(_ context.Context, key string, def float64) float64 {
	if v, ok := s.values[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func (s *stubIPSettings) GetInt(_ context.Context, key string, def int) int {
	if v, ok := s.values[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// ipSettingsNoBackpressure returns settings with IP limiting enabled but zero
// backpressure, so requests beyond burst get immediate 429s.
func ipSettingsNoBackpressure() *stubIPSettings {
	return &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPMaxWaitMs: "0",
	}}
}

// ---------------------------------------------------------------------------
// IPLimiter tests
// ---------------------------------------------------------------------------

func TestIPLimiter_AllowsWithinBurst(t *testing.T) {
	lim := NewIPLimiter(10, 5, nil, nil)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}
}

func TestIPLimiter_BlocksBeyondBurst(t *testing.T) {
	lim := NewIPLimiter(10, 3, nil, ipSettingsNoBackpressure())
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "5.6.7.8:5678"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "5.6.7.8:5678"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst exhausted, got %d", rr.Code)
	}
}

func TestIPLimiter_PerIPIsolation(t *testing.T) {
	lim := NewIPLimiter(10, 2, nil, ipSettingsNoBackpressure())
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// Exhaust IP-A
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "10.0.0.1:1000"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1000"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("IP-A: expected 429, got %d", rr.Code)
	}

	// IP-B should still succeed
	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "10.0.0.2:2000"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("IP-B: expected 200, got %d", rr.Code)
	}
}

func TestIPLimiter_HeadersOnSuccess(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, nil)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if h := rr.Header().Get("X-RateLimit-Scope"); h != "ip" {
		t.Errorf("expected X-RateLimit-Scope=ip, got %q", h)
	}
	if h := rr.Header().Get("X-RateLimit-Limit"); h == "" {
		t.Error("X-RateLimit-Limit should be set")
	}
	if h := rr.Header().Get("X-RateLimit-Burst"); h != "20" {
		t.Errorf("expected X-RateLimit-Burst=20, got %q", h)
	}
}

func TestIPLimiter_RetryAfterOn429(t *testing.T) {
	lim := NewIPLimiter(1, 1, nil, nil)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "9.8.7.6:4321"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "9.8.7.6:4321"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if h := rr.Header().Get("Retry-After"); h == "" {
		t.Error("Retry-After should be set on 429")
	}
}

func TestIPLimiter_DefaultsWhenZero(t *testing.T) {
	lim := NewIPLimiter(0, 0, nil, nil)
	defer lim.Stop()
	if lim.defaultRPS != defaultIPRPS {
		t.Errorf("rps = %v, want %v", lim.defaultRPS, defaultIPRPS)
	}
	if lim.defaultBurst != defaultIPBurst {
		t.Errorf("burst = %d, want %d", lim.defaultBurst, defaultIPBurst)
	}
}

func TestIPLimiter_CleanupRemovesStale(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, nil)
	defer lim.Stop()

	lim.mu.Lock()
	lim.limiters["stale-ip"] = &ipEntry{
		limiter:  rate.NewLimiter(10, 20),
		lastUsed: time.Now().Add(-15 * time.Minute),
	}
	lim.limiters["fresh-ip"] = &ipEntry{
		limiter:  rate.NewLimiter(10, 20),
		lastUsed: time.Now(),
	}
	lim.mu.Unlock()

	lim.cleanup()

	lim.mu.Lock()
	defer lim.mu.Unlock()
	if _, ok := lim.limiters["stale-ip"]; ok {
		t.Error("stale entry should have been removed")
	}
	if _, ok := lim.limiters["fresh-ip"]; !ok {
		t.Error("fresh entry should still be present")
	}
}

// ---------------------------------------------------------------------------
// extractClientIP tests
// ---------------------------------------------------------------------------

func TestExtractClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1:54321"
	ip := extractClientIP(r, nil)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %q", ip)
	}
}

func TestExtractClientIP_RemoteAddrNoPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1"
	ip := extractClientIP(r, nil)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %q", ip)
	}
}

func TestExtractClientIP_XFFIgnoredWhenUntrusted(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 3.3.3.3")
	// nil trustedProxies means header is ignored
	ip := extractClientIP(r, nil)
	if ip != "10.0.0.1" {
		t.Errorf("expected RemoteAddr when no trusted proxies, got %q", ip)
	}
}

func TestExtractClientIP_XRealIPIgnoredWhenUntrusted(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "8.8.8.8")
	ip := extractClientIP(r, nil)
	if ip != "10.0.0.1" {
		t.Errorf("expected RemoteAddr when no trusted proxies, got %q", ip)
	}
}

func TestExtractClientIP_XFFHonoredWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	ip := extractClientIP(r, trusted)
	// Rightmost non-trusted IP is returned (2.2.2.2 is not in 10.0.0.0/8)
	if ip != "2.2.2.2" {
		t.Errorf("expected rightmost non-trusted XFF IP, got %q", ip)
	}
}

func TestExtractClientIP_XRealIPHonoredWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "8.8.8.8")
	ip := extractClientIP(r, trusted)
	if ip != "8.8.8.8" {
		t.Errorf("expected X-Real-IP when trusted, got %q", ip)
	}
}

func TestExtractClientIP_XFFPriorityWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "4.4.4.4")
	r.Header.Set("X-Real-IP", "5.5.5.5")
	ip := extractClientIP(r, trusted)
	if ip != "4.4.4.4" {
		t.Errorf("X-Forwarded-For should take priority when trusted, got %q", ip)
	}
}

func TestExtractClientIP_HeadersIgnoredWhenRemoteNotTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1:1234" // not in trusted CIDR
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	r.Header.Set("X-Real-IP", "2.2.2.2")
	ip := extractClientIP(r, trusted)
	if ip != "192.168.1.1" {
		t.Errorf("expected RemoteAddr when remote not trusted, got %q", ip)
	}
}

func TestExtractClientIP_EmptyXFFWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "")
	r.Header.Set("X-Real-IP", "9.9.9.9")
	ip := extractClientIP(r, trusted)
	if ip != "9.9.9.9" {
		t.Errorf("expected fallback to X-Real-IP when trusted, got %q", ip)
	}
}

func TestExtractClientIP_IPv6(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "[::1]:12345"
	ip := extractClientIP(r, nil)
	if ip != "::1" {
		t.Errorf("expected ::1 for IPv6, got %q", ip)
	}
}

func TestExtractClientIP_RightmostNonTrustedMultiHop(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	// Chain: client (1.1.1.1) → CDN (2.2.2.2) → LB (10.0.0.5) → app
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 10.0.0.5")
	ip := extractClientIP(r, trusted)
	// 10.0.0.5 is trusted, 2.2.2.2 is not — should return 2.2.2.2
	if ip != "2.2.2.2" {
		t.Errorf("expected rightmost non-trusted 2.2.2.2, got %q", ip)
	}
}

func TestExtractClientIP_SpoofPrevention(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	// Attacker behind trusted proxy injects a fake leftmost IP
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "spoofed-ip, 9.9.9.9, 10.0.0.1")
	ip := extractClientIP(r, trusted)
	// 10.0.0.1 is trusted, 9.9.9.9 is not — should return 9.9.9.9
	if ip != "9.9.9.9" {
		t.Errorf("expected 9.9.9.9 (non-trusted), not spoofed leftmost, got %q", ip)
	}
}

func TestExtractClientIP_AllTrustedFallsBackToLeftmost(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	// Unusual: all XFF entries are in trusted range
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "10.0.0.2, 10.0.0.3")
	ip := extractClientIP(r, trusted)
	// Falls back to leftmost
	if ip != "10.0.0.2" {
		t.Errorf("expected leftmost fallback 10.0.0.2, got %q", ip)
	}
}

func TestExtractClientIP_IPv6XFFTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "[2001:db8::1]:1234"
	r.Header.Set("X-Forwarded-For", "2001:db8:1::100, 2001:db8::1")
	ip := extractClientIP(r, trusted)
	// 2001:db8::1 is trusted, 2001:db8:1::100 is in trusted range too
	// All trusted → falls back to leftmost
	if ip != "2001:db8:1::100" {
		t.Errorf("expected leftmost fallback 2001:db8:1::100, got %q", ip)
	}
}

func TestExtractClientIP_IPv6XFFMixed(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "[2001:db8::1]:1234"
	r.Header.Set("X-Forwarded-For", "fe80::1, 2001:db8::1")
	ip := extractClientIP(r, trusted)
	// 2001:db8::1 is trusted, fe80::1 is not → return fe80::1
	if ip != "fe80::1" {
		t.Errorf("expected rightmost non-trusted fe80::1, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access test
// ---------------------------------------------------------------------------

// ipSettingsDisabled returns settings with IP limiting disabled.
func ipSettingsDisabled() *stubIPSettings {
	return &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "false",
		settingsKeyIPMaxWaitMs: "200",
	}}
}

// ipSettingsWithBackpressure returns settings with IP limiting enabled and
// a configurable max wait time for backpressure.
func ipSettingsWithBackpressure(maxWaitMs int) *stubIPSettings {
	return &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPMaxWaitMs: strconv.Itoa(maxWaitMs),
	}}
}

func TestIPLimiter_DisabledViaSettings(t *testing.T) {
	// With rate_limit_ip_enabled=false, all requests should pass through
	// regardless of rate limit state.
	lim := NewIPLimiter(1, 1, nil, ipSettingsDisabled())
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// Exhaust the burst
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Second request would normally get 429, but limiter is disabled
	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "1.2.3.4:1234"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("second request with limiter disabled: expected 200, got %d", rr.Code)
	}
}

func TestIPLimiter_BackpressureWithinMaxWait(t *testing.T) {
	// With RPS=10 and burst=1, second request requires ~100ms wait (1/10 = 0.1s).
	// If max_wait_ms is 500ms, the request should sleep and succeed.
	lim := NewIPLimiter(10, 1, nil, ipSettingsWithBackpressure(500))
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// First request consumes the burst
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "5.5.5.5:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Second request should wait (within maxWait) and succeed
	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "5.5.5.5:1234"
	rr = httptest.NewRecorder()
	start := time.Now()
	handler.ServeHTTP(rr, req)
	elapsed := time.Since(start)

	if rr.Code != http.StatusOK {
		t.Errorf("backpressure request: expected 200, got %d", rr.Code)
	}
	// Should have slept for ~100ms (1/10 RPS = 100ms per token)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected backpressure sleep, but elapsed time was only %v", elapsed)
	}
}

func TestIPLimiter_BackpressureExceedsMaxWait(t *testing.T) {
	// With very low RPS and burst=1, second request requires waiting.
	// If max_wait_ms is very low, request should get 429 immediately.
	lim := NewIPLimiter(0.1, 1, nil, ipSettingsWithBackpressure(10))
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// First request consumes the burst
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "6.6.6.6:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr.Code)
	}

	// Second request should get 429 (wait would exceed maxWait of 10ms)
	req = httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "6.6.6.6:1234"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("backpressure exceeded: expected 429, got %d", rr.Code)
	}
}

func TestIPLimiter_RuntimeSettingsOverrideRPS(t *testing.T) {
	// Constructor defaults: RPS=1, burst=1 (very restrictive).
	// Override via settings: RPS=1000, burst=1000 (effectively unlimited).
	settings := &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPRPS:       "1000",
		settingsKeyIPBurst:     "1000",
		settingsKeyIPMaxWaitMs: "200",
	}}
	lim := NewIPLimiter(1, 1, nil, settings)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// With RPS=1000 and burst=1000, 50 rapid requests should all succeed
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "10.0.0.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200 with settings override, got %d", i, rr.Code)
		}
	}
}

func TestIPLimiter_SettingsFallbackToDefaults(t *testing.T) {
	// When settings reader is nil, constructor defaults should be used.
	// Use RPS=0.1 (1 request per 10s) so backpressure can't help within
	// the 200ms default max_wait (wait would be ~10s, well above 200ms).
	lim := NewIPLimiter(0.1, 2, nil, nil)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// Constructor defaults: RPS=0.1, burst=2
	// Both burst requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "10.0.0.2:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("burst request %d: expected 200, got %d", i, rr.Code)
		}
	}

	// 3rd should be rejected (burst exhausted, RPS=0.1 means 10s wait, max_wait=200ms)
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "10.0.0.2:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("post-burst request: expected 429, got %d", rr.Code)
	}
}

func TestIPLimiter_ConcurrentAccess(t *testing.T) {
	lim := NewIPLimiter(1000, 1000, nil, nil)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/", http.NoBody)
			// Use a few different IPs
			switch idx % 3 {
			case 0:
				req.RemoteAddr = "1.1.1.1:1234"
			case 1:
				req.RemoteAddr = "2.2.2.2:5678"
			case 2:
				req.RemoteAddr = "3.3.3.3:9012"
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			// With 1000 burst, none should be rejected
			if rr.Code != http.StatusOK {
				errors <- fmt.Errorf("goroutine %d: expected 200, got %d", idx, rr.Code)
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from ip_limiter_coverage_test.go
// ---------------------------------------------------------------------------

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
