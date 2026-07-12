package adminauth

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// RequireAdminOrSession wraps next so the request proceeds only when the bearer
// token is either the raw admin token or a session token belonging to the admin
// identity.
//
// The raw admin token is accepted ONLY when TOTP is disabled. With TOTP on, the
// raw admin token is a first factor only and must not unlock admin-gated
// endpoints (passkey/TOTP management), or a bare admin-token bearer could bypass
// the second factor.
//
// The session branch must resolve the session's identity and admit only admin
// sessions (UserID == "admin"): passkey, TOTP, OIDC-admin, and GitHub-admin
// logins all mint sessions carrying []byte("admin"). Multi-user password / SSO
// user logins share the same SessionManager but carry a user UUID, so a bare
// sessionMgr.Validate would let any authenticated regular user reach these
// admin-only routes and mint an admin session (CWE-863 privilege escalation).
//
// Moved from internal/api/auth_middleware.go so the WebAuthn and TOTP handlers
// carry their gate with them into the shared package.
func RequireAdminOrSession(
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := util.ParseBearerToken(r)
		if !ok {
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		if (totpEnabled == nil || !totpEnabled()) && adminMgr.Validate(token) {
			next.ServeHTTP(w, r)
			return
		}

		if sessionMgr != nil {
			// Admin-only gate: resolve the session's identity and admit only the
			// admin session (UserID == "admin"). A UUID-carrying multi-user/SSO
			// user session must NOT pass, or a regular user could enroll admin
			// TOTP or register an admin passkey and escalate to full admin.
			if userID, ok := sessionMgr.TokenUser(r.Context(), token); ok && string(userID) == "admin" {
				next.ServeHTTP(w, r)
				return
			}
		}

		http.Error(w, "Invalid admin token or session token", http.StatusUnauthorized)
	})
}
