package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// doSession issues a request authenticated by the given bearer (a session
// token or the raw admin token both ride the Authorization header).
func doSession(t *testing.T, srv *Server, method, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

type fdSessionsResult struct {
	Sessions []webauthn.AuthSessionInfo `json:"sessions"`
}

// The list shows the admin identity's live sessions with device metadata and
// marks the calling one current; on the raw admin token nothing is current.
func TestFDSessions_ListMarksCurrent(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := context.Background()

	here, err := srv.sessionMgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{UserAgent: "here", IP: "203.0.113.7"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.sessionMgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{UserAgent: "phone"}); err != nil {
		t.Fatal(err)
	}

	rec := doSession(t, srv, http.MethodGet, "/api/auth/sessions", here)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var list fdSessionsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(list.Sessions))
	}
	for _, s := range list.Sessions {
		switch s.UserAgent {
		case "here":
			if !s.Current {
				t.Error("the calling session is not marked current")
			}
			if s.IP != "203.0.113.7" {
				t.Errorf("IP = %q, want 203.0.113.7", s.IP)
			}
		case "phone":
			if s.Current {
				t.Error("the other session is marked current")
			}
		default:
			t.Errorf("unexpected row: %+v", s)
		}
	}

	// Raw admin token: same rows, nothing current.
	rec = doSession(t, srv, http.MethodGet, "/api/auth/sessions", testFrontdeskToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("raw-token status = %d (%s)", rec.Code, rec.Body.String())
	}
	list = fdSessionsResult{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, s := range list.Sessions {
		if s.Current {
			t.Error("a session is current though the caller holds the raw admin token")
		}
	}
}

// Per-row revoke: another session dies on request, the calling one is refused
// with the coded 409, a junk id is a 400 and a missing one a 404.
func TestFDSessions_RevokeByID(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := context.Background()
	mgr := srv.sessionMgr

	here, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{UserAgent: "here"})
	if err != nil {
		t.Fatal(err)
	}
	phone, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{UserAgent: "phone"})
	if err != nil {
		t.Fatal(err)
	}

	rec := doSession(t, srv, http.MethodGet, "/api/auth/sessions", here)
	var list fdSessionsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var hereID, phoneID string
	for _, s := range list.Sessions {
		if s.Current {
			hereID = s.ID.String()
		} else {
			phoneID = s.ID.String()
		}
	}

	if rec = doSession(t, srv, http.MethodDelete, "/api/auth/sessions/"+hereID, here); rec.Code != http.StatusConflict {
		t.Errorf("delete current = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if _, ok := mgr.TokenUser(ctx, here); !ok {
		t.Fatal("the current session died despite the refusal")
	}

	if rec = doSession(t, srv, http.MethodDelete, "/api/auth/sessions/"+phoneID, here); rec.Code != http.StatusNoContent {
		t.Errorf("delete other = %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	if _, ok := mgr.TokenUser(ctx, phone); ok {
		t.Error("the targeted session survived")
	}

	if rec = doSession(t, srv, http.MethodDelete, "/api/auth/sessions/not-a-uuid", here); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id = %d, want 400", rec.Code)
	}
	if rec = doSession(t, srv, http.MethodDelete, "/api/auth/sessions/"+phoneID, here); rec.Code != http.StatusNotFound {
		t.Errorf("already-gone id = %d, want 404", rec.Code)
	}
}

// The bulk action Front Desk lacked: every other session goes, the caller's
// stays; on the raw admin token everything goes.
func TestFDSessions_RevokeOthers(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := context.Background()
	mgr := srv.sessionMgr

	here, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	phone, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	rec := doSession(t, srv, http.MethodPost, "/api/auth/sessions/revoke-others", here)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var result struct {
		Revoked int64 `json:"revoked"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Revoked != 1 {
		t.Errorf("revoked = %d, want 1", result.Revoked)
	}
	if _, ok := mgr.TokenUser(ctx, here); !ok {
		t.Error("the caller's own session was revoked")
	}
	if _, ok := mgr.TokenUser(ctx, phone); ok {
		t.Error("the other session survived")
	}
}

// A store failure must read as a server error on every session endpoint, not
// as an empty list or a successful revoke. The raw admin token authenticates
// without the store (file-backed admin manager), so closing the database
// isolates exactly the handlers' store dependency.
func TestFDSessions_StoreFailureIsAServerError(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/auth/sessions"},
		{http.MethodDelete, "/api/auth/sessions/00000000-0000-4000-8000-000000000000"},
		{http.MethodPost, "/api/auth/sessions/revoke-others"},
	} {
		if rec := doSession(t, srv, tc.method, tc.path, testFrontdeskToken); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s = %d, want 500 on a store failure", tc.method, tc.path, rec.Code)
		}
	}
}

// Session hygiene is web-UI administration: a paired device, whatever its
// role, must not see or end the operator's browser sessions.
func TestFDSessions_DeviceTokensForbidden(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	token, hash, err := mintDeviceToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePairedDevice(ctx, "Pixel", hash, RoleOperator); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/auth/sessions"},
		{http.MethodDelete, "/api/auth/sessions/00000000-0000-4000-8000-000000000000"},
		{http.MethodPost, "/api/auth/sessions/revoke-others"},
	} {
		if rec := doSession(t, srv, tc.method, tc.path, token); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403 for a device token", tc.method, tc.path, rec.Code)
		}
	}
}
