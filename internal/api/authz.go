package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// UserStore is the slice of the user repository the auth middleware and the
// users admin API need. Implemented by *user.Repository.
type UserStore interface {
	Get(ctx context.Context, id uuid.UUID) (*user.User, error)
	List(ctx context.Context) ([]*user.User, error)
	Create(ctx context.Context, username, displayName string, email *string, passwordHash string, role user.Role, grants []string) (*user.User, error)
	Update(ctx context.Context, id uuid.UUID, username, displayName string, email *string, role user.Role, grants []string, enabled bool) (*user.User, error)
	SetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// SessionRevoker revokes every session of a user (disable/delete/password
// reset). Implemented by *webauthn.Repository; nil-safe wiring for tests.
type SessionRevoker interface {
	DeleteSessionsByUserID(ctx context.Context, userID []byte) (int64, error)
}

// resolveIdentity maps a validated session's user handle to a request
// identity. Only the exact legacy handle "admin" (what every pre-multi-user
// login minted) is the admin identity; any other non-UUID handle is rejected
// rather than escalated, so a future session-writing path passing arbitrary
// bytes cannot silently mint admin. A UUID handle must resolve to an enabled
// users row or the whole request is rejected: a deleted or disabled user's
// surviving tokens die here even if explicit revocation missed them.
func (h *Handler) resolveIdentity(ctx context.Context, sessionUserID []byte) (*user.Identity, bool) {
	if string(sessionUserID) == "admin" {
		return user.AdminIdentity(), true
	}
	uid, err := uuid.Parse(string(sessionUserID))
	if err != nil {
		return nil, false
	}
	if h.userRepo == nil {
		// Session references a users row but no user store is wired: fail
		// closed rather than escalate to admin.
		return nil, false
	}
	u, err := h.userRepo.Get(ctx, uid)
	if err != nil || u == nil || !u.Enabled {
		return nil, false
	}
	return &user.Identity{
		Role:     u.Role,
		Grants:   u.Grants,
		UserID:   &u.ID,
		Username: u.Username,
	}, true
}

// requireAdmin rejects non-admin identities with 403. Requests that never
// passed AuthMiddleware (no identity in context) are rejected too: the guard
// fails closed if a route is ever mounted outside the auth gate.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := user.IdentityFrom(r.Context())
		if !id.IsAdmin() {
			forbid(w, r, id)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireGrant admits admins and users holding at least one of the listed
// grants (multiple grants express "this data serves several pages", e.g. the
// models list feeding both the Models and Chat UIs).
func requireGrant(grants ...user.Grant) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := user.IdentityFrom(r.Context())
			for _, g := range grants {
				if id.Can(g) {
					next.ServeHTTP(w, r)
					return
				}
			}
			forbid(w, r, id)
		})
	}
}

// RequireGrant is the exported guard for routes mounted outside Register
// (the admin chat group in main.go). Method on Handler only so callers reach
// it through the wired API handler.
func (h *Handler) RequireGrant(g user.Grant) func(http.Handler) http.Handler {
	return requireGrant(g)
}

func forbid(w http.ResponseWriter, r *http.Request, id *user.Identity) {
	username := ""
	if id != nil {
		username = id.Username
	}
	debuglog.Warn("auth: insufficient permissions", "username", username, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
	http.Error(w, "insufficient permissions", http.StatusForbidden)
}

// meResponse is the GET /api/auth/me payload the SPA gates its navigation on.
type meResponse struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name,omitempty"`
	Role        string   `json:"role"`
	Grants      []string `json:"grants"`
}

// Me reports the caller's resolved identity. Mounted inside the
// authenticated group, so an identity is always present.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	id := user.IdentityFrom(r.Context())
	if id == nil {
		http.Error(w, "no identity", http.StatusUnauthorized)
		return
	}
	resp := meResponse{Username: id.Username, Role: string(id.Role), Grants: id.Grants}
	if resp.Grants == nil {
		resp.Grants = []string{}
	}
	if id.UserID != nil && h.userRepo != nil {
		if u, err := h.userRepo.Get(r.Context(), *id.UserID); err == nil {
			resp.DisplayName = u.DisplayName
		}
	}
	writeJSON(w, resp)
}
