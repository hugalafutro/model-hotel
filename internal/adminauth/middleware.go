package adminauth

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
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
		// Cookie path (browser). The session token rides an HttpOnly cookie
		// instead of an Authorization header. This branch is additive and keeps
		// the same admin-only gate: it admits only the admin session
		// (UserID == "admin"). A valid but non-admin (UUID) session cookie, or
		// an absent/expired cookie, falls through to the header logic below so
		// header (admin-token / bearer) callers stay unaffected. On unsafe
		// methods a matching CSRF header is also required.
		if tok, ok := authcookie.SessionToken(r); ok && sessionMgr != nil {
			if userID, ok := sessionMgr.TokenUser(r.Context(), tok); ok && string(userID) == "admin" {
				if !authcookie.IsSafeMethod(r.Method) && !authcookie.ValidCSRF(r) {
					http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			// Non-admin or invalid cookie session: fall through to header logic.
		}

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
