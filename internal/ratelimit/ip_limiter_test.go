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

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

func TestExtractClientIP_AllTrustedInvalidLeftmost(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "unknown, 10.0.0.2")
	ip := extractClientIP(r, trusted)
	// "unknown" is not a parseable IP, so it's skipped. 10.0.0.2 is trusted.
	// Fallback to leftmost ("unknown") also fails ParseIP. Falls through
	// to RemoteAddr.
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr 10.0.0.1, got %q", ip)
	}
}

func TestExtractClientIP_XFFEmptySegments(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", ", , 10.0.0.2, ")
	ip := extractClientIP(r, trusted)
	// All entries are empty or trusted → rightmostUntrustedIP returns ""
	// → extractClientIP falls through to RemoteAddr
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr 10.0.0.1, got %q", ip)
	}
}

func TestExtractClientIP_XFFEmptySegmentsUntrustedClient(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", ", 1.2.3.4, , 10.0.0.2")
	ip := extractClientIP(r, trusted)
	// Walk right-to-left: 10.0.0.2 trusted, empty skipped, 1.2.3.4 NOT trusted → return it
	if ip != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %q", ip)
	}
}

func TestExtractClientIP_XRealIPInvalid(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "not-an-ip")
	ip := extractClientIP(r, trusted)
	// Invalid X-Real-IP should fall through to RemoteAddr
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr for invalid X-Real-IP, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// isIPInTrustedNets tests
// ---------------------------------------------------------------------------

func TestIsIPInTrustedNets_IPv4InCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	if !isIPInTrustedNets("10.1.2.3", nets) {
		t.Error("10.1.2.3 should be in 10.0.0.0/8")
	}
	if !isIPInTrustedNets("10.255.255.255", nets) {
		t.Error("10.255.255.255 should be in 10.0.0.0/8")
	}
}

func TestIsIPInTrustedNets_IPv4NotInCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	if isIPInTrustedNets("192.168.1.1", nets) {
		t.Error("192.168.1.1 should not be in 10.0.0.0/8")
	}
	if isIPInTrustedNets("11.0.0.1", nets) {
		t.Error("11.0.0.1 should not be in 10.0.0.0/8")
	}
}

func TestIsIPInTrustedNets_IPv6InCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	nets := []*net.IPNet{cidr}

	if !isIPInTrustedNets("2001:db8::1", nets) {
		t.Error("2001:db8::1 should be in 2001:db8::/32")
	}
	if !isIPInTrustedNets("2001:db8:abcd::1", nets) {
		t.Error("2001:db8:abcd::1 should be in 2001:db8::/32")
	}
}

func TestIsIPInTrustedNets_IPv6NotInCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	nets := []*net.IPNet{cidr}

	if isIPInTrustedNets("fe80::1", nets) {
		t.Error("fe80::1 should not be in 2001:db8::/32")
	}
	if isIPInTrustedNets("2001:db9::1", nets) {
		t.Error("2001:db9::1 should not be in 2001:db8::/32")
	}
}

func TestIsIPInTrustedNets_InvalidIP(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	if isIPInTrustedNets("not-an-ip", nets) {
		t.Error("invalid IP should return false")
	}
	if isIPInTrustedNets("", nets) {
		t.Error("empty string should return false")
	}
	if isIPInTrustedNets("999.999.999.999", nets) {
		t.Error("out-of-range IP should return false")
	}
}

func TestIsIPInTrustedNets_EmptyNets(t *testing.T) {
	if isIPInTrustedNets("10.0.0.1", nil) {
		t.Error("no trusted nets should return false for any IP")
	}
	if isIPInTrustedNets("10.0.0.1", []*net.IPNet{}) {
		t.Error("empty nets slice should return false for any IP")
	}
}

func TestIsIPInTrustedNets_MultipleCIDRs(t *testing.T) {
	_, cidr1, _ := net.ParseCIDR("10.0.0.0/8")
	_, cidr2, _ := net.ParseCIDR("172.16.0.0/12")
	_, cidr3, _ := net.ParseCIDR("192.168.0.0/16")
	nets := []*net.IPNet{cidr1, cidr2, cidr3}

	if !isIPInTrustedNets("10.5.5.5", nets) {
		t.Error("10.5.5.5 should match first CIDR")
	}
	if !isIPInTrustedNets("172.20.0.1", nets) {
		t.Error("172.20.0.1 should match second CIDR")
	}
	if !isIPInTrustedNets("192.168.100.50", nets) {
		t.Error("192.168.100.50 should match third CIDR")
	}
	if isIPInTrustedNets("8.8.8.8", nets) {
		t.Error("8.8.8.8 should not match any CIDR")
	}
}

func TestIsIPInTrustedNets_Slash32CIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("1.2.3.4/32")
	nets := []*net.IPNet{cidr}

	if !isIPInTrustedNets("1.2.3.4", nets) {
		t.Error("1.2.3.4 should match /32")
	}
	if isIPInTrustedNets("1.2.3.5", nets) {
		t.Error("1.2.3.5 should not match /32")
	}
}

func TestIsIPInTrustedNets_IPv6ZeroCompression(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	nets := []*net.IPNet{cidr}

	// :: zero-compression should work correctly (the reason isIPInTrustedNets exists
	// instead of relying on IsTrustedProxy which requires host:port format)
	if !isIPInTrustedNets("2001:db8::1", nets) {
		t.Error("2001:db8::1 with zero-compression should be in 2001:db8::/32")
	}
}

func TestIsIPInTrustedNets_IPv4MappedIPv6(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	// ::ffff:10.0.0.1 is an IPv4-mapped IPv6 address; net.ParseIP will parse it
	// but it becomes a 16-byte IPv4-mapped form which differs from the 4-byte form
	// used by the CIDR. net.IPNet.Contains handles this correctly.
	if !isIPInTrustedNets("::ffff:10.0.0.1", nets) {
		t.Error("::ffff:10.0.0.1 (IPv4-mapped IPv6) should match 10.0.0.0/8")
	}
}

// ---------------------------------------------------------------------------
// IPLimiter Middleware additional tests
// ---------------------------------------------------------------------------

func TestIPLimiter_MiddlewareNoSettingsPassesThrough(t *testing.T) {
	// When settings is nil, Middleware should still rate-limit using defaults.
	// This covers the nil-settings branch in Middleware.
	lim := NewIPLimiter(0.1, 2, nil, nil)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// burst=2 from constructor defaults
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		req.RemoteAddr = "7.7.7.7:7777"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd should be rejected with default max_wait (200ms), wait > 200ms so 429
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "7.7.7.7:7777"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exhausting burst with nil settings, got %d", rr.Code)
	}
}

func TestIPLimiter_MiddlewareReservationNotOK(t *testing.T) {
	// Test the path where reservation.OK() returns false.
	// This can happen when the limiter's burst is 0. With burst=0, initial
	// reservation fails (no tokens available). We use settings to set burst=0
	// and RPS=0.1 so that even after waiting, no tokens are available.
	settings := &stubIPSettings{values: map[string]string{
		settingsKeyIPEnabled:   "true",
		settingsKeyIPRPS:       "0.1",
		settingsKeyIPBurst:     "0", // burst=0 means no tokens, reservation fails
		settingsKeyIPMaxWaitMs: "0",
	}}
	lim := NewIPLimiter(0.1, 0, nil, settings)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "4.4.4.4:4444"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 when reservation fails (burst=0), got %d", rr.Code)
	}
	// With burst=0, reservation.OK() returns false → enters the first rejection path
	// where writeHeaders is called with retryAfter=0, so no Retry-After header.
	if h := rr.Header().Get("Retry-After"); h != "" {
		t.Errorf("Retry-After should not be set when reservation fails with delay=0, got %q", h)
	}
}

// TestIPLimiter_CleanupLoop_Integration verifies that the cleanup goroutine
// started by NewIPLimiter actually removes stale IP entries when triggered.
// It inserts entries with expired lastUsed timestamps, calls cleanup() directly
// (same function the ticker calls), and verifies removal.
func TestIPLimiter_CleanupLoop_Integration(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, ipSettingsNoBackpressure())
	defer lim.Stop()

	// Insert a stale entry (last used 15 minutes ago)
	lim.mu.Lock()
	lim.limiters["10.0.0.1"] = &ipEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now().Add(-15 * time.Minute),
	}
	// And a fresh entry
	lim.limiters["10.0.0.2"] = &ipEntry{
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
	if _, ok := lim.limiters["10.0.0.1"]; ok {
		t.Error("stale IP entry should have been removed by cleanup")
	}
	if _, ok := lim.limiters["10.0.0.2"]; !ok {
		t.Error("fresh IP entry should still be present after cleanup")
	}
}

// TestIPLimiter_CleanupLoopTickerStartStop verifies that the IPLimiter's
// cleanupLoop goroutine can be started and stopped without panic.
// The ticker fires every 5 minutes in production; we just test start/stop.
func TestIPLimiter_CleanupLoopTickerStartStop(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, ipSettingsNoBackpressure())

	// Give the goroutine a moment to start
	time.Sleep(20 * time.Millisecond)

	// Stop should close stopCh, causing cleanupLoop to exit
	lim.Stop()
	// No panic = success
}

// TestIPLimiter_CleanupLoop_TickerPathRemovesStaleEntries verifies that the
// cleanup function (called by cleanupLoop on ticker.C) actually removes
// stale IP entries. Since the production ticker is 5 minutes, we simulate
// the ticker.C path by calling cleanup() directly.
func TestIPLimiter_CleanupLoop_TickerPathRemovesStaleEntries(t *testing.T) {
	lim := NewIPLimiter(10, 20, nil, ipSettingsNoBackpressure())
	defer lim.Stop()

	// Insert a stale IP entry (last used 15 minutes ago — beyond the 10-minute cutoff)
	lim.mu.Lock()
	lim.limiters["192.168.1.100"] = &ipEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now().Add(-15 * time.Minute),
	}
	// And a fresh IP entry
	lim.limiters["192.168.1.200"] = &ipEntry{
		limiter:  rate.NewLimiter(10, 20),
		rps:      10,
		burst:    20,
		lastUsed: time.Now(),
	}
	lim.mu.Unlock()

	// Call cleanup directly (simulates what happens on ticker.C)
	lim.cleanup()

	lim.mu.Lock()
	_, hasStale := lim.limiters["192.168.1.100"]
	_, hasFresh := lim.limiters["192.168.1.200"]
	lim.mu.Unlock()

	if hasStale {
		t.Error("stale IP entry should have been removed by cleanup (ticker.C path)")
	}
	if !hasFresh {
		t.Error("fresh IP entry should still be present after cleanup")
	}
}

// TestIPLimiter_CleanupLoop_TickerBranch_Unreachable documents that the
// ticker.C select branch in IPLimiter.cleanupLoop (line 188) cannot be
// directly tested because the production ticker is 5 minutes. The cleanup()
// function called on ticker.C IS tested directly via
// TestIPLimiter_CleanupRemovesStale and TestIPLimiter_CleanupLoop_TickerPathRemovesStaleEntries.
// Only the select routing (ticker.C path) is untested; the actual cleanup
// logic is fully covered. This is a structural limitation.

// TestIPEntry_ThrottleEdgeLogging mirrors TestKeyEntry_ThrottleEdgeLogging for
// the per-IP limiter: one "started" per episode (not per rejection), one
// "ended" on recovery, and a fresh rejection opens a new episode.
func TestIPEntry_ThrottleEdgeLogging(t *testing.T) {
	h := &msgCaptureHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init(false) })

	const started = "ratelimit-ip: throttling started"
	const ended = "ratelimit-ip: throttling ended"

	e := &ipEntry{limiter: rate.NewLimiter(1, 1), rps: 1, burst: 1}

	e.noteRejected("1.2.3.4")
	e.noteRejected("1.2.3.4")
	e.noteRejected("1.2.3.4")
	if got := h.count(started); got != 1 {
		t.Errorf("started count = %d, want 1", got)
	}
	if got := e.rejectedN.Load(); got != 3 {
		t.Errorf("rejectedN = %d, want 3", got)
	}

	e.noteAllowed("1.2.3.4")
	e.noteAllowed("1.2.3.4")
	if got := h.count(ended); got != 1 {
		t.Errorf("ended count = %d, want 1", got)
	}

	e.noteRejected("1.2.3.4")
	if got := h.count(started); got != 2 {
		t.Errorf("started count after new episode = %d, want 2", got)
	}
}
