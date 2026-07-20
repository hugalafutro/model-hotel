package api

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// AuthLogout revokes the caller's session (if any) and clears the auth cookies.
// Always mounted, unlike the passkey-gated /webauthn/logout, so the dashboard
// can log out regardless of whether WebAuthn is configured. Safe unauthenticated:
// it only revokes the token the caller presents and clears the caller's own cookies.
func (h *Handler) AuthLogout(w http.ResponseWriter, r *http.Request) {
	tok, ok := authcookie.SessionToken(r)
	if !ok {
		tok, ok = util.ParseBearerToken(r)
	}

	if ok && h.webauthnSessionMgr != nil {
		h.webauthnSessionMgr.RevokeAuthToken(r.Context(), tok)
	}

	authcookie.ClearSession(w, authcookie.Secure(r, h.cfg.CookieSecure))

	writeJSON(w, map[string]bool{"success": true})
}
