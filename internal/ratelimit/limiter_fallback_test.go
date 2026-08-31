package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// The per-key stage runs on surfaces that reach it without a virtual key: the
// admin chat routes mount it without ProxyKeyMiddleware. Its fallback must be
// keyed on the client ADDRESS, so one client is one bucket however many TCP
// connections it opens. Keyed on r.RemoteAddr it was keyed on the connection,
// and a client that does not reuse connections drew a fresh full-burst bucket
// every request. The per-IP limiter still bounded that traffic at its own
// looser budget, so what the bug cost was the tighter per-key stage, not the
// surface.
func TestExtractKey_FallbackIsAddressKeyedNotConnectionKeyed(t *testing.T) {
	seen := map[string]bool{}
	for i := range 10 {
		r := httptest.NewRequest(http.MethodPost, "/api/chat/completions", http.NoBody)
		r.RemoteAddr = fmt.Sprintf("203.0.113.7:%d", 40000+i)
		seen[extractKey(r)] = true
	}
	if len(seen) != 1 {
		t.Errorf("ten connections from one address resolved to %d buckets, want 1: %v", len(seen), seen)
	}
	for k := range seen {
		if k != "203.0.113.7" {
			t.Errorf("bucket key = %q, want the port-stripped address", k)
		}
	}
}

// Distinct addresses must still be distinct buckets, or the fix would collapse
// every anonymous caller into one.
func TestExtractKey_DistinctAddressesStayDistinct(t *testing.T) {
	keys := map[string]bool{}
	for _, addr := range []string{"203.0.113.7:1111", "203.0.113.8:1111", "[2001:db8::1]:1111"} {
		r := httptest.NewRequest(http.MethodPost, "/api/chat/completions", http.NoBody)
		r.RemoteAddr = addr
		keys[extractKey(r)] = true
	}
	if len(keys) != 3 {
		t.Errorf("three addresses resolved to %d buckets, want 3: %v", len(keys), keys)
	}
}

// The virtual-key hash still wins when it is present, which is the /v1 shape
// and the reason that surface is unaffected either way.
func TestExtractKey_VirtualKeyHashTakesPrecedence(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	r.RemoteAddr = "203.0.113.7:40000"
	r = r.WithContext(context.WithValue(r.Context(), ctxkeys.VirtualKeyHashKey, "vk-hash"))
	if got := extractKey(r); got != "vk-hash" {
		t.Errorf("extractKey = %q, want the virtual-key hash", got)
	}
}

// The behavioural half: a keyless client opening one connection per request is
// bound by the configured budget exactly as a keep-alive client is.
func TestMiddleware_KeylessFreshConnectionsAreBounded(t *testing.T) {
	admitted := func(t *testing.T, samePort bool) int {
		t.Helper()
		lim, repo := newTestLimiter()
		defer lim.Stop()
		repo.set(settingsKeyRPS, "0.001")
		repo.set(settingsKeyBurst, "2")

		served := 0
		handler := lim.Middleware(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			served++
			w.WriteHeader(http.StatusOK)
		}))
		for i := range 10 {
			r := httptest.NewRequest(http.MethodPost, "/api/chat/completions", http.NoBody)
			port := 40000 + i
			if samePort {
				port = 40000
			}
			r.RemoteAddr = fmt.Sprintf("203.0.113.9:%d", port)
			handler.ServeHTTP(httptest.NewRecorder(), r)
		}
		return served
	}

	fresh := admitted(t, false)
	keepAlive := admitted(t, true)
	if fresh > 2 {
		t.Errorf("connection-per-request client admitted %d/10 against a burst of 2", fresh)
	}
	if fresh != keepAlive {
		t.Errorf("fresh connections admitted %d, keep-alive admitted %d: the budget must not depend on connection reuse", fresh, keepAlive)
	}
}
