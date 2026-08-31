package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ratelimit"
)

// probeBurst is the burst the probe limiter is given in these tests: small
// enough to exhaust deterministically, large enough to show the admitted half.
const probeBurst = 3

// probeLimiterFor builds a probe limiter tight enough to observe and stops its
// cleanup goroutine with the test, matching limiterFor in the login suite.
func probeLimiterFor(t *testing.T) *ratelimit.IPLimiter {
	t.Helper()
	lim := ratelimit.NewIPLimiter(0.01, probeBurst, nil, nil)
	t.Cleanup(lim.Stop)
	return lim
}

// get drives one request from a chosen address and returns the recorder.
func get(t *testing.T, srv *Server, method, path, addr, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, http.NoBody)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = addr
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// The probe runs the same two store reads a Traefik config poll does, on an
// unauthenticated route, so line-rate anonymous probing is a way to spend the
// control plane's CPU, and the cost grows with the fleet's member count.
//
// Admitted-then-refused, the shape the login suite uses: asserting only that
// something is refused would pass if the route were broken, and asserting only
// that something is served would pass on a full revert.
func TestHealthz_RidesItsOwnPerIPLimiter(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, probeLimiterFor(t))

	for i := range probeBurst {
		if rec := get(t, srv, http.MethodGet, "/healthz", "203.0.113.10:5000", ""); rec.Code != http.StatusOK {
			t.Fatalf("probe %d of the burst returned %d, want 200: the limiter is tighter than the burst it was given", i+1, rec.Code)
		}
	}
	if rec := get(t, srv, http.MethodGet, "/healthz", "203.0.113.10:5001", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("a probe past the burst returned %d, want %d: the route is not behind a limiter", rec.Code, http.StatusTooManyRequests)
	}
}

// Each address gets its own budget, so one noisy prober cannot deny the probe
// to an orchestrator polling from somewhere else.
func TestHealthz_LimitIsPerAddress(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, probeLimiterFor(t))

	for range probeBurst + 1 {
		get(t, srv, http.MethodGet, "/healthz", "203.0.113.12:5000", "")
	}
	if rec := get(t, srv, http.MethodGet, "/healthz", "203.0.113.12:5001", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the flooding address got %d, want it to have spent its own budget", rec.Code)
	}
	if rec := get(t, srv, http.MethodGet, "/healthz", "203.0.113.13:5000", ""); rec.Code != http.StatusOK {
		t.Errorf("a different address got %d, want its own budget", rec.Code)
	}
}

// The groups carry their limiter and nothing else does. Without this, an
// over-scoping regression would be invisible: every other route in the test
// server has a budget no test can reach, so they would keep answering 200
// whether or not a limiter had been wrapped around them.
func TestProbeLimiters_DoNotCoverTheOtherRoutes(t *testing.T) {
	srv, _ := newTestServerCfgTraefikLimited(t, probeLimiterFor(t), probeLimiterFor(t), "")

	const addr = "203.0.113.30:5000"
	for range probeBurst + 3 {
		get(t, srv, http.MethodGet, "/healthz", addr, "")
		get(t, srv, http.MethodGet, "/traefik/config", addr, "")
	}
	// Both budgets for this address are now spent.
	if rec := get(t, srv, http.MethodGet, "/healthz", addr, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the probe budget was not spent (%d): this test proves nothing", rec.Code)
	}
	for _, path := range []string{"/api/totp/status", "/api/auth/oidc/status"} {
		if rec := get(t, srv, http.MethodGet, path, addr, ""); rec.Code == http.StatusTooManyRequests {
			t.Errorf("%s was refused: a probe limiter is covering more than its own route", path)
		}
	}
}

// The probe's budget is deliberately NOT the login limiter's. Sharing one
// would turn an anonymous probe flood into a pairing and login outage for
// every client behind the same address — and FRONTDESK_TRUSTED_PROXIES ships
// empty, so under the documented nginx topology that is every operator at
// once.
//
// Both halves are asserted: the flood really is refused (so the test cannot
// pass on a revert that leaves the probe unlimited), and the login route still
// answers its own status for the same address afterwards, with its own limiter
// still live (a 429 arrives once ITS burst is spent, so "not 429" is not being
// satisfied by a dead route).
func TestHealthz_FloodDoesNotSpendTheLoginBudget(t *testing.T) {
	login := limiterFor(t)
	srv, _ := newTestServerCfgFull(t, nil, login, probeLimiterFor(t), "")

	refused := 0
	for range probeBurst + 5 {
		if get(t, srv, http.MethodGet, "/healthz", "203.0.113.14:5000", "").Code == http.StatusTooManyRequests {
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("the probe flood was never refused: the probe limiter is not in play, so this proves nothing about the login budget")
	}

	// The same address must still reach the pairing exchange and get its own
	// verdict on the payload rather than a rate-limit refusal.
	rec := get(t, srv, http.MethodPost, "/api/pair", "203.0.113.14:5001", `{"code":"nope"}`)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("a /healthz flood exhausted the login limiter's budget for the same address")
	}
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnauthorized {
		t.Errorf("pairing returned %d, want the route's own rejection of a bad code (400 or 401)", rec.Code)
	}

	// And the login limiter is genuinely live, so the assertion above is not
	// being satisfied by a route that can never be refused.
	for range loginBurst + 1 {
		rec = get(t, srv, http.MethodPost, "/api/pair", "203.0.113.14:5002", `{"code":"nope"}`)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("the login limiter never refused past its burst (%d): it is not bounding this route at all", rec.Code)
	}
}

// The ungated Traefik poll is the costlier sibling of the probe: the same two
// store reads plus BuildTraefikConfig and a member-sized marshal, and its body
// discloses member URLs and settings. It carries its own budget, so a flood of
// one endpoint cannot starve the other.
func TestTraefikConfig_UngatedRidesItsOwnLimiter(t *testing.T) {
	traefik := probeLimiterFor(t)
	srv, _ := newTestServerCfgTraefikLimited(t, probeLimiterFor(t), traefik, "")

	for i := range probeBurst {
		if rec := get(t, srv, http.MethodGet, "/traefik/config", "203.0.113.20:5000", ""); rec.Code != http.StatusOK {
			t.Fatalf("poll %d of the burst returned %d, want 200", i+1, rec.Code)
		}
	}
	if rec := get(t, srv, http.MethodGet, "/traefik/config", "203.0.113.20:5001", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("an ungated poll past the burst returned %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	// The probe keeps its own budget: Traefik's poll is the data plane's
	// lifeline and the two must not share a bucket.
	if rec := get(t, srv, http.MethodGet, "/healthz", "203.0.113.20:5002", ""); rec.Code != http.StatusOK {
		t.Errorf("a /traefik/config flood spent the probe's budget for the same address: got %d", rec.Code)
	}
}

// Once the token gates it, an unauthenticated caller is refused before any of
// the work happens, so the limiter is skipped and Traefik's own polling is
// never throttled by someone else's rejected traffic.
func TestTraefikConfig_GatedSkipsTheLimiter(t *testing.T) {
	srv, _ := newTestServerCfgTraefikLimited(t, probeLimiterFor(t), probeLimiterFor(t), "traefik-token")

	for range probeBurst + 5 {
		if rec := get(t, srv, http.MethodGet, "/traefik/config", "203.0.113.21:5000", ""); rec.Code == http.StatusTooManyRequests {
			t.Fatal("a gated poll was rate-limited: the cheap auth refusal should run instead")
		}
	}
	// And the real Traefik still gets through after all of that.
	req := httptest.NewRequest(http.MethodGet, "/traefik/config", http.NoBody)
	req.RemoteAddr = "203.0.113.21:5001"
	req.Header.Set("Authorization", "Bearer traefik-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("the token-carrying poll returned %d, want 200", rec.Code)
	}
}

// A nil limiter leaves the probe unlimited, which is what a server built
// without one gets. The route must still be mounted and answer.
func TestHealthz_NilLimiterLeavesTheRouteWorking(t *testing.T) {
	srv, _ := newTestServerHealthzLimited(t, nil)
	for range probeBurst + 10 {
		if rec := get(t, srv, http.MethodGet, "/healthz", "203.0.113.15:5000", ""); rec.Code != http.StatusOK {
			t.Fatalf("probe got %d with no limiter configured, want 200", rec.Code)
		}
	}
}
