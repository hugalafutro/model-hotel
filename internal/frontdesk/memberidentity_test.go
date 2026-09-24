package frontdesk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMemberIdentityUnparseableBody covers the fail-open path: a host that
// answers /api/system with a 200 but a body we cannot parse is reported as
// ok=false (identity unknown), never wrongly treated as primary or as carrying
// an instance_id. Callers then fall open and add it as an ordinary member.
func TestMemberIdentityUnparseableBody(t *testing.T) {
	srv, _ := newTestServer(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{ this is not json")
	}))
	t.Cleanup(bad.Close)

	ident, ok := srv.memberIdentity(t.Context(), bad.URL, "tok")
	if ok || ident != (memberFleetIdentity{}) {
		t.Fatalf("unparseable /api/system: got (%+v, ok=%v), want (zero identity, false)", ident, ok)
	}
}

// fleetIdentityStub stands in for a member host whose /api/system reports the
// given fleet block verbatim, so a test can pin a state, a lingering is_primary
// and an owning Front Desk id together. It answers every other path 200, which
// is what the token probe needs.
func fleetIdentityStub(t *testing.T, fleetJSON, instanceID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/system") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"fleet":`+fleetJSON+`,"instance_id":"`+instanceID+`"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCreateMemberErrorsCarryCodes checks the add-failure responses carry a
// stable machine-readable code (and JSON shape) the frontend routes on, so the
// UI never has to match translatable English text.
func TestCreateMemberErrorsCarryCodes(t *testing.T) {
	codeOf := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		var body struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("error body not JSON: %v (%s)", err, rec.Body.String())
		}
		if body.Error == "" {
			t.Errorf("coded error has empty message: %s", rec.Body.String())
		}
		return body.Code
	}

	t.Run("token_required", func(t *testing.T) {
		srv, _ := newTestServer(t)
		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"m","url":"https://h:8081","token":""}`, true)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "token_required" {
			t.Fatalf("got %d code=%q, want 400 token_required", rec.Code, codeOf(t, rec))
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		srv, _ := newTestServer(t)
		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"m","url":"http://127.0.0.1:9","token":"tok"}`, true)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "unreachable" {
			t.Fatalf("got %d code=%q, want 400 unreachable", rec.Code, codeOf(t, rec))
		}
	})
	t.Run("already_primary", func(t *testing.T) {
		srv, _ := newTestServer(t)
		host := systemMemberServer(t, true)
		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"m","url":"`+host.URL+`","token":"tok"}`, true)
		if rec.Code != http.StatusConflict || codeOf(t, rec) != "already_primary" {
			t.Fatalf("got %d code=%q, want 409 already_primary", rec.Code, codeOf(t, rec))
		}
	})
	// A host still carrying is_primary from a fleet THIS Front Desk disbanded
	// must be addable at once. Disbanding drops every member row together, so
	// nothing is left to announce the demotion: the flag sits there for
	// fleetForgetTTL (24h) while the state degrades to "warning" after 90s. The
	// host names this desk as its owner, and this desk knows it is no longer
	// announcing, so the role it reports is one only this desk could have given.
	t.Run("own stale ex-primary is addable", func(t *testing.T) {
		srv, store := newTestServer(t)
		ownID, err := store.EnsureFrontdeskID(t.Context())
		if err != nil {
			t.Fatalf("frontdesk id: %v", err)
		}
		host := fleetIdentityStub(t, `{"state":"warning","is_primary":true,"frontdesk_id":"`+ownID+`"}`, "iid-ex-primary")

		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"re-added","url":"`+host.URL+`","token":"tok"}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("re-add of our own stale ex-primary = %d code=%q, want 201", rec.Code, codeOf(t, rec))
		}
		if members, _ := store.ListMembers(t.Context()); len(members) != 1 {
			t.Errorf("members = %d after the re-add, want 1", len(members))
		}
	})
	// The same stale report naming a DIFFERENT Front Desk is refused. That desk
	// may only be unreachable for the moment, and its member adopts a new owner
	// as soon as the old one's heartbeat goes stale - so enrolling it here would
	// quietly take a live fleet's config source away from it.
	t.Run("another desk's stale primary is refused", func(t *testing.T) {
		srv, store := newTestServer(t)
		host := fleetIdentityStub(t, `{"state":"warning","is_primary":true,"frontdesk_id":"fd-somewhere-else"}`, "iid-theirs")

		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"theirs","url":"`+host.URL+`","token":"tok"}`, true)
		if rec.Code != http.StatusConflict || codeOf(t, rec) != "already_primary" {
			t.Fatalf("got %d code=%q, want 409 already_primary", rec.Code, codeOf(t, rec))
		}
		if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
			t.Errorf("members = %d after the refused add, want 0", len(members))
		}
	})
	// A host too old to report which Front Desk manages it keeps the refusal it
	// has always had: nothing it says can place its fleet, so the conservative
	// answer is the safe one.
	t.Run("stale primary without a desk id is refused", func(t *testing.T) {
		srv, _ := newTestServer(t)
		host := fleetIdentityStub(t, `{"state":"warning","is_primary":true}`, "iid-legacy")

		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"legacy","url":"`+host.URL+`","token":"tok"}`, true)
		if rec.Code != http.StatusConflict || codeOf(t, rec) != "already_primary" {
			t.Fatalf("got %d code=%q, want 409 already_primary", rec.Code, codeOf(t, rec))
		}
	})
	// A stale ex-MEMBER (never a primary) is addable whoever managed it: the
	// ownership question only arises for the one host a fleet cannot do without.
	t.Run("another desk's stale member is addable", func(t *testing.T) {
		srv, _ := newTestServer(t)
		host := fleetIdentityStub(t, `{"state":"warning","is_primary":false,"frontdesk_id":"fd-somewhere-else"}`, "iid-plain")

		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"plain","url":"`+host.URL+`","token":"tok"}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("add of another desk's stale member = %d code=%q, want 201", rec.Code, codeOf(t, rec))
		}
	})
	t.Run("already_member", func(t *testing.T) {
		srv, _ := newTestServer(t)
		h1 := systemMemberServerID(t, false, "iid-dup")
		if rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"a","url":"`+h1.URL+`","token":"tok"}`, true); rec.Code != http.StatusCreated {
			t.Fatalf("first add = %d, want 201", rec.Code)
		}
		h2 := systemMemberServerID(t, false, "iid-dup")
		rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"a-lan","url":"`+h2.URL+`","token":"tok"}`, true)
		if rec.Code != http.StatusConflict || codeOf(t, rec) != "already_member" {
			t.Fatalf("got %d code=%q, want 409 already_member", rec.Code, codeOf(t, rec))
		}
	})
}

// TestCreateMemberRejectsUnreadableIdentity covers the fail-closed identity
// path: the token probe succeeds (the host answers /api/settings with 200) but
// the fleet-identity read (/api/system) fails, so Front Desk cannot confirm the
// host is not the fleet primary or an already-registered member. Rather than
// fail open and admit it, the add is rejected with a stable identity_unverified
// code and the just-created row is rolled back.
func TestCreateMemberRejectsUnreadableIdentity(t *testing.T) {
	srv, store := newTestServer(t)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/system") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK) // token probe (/api/settings) passes
	}))
	t.Cleanup(host.Close)

	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"m","url":"`+host.URL+`","token":"tok"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create with unreadable /api/system = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "identity_unverified" {
		t.Fatalf("code = %q, want identity_unverified (%s)", body.Code, rec.Body.String())
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after rejected add, want 0 (rollback)", len(members))
	}
}

// TestCreateMemberRejectsUnexpectedStatus covers the add's default rollback
// branch: the host is reachable and does not reject the token (not 401/403) but
// answers the verification probe with an unexpected non-200 (500 here), so it is
// not confirmed a genuine model-hotel member. The add is refused and rolled back.
func TestCreateMemberRejectsUnexpectedStatus(t *testing.T) {
	srv, store := newTestServer(t)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(host.Close)

	code, _ := createMemberJSON(t, srv, "m1", host.URL, "tok")
	if code != http.StatusBadRequest {
		t.Fatalf("create against 500 probe = %d, want 400", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after rejected add, want 0 (rollback)", len(members))
	}
}
