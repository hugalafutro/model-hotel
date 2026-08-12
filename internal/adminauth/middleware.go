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
	jar authcookie.Jar,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if validAdminCookie(r, sessionMgr, jar) {
			if !authcookie.IsSafeMethod(r.Method) && !jar.ValidCSRF(r) {
				http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// A caller with no bearer at all is told what is missing; a caller
		// carrying one that does not resolve is told it was rejected. The two
		// messages stay distinct, so the bearer branch is asked separately
		// rather than folded into one boolean.
		token, ok := util.ParseBearerToken(r)
		if !ok {
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		if validAdminBearer(r, token, adminMgr, sessionMgr, totpEnabled) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Invalid admin token or session token", http.StatusUnauthorized)
	})
}

// ValidAdminOrSession reports whether r carries a credential RequireAdminOrSession
// would admit: an admin session token (cookie or bearer), or the raw admin token
// while TOTP is disabled. CSRF is not checked here; it is an unsafe-method concern
// the middleware owns.
//
// It exists for long-lived connections that outlive the middleware: an SSE stream
// is gated once at connect, so the handler re-asks this question on its heartbeat
// to bound how long a revoked credential keeps a stream alive.
func ValidAdminOrSession(
	r *http.Request,
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
	jar authcookie.Jar,
) bool {
	if validAdminCookie(r, sessionMgr, jar) {
		return true
	}
	token, ok := util.ParseBearerToken(r)
	if !ok {
		return false
	}
	return validAdminBearer(r, token, adminMgr, sessionMgr, totpEnabled)
}

// validAdminCookie reports whether r carries an admin session on the jar's
// session cookie. The session token rides an HttpOnly cookie instead of an
// Authorization header, named by the caller's jar so the dashboard and Front
// Desk never read each other's cookie when they share a hostname. Only the
// admin session (UserID == "admin") qualifies: a valid but non-admin (UUID)
// session cookie, or an absent/expired cookie, reports false so callers fall
// through to the header path and header (admin-token / bearer) callers stay
// unaffected.
func validAdminCookie(r *http.Request, sessionMgr *webauthn.SessionManager, jar authcookie.Jar) bool {
	tok, ok := jar.SessionToken(r)
	if !ok || sessionMgr == nil {
		return false
	}
	userID, ok := sessionMgr.TokenUser(r.Context(), tok)
	return ok && string(userID) == "admin"
}

// validAdminBearer reports whether the bearer token parsed from r is admissible:
// the raw admin token with TOTP off, or an admin session token.
//
// The admin-only gate resolves the session's identity and admits only the admin
// session (UserID == "admin"). A UUID-carrying multi-user/SSO user session must
// NOT pass, or a regular user could enroll admin TOTP or register an admin
// passkey and escalate to full admin.
func validAdminBearer(
	r *http.Request,
	token string,
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
) bool {
	if (totpEnabled == nil || !totpEnabled()) && adminMgr.Validate(token) {
		return true
	}
	if sessionMgr == nil {
		return false
	}
	userID, ok := sessionMgr.TokenUser(r.Context(), token)
	return ok && string(userID) == "admin"
}
