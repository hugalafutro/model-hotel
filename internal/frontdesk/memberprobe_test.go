package frontdesk

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// These tests cover the add/edit-time admin-token probe: a token the member
// positively refuses (401) blocks the save, an unreachable member saves with a
// warning, and a good token saves cleanly. They reuse the fleet stub member,
// which answers /api/settings (the probe endpoint) only when the Bearer matches.

func createMemberJSON(t *testing.T, srv *Server, name, url, token string) (int, memberResponse) {
	t.Helper()
	body := `{"name":"` + name + `","url":"` + url + `","token":"` + token + `"}`
	rec := do(t, srv, http.MethodPost, "/api/members", body, true)
	var resp memberResponse
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	}
	return rec.Code, resp
}

func TestCreateMemberAcceptsGoodToken(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "good")

	code, resp := createMemberJSON(t, srv, "m1", stub.srv.URL, "good")
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	if resp.TokenWarning != "" {
		t.Errorf("unexpected token_warning: %q", resp.TokenWarning)
	}
	members, _ := store.ListMembers(t.Context())
	if len(members) != 1 {
		t.Fatalf("members = %d, want 1", len(members))
	}
}

func TestCreateMemberRejectsRefusedToken(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "right")

	code, _ := createMemberJSON(t, srv, "m1", stub.srv.URL, "wrong")
	if code != http.StatusBadRequest {
		t.Fatalf("create with wrong token = %d, want 400", code)
	}
	// The bad add must be rolled back, not left half-created.
	members, _ := store.ListMembers(t.Context())
	if len(members) != 0 {
		t.Fatalf("members = %d after rejected add, want 0 (rollback)", len(members))
	}
}

func TestCreateMemberRejectsUnreachable(t *testing.T) {
	srv, store := newTestServer(t)

	// A dead port: an add now requires a positive reply, so an unreachable host is
	// rejected outright (not saved with a warning) and nothing is persisted.
	code, _ := createMemberJSON(t, srv, "m1", "http://127.0.0.1:9", "tok")
	if code != http.StatusBadRequest {
		t.Fatalf("create unreachable = %d, want 400", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after rejected add, want 0", len(members))
	}
}

func TestCreateMemberRequiresToken(t *testing.T) {
	srv, store := newTestServer(t)
	// No token: Front Desk cannot verify the host's identity or fleet role, so the
	// add is refused before anything is persisted.
	code, _ := createMemberJSON(t, srv, "m1", "http://127.0.0.1:9", "")
	if code != http.StatusBadRequest {
		t.Fatalf("create without token = %d, want 400", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after tokenless add, want 0", len(members))
	}
}

func TestCreateMemberRejectsSelfReportedPrimary(t *testing.T) {
	srv, store := newTestServer(t)
	// The candidate answers the token probe AND self-reports is_primary=true, i.e.
	// it is the fleet primary reached under a different URL. Adding it as a member
	// is refused (409) so the source of truth is never duplicated into the pool.
	host := systemMemberServer(t, true)

	code, _ := createMemberJSON(t, srv, "hotel-1-lan", host.URL, "tok")
	if code != http.StatusConflict {
		t.Fatalf("create self-reported primary = %d, want 409", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after rejected primary add, want 0", len(members))
	}
}

func TestCreateMemberRejectsAlreadyMember(t *testing.T) {
	srv, store := newTestServer(t)
	// First host: a normal member, verified and stored with its instance_id.
	host := systemMemberServerID(t, false, "iid-shared")
	if code, _ := createMemberJSON(t, srv, "hotel-1", host.URL, "tok"); code != http.StatusCreated {
		t.Fatalf("first add = %d, want 201", code)
	}
	members, _ := store.ListMembers(t.Context())
	if len(members) != 1 || members[0].InstanceID != "iid-shared" {
		t.Fatalf("first member instance_id = %+v, want iid-shared stored", members)
	}

	// Second "host" at a different URL but the SAME instance_id: the same physical
	// instance reached under another address. The add is refused.
	sameHost := systemMemberServerID(t, false, "iid-shared")
	code, _ := createMemberJSON(t, srv, "hotel-1-lan", sameHost.URL, "tok")
	if code != http.StatusConflict {
		t.Fatalf("duplicate-instance add = %d, want 409", code)
	}
	if m, _ := store.ListMembers(t.Context()); len(m) != 1 {
		t.Errorf("members = %d after rejected duplicate, want 1", len(m))
	}
}

func TestCreateMemberBackfillsAndDedupsPreexisting(t *testing.T) {
	srv, store := newTestServer(t)
	// A member added before instance identity existed: stored with an empty
	// instance_id (simulated by creating it directly in the store).
	host := systemMemberServerID(t, false, "iid-old")
	old, err := store.CreateMember(t.Context(), "old", host.URL, "tok")
	if err != nil {
		t.Fatalf("seed old member: %v", err)
	}
	if old.InstanceID != "" {
		t.Fatalf("precondition: old.InstanceID = %q, want empty", old.InstanceID)
	}

	// Adding the same instance under a new URL must be caught by backfilling the
	// old member's identity during the dedup check.
	sameHost := systemMemberServerID(t, false, "iid-old")
	code, _ := createMemberJSON(t, srv, "old-lan", sameHost.URL, "tok")
	if code != http.StatusConflict {
		t.Fatalf("duplicate against pre-existing = %d, want 409", code)
	}
	// The dedup pass also backfilled the old member's instance_id.
	got, _ := store.GetMember(t.Context(), old.ID)
	if got.InstanceID != "iid-old" {
		t.Errorf("old member instance_id = %q after dedup, want backfilled iid-old", got.InstanceID)
	}
}

func TestPatchMemberRejectsRefusedToken(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "right")
	m, _ := store.CreateMember(t.Context(), "m1", stub.srv.URL, "right")

	rec := do(t, srv, http.MethodPatch, "/api/members/"+m.ID, `{"token":"wrong"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("patch with wrong token = %d, want 400", rec.Code)
	}
	// The refused token must not have replaced the working one.
	tok, ok, _ := store.MemberToken(t.Context(), m.ID)
	if !ok || tok != "right" {
		t.Errorf("stored token = %q (ok=%v), want unchanged %q", tok, ok, "right")
	}
}

func TestPatchMemberClearingTokenSkipsProbe(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "right")
	m, _ := store.CreateMember(t.Context(), "m1", stub.srv.URL, "right")

	rec := do(t, srv, http.MethodPatch, "/api/members/"+m.ID, `{"token":""}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear token = %d, want 200", rec.Code)
	}
	if _, ok, _ := store.MemberToken(t.Context(), m.ID); ok {
		t.Error("token should be cleared")
	}
}
