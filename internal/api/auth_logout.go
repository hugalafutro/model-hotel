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
//
// Credential-gated against forced-logout CSRF. A cross-site POST cannot carry
// either auth cookie (both are SameSite=Strict) nor an Authorization header, so
// it arrives here bare; emitting Set-Cookie deletions unconditionally would let
// any third-party page log the victim out. Requests with no credential at all
// therefore get a success answer with no cookie headers - there is nothing to
// log out. Same-site logout is unaffected, including an already-revoked or
// otherwise invalid session, because the cookies still ride along.
//
// Deliberately not gated on the CSRF double-submit header instead: SameSite
// strips the CSRF cookie from a cross-site request too, so the server cannot
// tell "no session" from "cross-site", and requiring the header would break
// bearer-token callers that never have one.
func (h *Handler) AuthLogout(w http.ResponseWriter, r *http.Request) {
	tok, ok := authcookie.SessionToken(r)
	if !ok {
		tok, ok = util.ParseBearerToken(r)
	}

	if ok && h.webauthnSessionMgr != nil {
		h.webauthnSessionMgr.RevokeAuthToken(r.Context(), tok)
	}

	// A stray CSRF cookie with no session still counts as the caller's own
	// state, so clear on either cookie rather than only on a full session.
	// Non-empty, matching SessionToken's treatment of the session cookie: a
	// bare "mh_csrf=" carries no state and must not stand in for a credential.
	csrf, err := r.Cookie(authcookie.CSRFCookie)
	csrfPresent := err == nil && csrf.Value != ""

	if ok || csrfPresent {
		authcookie.ClearSession(w, authcookie.Secure(r, h.cfg.CookieSecure))
	}

	writeJSON(w, map[string]bool{"success": true})
}
