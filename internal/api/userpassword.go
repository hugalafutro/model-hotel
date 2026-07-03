package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// ChangeOwnPassword lets a users-row identity rotate its own password. The
// current password must be presented (an admin resetting someone else goes
// through POST /users/{id}/password instead), failed checks back off per user
// so a hijacked session cannot brute-force the password it rode in on, and on
// success every session of the account is revoked - including the caller's,
// which signs back in with the new password. The env-token admin has no
// users row (its credential is the token itself) and is refused.
func (h *Handler) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	id := user.IdentityFrom(r.Context())
	if h.userRepo == nil {
		http.Error(w, "multi-user accounts are not available", http.StatusNotFound)
		return
	}
	if id == nil || id.UserID == nil {
		http.Error(w, "not a user account; the admin token has no password", http.StatusBadRequest)
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		respondBadRequest(w, "password must be at least 8 characters", nil)
		return
	}
	key := id.UserID.String()
	if ok, retry := h.pwThrottle.Allowed(key); !ok {
		debuglog.Warn("userpassword: throttled", "username", id.Username)
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}
	u, err := h.userRepo.Get(r.Context(), *id.UserID)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		respondError(w, "failed to load user", err, http.StatusInternalServerError)
		return
	}
	match, err := user.VerifyPassword(req.CurrentPassword, u.PasswordHash)
	if err != nil {
		respondError(w, "failed to verify password", err, http.StatusInternalServerError)
		return
	}
	if !match {
		h.pwThrottle.RecordFailure(key)
		debuglog.Warn("userpassword: wrong current password", "username", id.Username)
		http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		return
	}
	hash, err := user.HashPassword(req.NewPassword)
	if err != nil {
		respondError(w, "failed to hash password", err, http.StatusInternalServerError)
		return
	}
	if err := h.userRepo.SetPassword(r.Context(), *id.UserID, hash); err != nil {
		respondError(w, "failed to set password", err, http.StatusInternalServerError)
		return
	}
	h.pwThrottle.RecordSuccess(key)
	// Revoke every session (the caller's too): anything that held the old
	// password's sessions dies with it.
	h.revokeUserSessions(r, key)
	debuglog.Info("userpassword: changed", "username", id.Username)
	writeJSON(w, map[string]bool{"ok": true})
}
