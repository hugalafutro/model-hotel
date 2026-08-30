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
		// The control: this one was already limited, so a failure here means
		// the limiter itself stopped working rather than the scope regressing.
		{"pair", http.MethodPost, "/api/pair", `{"code":"nope"}`},
	}

	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			srv, _ := newTestServerLimited(t, nil, ratelimit.NewIPLimiter(1, loginBurst, nil, nil))

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

// TestLoginScreenPolls_AreNotRateLimited is the other half of the scope: the
// login screen polls these two to decide which buttons to render, and they do
// no work an attacker can amplify. Putting the whole /api tree behind a 5 rps
// limiter would throttle a dashboard cold-load, so the fix is deliberately
// scoped to the ceremonies rather than the tree.
func TestLoginScreenPolls_AreNotRateLimited(t *testing.T) {
	for _, path := range []string{"/api/totp/status", "/api/auth/oidc/status"} {
		t.Run(path, func(t *testing.T) {
			srv, _ := newTestServerLimited(t, nil, ratelimit.NewIPLimiter(1, loginBurst, nil, nil))

			for i := range loginBurst + 3 {
				if rec := do(t, srv, http.MethodGet, path, "", false); rec.Code == http.StatusTooManyRequests {
					t.Fatalf("GET %s was rate limited on request %d: the login screen's poll must not share the ceremonies' budget",
						path, i+1)
				}
			}
		})
	}
}
