package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakePinger is a controllable healthPinger for tests.
type fakePinger struct {
	err   error
	calls int
}

func (f *fakePinger) Ping(_ context.Context) error {
	f.calls++
	return f.err
}

// serveHealth runs one request through the handler and returns the recorder.
func serveHealth(h *HealthHandler) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", http.NoBody))
	return rr
}

func TestHealthHandler_HealthyReturns200OK(t *testing.T) {
	h := NewHealthHandler(&fakePinger{})

	rr := serveHealth(h)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Body.String(); got != "OK" {
		t.Fatalf("body = %q, want %q", got, "OK")
	}
}

func TestHealthHandler_UnreachableReturns503Degraded(t *testing.T) {
	h := NewHealthHandler(&fakePinger{err: errors.New("connection refused")})

	rr := serveHealth(h)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if got := rr.Body.String(); got != "DEGRADED" {
		t.Fatalf("body = %q, want %q", got, "DEGRADED")
	}
}

func TestHealthHandler_CachesWithinTTL(t *testing.T) {
	p := &fakePinger{}
	h := NewHealthHandler(p)
	now := time.Unix(1000, 0)
	h.now = func() time.Time { return now }

	serveHealth(h) // first call probes
	serveHealth(h) // within TTL: served from cache
	serveHealth(h)

	if p.calls != 1 {
		t.Fatalf("pinger calls = %d, want 1 (cached within TTL)", p.calls)
	}

	// Advance past the cache TTL: the next call must re-probe.
	now = now.Add(h.cacheTTL + time.Millisecond)
	serveHealth(h)

	if p.calls != 2 {
		t.Fatalf("pinger calls = %d, want 2 (re-probe after TTL)", p.calls)
	}
}

func TestHealthHandler_RefreshReflectsStateChangeAfterTTL(t *testing.T) {
	p := &fakePinger{err: errors.New("down")}
	h := NewHealthHandler(p)
	now := time.Unix(1000, 0)
	h.now = func() time.Time { return now }

	if rr := serveHealth(h); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("initial status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	// Database recovers, but within the TTL the cached degraded result stands.
	p.err = nil
	if rr := serveHealth(h); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("cached status = %d, want %d (still cached degraded)", rr.Code, http.StatusServiceUnavailable)
	}

	// After the TTL the refreshed probe reports healthy.
	now = now.Add(h.cacheTTL + time.Millisecond)
	if rr := serveHealth(h); rr.Code != http.StatusOK {
		t.Fatalf("refreshed status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHealthHandler_PingReceivesBoundedTimeout(t *testing.T) {
	var gotDeadline bool
	h := NewHealthHandler(pingFunc(func(ctx context.Context) error {
		_, gotDeadline = ctx.Deadline()
		return nil
	}))

	serveHealth(h)

	if !gotDeadline {
		t.Fatal("ping context had no deadline; want bounded timeout")
	}
}

// pingFunc adapts a function to healthPinger.
type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }
