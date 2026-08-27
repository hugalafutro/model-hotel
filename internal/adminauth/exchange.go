package adminauth

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// TokenExchange trades the raw admin token for an HttpOnly session cookie so
// the SPA never keeps the raw token in browser storage. It is a login
// front-end (the exchange IS the login): mount it auth-exempt beside the
// other login ceremonies, behind the same per-IP limiter. With TOTP enabled
// the raw token is only a first factor, so the exchange refuses and the
// client goes through /totp/login instead.
// ips resolves the client address for the minted session's device metadata;
// nil means forwarded headers are never trusted and the peer address is used.
func TokenExchange(
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
	jar authcookie.Jar,
	cookieSecure string,
	ips webauthn.ClientIPSource,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AdminToken string `json:"admin_token"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.AdminToken == "" {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if totpEnabled != nil && totpEnabled() {
			http.Error(w, "use TOTP login", http.StatusBadRequest)
			return
		}
		if sessionMgr == nil {
			http.Error(w, "session manager unavailable", http.StatusInternalServerError)
			return
		}
		// Validate before minting so an invalid token never yields a session.
		if !adminMgr.Validate(req.AdminToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		tok, err := sessionMgr.CreateAuthToken(r.Context(), []byte("admin"), nil, webauthn.MetaFromRequest(r, ips))
		if err != nil {
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}
		if err := jar.SetSession(w, tok, authcookie.Secure(r, cookieSecure), webauthn.AuthTokenTTL); err != nil {
			http.Error(w, "failed to set session cookie", http.StatusInternalServerError)
			return
		}
		// Neither the admin token nor the session token goes in the body; the
		// session travels only in the HttpOnly cookie set above.
		writeJSON(w, map[string]bool{"success": true})
	}
}
