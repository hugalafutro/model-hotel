package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newTraefikTestServer builds a test server with a dedicated Traefik poll
// token so the FRONTDESK_TRAEFIK_TOKEN gate can be exercised (newTestServer
// leaves it empty, which is the open compose-internal path).
func newTraefikTestServer(t *testing.T, traefikToken string) *Server {
	t.Helper()
	store := newTestStore(t)
	bus := events.NewBus()
	poller := NewPoller(store, bus, "")

	adminMgr, _, err := admin.New(t.TempDir(), testFrontdeskToken)
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	rp, err := webauthn.NewRelyingParty("localhost", "Front Desk", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}
	srv := NewServer(ServerConfig{
		Store:        store,
		Poller:       poller,
		Bus:          bus,
		AdminMgr:     adminMgr,
		MasterKey:    testMasterKey,
		RelyingParty: rp,
		IPLimiter:    ratelimit.NewIPLimiter(1000, 1000, nil, nil),
		TraefikToken: traefikToken,
	})
	t.Cleanup(srv.Wait)
	return srv
}

// pollConfig issues GET /traefik/config with an arbitrary bearer (empty = none).
func pollConfig(t *testing.T, srv *Server, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/traefik/config", http.NoBody)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// lastConfigPollAtIsZero reads the staleness watchdog's poll stamp.
func lastConfigPollAtIsZero(srv *Server) bool {
	srv.poller.mu.Lock()
	defer srv.poller.mu.Unlock()
	return srv.poller.lastConfigPollAt.IsZero()
}

// TestTraefikConfigTokenAuth covers the dedicated-token configuration: only
// the shared poll secret is accepted — not the admin token, which never leaves
// the operator's hands for Traefik's provider config.
func TestTraefikConfigTokenAuth(t *testing.T) {
	srv := newTraefikTestServer(t, "poll-secret")

	if rec := pollConfig(t, srv, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer = %d, want 401", rec.Code)
	}
	if rec := pollConfig(t, srv, "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer = %d, want 401", rec.Code)
	}
	if rec := pollConfig(t, srv, testFrontdeskToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin bearer with traefik token set = %d, want 401", rec.Code)
	}
	// Rejected polls must not feed the staleness watchdog: a scanner hammering
	// the endpoint with bad tokens would otherwise mask a Traefik whose polls
	// are failing on a token mismatch.
	if !lastConfigPollAtIsZero(srv) {
		t.Fatal("rejected polls recorded a config poll")
	}

	// The container liveness probe must keep answering with the gate up: the
	// image's HEALTHCHECK moved off /traefik/config for exactly this reason.
	hreq := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	hrec := httptest.NewRecorder()
	srv.ServeHTTP(hrec, hreq)
	if hrec.Code != http.StatusOK {
		t.Fatalf("GET /healthz with traefik token set = %d, want 200", hrec.Code)
	}

	rec := pollConfig(t, srv, "poll-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("poll token = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"routers"`) {
		t.Errorf("config body missing routers: %s", rec.Body.String())
	}
	if lastConfigPollAtIsZero(srv) {
		t.Error("accepted poll did not record a config poll")
	}
}

// TestTraefikConfigOpenWithoutToken pins the back-compat contract: with no
// token configured the endpoint stays open (Traefik polls credential-less, so
// unlike /metrics there is no admin-auth fallback to give it), and a stray
// bearer on an open poll is simply ignored rather than validated.
func TestTraefikConfigOpenWithoutToken(t *testing.T) {
	srv, _ := newTestServer(t)

	if rec := pollConfig(t, srv, ""); rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated poll without token = %d, want 200", rec.Code)
	}
	if rec := pollConfig(t, srv, "anything"); rec.Code != http.StatusOK {
		t.Fatalf("bearer on open endpoint = %d, want 200", rec.Code)
	}
}

// TestHealthzTracksTraefikConfigDependencies pins the probe's contract: it
// performs the same store reads as a Traefik config refresh, so a members
// query that would 500 /traefik/config also fails /healthz instead of leaving
// the container green while Traefik starves (Greptile P1 on PR #747: with only
// the settings read probed, dropping the members table split the two).
func TestHealthzTracksTraefikConfigDependencies(t *testing.T) {
	srv, store := newTestServer(t)

	healthz := func() int {
		req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := healthz(); got != http.StatusOK {
		t.Fatalf("healthz on intact store = %d, want 200", got)
	}

	// Break only the members read; settings stays queryable.
	if _, err := store.db.Exec(`ALTER TABLE members RENAME TO members_gone`); err != nil {
		t.Fatalf("rename members table: %v", err)
	}
	t.Cleanup(func() { _, _ = store.db.Exec(`ALTER TABLE members_gone RENAME TO members`) })

	if got := healthz(); got < 500 {
		t.Fatalf("healthz with broken members table = %d, want a 5xx", got)
	}
	if rec := pollConfig(t, srv, ""); rec.Code < 500 {
		t.Fatalf("traefik config with broken members table = %d, want a 5xx", rec.Code)
	}
}

// TestTraefikConfigWhitespaceTokenIsUnset guards against a blank-looking
// FRONTDESK_TRAEFIK_TOKEN (only spaces) being stored as a live bearer: the
// value is normalized to unset, so the endpoint stays open and the whitespace
// string is not itself a valid credential.
func TestTraefikConfigWhitespaceTokenIsUnset(t *testing.T) {
	srv := newTraefikTestServer(t, "   ")

	if rec := pollConfig(t, srv, ""); rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated poll with whitespace token = %d, want 200", rec.Code)
	}
}
