package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// adminTokenExchangeRequest is the JSON body of POST /api/auth/admin-exchange.
type adminTokenExchangeRequest struct {
	AdminToken string `json:"admin_token"`
}

// AdminTokenExchange trades a raw global admin token for an HttpOnly session
// cookie so the dashboard never has to keep the raw admin token in the browser.
// It is a login front-end (the exchange IS the login) and therefore lives in
// the auth-exempt route group. When TOTP 2FA is enabled the admin token alone
// is not a sufficient credential, so this refuses to mint a session and directs
// callers to the /api/totp/login flow instead.
func (h *Handler) AdminTokenExchange(w http.ResponseWriter, r *http.Request) {
	var req adminTokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AdminToken == "" {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	// With 2FA on, the admin token is only a first factor; minting a full
	// session from it here would bypass TOTP. Check before validating so a
	// valid admin token still cannot short-circuit the second factor.
	if h.TotpEnabled() {
		respondBadRequest(w, "use TOTP login", nil)
		return
	}

	// SetWebAuthnSessionManager is wired unconditionally in production, but guard
	// defensively so a misconfigured build fails closed rather than panicking.
	if h.webauthnSessionMgr == nil {
		http.Error(w, "session manager unavailable", http.StatusInternalServerError)
		return
	}

	// Validate before minting so an invalid admin token never yields a session.
	if !h.adminMgr.Validate(req.AdminToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tok, err := h.webauthnSessionMgr.CreateAuthToken(r.Context(), []byte("admin"), nil)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	if err := authcookie.SetSession(w, tok, authcookie.Secure(r, h.cfg.CookieSecure), webauthn.AuthTokenTTL); err != nil {
		http.Error(w, "failed to set session cookie", http.StatusInternalServerError)
		return
	}

	// Never echo the admin token nor the freshly minted session token; the
	// session travels only in the HttpOnly cookie set above.
	writeJSON(w, map[string]bool{"success": true})
}

// RegisterAuthExchange mounts the admin-token exchange endpoint and the
// always-available logout endpoint. Both must be registered in the
// auth-exempt group: the exchange runs before any session exists, and
// logout must work even for an already-expired or otherwise invalid
// session. Mounted under /api, this resolves to POST /api/auth/admin-exchange
// and POST /api/auth/logout.
func (h *Handler) RegisterAuthExchange(r chi.Router) {
	r.Post("/auth/admin-exchange", h.AdminTokenExchange)
	r.Post("/auth/logout", h.AuthLogout)
}
