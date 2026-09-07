package adminauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// ssoStateBytes is the entropy behind each SSO state and nonce value.
const ssoStateBytes = 32

// ssoLoginState is the per-login blob an SSO ceremony persists in its
// single-use login-state record. Only the state token is read by the shared
// consume path; each provider adds whatever else its protocol needs (OIDC
// carries a nonce and a PKCE verifier, GitHub neither).
type ssoLoginState interface {
	stateToken() string
}

// ssoLogin is the ceremony scaffolding OIDC and GitHub share: the single-use
// login-state cookie, the per-IP failure backoff, the coarse error redirect,
// and the session hand-off. Both handlers embed it, so the cookie lifecycle and
// the CSRF/state checks have one body and cannot drift apart.
type ssoLogin struct {
	// name prefixes this flow's log lines ("oidc", "github").
	name string
	// cookieName and cookiePath scope the login-state cookie to the flow's
	// route, so the two ceremonies never read each other's record id.
	cookieName, cookiePath string
	// ttl bounds how long an in-flight login stays valid.
	ttl        time.Duration
	sessionMgr *webauthn.SessionManager
	ipLimiter  IPLimiterMiddleware
	throttle   *totp.Throttle

	// jar names the cookie pair this handler's app owns (dashboard vs Front
	// Desk), so two apps on one hostname cannot overwrite each other's session.
	jar authcookie.Jar
	// cookieSecure ("auto"/"always"/"never") resolves the session cookie's
	// Secure attribute; only consulted when useCookieAuth is true.
	cookieSecure string
	// useCookieAuth delivers the session over the jar's HttpOnly cookie and a
	// clean redirect; false delivers it in the URL fragment under
	// tokenFragmentKey for header-bearer clients.
	useCookieAuth bool
	// tokenFragmentKey names the fragment slot the SPA reads the token from in
	// header-bearer mode.
	tokenFragmentKey string
}

// throttled applies the per-IP backoff. It returns the throttle key and whether
// the caller may proceed; a blocked caller has already been answered with a
// redirect back to the SPA (Retry-After set as a hint) rather than a plaintext
// 429, since the callback is always a browser navigation.
func (s *ssoLogin) throttled(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := s.ipLimiter.ClientIP(r)
	ok, retry := s.throttle.Allowed(key)
	if ok {
		return key, true
	}
	httpx.SetRetryAfter(w, retry)
	debuglog.Warn(s.name+": callback throttled", "remote_addr", clientip.From(r))
	s.redirectError(w, r, "throttled")
	return key, false
}

// beginState persists st as a single-use login-state record and puts its id in
// the flow's HttpOnly cookie. It writes its own error response and reports
// whether the caller may continue to the provider redirect.
func (s *ssoLogin) beginState(ctx context.Context, w http.ResponseWriter, st any) bool {
	blob, err := json.Marshal(st)
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return false
	}
	id, err := s.sessionMgr.CreateLoginState(ctx, blob, s.ttl)
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    id.String(),
		Path:     s.cookiePath,
		MaxAge:   int(s.ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		// Lax (not Strict) so the cookie survives the top-level GET redirect
		// back from the provider; the single-use state record carries the
		// CSRF/replay defense, not the cookie's SameSite mode.
		SameSite: http.SameSiteLaxMode,
	})
	return true
}

// consumeState runs the callback prologue every SSO flow shares: expire the
// login-state cookie (single use, whatever the outcome), read the record it
// named, decode it into dst, reject a provider-reported error, constant-time
// compare the returned state, and require an authorization code. It returns the
// code and whether the caller may continue; every miss has already recorded a
// failure and redirected.
func (s *ssoLogin) consumeState(w http.ResponseWriter, r *http.Request, throttleKey string, dst ssoLoginState) (string, bool) {
	// Clear the cookie first so a stale record id never lingers past a failure.
	s.clearCookie(w)

	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		s.fail(w, r, throttleKey, "missing login state", nil)
		return "", false
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		s.fail(w, r, throttleKey, "bad login state", err)
		return "", false
	}
	blob, err := s.sessionMgr.ConsumeLoginState(r.Context(), id)
	if err != nil {
		s.fail(w, r, throttleKey, "expired login state", nil)
		return "", false
	}
	if err := json.Unmarshal(blob, dst); err != nil {
		s.fail(w, r, throttleKey, "corrupt login state", err)
		return "", false
	}

	// A provider-reported error (e.g. access_denied) short-circuits before any
	// token work.
	if e := r.URL.Query().Get("error"); e != "" {
		debuglog.Warn(s.name+": provider returned error", "error", e)
		s.fail(w, r, throttleKey, "provider declined", nil)
		return "", false
	}
	// CSRF: the returned state must match what we issued. The record is already
	// single-use, so the constant-time compare is defense in depth.
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(dst.stateToken())) != 1 {
		s.fail(w, r, throttleKey, "state mismatch", nil)
		return "", false
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		s.fail(w, r, throttleKey, "missing code", nil)
		return "", false
	}
	return code, true
}

// finishLogin mints the same session token passkey and TOTP login mint, carrying
// the resolved identity handle (nil credentialID: no passkey to cascade-revoke),
// and delivers it. In cookie mode the session rides the jar's HttpOnly cookie
// and the browser is redirected to a clean "/", so the token never appears in
// the URL or the 302's Location header. In header-bearer mode it rides the URL
// *fragment*, which the SPA reads on mount and scrubs; the fragment is never
// sent back to the server, though it does appear in this 302's Location
// response header, so operators should redact Location on the callback route in
// their access logs.
func (s *ssoLogin) finishLogin(w http.ResponseWriter, r *http.Request, throttleKey string, handle []byte, logAttrs ...any) {
	sessionToken, err := s.sessionMgr.CreateAuthToken(r.Context(), handle, nil, webauthn.MetaFromRequest(r, s.ipLimiter))
	if err != nil {
		s.fail(w, r, throttleKey, "failed to create session", err)
		return
	}
	s.throttle.RecordSuccess(throttleKey)
	debuglog.Info(s.name+": login success", logAttrs...)

	if s.useCookieAuth {
		if err := s.jar.SetSession(w, sessionToken, authcookie.Secure(r, s.cookieSecure), webauthn.AuthTokenTTL); err != nil {
			debuglog.Error(s.name+": set session cookie failed", "error", err)
			s.redirectError(w, r, "session_error")
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/#"+s.tokenFragmentKey+"="+url.QueryEscape(sessionToken), http.StatusFound)
}

// fail records a per-IP failure, logs the reason, and redirects the browser back
// to the SPA login screen with a generic error marker. err (if any) is logged
// server-side only; the user-facing reason stays coarse to avoid an oracle.
func (s *ssoLogin) fail(w http.ResponseWriter, r *http.Request, throttleKey, reason string, err error) {
	s.throttle.RecordFailure(throttleKey)
	if err != nil {
		debuglog.Warn(s.name+": callback failed", "remote_addr", clientip.From(r), "reason", reason, "error", err)
	} else {
		debuglog.Warn(s.name+": callback failed", "remote_addr", clientip.From(r), "reason", reason)
	}
	s.redirectError(w, r, "failed")
}

// redirectError sends the browser back to the SPA with a coarse error code in
// the shared #oidc_error= fragment slot the SPA already consumes for every SSO
// failure. If a third SSO provider is ever added, rename it to a neutral
// sso_error across the handlers and the SPA's consume helpers in lockstep.
func (s *ssoLogin) redirectError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/#oidc_error="+url.QueryEscape(code), http.StatusFound)
}

// clearCookie expires the login-state cookie.
func (s *ssoLogin) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     s.cookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// newSSOThrottle is the per-IP failure backoff every SSO callback uses, matching
// the TOTP login defense: 5 failures, 1s doubling, capped at 5m.
func newSSOThrottle() *totp.Throttle {
	return totp.NewThrottle(5, time.Second, 5*time.Minute)
}

// decryptClientSecret decrypts an SSO client secret stored encrypted at rest,
// the same way provider keys are. An empty stored value yields an empty secret;
// a stored value with no MASTER_KEY is a configuration fault, not a blank secret.
func decryptClientSecret(enc, masterKey string) (string, error) {
	if enc == "" {
		return "", nil
	}
	if masterKey == "" {
		return "", fmt.Errorf("MASTER_KEY not configured")
	}
	dec, err := auth.DecryptString(enc, masterKey)
	if err != nil {
		return "", fmt.Errorf("decrypt client secret: %w", err)
	}
	return dec, nil
}
