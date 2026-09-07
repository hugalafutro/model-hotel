package adminauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
	githuboauth "golang.org/x/oauth2/github"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/netguard"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// GitHub SSO settings keys (mirrored in settings.AllowedSettings +
// api.allowedSettings; github_client_secret is also in api.secretSettingKeys).
const (
	githubEnabledKey       = "github_sso_enabled"
	githubClientIDKey      = "github_client_id"
	githubClientSecretKey  = "github_client_secret"
	githubAllowedEmailsKey = "github_allowed_emails"
	githubPublicBaseURLKey = "github_public_base_url"
)

// githubCallbackPath is the fixed redirect-URI path. The operator registers
// <github_public_base_url><githubCallbackPath> as the OAuth App callback URL.
const githubCallbackPath = "/api/auth/github/callback"

// githubHTTPTimeout bounds each outbound GitHub request (token exchange, /user,
// /user/emails). Mirrors oidcHTTPTimeout; the netguard client's own dialer also
// enforces this as its dial/keepalive timeout.
const githubHTTPTimeout = 15 * time.Second

// githubAPIBaseURL is GitHub's REST API root. It is a package constant (GitHub
// has no discovery document); the value is copied into each githubRuntime so
// tests can build a runtime pointing at a mock server without a production seam.
const githubAPIBaseURL = "https://api.github.com"

// githubScopes requests the read-only profile (id, login) and the user's email
// list. user:email is required to read /user/emails (the only place a *verified*
// flag is exposed); /user.email is unreliable (public-only, no verified flag).
const githubScopes = "read:user user:email"

// githubLoginTTL bounds how long an in-flight login (state) stays valid. Long
// enough for a human to authorize at GitHub, short enough that a leaked cookie
// is quickly useless. Mirrors oidcLoginTTL.
const githubLoginTTL = 10 * time.Minute

// githubCookieName carries the login-state record id across the GitHub round trip.
const githubCookieName = "mh_github"

// GitHubSettings is the slice of the settings repository the GitHub handler
// needs. Depending on the interface (not *settings.Repository) keeps this
// package free of a Postgres/pgx dependency, mirroring OIDCSettings.
type GitHubSettings interface {
	GetBool(ctx context.Context, key string, def bool) bool
	GetWithDefault(ctx context.Context, key, def string) string
}

// GitHubHandler serves the GitHub OAuth2 login endpoints. Like OIDCHandler it is
// a front-end to the same admin session token minted by passkey and TOTP login:
// after GitHub confirms an allowlisted, verified-email identity it calls
// SessionManager.CreateAuthToken, so every downstream gate keeps working.
//
// GitHub is OAuth2 only (no ID token, no discovery, no PKCE for OAuth Apps), so
// identity comes from the REST API (/user + /user/emails) rather than a verified
// ID token, and the state parameter + single-use login-state record + the
// confidential client secret carry the CSRF/replay defense.
type GitHubHandler struct {
	// ssoLogin carries the login-state cookie, per-IP backoff, error redirect
	// and session hand-off this flow shares with the OIDC handler.
	ssoLogin

	settings  GitHubSettings
	masterKey string
	users     SSOUserResolver // nil = no user email binding (admin allowlist only)

	// httpClient is an SSRF-guarded client used for every outbound GitHub request
	// (token exchange, /user, /user/emails). Without it oauth2 falls back to
	// http.DefaultClient; routing through netguard blocks metadata/link-local
	// targets and keeps parity with the OIDC handler. github.com is public, so
	// normal login is unaffected (netguard permits only what it must). It also
	// retries a request that failed before reaching GitHub so a momentary DNS or
	// dial fault does not cost the user the whole login.
	httpClient *http.Client

	// cached holds the lazily-built oauth2 config for one config fingerprint,
	// rebuilt only when the fingerprint changes. Mirrors OIDCHandler; there is no
	// network discovery to amortize here, but the cache keeps the allowlist parse
	// and secret decrypt off the hot path.
	mu       sync.Mutex
	cached   *githubRuntime
	cachedFP string
}

// NewGitHubHandler constructs a GitHubHandler. masterKey decrypts the stored
// client secret (encrypted at rest like provider keys and the OIDC secret).
// GitHub is a dashboard-only login front-end (Front Desk does not use it), so
// the successful callback always delivers the session via the HttpOnly
// mh_session cookie; cookieSecure is the authcookie.Secure mode passed through
// from config.
func NewGitHubHandler(
	settings GitHubSettings,
	sessionMgr *webauthn.SessionManager,
	ipLimiter IPLimiterMiddleware,
	masterKey string,
	cookieSecure string,
) *GitHubHandler {
	return &GitHubHandler{
		ssoLogin: ssoLogin{
			name:          "github",
			cookieName:    githubCookieName,
			cookiePath:    "/api/auth/github",
			ttl:           githubLoginTTL,
			sessionMgr:    sessionMgr,
			ipLimiter:     ipLimiter,
			throttle:      newSSOThrottle(),
			jar:           authcookie.Dashboard,
			cookieSecure:  cookieSecure,
			useCookieAuth: true,
		},
		settings:   settings,
		masterKey:  masterKey,
		httpClient: netguard.NewClientWithRetry(githubHTTPTimeout),
	}
}

// SetUserResolver enables user email binding: verified emails that miss the
// admin allowlist are looked up against user accounts, and a match logs in as
// that user. Optional; nil keeps the historical admin-only behavior.
func (h *GitHubHandler) SetUserResolver(users SSOUserResolver) {
	h.users = users
}

// githubRuntime is the built, ready-to-use GitHub OAuth2 configuration for one
// config fingerprint.
type githubRuntime struct {
	enabled      bool
	apiBaseURL   string // copied from githubAPIBaseURL in prod; mock URL in tests
	oauth2Config *oauth2.Config
	allowed      map[string]bool // lowercased allowlisted emails; empty = deny all
}

// githubLoginState is the per-login blob persisted in the login-state record and
// validated on callback. No nonce/PKCE: GitHub OAuth Apps support neither.
type githubLoginState struct {
	State string `json:"state"`
}

func (st *githubLoginState) stateToken() string { return st.State }

// Register mounts the GitHub OAuth routes. All three are unauthenticated because
// they ARE the login flow; the allowlist (not a bearer token) gates who may
// complete it. Mount on the same unauthenticated group as the OIDC/WebAuthn/TOTP
// login routes.
func (h *GitHubHandler) Register(r chi.Router) {
	r.Route("/auth/github", func(r chi.Router) {
		r.Get("/status", h.Status)
		r.Get("/start", h.Start)
		r.Get("/callback", h.Callback)
	})
}

// githubStatusResponse is the public GET /api/auth/github/status payload: just
// enough for the login screen to decide whether to show the GitHub button. The
// button label is fixed ("GitHub"), so no display name is returned.
type githubStatusResponse struct {
	Enabled bool `json:"enabled"`
}

// Status reports whether GitHub SSO is enabled and configured enough to show the
// login button. Public and cheap: it never builds the oauth2 config or touches
// the network. It deliberately does NOT read the client secret (a secret-bearing
// settings row): mirroring OIDC's Status, the secret is enforced only on the
// privileged path, where Start -> runtime() -> build() refuses an empty secret.
// Keeping the secret out of this unauthenticated, login-screen-polled endpoint
// avoids both a per-poll secret read and leaking whether a secret is set.
func (h *GitHubHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	enabled := h.settings.GetBool(ctx, githubEnabledKey, false)
	clientID := strings.TrimSpace(h.settings.GetWithDefault(ctx, githubClientIDKey, ""))
	baseURL := strings.TrimSpace(h.settings.GetWithDefault(ctx, githubPublicBaseURLKey, ""))

	resp := githubStatusResponse{
		Enabled: enabled && clientID != "" && baseURL != "",
	}
	writeJSON(w, resp)
}

// Start begins the authorization-code flow: it mints a state token, persists it
// in a single-use login-state record (id in an HttpOnly cookie), and 302s to
// GitHub's authorization endpoint.
func (h *GitHubHandler) Start(w http.ResponseWriter, r *http.Request) {
	rt, err := h.runtime(r.Context())
	if err != nil {
		respondError(w, "SSO is not available", err, http.StatusServiceUnavailable)
		return
	}
	if rt == nil || !rt.enabled {
		respondError(w, "SSO is not enabled", nil, http.StatusBadRequest)
		return
	}

	state, err := util.RandomHex(ssoStateBytes)
	if err != nil {
		respondError(w, "failed to start SSO", err, http.StatusInternalServerError)
		return
	}

	if !h.beginState(r.Context(), w, githubLoginState{State: state}) {
		return
	}

	http.Redirect(w, r, rt.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

// githubUser is the subset of GET /user we consume.
type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// githubEmail is one entry of GET /user/emails.
type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// Callback completes the flow: it validates the login-state record (single use)
// and state, exchanges the code for an access token, fetches the GitHub identity
// and verified emails over the REST API, enforces the verified-email allowlist,
// and on success mints a session token, delivers it in the HttpOnly mh_session
// cookie, and redirects to a clean SPA URL (no token in the URL). All denials
// redirect with a generic error code.
func (h *GitHubHandler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	throttleKey, ok := h.throttled(w, r)
	if !ok {
		return
	}

	// The login-state cookie is expired on every callback, including the
	// disabled-runtime short-circuit below, so a stale cookie never lingers.
	rt, err := h.runtime(ctx)
	if err != nil || rt == nil || !rt.enabled {
		h.clearCookie(w)
		h.redirectError(w, r, "unavailable")
		return
	}

	var st githubLoginState
	code, ok := h.consumeState(w, r, throttleKey, &st)
	if !ok {
		return
	}

	// Route the token exchange AND the derived API client (fetchUser /
	// fetchVerifiedEmails, built by oauth2Config.Client below) through the
	// SSRF-guarded transport. oauth2 reads the base client from this context key.
	ctx = context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)

	// Exchange the code for an access token (no PKCE verifier: GitHub OAuth Apps
	// don't support it; the confidential client secret authenticates this call).
	token, err := rt.oauth2Config.Exchange(ctx, code)
	if err != nil {
		h.fail(w, r, throttleKey, "code exchange failed", err)
		return
	}

	client := rt.oauth2Config.Client(ctx, token)

	user, err := h.fetchUser(ctx, client, rt.apiBaseURL)
	if err != nil {
		h.fail(w, r, throttleKey, "user fetch failed", err)
		return
	}
	verified, err := h.fetchVerifiedEmails(ctx, client, rt.apiBaseURL)
	if err != nil {
		h.fail(w, r, throttleKey, "email fetch failed", err)
		return
	}
	if len(verified) == 0 {
		debuglog.Warn("github: login denied: no verified email", "gh_id", user.ID, "gh_login", user.Login)
		h.fail(w, r, throttleKey, "no verified email", nil)
		return
	}

	// Match the allowlist against the SET of verified emails: GitHub accounts can
	// hold several, and a user may sign in/commit with a non-primary one. An
	// unverified address is never in this set, so it can never satisfy the
	// allowlist. We audit on the numeric GitHub id + login — the stable identifier
	// that, unlike email or username, never changes or gets reassigned.
	var matched string
	for _, e := range verified {
		if rt.allowed[e] {
			matched = e
			break
		}
	}
	// No allowlist match: try user email binding against each verified address
	// (multi-user). The allowlist wins so an allowlisted operator is never
	// downgraded to a user identity by a stray user row.
	sessionHandle := []byte("admin")
	if matched == "" {
		// Bind on GitHub's numeric account id -- stable and never reassigned,
		// unlike the email set -- so the account locks to this GitHub user, not
		// to whoever can next assert one of these verified addresses.
		subject := strconv.FormatInt(user.ID, 10)
		for _, e := range verified {
			if u := resolveSSOUser(ctx, h.users, "github", subject, e); u != nil {
				matched = e
				sessionHandle = []byte(u.ID.String())
				break
			}
		}
	}
	if matched == "" {
		debuglog.Warn("github: login denied: no allowlisted or user-bound verified email",
			"gh_id", user.ID, "gh_login", user.Login)
		h.fail(w, r, throttleKey, "not allowed", nil)
		return
	}

	// GitHub is dashboard-only (Front Desk does not use this handler), so the
	// session always rides the HttpOnly dashboard cookie: useCookieAuth is fixed
	// true in the constructor and the fragment delivery never applies here.
	h.finishLogin(w, r, throttleKey, sessionHandle,
		"email_masked", maskEmail(matched), "gh_id", user.ID, "gh_login", user.Login)
}

// fetchUser GETs {apiBase}/user and decodes the id + login.
func (h *GitHubHandler) fetchUser(ctx context.Context, client *http.Client, apiBase string) (githubUser, error) {
	var u githubUser
	if err := getGitHubJSON(ctx, client, apiBase+"/user", &u); err != nil {
		return githubUser{}, err
	}
	return u, nil
}

// fetchVerifiedEmails GETs {apiBase}/user/emails and returns the lowercased set
// of verified addresses. A 403/404 (the user:email scope was not granted) is
// surfaced as an error so the caller fails the login cleanly.
func (h *GitHubHandler) fetchVerifiedEmails(ctx context.Context, client *http.Client, apiBase string) ([]string, error) {
	var emails []githubEmail
	if err := getGitHubJSON(ctx, client, apiBase+"/user/emails", &emails); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range emails {
		if e.Verified {
			out = append(out, strings.ToLower(strings.TrimSpace(e.Email)))
		}
	}
	return out, nil
}

// getGitHubJSON performs an authenticated GET and decodes a JSON body, treating
// any non-2xx status as an error.
func getGitHubJSON(ctx context.Context, client *http.Client, urlStr string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// Pin the REST API version (GitHub's recommendation) so a future default-version
	// bump can't silently change the shape of /user or /user/emails out from under us.
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Bound the body read so a hostile/buggy endpoint can't stream forever.
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return fmt.Errorf("github API %s: status %d", urlStr, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// runtime returns the built GitHub runtime for the current settings, rebuilding
// it only when the config fingerprint changes. A disabled or under-configured
// state returns a runtime with enabled=false and no error. The build runs
// outside the lock; a rare double build is harmless (last writer wins).
func (h *GitHubHandler) runtime(ctx context.Context) (*githubRuntime, error) {
	enabled := h.settings.GetBool(ctx, githubEnabledKey, false)
	clientID := strings.TrimSpace(h.settings.GetWithDefault(ctx, githubClientIDKey, ""))
	clientSecretEnc := h.settings.GetWithDefault(ctx, githubClientSecretKey, "")
	baseURL := strings.TrimSpace(h.settings.GetWithDefault(ctx, githubPublicBaseURLKey, ""))
	allowedRaw := h.settings.GetWithDefault(ctx, githubAllowedEmailsKey, "")

	fp := strings.Join([]string{
		strconv.FormatBool(enabled), clientID, clientSecretEnc, baseURL, allowedRaw,
	}, "\x00")

	h.mu.Lock()
	if h.cached != nil && h.cachedFP == fp {
		rt := h.cached
		h.mu.Unlock()
		return rt, nil
	}
	h.mu.Unlock()

	rt, err := h.build(enabled, clientID, clientSecretEnc, baseURL, allowedRaw)
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
// resolve to a disabled runtime (no error); only a genuine decrypt failure
// returns an error.
func (h *GitHubHandler) build(enabled bool, clientID, clientSecretEnc, baseURL, allowedRaw string) (*githubRuntime, error) {
	if !enabled || clientID == "" || baseURL == "" {
		return &githubRuntime{enabled: false}, nil
	}

	clientSecret, err := decryptClientSecret(clientSecretEnc, h.masterKey)
	if err != nil {
		return nil, err
	}
	if clientSecret == "" {
		// No usable secret: token exchange would fail, so report under-configured.
		return &githubRuntime{enabled: false}, nil
	}

	redirectURL := strings.TrimRight(baseURL, "/") + githubCallbackPath
	return &githubRuntime{
		enabled:    true,
		apiBaseURL: githubAPIBaseURL,
		oauth2Config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     githuboauth.Endpoint,
			RedirectURL:  redirectURL,
			Scopes:       strings.Fields(githubScopes),
		},
		allowed: parseAllowlist(allowedRaw),
	}, nil
}
