package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// UserTotpFactory builds a TOTP repository bound to one user's rows
// (user_totp tables). Wired from main.go; nil disables the whole surface.
// Aliased to totp.UserFactory so the login handler and this surface share one
// signature (a change to the factory shape touches a single declaration).
type UserTotpFactory = totp.UserFactory

// SetUserTotp wires the per-user TOTP factory into the self-service and
// admin-reset endpoints.
func (h *Handler) SetUserTotp(factory UserTotpFactory) {
	h.userTotp = factory
}

// RegisterUserTotp mounts the self-service TOTP endpoints for users-row
// identities. Mounted inside the authenticated group; every handler resolves
// the caller's own user id from the request identity, so no cross-user access
// is expressible. The env-token admin has no users row and is pointed at the
// existing single-admin /api/totp machinery instead. The user_totp tables are
// instance-local (not fleet-synced), so there is no managedWriteGuard here.
func (h *Handler) RegisterUserTotp(r chi.Router) {
	r.Route("/auth/totp", func(r chi.Router) {
		r.Get("/status", h.UserTotpStatus)
		r.Post("/enroll/start", h.UserTotpEnrollStart)
		r.Post("/enroll/verify", h.UserTotpEnrollVerify)
		r.Post("/disable", h.UserTotpDisable)
	})
}

// callerTotpRepo resolves the caller's users-row identity into a bound TOTP
// repository. Writes the error response and returns ok=false when the surface
// is not wired or the caller is not a users-row account.
func (h *Handler) callerTotpRepo(w http.ResponseWriter, r *http.Request) (*totp.Repository, *user.Identity, bool) {
	id := user.IdentityFrom(r.Context())
	if h.userTotp == nil {
		http.Error(w, "per-user TOTP is not available", http.StatusNotFound)
		return nil, nil, false
	}
	if id == nil || id.UserID == nil {
		// Env-token admin (or a route mounted outside the auth gate): this
		// surface only manages users-row accounts.
		http.Error(w, "not a user account; admins use /api/totp", http.StatusBadRequest)
		return nil, nil, false
	}
	return h.userTotp(*id.UserID), id, true
}

// UserTotpStatus reports the caller's own TOTP state for the Security UI.
func (h *Handler) UserTotpStatus(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.callerTotpRepo(w, r)
	if !ok {
		return
	}
	enabled, err := repo.IsEnabled(r.Context())
	if err != nil {
		respondError(w, "failed to read TOTP status", err, http.StatusInternalServerError)
		return
	}
	resp := map[string]any{"enabled": enabled}
	if enabled {
		if at, ok, err := repo.EnabledAt(r.Context()); err == nil && ok {
			resp["enabled_at"] = at.UTC().Format(time.RFC3339)
		}
		if info, err := repo.Info(r.Context()); err == nil {
			resp["recovery_remaining"] = info.RecoveryRemaining
			resp["recovery_total"] = info.RecoveryTotal
		}
	}
	writeJSON(w, resp)
}

// UserTotpEnrollStart generates a provisional secret for the caller and
// returns the otpauth URI (QR) + base32 secret. Refused while TOTP is active,
// mirroring the admin flow: rotating requires an authorized disable first, so
// the enforcement gate never moves under a live second factor.
func (h *Handler) UserTotpEnrollStart(w http.ResponseWriter, r *http.Request) {
	repo, id, ok := h.callerTotpRepo(w, r)
	if !ok {
		return
	}
	enabled, err := repo.IsEnabled(r.Context())
	if err != nil {
		respondError(w, "failed to read TOTP status", err, http.StatusInternalServerError)
		return
	}
	if enabled {
		http.Error(w, "disable TOTP before re-enrolling", http.StatusConflict)
		return
	}
	uri, secret, err := repo.EnrollAs(r.Context(), id.Username)
	if err != nil {
		respondError(w, "totp: enroll failed", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"uri": uri, "secret": secret})
}

// UserTotpEnrollVerify confirms the provisional secret with a live code,
// enables the second factor, and returns the single-use recovery codes
// (generated BEFORE enable so a failure can never leave 2FA on without them).
// The caller's session stays valid: unlike the admin flow, enabling a user's
// TOTP changes login requirements only, not the bearer they already hold.
func (h *Handler) UserTotpEnrollVerify(w http.ResponseWriter, r *http.Request) {
	repo, id, ok := h.callerTotpRepo(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	verified, err := repo.Verify(r.Context(), req.Code)
	if err != nil {
		respondError(w, "totp: verify failed", err, http.StatusInternalServerError)
		return
	}
	if !verified {
		respondBadRequest(w, "invalid TOTP code", nil)
		return
	}
	codes, err := repo.GenerateRecoveryCodes(r.Context())
	if err != nil {
		respondError(w, "totp: failed to generate recovery codes", err, http.StatusInternalServerError)
		return
	}
	if err := repo.Enable(r.Context()); err != nil {
		respondError(w, "totp: enable failed", err, http.StatusInternalServerError)
		return
	}
	debuglog.Info("usertotp: enabled", "username", id.Username)
	writeJSON(w, map[string]any{"recovery_codes": codes})
}

// UserTotpDisable turns the caller's second factor off. Requires a valid
// current TOTP or recovery code (401 on mismatch), consumed atomically with
// the disable so a transient failure cannot burn the code and leave 2FA on.
// Failed codes back off per user (shared with the password-change throttle):
// a hijacked session must not be a free brute-force oracle for the 6-digit
// window, matching the login and password-change paths.
func (h *Handler) UserTotpDisable(w http.ResponseWriter, r *http.Request) {
	repo, id, ok := h.callerTotpRepo(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}
	key := id.UserID.String()
	if ok, retry := h.pwThrottle.Allowed(key); !ok {
		debuglog.Warn("usertotp: disable throttled", "username", id.Username)
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}
	authorized, err := repo.DisableWithCode(r.Context(), req.Code)
	if err != nil {
		respondError(w, "totp: disable failed", err, http.StatusInternalServerError)
		return
	}
	if !authorized {
		h.pwThrottle.RecordFailure(key)
		http.Error(w, "invalid TOTP or recovery code", http.StatusUnauthorized)
		return
	}
	h.pwThrottle.RecordSuccess(key)
	debuglog.Info("usertotp: disabled", "username", id.Username)
	writeJSON(w, map[string]bool{"ok": true})
}

// ResetUserTotp is the admin lockout-recovery path: it unconditionally
// disables TOTP and deletes the recovery codes for the target user. Admin-only
// (mounted under /users). Not managed-write-guarded: user_totp is
// instance-local state that a fleet sync never touches.
func (h *Handler) ResetUserTotp(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	if h.userTotp == nil {
		http.Error(w, "per-user TOTP is not available", http.StatusNotFound)
		return
	}
	// Confirm the target exists so a typo'd id is a 404, not a silent no-op.
	if _, err := h.userRepo.Get(r.Context(), id); err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err := h.userTotp(id).Disable(r.Context()); err != nil {
		respondError(w, "failed to reset TOTP", err, http.StatusInternalServerError)
		return
	}
	debuglog.Info("usertotp: admin reset", "user_id", id.String())
	writeJSON(w, map[string]bool{"ok": true})
}
