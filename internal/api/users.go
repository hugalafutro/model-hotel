package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// minPasswordLen is deliberately length-only (NIST 800-63B: no composition
// rules); the argon2id cost and login backoff carry the rest.
const minPasswordLen = 8

// RegisterUsers mounts the user management API. Admin-only: users cannot see
// or edit each other, and grants never unlock this surface.
//
// Reads (the roster, the grant catalog) stay open on a managed fleet member:
// the list matches the primary's, and the operator may browse it. Writes are
// guarded by managedWriteGuard because the user roster is synced config —
// applyUsers deletes any account absent from the primary's export on the next
// sync, so a local create/edit/delete would "succeed" and then be undone.
func (h *Handler) RegisterUsers(r chi.Router) {
	r.Route("/users", func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/", h.ListUsers)
		r.Get("/grants", h.ListGrantCatalog)
		// TOTP reset is deliberately outside managedWriteGuard: user_totp is
		// instance-local (never fleet-synced), so a local reset sticks.
		r.Post("/{id}/totp/reset", h.ResetUserTotp)
		r.Group(func(r chi.Router) {
			r.Use(managedWriteGuard(h.settingsRepo))
			r.Post("/", h.CreateUser)
			r.Put("/{id}", h.UpdateUser)
			r.Post("/{id}/password", h.SetUserPassword)
			r.Delete("/{id}", h.DeleteUser)
		})
	})
}

// ListGrantCatalog returns the valid grant keys so the edit modal renders its
// checkboxes from the backend catalog instead of a hardcoded copy.
func (h *Handler) ListGrantCatalog(w http.ResponseWriter, _ *http.Request) {
	grants := user.AllGrants()
	keys := make([]string, len(grants))
	for i, g := range grants {
		keys[i] = string(g)
	}
	writeJSON(w, map[string][]string{"grants": keys})
}

// ListUsers returns all users (password hashes never serialize).
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list users", err, http.StatusInternalServerError)
		return
	}
	if users == nil {
		users = []*user.User{}
	}
	h.fillTotpEnabled(r.Context(), users)
	writeJSON(w, users)
}

// fillTotpEnabled stamps TotpEnabled from user_totp in one query. Best-effort:
// on error the flags stay false (the badge is informational; enforcement lives
// in the login path, which reads its own state).
func (h *Handler) fillTotpEnabled(ctx context.Context, users []*user.User) {
	if len(users) == 0 {
		return
	}
	ids := make([]uuid.UUID, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	rows, err := h.dbPool.Pool().Query(ctx, `SELECT user_id FROM user_totp WHERE enabled AND user_id = ANY($1)`, ids)
	if err != nil {
		debuglog.Error("users: failed to read totp state", "error", err)
		return
	}
	defer rows.Close()
	enabled := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			debuglog.Error("users: failed to scan totp state", "error", err)
			return
		}
		enabled[id] = true
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("users: failed to read totp state", "error", err)
		return
	}
	for _, u := range users {
		u.TotpEnabled = enabled[u.ID]
	}
}

type userRequest struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Email       *string  `json:"email"`
	Password    string   `json:"password,omitempty"` // create only
	Role        string   `json:"role"`
	Grants      []string `json:"grants"`
	Enabled     *bool    `json:"enabled,omitempty"` // update only; create is always enabled
	// Aggregate proxy limits across the user's owned virtual keys. Null (or
	// omitted) means no cap; both create and update always write all three,
	// matching the virtual-key semantics.
	RateLimitRPS   *float64 `json:"rate_limit_rps"`
	RateLimitBurst *int     `json:"rate_limit_burst"`
	RateLimitTPM   *int     `json:"rate_limit_tpm"`
}

func (req *userRequest) limits() user.Limits {
	return user.Limits{RPS: req.RateLimitRPS, Burst: req.RateLimitBurst, TPM: req.RateLimitTPM}
}

// validate normalizes and checks the shared create/update fields.
func (req *userRequest) validate() (user.Role, error) {
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Username) > 64 {
		return "", errors.New("username must be 1-64 characters")
	}
	if strings.ContainsAny(req.Username, " \t\n") {
		return "", errors.New("username must not contain whitespace")
	}
	if len(req.DisplayName) > 128 {
		return "", errors.New("display name too long (max 128 characters)")
	}
	role := user.Role(req.Role)
	if role != user.RoleAdmin && role != user.RoleUser {
		return "", errors.New("role must be admin or user")
	}
	if err := user.ValidateGrants(req.Grants); err != nil {
		return "", err
	}
	return role, nil
}

// CreateUser adds a user account.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	role, err := req.validate()
	if err != nil {
		respondBadRequest(w, err.Error(), nil)
		return
	}
	if err := validateRateLimits(req.RateLimitRPS, req.RateLimitBurst, req.RateLimitTPM, w); err != nil {
		return
	}
	if len(req.Password) < minPasswordLen {
		respondBadRequest(w, "password must be at least 8 characters", nil)
		return
	}
	hash, err := user.HashPassword(req.Password)
	if err != nil {
		respondError(w, "failed to hash password", err, http.StatusInternalServerError)
		return
	}
	u, err := h.userRepo.Create(r.Context(), req.Username, req.DisplayName, req.Email, hash, role, req.Grants, req.limits())
	if err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "a user with this username or email already exists", http.StatusConflict)
			return
		}
		respondError(w, "failed to create user", err, http.StatusInternalServerError)
		return
	}
	writeJSONCreated(w, u)
}

// UpdateUser rewrites profile fields. Disabling a user revokes their live
// sessions immediately; self-disable is refused so an admin editing their own
// row cannot saw off the branch they sit on (the env token would still work,
// but the footgun is cheap to remove).
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	var req userRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	role, err := req.validate()
	if err != nil {
		respondBadRequest(w, err.Error(), nil)
		return
	}
	if err := validateRateLimits(req.RateLimitRPS, req.RateLimitBurst, req.RateLimitTPM, w); err != nil {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	caller := user.IdentityFrom(r.Context())
	self := caller != nil && caller.UserID != nil && *caller.UserID == id
	if self && (!enabled || role != user.RoleAdmin) {
		http.Error(w, "you cannot disable or demote your own account", http.StatusConflict)
		return
	}

	u, err := h.userRepo.Update(r.Context(), id, req.Username, req.DisplayName, req.Email, role, req.Grants, enabled, req.limits())
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if isUniqueViolation(err) {
			http.Error(w, "a user with this username or email already exists", http.StatusConflict)
			return
		}
		respondError(w, "failed to update user", err, http.StatusInternalServerError)
		return
	}
	if !enabled {
		h.revokeUserSessions(r, id.String())
	}
	writeJSON(w, u)
}

// SetUserPassword resets a user's password and revokes their sessions, so a
// reset always forces a fresh login (compromised-credential hygiene).
func (h *Handler) SetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	if len(req.Password) < minPasswordLen {
		respondBadRequest(w, "password must be at least 8 characters", nil)
		return
	}
	hash, err := user.HashPassword(req.Password)
	if err != nil {
		respondError(w, "failed to hash password", err, http.StatusInternalServerError)
		return
	}
	if err := h.userRepo.SetPassword(r.Context(), id, hash); err != nil {
		if errors.Is(err, user.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		respondError(w, "failed to set password", err, http.StatusInternalServerError)
		return
	}
	h.revokeUserSessions(r, id.String())
	writeJSON(w, map[string]bool{"ok": true})
}

// DeleteUser removes a user and revokes their sessions. Self-delete is
// refused for the same reason as self-disable.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	caller := user.IdentityFrom(r.Context())
	if caller != nil && caller.UserID != nil && *caller.UserID == id {
		http.Error(w, "you cannot delete your own account", http.StatusConflict)
		return
	}
	if err := h.userRepo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, user.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		respondError(w, "failed to delete user", err, http.StatusInternalServerError)
		return
	}
	h.revokeUserSessions(r, id.String())
	w.WriteHeader(http.StatusNoContent)
}

// revokeUserSessions best-effort kills every session of the user. Failures
// are logged, not fatal: the auth middleware re-checks the users row on every
// request, so a disabled/deleted user is locked out either way; revocation
// just tidies the sessions table.
func (h *Handler) revokeUserSessions(r *http.Request, userID string) {
	if h.sessionRevoker == nil {
		return
	}
	n, err := h.sessionRevoker.DeleteSessionsByUserID(r.Context(), []byte(userID))
	if err != nil {
		debuglog.Error("users: failed to revoke sessions", "user_id", userID, "error", err)
		return
	}
	if n > 0 {
		debuglog.Info("users: revoked sessions", "user_id", userID, "count", n)
	}
}
