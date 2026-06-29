package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// OIDC settings keys (mirrored in settings.AllowedSettings + api.allowedSettings).
const (
	oidcEnabledKey       = "oidc_enabled"
	oidcIssuerURLKey     = "oidc_issuer_url"
	oidcClientIDKey      = "oidc_client_id"
	oidcClientSecretKey  = "oidc_client_secret"
	oidcAllowedEmailsKey = "oidc_allowed_emails"
	oidcPublicBaseURLKey = "oidc_public_base_url"
)

// oidcCallbackPath is the fixed redirect-URI path. The operator registers
// <oidc_public_base_url><oidcCallbackPath> with their IdP.
const oidcCallbackPath = "/api/auth/oidc/callback"

// oidcLoginTTL bounds how long an in-flight login (state/nonce/PKCE) stays
// valid. Long enough for a human to authenticate at the IdP, short enough that
// a leaked cookie is quickly useless.
const oidcLoginTTL = 10 * time.Minute

// oidcCookieName carries the login-state record id across the IdP round trip.
const oidcCookieName = "mh_oidc"

// OIDCSettings is the slice of the settings repository the OIDC handler needs.
// Depending on the interface (not *settings.Repository) keeps this package free
// of a Postgres/pgx dependency so the same handler can back Front Desk's SQLite
// control plane in Phase 2.
type OIDCSettings interface {
	GetBool(ctx context.Context, key string, def bool) bool
	GetWithDefault(ctx context.Context, key, def string) string
}

// OIDCHandler serves the OpenID Connect login endpoints. It is a third front-end
// to the same admin session token minted by passkey and TOTP login: after the
// IdP confirms an allowlisted identity, it calls SessionManager.CreateAuthToken,
// so every downstream gate (RequireAdminOrSession, the proxy, virtual keys)
// keeps working unchanged.
type OIDCHandler struct {
	settings   OIDCSettings
	sessionMgr *webauthn.SessionManager
	ipLimiter  IPLimiterMiddleware
	masterKey  string

	// loginThrottle applies per-IP exponential backoff to the callback, mirroring
	// the TOTP login defense (5 failures, 1s doubling, capped at 5m).
	loginThrottle *totp.Throttle

	// cached holds the lazily-built provider/verifier/oauth2 config. It is rebuilt
	// only when the config fingerprint changes (mirroring RefreshTotpEnabled's
	// cache-refresh intent), so a settings edit applies without a restart and a
	// steady state never re-runs OIDC discovery.
	mu       sync.Mutex
	cached   *oidcRuntime
	cachedFP string
}

// NewOIDCHandler constructs an OIDCHandler. masterKey decrypts the stored client
// secret (encrypted at rest like provider keys).
func NewOIDCHandler(
	settings OIDCSettings,
	sessionMgr *webauthn.SessionManager,
	ipLimiter IPLimiterMiddleware,
	masterKey string,
) *OIDCHandler {
	return &OIDCHandler{
		settings:      settings,
		sessionMgr:    sessionMgr,
		ipLimiter:     ipLimiter,
		masterKey:     masterKey,
		loginThrottle: totp.NewThrottle(5, time.Second, 5*time.Minute),
	}
}

// oidcRuntime is the built, ready-to-use OIDC configuration for one config
// fingerprint.
type oidcRuntime struct {
	enabled      bool
	displayName  string // IdP host, for the login button label
	redirectURL  string
	provider     *oidc.Provider // kept for the UserInfo fallback
	verifier     *oidc.IDTokenVerifier
	oauth2Config *oauth2.Config
	allowed      map[string]bool // lowercased allowlisted emails; empty = deny all
}

// oidcLoginState is the per-login blob persisted in the login-state record and
// validated on callback.
type oidcLoginState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

// Register mounts the OIDC routes. All three are unauthenticated because they
// ARE the login flow; the allowlist (not a bearer token) gates who may complete
// it. Mount on the same unauthenticated group as the WebAuthn/TOTP login routes.
func (h *OIDCHandler) Register(r chi.Router) {
	r.Route("/auth/oidc", func(r chi.Router) {
		r.Get("/status", h.Status)
		r.Get("/start", h.Start)
		r.Get("/callback", h.Callback)
	})
}

// oidcStatusResponse is the public GET /api/auth/oidc/status payload: just
// enough for the login screen to decide whether to show the SSO button and what
// to label it. No secrets, no issuer internals beyond the host.
type oidcStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	DisplayName string `json:"display_name,omitempty"`
}

// Status reports whether SSO is enabled and configured, plus a display name
// (the IdP host) for the button. Public and cheap: it never builds the provider
// or touches the network, so the polled login screen stays light.
func (h *OIDCHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	enabled := h.settings.GetBool(ctx, oidcEnabledKey, false)
	issuer := strings.TrimSpace(h.settings.GetWithDefault(ctx, oidcIssuerURLKey, ""))
	clientID := strings.TrimSpace(h.settings.GetWithDefault(ctx, oidcClientIDKey, ""))
	baseURL := strings.TrimSpace(h.settings.GetWithDefault(ctx, oidcPublicBaseURLKey, ""))

	resp := oidcStatusResponse{}
	if enabled && issuer != "" && clientID != "" && baseURL != "" {
		resp.Enabled = true
		if u, err := url.Parse(issuer); err == nil && u.Host != "" {
			resp.DisplayName = u.Host
		}
	}
	writeJSON(w, resp)
}

// Start begins the authorization-code flow: it mints state/nonce/PKCE-verifier,
// persists them in a single-use login-state record (id in an HttpOnly cookie),
// and 302s to the IdP's authorization endpoint.
func (h *OIDCHandler) Start(w http.ResponseWriter, r *http.Request) {
	rt, err := h.runtime(r.Context())
	if err != nil {
		respondError(w, "SSO is not available", err, http.StatusServiceUnavailable)
		return
	}
	if rt == nil || !rt.enabled {
		respondError(w, "SSO is not enabled", nil, http.StatusBadRequest)
		return
	}

	state, err := randToken()
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return
	}
	nonce, err := randToken()
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()

	blob, err := json.Marshal(oidcLoginState{State: state, Nonce: nonce, Verifier: verifier})
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return
	}
	id, err := h.sessionMgr.CreateLoginState(r.Context(), blob, oidcLoginTTL)
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oidcCookieName,
		Value:    id.String(),
		Path:     "/api/auth/oidc",
		MaxAge:   int(oidcLoginTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		// Lax (not Strict) so the cookie survives the top-level GET redirect
		// back from the IdP; the state+nonce+PKCE triple carries the CSRF/replay
		// defense, not the cookie's SameSite mode.
		SameSite: http.SameSiteLaxMode,
	})

	authURL := rt.oauth2Config.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback completes the flow: it validates the login-state record (single use),
// state, PKCE, and the IdP error param; exchanges the code; verifies the ID
// token (iss/aud/exp/signature via go-oidc, plus nonce here); enforces the
// verified-email allowlist; and on success mints a normal session token and
// redirects to the SPA with the token in the URL fragment (never sent back to
// the server, so it can't be logged). All denials redirect with an error code.
func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Per-IP backoff (defense in depth atop the /api per-IP rate limit).
	// Redirect back to the SPA with an error fragment like every other failure
	// (Retry-After still set as a hint) rather than a plaintext 429: the callback
	// is always a browser navigation, so a raw 429 page would strand the user
	// off the SPA. The throttle still blocks all work before this point.
	throttleKey := h.ipLimiter.ClientIP(r)
	if ok, retry := h.loginThrottle.Allowed(throttleKey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		debuglog.Warn("oidc: callback throttled", "remote_addr", r.RemoteAddr)
		h.redirectError(w, r, "throttled")
		return
	}

	rt, err := h.runtime(ctx)
	if err != nil || rt == nil || !rt.enabled {
		h.redirectError(w, r, "unavailable")
		return
	}

	// The login-state cookie is consumed (single use) regardless of outcome.
	cookie, err := r.Cookie(oidcCookieName)
	h.clearCookie(w)
	if err != nil {
		h.fail(w, r, throttleKey, "missing login state", nil)
		return
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		h.fail(w, r, throttleKey, "bad login state", err)
		return
	}
	blob, err := h.sessionMgr.ConsumeLoginState(ctx, id)
	if err != nil {
		h.fail(w, r, throttleKey, "expired login state", nil)
		return
	}
	var st oidcLoginState
	if err := json.Unmarshal(blob, &st); err != nil {
		h.fail(w, r, throttleKey, "corrupt login state", err)
		return
	}

	// An IdP-reported error (e.g. access_denied) short-circuits before any token work.
	if e := r.URL.Query().Get("error"); e != "" {
		debuglog.Warn("oidc: idp returned error", "error", e)
		h.fail(w, r, throttleKey, "provider declined", nil)
		return
	}
	// CSRF: the returned state must match what we issued. Constant-time compare
	// for consistency with the rest of the codebase (session.go); the record is
	// already single-use so this is defense in depth, not load-bearing.
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(st.State)) != 1 {
		h.fail(w, r, throttleKey, "state mismatch", nil)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.fail(w, r, throttleKey, "missing code", nil)
		return
	}

	// Exchange the code (with the PKCE verifier) for tokens.
	token, err := rt.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		h.fail(w, r, throttleKey, "code exchange failed", err)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		h.fail(w, r, throttleKey, "no id_token in response", nil)
		return
	}
	// Verify covers issuer, audience, expiry, and JWKS signature.
	idToken, err := rt.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		h.fail(w, r, throttleKey, "id_token verification failed", err)
		return
	}
	// Replay defense: the nonce baked into the auth request must come back.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(st.Nonce)) != 1 {
		h.fail(w, r, throttleKey, "nonce mismatch", nil)
		return
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		h.fail(w, r, throttleKey, "failed to parse claims", err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	emailVerified := claims.EmailVerified
	// Many IdPs (Authelia 4.38+, others) keep email out of the ID token by
	// default and only expose it at the UserInfo endpoint. Fall back to UserInfo
	// when the ID token carries no email, so SSO works without forcing the
	// operator to configure a custom claims policy. Only one extra call, and
	// only when needed.
	if email == "" {
		ui, uiErr := rt.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if uiErr != nil {
			h.fail(w, r, throttleKey, "userinfo fetch failed", uiErr)
			return
		}
		// OIDC core 5.3.2: the UserInfo subject MUST match the verified ID-token
		// subject. Without this bind, an access token could surface a different
		// identity's (possibly allowlisted) email and authorize the wrong user.
		if ui.Subject != idToken.Subject {
			h.fail(w, r, throttleKey, "userinfo subject mismatch", nil)
			return
		}
		email = strings.ToLower(strings.TrimSpace(ui.Email))
		emailVerified = ui.EmailVerified
	}
	// Require a verified email: an unverified address is attacker-settable at
	// many IdPs, so it must never satisfy the allowlist.
	if email == "" || !emailVerified {
		debuglog.Warn("oidc: login denied: unverified or missing email",
			"sub", idToken.Subject, "iss", idToken.Issuer)
		h.fail(w, r, throttleKey, "email not verified", nil)
		return
	}
	// Allowlist matches on email; we audit on (iss, sub) — the IdP's stable,
	// opaque per-user id that, unlike email, never changes or gets reassigned.
	if !rt.allowed[email] {
		debuglog.Warn("oidc: login denied: email not allowlisted",
			"email_masked", maskEmail(email), "sub", idToken.Subject, "iss", idToken.Issuer)
		h.fail(w, r, throttleKey, "not allowed", nil)
		return
	}

	// Mint the same session token as passkey/TOTP login: single admin identity,
	// no passkey credential to cascade-revoke (nil credentialID).
	sessionToken, err := h.sessionMgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		h.fail(w, r, throttleKey, "failed to create session", err)
		return
	}
	h.loginThrottle.RecordSuccess(throttleKey)
	debuglog.Info("oidc: login success",
		"email_masked", maskEmail(email), "sub", idToken.Subject, "iss", idToken.Issuer)

	// Deliver the token in the URL *fragment*: the SPA reads it on mount, stores
	// it, and scrubs the URL. The fragment is never sent to the server on the
	// follow-up request (no Referer leak, nothing in our own request logs). It
	// does, however, appear in this 302's Location response header, so a proxy
	// that logs response headers would capture it -- operators should redact
	// `Location` on /api/auth/oidc/callback in their access logs (see README).
	http.Redirect(w, r, "/#oidc_token="+url.QueryEscape(sessionToken), http.StatusFound)
}

// fail records a per-IP failure, logs the reason, and redirects the browser back
// to the SPA login screen with a generic error marker. err (if any) is logged
// server-side only; the user-facing reason stays coarse to avoid an oracle.
func (h *OIDCHandler) fail(w http.ResponseWriter, r *http.Request, throttleKey, reason string, err error) {
	h.loginThrottle.RecordFailure(throttleKey)
	if err != nil {
		debuglog.Warn("oidc: callback failed", "reason", reason, "error", err, "remote_addr", r.RemoteAddr)
	} else {
		debuglog.Warn("oidc: callback failed", "reason", reason, "remote_addr", r.RemoteAddr)
	}
	h.redirectError(w, r, "failed")
}

// redirectError sends the browser back to the SPA with an error code in the
// fragment so the login screen can show a message. The code is intentionally
// coarse (no per-cause detail leaks to the client).
func (h *OIDCHandler) redirectError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/#oidc_error="+url.QueryEscape(code), http.StatusFound)
}

// clearCookie expires the login-state cookie.
func (h *OIDCHandler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcCookieName,
		Value:    "",
		Path:     "/api/auth/oidc",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// runtime returns the built OIDC runtime for the current settings, rebuilding it
// (including OIDC discovery, a network call) only when the config fingerprint
// changes. A disabled or under-configured state returns a runtime with
// enabled=false and no error. The build runs outside the lock so concurrent
// logins don't serialize on a network call; a rare double build is harmless
// (last writer wins).
func (h *OIDCHandler) runtime(ctx context.Context) (*oidcRuntime, error) {
	enabled := h.settings.GetBool(ctx, oidcEnabledKey, false)
	issuer := strings.TrimSpace(h.settings.GetWithDefault(ctx, oidcIssuerURLKey, ""))
	clientID := strings.TrimSpace(h.settings.GetWithDefault(ctx, oidcClientIDKey, ""))
	clientSecretEnc := h.settings.GetWithDefault(ctx, oidcClientSecretKey, "")
	baseURL := strings.TrimSpace(h.settings.GetWithDefault(ctx, oidcPublicBaseURLKey, ""))
	allowedRaw := h.settings.GetWithDefault(ctx, oidcAllowedEmailsKey, "")

	fp := strings.Join([]string{
		strconv.FormatBool(enabled), issuer, clientID, clientSecretEnc, baseURL, allowedRaw,
	}, "\x00")

	h.mu.Lock()
	if h.cached != nil && h.cachedFP == fp {
		rt := h.cached
		h.mu.Unlock()
		return rt, nil
	}
	h.mu.Unlock()

	rt, err := h.build(ctx, enabled, issuer, clientID, clientSecretEnc, baseURL, allowedRaw)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	h.cached = rt
	h.cachedFP = fp
	h.mu.Unlock()
	return rt, nil
}

// build constructs the runtime for a config snapshot. Under-configured states
// resolve to a disabled runtime (no error); only a genuine discovery/decrypt
// failure returns an error.
func (h *OIDCHandler) build(ctx context.Context, enabled bool, issuer, clientID, clientSecretEnc, baseURL, allowedRaw string) (*oidcRuntime, error) {
	if !enabled || issuer == "" || clientID == "" || baseURL == "" {
		return &oidcRuntime{enabled: false}, nil
	}

	clientSecret := ""
	if clientSecretEnc != "" {
		if h.masterKey == "" {
			return nil, fmt.Errorf("MASTER_KEY not configured")
		}
		dec, err := auth.DecryptString(clientSecretEnc, h.masterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt client secret: %w", err)
		}
		clientSecret = dec
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	redirectURL := strings.TrimRight(baseURL, "/") + oidcCallbackPath
	rt := &oidcRuntime{
		enabled:     true,
		redirectURL: redirectURL,
		provider:    provider,
		verifier:    provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth2Config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		allowed: parseAllowlist(allowedRaw),
	}
	if u, err := url.Parse(issuer); err == nil && u.Host != "" {
		rt.displayName = u.Host
	}
	return rt, nil
}

// parseAllowlist splits a comma/newline/space-separated email list into a
// lowercased set. An empty result means deny all (fail closed).
func parseAllowlist(raw string) map[string]bool {
	set := make(map[string]bool)
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ';'
	}) {
		e := strings.ToLower(strings.TrimSpace(f))
		if e != "" {
			set[e] = true
		}
	}
	return set
}

// randToken returns a 32-byte hex random string for state/nonce.
func randToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// maskEmail reduces an email to a non-identifying form for audit logs:
// "alice@example.com" -> "a***@example.com". Never logs the full local part.
func maskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return "***"
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 1 {
		return "*" + domain
	}
	return local[:1] + "***" + domain
}
