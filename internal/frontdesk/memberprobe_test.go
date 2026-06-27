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

func TestCreateMemberWarnsWhenUnreachable(t *testing.T) {
	srv, _ := newTestServer(t)

	// A dead port: the probe cannot reach it, so the add still succeeds but warns.
	code, resp := createMemberJSON(t, srv, "m1", "http://127.0.0.1:9", "tok")
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	if resp.TokenWarning == "" {
		t.Error("expected a token_warning for an unreachable member")
	}
}

func TestCreateMemberWithoutTokenSkipsProbe(t *testing.T) {
	srv, _ := newTestServer(t)
	// No token and an unreachable URL: nothing to probe, so no warning, no error.
	code, resp := createMemberJSON(t, srv, "m1", "http://127.0.0.1:9", "")
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	if resp.TokenWarning != "" {
		t.Errorf("unexpected token_warning with no token: %q", resp.TokenWarning)
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
