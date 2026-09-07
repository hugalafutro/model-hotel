package api

import (
	"context"
	"errors"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// settingKeyPwnedPasswordCheck is the DB-backed runtime toggle for the
// breached-password check. Absent (the default) reads as enabled, so the
// feature is on out of the box; an operator can turn it off without a
// redeploy. The env kill-switch (PWNED_PASSWORD_CHECK_ENABLED=false) is
// evaluated first and cannot be overridden back on from the DB.
const settingKeyPwnedPasswordCheck = "pwned_password_check_enabled"

// Password-policy rejection reasons, surfaced to the client as the 400 body
// text. They are stable strings the dashboard maps to localized copy, so
// changing their wording is a frontend-visible contract change.
var (
	errPasswordTooShort = errors.New("password must be at least 8 characters")
	errPasswordBreached = errors.New("this password has appeared in a known data breach; choose a different one")
)

// validateNewPassword enforces the password policy for the admin-driven
// create/reset flows: a minimum length, then (when enabled) a Have I Been
// Pwned breach check. It returns errPasswordTooShort or errPasswordBreached on
// a policy failure so callers can respondBadRequest(w, err.Error(), nil), nil
// on success, and — deliberately — nil on any breach-check infrastructure
// failure (fail open). ChangeOwnPassword does not use this helper: it runs the
// breach check only after verifying the current password (see there).
func (h *Handler) validateNewPassword(ctx context.Context, password string) error {
	if len(password) < minPasswordLen {
		return errPasswordTooShort
	}
	if h.passwordBreached(ctx, password) {
		return errPasswordBreached
	}
	return nil
}

// passwordBreached reports whether the breach check is enabled AND positively
// identifies the password as breached. Every other outcome returns false:
// feature disabled by the env kill-switch or the DB toggle, checker not wired,
// or the range endpoint erroring/timing out. That fail-open stance means a HIBP
// outage or an offline/air-gapped deployment can never lock an operator out of
// setting a password.
func (h *Handler) passwordBreached(ctx context.Context, password string) bool {
	if h.pwnedChecker == nil || h.cfg == nil || !h.cfg.PwnedPasswordCheckEnabled {
		return false
	}
	if h.settingsRepo != nil && !h.settingsRepo.GetBool(ctx, settingKeyPwnedPasswordCheck, true) {
		return false
	}
	breached, count, err := h.pwnedChecker.Breached(ctx, password)
	if err != nil {
		// Fail open: never block a password change because the breach service is
		// unreachable. No password material is logged.
		debuglog.Warn("password-policy: breach check failed, allowing password", "error", err)
		return false
	}
	if breached {
		// count is the corpus breach frequency, not password material, so it is
		// safe to log and gives operators a sense of how common the reject was.
		debuglog.Info("password-policy: rejected a breached password", "breach_count", count)
	}
	return breached
}
