package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ratelimit"
)

// probe drives GET /healthz from one address and returns the status.
func probe(t *testing.T, srv *Server, addr string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	req.RemoteAddr = addr
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code
}

// The probe runs the same two store reads a Traefik config poll does, on an
// unauthenticated route, so line-rate anonymous probing is a way to spend the
// control plane's CPU — and the cost grows with the fleet's member count.
func TestHealthz_IsRateLimited(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, ratelimit.NewIPLimiter(0.001, 2, nil, nil))

	served, throttled := 0, 0
	for range 10 {
		if probe(t, srv, "203.0.113.10:5000") == http.StatusOK {
			served++
		} else {
			throttled++
		}
	}
	if throttled == 0 {
		t.Errorf("10 probes from one address were all served: the route is unlimited")
	}
	if served == 0 {
		t.Errorf("no probe was served at all: the limiter is not letting the burst through")
	}
}

// The container HEALTHCHECK polls every 30 seconds, so the real cadence has to
// sit far inside the budget. A burst-sized run of probes must all succeed.
func TestHealthz_ContainerCadenceIsUnaffected(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, ratelimit.NewIPLimiter(2, 5, nil, nil))
	for i := range 5 {
		if code := probe(t, srv, "203.0.113.11:5000"); code != http.StatusOK {
			t.Fatalf("probe %d of a burst of 5 got %d, want 200", i+1, code)
		}
	}
}

// Each address gets its own budget, so one noisy prober cannot deny the probe
// to an orchestrator polling from somewhere else.
func TestHealthz_LimitIsPerAddress(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, ratelimit.NewIPLimiter(0.001, 1, nil, nil))
	if code := probe(t, srv, "203.0.113.12:5000"); code != http.StatusOK {
		t.Fatalf("first address got %d, want 200", code)
	}
	if code := probe(t, srv, "203.0.113.12:5001"); code == http.StatusOK {
		t.Fatal("the same address should have exhausted its own budget")
	}
	if code := probe(t, srv, "203.0.113.13:5000"); code != http.StatusOK {
		t.Errorf("a different address got %d, want its own budget", code)
	}
}

// The probe's budget is deliberately NOT the login limiter's. Sharing one
// would turn an anonymous probe flood into a pairing and login outage for
// every client behind the same address, which is a worse failure than the one
// the limit exists to prevent.
func TestHealthz_FloodDoesNotSpendTheLoginBudget(t *testing.T) {
	login := ratelimit.NewIPLimiter(0.001, 2, nil, nil)
	srv, _ := newTestServerCfgFull(t, nil, login, ratelimit.NewIPLimiter(0.001, 2, nil, nil), "")

	// Exhaust the probe's budget from one address.
	for range 10 {
		probe(t, srv, "203.0.113.14:5000")
	}

	// The login-limited surface must still answer that same address. A 429
	// here would mean the two budgets are the same bucket; any other status
	// (400/401 for a malformed pairing attempt) means the limiter let it
	// through, which is what this asserts.
	req := httptest.NewRequest(http.MethodPost, "/api/pair", http.NoBody)
	req.RemoteAddr = "203.0.113.14:5001"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Error("a /healthz flood exhausted the login limiter's budget for the same address")
	}
}

// A nil limiter leaves the probe unlimited, which is what a server built
// without one gets. The route must still be mounted and answer.
func TestHealthz_NilLimiterLeavesTheRouteWorking(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, nil)
	for range 20 {
		if code := probe(t, srv, "203.0.113.15:5000"); code != http.StatusOK {
			t.Fatalf("probe got %d with no limiter configured, want 200", code)
		}
	}
}
