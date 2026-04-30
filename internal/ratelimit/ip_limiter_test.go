package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// ---------------------------------------------------------------------------
// IPLimiter tests
// ---------------------------------------------------------------------------

func TestIPLimiter_AllowsWithinBurst(t *testing.T) {
	lim := NewIPLimiter(10, 5)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}
}

func TestIPLimiter_BlocksBeyondBurst(t *testing.T) {
	lim := NewIPLimiter(10, 3)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.RemoteAddr = "5.6.7.8:5678"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.RemoteAddr = "5.6.7.8:5678"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst exhausted, got %d", rr.Code)
	}
}

func TestIPLimiter_PerIPIsolation(t *testing.T) {
	lim := NewIPLimiter(10, 2)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	// Exhaust IP-A
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.RemoteAddr = "10.0.0.1:1000"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.RemoteAddr = "10.0.0.1:1000"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("IP-A: expected 429, got %d", rr.Code)
	}

	// IP-B should still succeed
	req = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.RemoteAddr = "10.0.0.2:2000"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("IP-B: expected 200, got %d", rr.Code)
	}
}

func TestIPLimiter_HeadersOnSuccess(t *testing.T) {
	lim := NewIPLimiter(10, 20)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	lim := NewIPLimiter(1, 1)
	defer lim.Stop()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := lim.Middleware(next)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.RemoteAddr = "9.8.7.6:4321"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	req = httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	lim := NewIPLimiter(0, 0)
	defer lim.Stop()
	if lim.rps != defaultIPRPS {
		t.Errorf("rps = %v, want %v", lim.rps, defaultIPRPS)
	}
	if lim.burst != defaultIPBurst {
		t.Errorf("burst = %d, want %d", lim.burst, defaultIPBurst)
	}
}

func TestIPLimiter_CleanupRemovesStale(t *testing.T) {
	lim := NewIPLimiter(10, 20)
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
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.168.1.1:54321"
	ip := extractClientIP(r)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %q", ip)
	}
}

func TestExtractClientIP_RemoteAddrNoPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.168.1.1"
	ip := extractClientIP(r)
	// Should return the original string when SplitHostPort fails
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %q", ip)
	}
}

func TestExtractClientIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "8.8.8.8")
	ip := extractClientIP(r)
	if ip != "8.8.8.8" {
		t.Errorf("expected 8.8.8.8 from X-Real-IP, got %q", ip)
	}
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 3.3.3.3")
	ip := extractClientIP(r)
	if ip != "1.1.1.1" {
		t.Errorf("expected first IP from X-Forwarded-For, got %q", ip)
	}
}

func TestExtractClientIP_XForwardedForPriority(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "4.4.4.4")
	r.Header.Set("X-Real-IP", "5.5.5.5")
	ip := extractClientIP(r)
	// X-Forwarded-For takes priority over X-Real-IP
	if ip != "4.4.4.4" {
		t.Errorf("X-Forwarded-For should take priority, got %q", ip)
	}
}

func TestExtractClientIP_IPv6(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "[::1]:12345"
	ip := extractClientIP(r)
	if ip != "::1" {
		t.Errorf("expected ::1 for IPv6, got %q", ip)
	}
}

func TestExtractClientIP_EmptyXFF(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "")
	r.Header.Set("X-Real-IP", "9.9.9.9")
	ip := extractClientIP(r)
	// Empty XFF should fall through to X-Real-IP
	if ip != "9.9.9.9" {
		t.Errorf("expected fallback to X-Real-IP, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access test
// ---------------------------------------------------------------------------

func TestIPLimiter_ConcurrentAccess(t *testing.T) {
	lim := NewIPLimiter(1000, 1000)
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
			req := httptest.NewRequest("POST", "/", nil)
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
