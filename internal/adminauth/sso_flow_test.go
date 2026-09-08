package adminauth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// ssoTestState is the smallest ssoLoginState a shared-flow test needs: the
// state token consumeState compares against the callback query.
type ssoTestState struct {
	State string `json:"state"`
}

func (s *ssoTestState) stateToken() string { return s.State }

func newSSOFlowFixture(t *testing.T, name, fragmentKey, peerLabel string, cookieAuth bool) ssoLogin {
	t.Helper()
	return ssoLogin{
		name:             name,
		cookieName:       name + "_login",
		cookiePath:       "/api/auth/" + name,
		ttl:              time.Minute,
		sessionMgr:       webauthn.NewSessionManager(newMemStore()),
		ipLimiter:        mockIPLimiter{},
		throttle:         newSSOThrottle(),
		jar:              authcookie.Dashboard,
		cookieSecure:     "never",
		useCookieAuth:    cookieAuth,
		tokenFragmentKey: fragmentKey,
		peerLabel:        peerLabel,
	}
}

// TestFinishLogin_FragmentKeyDefaultsToFlowName covers a header-bearer flow
// whose constructor left tokenFragmentKey unset: the token must land in a named
// fragment slot rather than in a bare "#=<token>" the SPA cannot read.
func TestFinishLogin_FragmentKeyDefaultsToFlowName(t *testing.T) {
	s := newSSOFlowFixture(t, "github", "", "provider", false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback", http.NoBody)
	s.finishLogin(rr, req, "1.2.3.4", []byte("handle"))

	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/#github_token=") {
		t.Fatalf("Location = %q, want a /#github_token= fragment", loc)
	}
	if strings.HasPrefix(loc, "/#=") {
		t.Fatalf("Location = %q, token landed in an unnamed fragment slot", loc)
	}
}

// TestFinishLogin_NamedFragmentKeyWins keeps the configured slot authoritative,
// so the default cannot rename OIDC's oidc_token fragment.
func TestFinishLogin_NamedFragmentKeyWins(t *testing.T) {
	s := newSSOFlowFixture(t, "oidc", "oidc_token", "idp", false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback", http.NoBody)
	s.finishLogin(rr, req, "1.2.3.4", []byte("handle"))

	if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/#oidc_token=") {
		t.Fatalf("Location = %q, want a /#oidc_token= fragment", loc)
	}
}

// TestConsumeState_ProviderErrorLogWording pins the per-flow wording of the
// callback error line: OIDC names the idp, GitHub the provider. Log-based
// alerting keys on the exact text.
func TestConsumeState_ProviderErrorLogWording(t *testing.T) {
	for _, tc := range []struct{ name, peerLabel, want string }{
		{"oidc", "idp", "oidc: idp returned error"},
		{"github", "provider", "github: provider returned error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logged strings.Builder
			debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
			t.Cleanup(func() { debuglog.SetHandler(debuglog.StdoutHandler()) })

			s := newSSOFlowFixture(t, tc.name, "", tc.peerLabel, true)
			blob, err := json.Marshal(ssoTestState{State: "st"})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			id, err := s.sessionMgr.CreateLoginState(context.Background(), blob, time.Minute)
			if err != nil {
				t.Fatalf("create login state: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/callback?error=access_denied&state=st", http.NoBody)
			req.AddCookie(&http.Cookie{Name: s.cookieName, Value: id.String()})
			rr := httptest.NewRecorder()

			if _, ok := s.consumeState(rr, req, "1.2.3.4", &ssoTestState{}); ok {
				t.Fatal("consumeState accepted a provider-reported error")
			}
			if !strings.Contains(logged.String(), tc.want) {
				t.Errorf("log = %q, want it to contain %q", logged.String(), tc.want)
			}
		})
	}
}
