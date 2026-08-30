package adminauth

import (
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
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
// A cookie-authenticated request whose session just slid forward (see
// webauthn.SessionManager.Authenticate) gets its cookie pair re-issued with the
// new lifetime before next runs, since the browser enforces MaxAge on its own.
// cookieSecure is the authcookie.Secure mode the re-issued cookies use.
//
// Moved from internal/api/auth_middleware.go so the WebAuthn and TOTP handlers
// carry their gate with them into the shared package.
func RequireAdminOrSession(
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
	jar authcookie.Jar,
	cookieSecure string,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, res, ok := adminCookieSession(r, sessionMgr, jar, true); ok {
			if !authcookie.IsSafeMethod(r.Method) && !jar.ValidCSRF(r) {
				debuglog.Warn("auth: CSRF check failed", "remote_addr", clientip.From(r), "path", r.URL.Path)
				http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
				return
			}
			if res.Extended {
				RefreshSessionCookies(w, r, jar, tok, res, cookieSecure)
			}
			next.ServeHTTP(w, r)
			return
		}

		// A caller with no bearer at all is told what is missing; a caller
		// carrying one that does not resolve is told it was rejected. The two
		// messages stay distinct, so the bearer branch is asked separately
		// rather than folded into one boolean.
		//
		// Both rejections are logged at warning with the client address, never
		// the token, so repeated attempts against the admin surface are visible
		// to abuse detection without polluting the operator-actionable Error
		// stream. The messages match the ones the gateway's own admin gate
		// emits (internal/api/admin.go), so one log parser covers both binaries;
		// the caller-controlled path goes last.
		token, ok := util.ParseBearerToken(r)
		if !ok {
			debuglog.Warn("auth: admin request missing bearer token", "remote_addr", clientip.From(r), "path", r.URL.Path)
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		if validAdminBearer(r, token, adminMgr, sessionMgr, totpEnabled, true) {
			next.ServeHTTP(w, r)
			return
		}

		debuglog.Warn("auth: admin request with invalid token", "remote_addr", clientip.From(r), "path", r.URL.Path)
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
// to bound how long a revoked credential keeps a stream alive. That heartbeat is
// the server's, not the person's, so it verifies without stamping last-seen or
// sliding the session's expiry (webauthn.SessionManager.Verify): otherwise every
// open tab would keep its own session alive with nobody at it.
func ValidAdminOrSession(
	r *http.Request,
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
	jar authcookie.Jar,
) bool {
	if _, _, ok := adminCookieSession(r, sessionMgr, jar, false); ok {
		return true
	}
	token, ok := util.ParseBearerToken(r)
	if !ok {
		return false
	}
	return validAdminBearer(r, token, adminMgr, sessionMgr, totpEnabled, false)
}

// RefreshSessionCookies re-issues the jar's cookie pair for a session whose
// expiry just slid to res.ExpiresAt. Best-effort: a failure to write the
// cookies is logged and the request still proceeds, since the authentication
// itself already passed and the browser merely keeps the older lifetime.
func RefreshSessionCookies(w http.ResponseWriter, r *http.Request, jar authcookie.Jar, token string, res webauthn.AuthResult, cookieSecure string) {
	if err := jar.RefreshSession(w, r, token, authcookie.Secure(r, cookieSecure), time.Until(res.ExpiresAt)); err != nil {
		debuglog.Error("auth: failed to re-issue session cookie", "error", err)
	}
}

// adminCookieSession resolves the admin session riding the jar's session
// cookie: the token, the authentication result (which says whether the expiry
// just slid, so the caller can re-issue the cookie), and whether it is admitted.
// use says whether the request counts as the person using the session (stamp
// last-seen, slide) or is a server-driven re-check (pure lookup).
// The session token rides an HttpOnly cookie instead of an Authorization
// header, named by the caller's jar so the dashboard and Front Desk never read
// each other's cookie when they share a hostname. Only the admin session
// (UserID == "admin") qualifies: a valid but non-admin (UUID) session cookie, or
// an absent/expired cookie, reports false so callers fall through to the header
// path and header (admin-token / bearer) callers stay unaffected.
func adminCookieSession(r *http.Request, sessionMgr *webauthn.SessionManager, jar authcookie.Jar, use bool) (string, webauthn.AuthResult, bool) {
	tok, ok := jar.SessionToken(r)
	if !ok || sessionMgr == nil {
		return "", webauthn.AuthResult{}, false
	}
	res, ok := sessionAuth(r, sessionMgr, tok, use)
	if !ok || string(res.UserID) != "admin" {
		return "", webauthn.AuthResult{}, false
	}
	return tok, res, true
}

// sessionAuth validates a session token, sliding it when the request is the
// person's own use and merely verifying it for a server-driven re-check.
func sessionAuth(r *http.Request, sessionMgr *webauthn.SessionManager, token string, use bool) (webauthn.AuthResult, bool) {
	if use {
		return sessionMgr.Authenticate(r.Context(), token)
	}
	return sessionMgr.Verify(r.Context(), token)
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
	use bool,
) bool {
	if (totpEnabled == nil || !totpEnabled()) && adminMgr.Validate(token) {
		return true
	}
	if sessionMgr == nil {
		return false
	}
	res, ok := sessionAuth(r, sessionMgr, token, use)
	return ok && string(res.UserID) == "admin"
}
