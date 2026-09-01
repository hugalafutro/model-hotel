package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The fleet-wide reset asks every member that has a token, whole ledger or one
// group, sums what they report, and names the ones that failed rather than
// stopping at them.
func TestFleetCircuitReset(t *testing.T) {
	srv, store := newTestServer(t)

	var groupHits, allHits atomic.Int32
	fake := func(token string, cleared, recovered int, fail bool) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch r.URL.Path {
			case "/api/failover-groups/circuit-breaker/reset":
				allHits.Add(1)
			case "/api/failover-groups/11111111-2222-3333-4444-555555555555/circuit-breaker/reset":
				groupHits.Add(1)
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"cleared": cleared, "recovered": recovered})
		}))
		t.Cleanup(s.Close)
		return s
	}
	a := fake("atoken", 3, 2, false)
	b := fake("btoken", 1, 0, false)
	c := fake("ctoken", 0, 0, true)
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(garbled.Close)
	_, _ = store.CreateMember(t.Context(), "a", a.URL, "atoken")
	_, _ = store.CreateMember(t.Context(), "b", b.URL, "btoken")
	cm, _ := store.CreateMember(t.Context(), "c", c.URL, "ctoken")
	tl, err := store.CreateMember(t.Context(), "tokenless", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatalf("create tokenless member: %v", err)
	}
	// A distinct dead address from the tokenless member's: the store refuses
	// a duplicate URL, and a member that was never created has no ID to check.
	um, err := store.CreateMember(t.Context(), "unreachable", "http://127.0.0.1:2", "utoken")
	if err != nil {
		t.Fatalf("create unreachable member: %v", err)
	}
	gm, err := store.CreateMember(t.Context(), "garbled", garbled.URL, "gtoken")
	if err != nil {
		t.Fatalf("create garbled member: %v", err)
	}

	rec := do(t, srv, http.MethodPost, "/api/fleet/circuit-breaker/reset", `{"group_id":"11111111-2222-3333-4444-555555555555"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp fleetCircuitResetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Members) != 6 {
		t.Fatalf("members = %+v, want all six, the tokenless one included", resp.Members)
	}
	if resp.Cleared != 4 || resp.Recovered != 2 || resp.Failed != 3 || resp.Skipped != 1 {
		t.Errorf("totals = cleared %d recovered %d failed %d skipped %d, want 4/2/3/1", resp.Cleared, resp.Recovered, resp.Failed, resp.Skipped)
	}
	wantErr := map[string]string{
		cm.ID: "member answered 500", um.ID: "could not reach this member",
		gm.ID: "member answered with an unreadable body", tl.ID: "no stored admin token",
	}
	for _, m := range resp.Members {
		if want, failing := wantErr[m.MemberID]; failing {
			if m.OK || m.Error != want {
				t.Errorf("failing member %s = %+v, want error %q", m.Name, m, want)
			}
			if m.Skipped != (m.MemberID == tl.ID) {
				t.Errorf("member %s skipped = %v; only the tokenless one is skipped rather than failed", m.Name, m.Skipped)
			}
		} else if !m.OK || m.Error != "" {
			t.Errorf("member %s = %+v, want ok", m.Name, m)
		}
	}
	if groupHits.Load() != 3 || allHits.Load() != 0 {
		t.Errorf("group path hit %d times, all path %d; want the group path on all three", groupHits.Load(), allHits.Load())
	}

	// Without a group: the whole ledger on every member.
	rec = do(t, srv, http.MethodPost, "/api/fleet/circuit-breaker/reset", `{}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset all = %d (%s)", rec.Code, rec.Body.String())
	}
	if allHits.Load() != 3 {
		t.Errorf("all path hit %d times, want 3", allHits.Load())
	}

	// A body that is not JSON is refused before any member is asked.
	if rec := do(t, srv, http.MethodPost, "/api/fleet/circuit-breaker/reset", `{bad`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body = %d, want 400", rec.Code)
	}
	// A member whose row claims a token the store cannot produce is a failure
	// with the same reason, not a skip: something is wrong, not configured.
	if res := srv.resetMemberCircuits(t.Context(), &Member{ID: "no-such-member", Name: "ghost", HasToken: true}, ""); res.OK || res.Skipped || res.Error != "no stored admin token" {
		t.Errorf("tokenless member = %+v", res)
	}

	// A malformed group id is refused before any member is asked.
	rec = do(t, srv, http.MethodPost, "/api/fleet/circuit-breaker/reset", `{"group_id":"nope"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed group id = %d, want 400", rec.Code)
	}
	if groupHits.Load() != 3 {
		t.Errorf("a refused request still reached a member")
	}
}

// The group picker's relay: the primary's list reduced to id, names, entry
// count and enabled flag; a missing primary or an unreachable one is a coded
// error rather than an empty list.
func TestFleetFailoverGroups(t *testing.T) {
	srv, store := newTestServer(t)
	name := "GLM 5.3"
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ptoken" || r.URL.Path != "/api/failover-groups" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "11111111-2222-3333-4444-555555555555", "display_model": "glm53", "display_name": name, "group_enabled": true, "entries": []any{1, 2, 3}},
			{"id": "66666666-7777-8888-9999-000000000000", "display_model": "gemma", "display_name": nil, "group_enabled": false, "entries": []any{1}},
		})
	}))
	t.Cleanup(primary.Close)
	pm, _ := store.CreateMember(t.Context(), "primary", primary.URL, "ptoken")

	rec := do(t, srv, http.MethodGet, "/api/fleet/failover-groups?primary_id="+pm.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("groups = %d (%s)", rec.Code, rec.Body.String())
	}
	var groups []fleetFailoverGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(groups) != 2 || groups[0].DisplayModel != "glm53" || groups[0].Entries != 3 || groups[0].DisplayName == nil || *groups[0].DisplayName != name || !groups[0].GroupEnabled || groups[1].GroupEnabled {
		t.Errorf("groups = %+v", groups)
	}
	if rec := do(t, srv, http.MethodGet, "/api/fleet/failover-groups", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("no primary = %d, want 400", rec.Code)
	}
	dead, _ := store.CreateMember(t.Context(), "dead", "http://127.0.0.1:9", "dtoken")
	if rec := do(t, srv, http.MethodGet, "/api/fleet/failover-groups?primary_id="+dead.ID, "", true); rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable primary = %d, want 502", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/fleet/failover-groups?primary_id=no-such-member", "", true); rec.Code == http.StatusOK {
		t.Errorf("unknown primary answered 200")
	}
	// Primaries that answer, but not with a group list. Two servers, because
	// the store refuses a second member on the same URL.
	erroring := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(erroring.Close)
	garbling := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(garbling.Close)
	b500, err := store.CreateMember(t.Context(), "b500", erroring.URL, "b500")
	if err != nil {
		t.Fatalf("create erroring primary: %v", err)
	}
	if rec := do(t, srv, http.MethodGet, "/api/fleet/failover-groups?primary_id="+b500.ID, "", true); rec.Code != http.StatusBadGateway {
		t.Errorf("primary answering 500 = %d, want 502", rec.Code)
	}
	bgarb, err := store.CreateMember(t.Context(), "bgarb", garbling.URL, "garble")
	if err != nil {
		t.Fatalf("create garbling primary: %v", err)
	}
	if rec := do(t, srv, http.MethodGet, "/api/fleet/failover-groups?primary_id="+bgarb.ID, "", true); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "unreadable") {
		t.Errorf("primary answering garbage = %d (%s), want 502 unreadable", rec.Code, rec.Body.String())
	}
}
