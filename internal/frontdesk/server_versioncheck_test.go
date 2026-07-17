package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFleetVersionCheck: the wizard's pre-sync gate re-polls every tokened
// member's app_version on demand and reports which differ from the chosen
// primary's. The primary itself and tokenless members never appear as skewed,
// and the response reflects the fresh poll, not the poller's cached versions.
func TestFleetVersionCheck(t *testing.T) {
	srv, store := newTestServer(t)

	fake := func(version, token string) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Method == http.MethodGet && r.URL.Path == "/api/settings" {
				_ = json.NewEncoder(w).Encode(map[string]string{"app_version": version})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(s.Close)
		return s
	}

	primary := fake("v1.0.0", "ptoken")
	aligned := fake("v1.0.0", "atoken")
	skewed := fake("v0.9.0", "stoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.URL, "ptoken")
	_, _ = store.CreateMember(t.Context(), "aligned", aligned.URL, "atoken")
	sm, _ := store.CreateMember(t.Context(), "skewed", skewed.URL, "stoken")
	_, _ = store.CreateMember(t.Context(), "tokenless", "http://127.0.0.1:9", "")

	// A stale cached read must not mask the member's real version: the operator
	// just up/downgraded it, and Refresh exists precisely to see that immediately.
	setMemberVersion(srv, sm.ID, "v1.0.0")

	rec := do(t, srv, http.MethodPost, "/api/fleet/version-check", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("version-check = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp versionCheckResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PrimaryID != pm.ID || resp.PrimaryVersion != "v1.0.0" {
		t.Errorf("primary = %q @ %q, want %q @ v1.0.0", resp.PrimaryID, resp.PrimaryVersion, pm.ID)
	}
	if len(resp.Skewed) != 1 {
		t.Fatalf("skewed = %+v, want exactly the one skewed member", resp.Skewed)
	}
	if got := resp.Skewed[0]; got.MemberID != sm.ID || got.Name != "skewed" || got.Version != "v0.9.0" {
		t.Errorf("skewed[0] = %+v, want member %q at freshly-polled v0.9.0", got, sm.ID)
	}
}

// TestFleetVersionCheckUnknownPrimary: an unknown primary is a client error,
// not an empty aligned response.
func TestFleetVersionCheckUnknownPrimary(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodPost, "/api/fleet/version-check", `{"primary_id":"nope"}`, true)
	if rec.Code < 400 {
		t.Fatalf("version-check with unknown primary = %d, want an error status", rec.Code)
	}
}

func TestFleetVersionCheckBadBody(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodPost, "/api/fleet/version-check", `{`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("version-check with malformed body = %d, want 400", rec.Code)
	}
}
