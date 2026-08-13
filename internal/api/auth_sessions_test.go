package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// The handler must hand the manager the token the request actually carried, or
// the manager cannot tell which session to keep and would sign the caller out
// of the page they clicked from.
func TestRevokeOtherSessions_PassesTheCallersSessionCookie(t *testing.T) {
	h := &Handler{}
	var gotIdentity []byte
	var gotTokens []string
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeOthersFn: func(_ context.Context, identity []byte, candidateTokens ...string) (int64, error) {
			gotIdentity, gotTokens = identity, candidateTokens
			return 3, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "session-abc"})
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if !slices.Contains(gotTokens, "session-abc") {
		t.Errorf("candidate tokens = %v, want the session cookie's value among them", gotTokens)
	}
	if string(gotIdentity) != "admin" {
		t.Errorf("identity = %q, want admin", gotIdentity)
	}

	var result revokeOthersResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Revoked != 3 {
		t.Errorf("revoked = %d, want 3", result.Revoked)
	}
}

// A caller on the raw admin token holds no session cookie; the bearer token is
// still what identifies them, so it must reach the manager.
func TestRevokeOtherSessions_FallsBackToTheBearerToken(t *testing.T) {
	h := &Handler{}
	var gotTokens []string
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeOthersFn: func(_ context.Context, _ []byte, candidateTokens ...string) (int64, error) {
			gotTokens = candidateTokens
			return 0, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req.Header.Set("Authorization", "Bearer raw-admin-token")
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if !slices.Contains(gotTokens, "raw-admin-token") {
		t.Errorf("candidate tokens = %v, want the bearer token among them", gotTokens)
	}
}

func TestRevokeOtherSessions_ReportsStoreFailure(t *testing.T) {
	h := &Handler{}
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeOthersFn: func(context.Context, []byte, ...string) (int64, error) {
			return 0, context.DeadlineExceeded
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: a failed revoke must not report success", w.Code)
	}
}

// The list handler passes the middleware-resolved identity and the request's
// credentials through, and returns the manager's rows as JSON. The identity
// must come from the auth layer, not the request, for the same reason as the
// revoke: the two can disagree.
func TestListAuthSessions_PassesIdentityAndCandidates(t *testing.T) {
	h := &Handler{}
	var gotIdentity []byte
	var gotTokens []string
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		listFn: func(_ context.Context, identity []byte, candidateTokens ...string) ([]webauthn.AuthSessionInfo, error) {
			gotIdentity, gotTokens = identity, candidateTokens
			return []webauthn.AuthSessionInfo{
				{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), UserAgent: "phone", Current: false},
				{ID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), UserAgent: "here", Current: true},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/sessions", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "session-abc"})
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.ListAuthSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if string(gotIdentity) != "admin" {
		t.Errorf("identity = %q, want admin", gotIdentity)
	}
	if !slices.Contains(gotTokens, "session-abc") {
		t.Errorf("candidate tokens = %v, want the session cookie's value among them", gotTokens)
	}

	var result listSessionsResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(result.Sessions))
	}
	if !result.Sessions[1].Current || result.Sessions[1].UserAgent != "here" {
		t.Errorf("rows lost fields: %+v", result.Sessions)
	}
}

func TestListAuthSessions_WithoutSessionManager(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/auth/sessions", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.ListAuthSessions(w, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", w.Code)
	}
}

func TestListAuthSessions_ReportsStoreFailure(t *testing.T) {
	h := &Handler{}
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		listFn: func(context.Context, []byte, ...string) ([]webauthn.AuthSessionInfo, error) {
			return nil, context.DeadlineExceeded
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/sessions", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.ListAuthSessions(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// An identity with no sessions gets an empty list, not null: the frontend
// iterates the field without a null guard.
func TestListAuthSessions_EmptyListIsNotNull(t *testing.T) {
	h := &Handler{}
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{})

	req := httptest.NewRequest(http.MethodGet, "/auth/sessions", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.ListAuthSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if string(raw["sessions"]) != "[]" {
		t.Errorf("sessions = %s, want []", raw["sessions"])
	}
}

// The per-session revoke maps the manager's outcomes onto the API: gone is
// 204, not-yours-or-missing is 404, and the session the request rides on is a
// 409 so the UI can say "use logout for that".
func TestRevokeAuthSessionByID_MapsManagerOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		managerErr error
		wantStatus int
	}{
		{"revoked", nil, http.StatusNoContent},
		{"missing or foreign", webauthn.ErrNotFound, http.StatusNotFound},
		{"current session", webauthn.ErrCurrentSession, http.StatusConflict},
		{"store failure", context.DeadlineExceeded, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{}
			var gotID uuid.UUID
			var gotIdentity []byte
			var gotTokens []string
			h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
				revokeByIDFn: func(_ context.Context, identity []byte, id uuid.UUID, candidateTokens ...string) error {
					gotIdentity, gotID, gotTokens = identity, id, candidateTokens
					return tt.managerErr
				},
			})

			target := "33333333-3333-4333-8333-333333333333"
			req := httptest.NewRequest(http.MethodDelete, "/auth/sessions/"+target, http.NoBody)
			req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "session-abc"})
			req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
			w := httptest.NewRecorder()
			routeWithSessionID(h).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if gotID.String() != target {
				t.Errorf("id = %s, want %s", gotID, target)
			}
			if string(gotIdentity) != "admin" {
				t.Errorf("identity = %q, want admin", gotIdentity)
			}
			if !slices.Contains(gotTokens, "session-abc") {
				t.Errorf("candidate tokens = %v, want the session cookie among them", gotTokens)
			}
		})
	}
}

// A malformed id never reaches the manager.
func TestRevokeAuthSessionByID_RejectsMalformedID(t *testing.T) {
	h := &Handler{}
	called := false
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeByIDFn: func(context.Context, []byte, uuid.UUID, ...string) error {
			called = true
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/auth/sessions/not-a-uuid", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	routeWithSessionID(h).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if called {
		t.Error("a malformed id reached the manager")
	}
}

func TestRevokeAuthSessionByID_WithoutSessionManager(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodDelete, "/auth/sessions/33333333-3333-4333-8333-333333333333", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	routeWithSessionID(h).ServeHTTP(w, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", w.Code)
	}
}

// routeWithSessionID mounts the delete handler behind a chi router so the {id}
// URL parameter resolves the way it does in production.
func routeWithSessionID(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Delete("/auth/sessions/{id}", h.RevokeAuthSessionByID)
	return r
}

// With no session manager wired there is nothing to revoke, and claiming
// success would tell an operator their other sessions are gone when they are
// not.
func TestRevokeOtherSessions_WithoutSessionManager(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", w.Code)
	}
}
