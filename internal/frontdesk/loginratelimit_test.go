package frontdesk

import (
	"net/http"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ratelimit"
)

// loginBurst is the burst the tests below run against. Two is the smallest
// value that still tells "the limiter covers this route" apart from "the route
// is broken": the first requests must be ADMITTED and only the one past the
// burst refused.
const loginBurst = 2

// loginRPS refills the bucket once every 100 seconds, so a burst spent inside a
// test cannot refill mid-test. At 1 rps the middleware's graceful backpressure
// sleeps and serves whenever ~800ms passes between the burst and the probe,
// which would make these tests depend on how long the routes' own DB work took.
const loginRPS = 0.01

// limiterFor builds a per-IP limiter tight enough to observe and stops its
// cleanup goroutine with the test.
func limiterFor(t *testing.T) *ratelimit.IPLimiter {
	t.Helper()
	lim := ratelimit.NewIPLimiter(loginRPS, loginBurst, nil, nil)
	t.Cleanup(lim.Stop)
	return lim
}

// TestPublicLoginRoutes_RideThePerIPLimiter pins the property the Front Desk
// route table claims: every unauthenticated login front-end is behind the
// per-IP request limiter.
//
// TOTP login and the OIDC redirect endpoints were reachable at unlimited rate
// while /api/pair next to them was limited, which left the TOTP second factor
// without a rate bound (its own throttle counts FAILURES, so it does not bound
// the rate of the attempts before it engages) and let an unauthenticated caller
// drive one webauthn_sessions INSERT per OIDC start with only an hourly sweep
// to reclaim them.
//
// Each route gets its own server so it starts on a full bucket, which is what
// makes an admitted-then-refused sequence evidence about THAT route rather than
// about a bucket some earlier route drained.
func TestPublicLoginRoutes_RideThePerIPLimiter(t *testing.T) {
	routes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"totp login", http.MethodPost, "/api/totp/login", `{"code":"000000"}`},
		{"oidc start", http.MethodGet, "/api/auth/oidc/start", ""},
		{"oidc callback", http.MethodGet, "/api/auth/oidc/callback", ""},
		// Public like the two status polls, but it runs an uncached credential
		// listing per request, so it is limited rather than exempt.
		{"webauthn available", http.MethodGet, "/api/webauthn/available", ""},
		// The control: this one was already limited, so a failure here means
		// the limiter itself stopped working rather than the scope regressing.
		{"pair", http.MethodPost, "/api/pair", `{"code":"nope"}`},
	}

	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			srv, _ := newTestServerLimited(t, nil, limiterFor(t))

			for i := range loginBurst {
				if rec := do(t, srv, rt.method, rt.path, rt.body, false); rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d of the burst was refused: the limiter is tighter than the burst it was given", i+1)
				}
			}
			if rec := do(t, srv, rt.method, rt.path, rt.body, false); rec.Code != http.StatusTooManyRequests {
				t.Errorf("%s %s past the burst returned %d, want %d: the route is not behind the per-IP limiter",
					rt.method, rt.path, rec.Code, http.StatusTooManyRequests)
			}
		})
	}
}

// TestLoginScreenPolls_AreNotRateLimited is the other half of the scope: these
// two polls are served from a TTL cache and write nothing, so they stay outside
// the limiter. Putting the whole /api tree behind Front Desk's 5 rps limiter
// would throttle a dashboard cold load, so the fix is scoped to the ceremonies
// rather than the tree.
//
// The 200 assertion is load-bearing: without it, deleting the route entirely
// would 404 and satisfy a "never 429" check.
func TestLoginScreenPolls_AreNotRateLimited(t *testing.T) {
	for _, path := range []string{"/api/totp/status", "/api/auth/oidc/status"} {
		t.Run(path, func(t *testing.T) {
			srv, _ := newTestServerLimited(t, nil, limiterFor(t))

			for i := range loginBurst + 3 {
				if rec := do(t, srv, http.MethodGet, path, "", false); rec.Code != http.StatusOK {
					t.Fatalf("GET %s returned %d on request %d, want %d: the login screen's poll must stay served and outside the ceremonies' budget",
						path, rec.Code, i+1, http.StatusOK)
				}
			}
		})
	}
}
