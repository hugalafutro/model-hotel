package adminauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// --- mock GitHub OAuth + REST API ---

// githubMock stands in for github.com (token endpoint) and api.github.com
// (/user, /user/emails). Behavior flags are read under mu so the httptest
// handler goroutines never race the test goroutine under -race.
type githubMock struct {
	server *httptest.Server

	mu              sync.Mutex
	id              int64
	login           string
	emails          []githubEmail
	tokenError      bool // /access_token returns an OAuth error (exchange fails)
	userError       bool // /user returns 500
	emailsForbidden bool // /user/emails returns 403 (user:email scope not granted)
}

func newGitHubMock(t *testing.T) *githubMock {
	t.Helper()
	m := &githubMock{
		id:    4242,
		login: "octocat",
		emails: []githubEmail{
			{Email: "admin@example.com", Primary: true, Verified: true},
		},
	}
	mux := http.NewServeMux()
	m.server = httptest.NewServer(mux)

	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.tokenError {
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{"error": "bad_verification_code"})
			return
		}
		writeTestJSON(w, map[string]any{
			"access_token": "gho_access-token",
			"token_type":   "bearer",
			"scope":        "read:user,user:email",
		})
	})
	// The handler must pin the REST API version on every identity call.
	assertAPIVersion := func(r *http.Request) {
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("%s: X-GitHub-Api-Version = %q, want 2022-11-28", r.URL.Path, got)
		}
	}
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		assertAPIVersion(r)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.userError {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeTestJSON(w, githubUser{ID: m.id, Login: m.login})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		assertAPIVersion(r)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.emailsForbidden {
			w.WriteHeader(http.StatusForbidden)
			writeTestJSON(w, map[string]any{"message": "Requires authentication"})
			return
		}
		writeTestJSON(w, m.emails)
	})

	t.Cleanup(m.server.Close)
	return m
}

func (m *githubMock) behavior(fn func(*githubMock)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m)
}

// --- harness ---

const githubTestClientID = "Iv1.testclientid"

// newGitHubTestHandler builds a handler whose cached runtime is pre-pointed at
// the mock server. It drives runtime() once to build against the real github.com
// endpoints, then rewrites the cached runtime's oauth2 endpoint + apiBaseURL to
// the mock (runtime() returns the same cached pointer on later calls because the
// config fingerprint is unchanged). This keeps the production path free of any
// test-only injection seam.
func newGitHubTestHandler(t *testing.T, m *githubMock, allowed string) (*GitHubHandler, *fakeSettings, *webauthn.SessionManager) {
	t.Helper()
	store := newMemStore()
	sessionMgr := webauthn.NewSessionManager(store)
	enc, err := auth.EncryptString("client-secret-value", testMasterKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	fs := newFakeSettings(map[string]string{
		githubEnabledKey:       "true",
		githubClientIDKey:      githubTestClientID,
		githubClientSecretKey:  enc,
		githubAllowedEmailsKey: allowed,
		githubPublicBaseURLKey: "https://mh.example.test",
	})
	h := NewGitHubHandler(fs, sessionMgr, mockIPLimiter{}, testMasterKey)

	// Build once, then redirect the cached runtime at the mock server.
	rt, err := h.runtime(context.Background())
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if rt == nil || !rt.enabled {
		t.Fatal("expected an enabled runtime for a fully configured handler")
	}
	rt.apiBaseURL = m.server.URL
	rt.oauth2Config.Endpoint = oauth2.Endpoint{
		AuthURL:  m.server.URL + "/login/oauth/authorize",
		TokenURL: m.server.URL + "/login/oauth/access_token",
	}
	return h, fs, sessionMgr
}

func runGitHubStart(t *testing.T, h *GitHubHandler) (*url.URL, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/start", http.NoBody)
	rec := httptest.NewRecorder()
	h.Start(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("Start status = %d, want 302", res.StatusCode)
	}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == githubCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("Start did not set the login-state cookie")
	}
	return loc, cookie
}

// runGitHubCallback drives GET /callback and returns the fragment of the redirect
// Location (the part after '#').
func runGitHubCallback(t *testing.T, h *GitHubHandler, cookie *http.Cookie, query url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?"+query.Encode(), http.NoBody)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("Callback status = %d, want 302 (body=%s)", res.StatusCode, rec.Body.String())
	}
	loc := res.Header.Get("Location")
	if _, after, ok := strings.Cut(loc, "#"); ok {
		return after
	}
	return ""
}

// successToken drives start+callback and asserts a session token came back.
func successToken(t *testing.T, h *GitHubHandler, sessionMgr *webauthn.SessionManager) {
	t.Helper()
	loc, cookie := runGitHubStart(t, h)
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatalf("auth URL missing state: %s", loc.String())
	}
	frag := runGitHubCallback(t, h, cookie, url.Values{"state": {state}, "code": {"auth-code"}})
	const prefix = "oidc_token="
	if !strings.HasPrefix(frag, prefix) {
		t.Fatalf("expected token fragment, got %q", frag)
	}
	token, err := url.QueryUnescape(strings.TrimPrefix(frag, prefix))
	if err != nil {
		t.Fatalf("unescape token: %v", err)
	}
	if !sessionMgr.Validate(context.Background(), token) {
		t.Fatal("minted session token failed Validate")
	}
}

func TestGitHubStatus(t *testing.T) {
	m := newGitHubMock(t)
	h, fs, _ := newGitHubTestHandler(t, m, "admin@example.com")

	get := func() githubStatusResponse {
		rec := httptest.NewRecorder()
		h.Status(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github/status", http.NoBody))
		var resp githubStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return resp
	}

	if !get().Enabled {
		t.Fatal("expected enabled=true when fully configured")
	}

	// Each term of the AND-chain (enabled && clientID && baseURL) independently
	// gates the response. The client secret is deliberately NOT read here (it is
	// enforced on the privileged Start path), so clearing it must NOT flip
	// enabled to false.
	for _, tc := range []struct {
		name string
		key  string
		val  string
	}{
		{"disabled", githubEnabledKey, "false"},
		{"no client id", githubClientIDKey, ""},
		{"no base url", githubPublicBaseURLKey, ""},
	} {
		fs := newFakeSettings(map[string]string{
			githubEnabledKey:       "true",
			githubClientIDKey:      githubTestClientID,
			githubClientSecretKey:  "enc-secret",
			githubPublicBaseURLKey: "https://mh.example.test",
		})
		fs.set(tc.key, tc.val)
		h := NewGitHubHandler(fs, nil, mockIPLimiter{}, testMasterKey)
		rec := httptest.NewRecorder()
		h.Status(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github/status", http.NoBody))
		var resp githubStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode status: %v", tc.name, err)
		}
		if resp.Enabled {
			t.Fatalf("%s: expected enabled=false", tc.name)
		}
	}

	// Clearing only the secret must keep enabled=true (Status does not read it).
	fs.set(githubClientSecretKey, "")
	if !get().Enabled {
		t.Fatal("clearing the client secret must not flip Status enabled (secret is not read here)")
	}
}

func TestGitHubLoginRoundTrip(t *testing.T) {
	m := newGitHubMock(t)
	// Allowlist match is case-insensitive: configured mixed case, GitHub returns lowercase.
	h, _, sessionMgr := newGitHubTestHandler(t, m, "Admin@Example.com")
	successToken(t, h, sessionMgr)
}

// A verified non-primary email satisfies the allowlist (GitHub accounts can hold
// several verified addresses and may sign in/commit with a non-primary one).
func TestGitHubLoginNonPrimaryVerifiedEmail(t *testing.T) {
	m := newGitHubMock(t)
	m.behavior(func(g *githubMock) {
		g.emails = []githubEmail{
			{Email: "public@example.com", Primary: true, Verified: true},
			{Email: "admin@example.com", Primary: false, Verified: true},
		}
	})
	h, _, sessionMgr := newGitHubTestHandler(t, m, "admin@example.com")
	successToken(t, h, sessionMgr)
}

func TestGitHubCallbackRejections(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, h *GitHubHandler, m *githubMock) string
	}{
		{
			name: "bad state",
			run: func(t *testing.T, h *GitHubHandler, _ *githubMock) string {
				_, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {"wrong"}, "code": {"c"}})
			},
		},
		{
			name: "missing login state cookie",
			run: func(t *testing.T, h *GitHubHandler, _ *githubMock) string {
				loc, _ := runGitHubStart(t, h)
				return runGitHubCallback(t, h, nil, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
			},
		},
		{
			name: "missing code",
			run: func(t *testing.T, h *GitHubHandler, _ *githubMock) string {
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}})
			},
		},
		{
			name: "provider error param",
			run: func(t *testing.T, h *GitHubHandler, _ *githubMock) string {
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{
					"state": {loc.Query().Get("state")}, "error": {"access_denied"},
				})
			},
		},
		{
			name: "single use replay",
			run: func(t *testing.T, h *GitHubHandler, _ *githubMock) string {
				loc, cookie := runGitHubStart(t, h)
				state := loc.Query().Get("state")
				_ = runGitHubCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}}) // consumes
				return runGitHubCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "token exchange fails",
			run: func(t *testing.T, h *GitHubHandler, m *githubMock) string {
				m.behavior(func(g *githubMock) { g.tokenError = true })
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
			},
		},
		{
			name: "user fetch fails",
			run: func(t *testing.T, h *GitHubHandler, m *githubMock) string {
				m.behavior(func(g *githubMock) { g.userError = true })
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
			},
		},
		{
			name: "email scope not granted",
			run: func(t *testing.T, h *GitHubHandler, m *githubMock) string {
				m.behavior(func(g *githubMock) { g.emailsForbidden = true })
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
			},
		},
		{
			name: "no verified email",
			run: func(t *testing.T, h *GitHubHandler, m *githubMock) string {
				m.behavior(func(g *githubMock) {
					g.emails = []githubEmail{{Email: "admin@example.com", Primary: true, Verified: false}}
				})
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
			},
		},
		{
			name: "email not allowlisted",
			run: func(t *testing.T, h *GitHubHandler, m *githubMock) string {
				m.behavior(func(g *githubMock) {
					g.emails = []githubEmail{{Email: "intruder@example.com", Primary: true, Verified: true}}
				})
				loc, cookie := runGitHubStart(t, h)
				return runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newGitHubMock(t)
			h, _, sessionMgr := newGitHubTestHandler(t, m, "admin@example.com")
			frag := tc.run(t, h, m)
			if !strings.HasPrefix(frag, "oidc_error=") {
				t.Fatalf("expected error fragment, got %q", frag)
			}
			// No session token may leak in a rejection (token is in byHash, not the fragment).
			if strings.Contains(frag, "oidc_token=") {
				t.Fatalf("rejection leaked a token: %q", frag)
			}
			_ = sessionMgr
		})
	}
}

// Empty allowlist fails closed: even a verified, otherwise-valid identity is denied.
func TestGitHubEmptyAllowlistFailsClosed(t *testing.T) {
	m := newGitHubMock(t)
	h, _, _ := newGitHubTestHandler(t, m, "   ") // whitespace-only -> empty set
	loc, cookie := runGitHubStart(t, h)
	frag := runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
	if !strings.HasPrefix(frag, "oidc_error=") {
		t.Fatalf("empty allowlist must deny, got %q", frag)
	}
}

func TestGitHubDisabled(t *testing.T) {
	m := newGitHubMock(t)
	h, fs, _ := newGitHubTestHandler(t, m, "admin@example.com")
	fs.set(githubEnabledKey, "false")

	// Start refuses when disabled.
	rec := httptest.NewRecorder()
	h.Start(rec, httptest.NewRequest(http.MethodGet, "/api/auth/github/start", http.NoBody))
	if rec.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("Start when disabled = %d, want 400", rec.Result().StatusCode)
	}
}

func TestGitHubCallbackThrottled(t *testing.T) {
	m := newGitHubMock(t)
	h, _, _ := newGitHubTestHandler(t, m, "nobody@example.com") // always denied -> records failures

	// Backoff begins only once failures EXCEED maxFailures (5): the 6th failure
	// arms the lock, so the 7th callback is throttled before any work.
	var frag string
	for range 7 {
		loc, cookie := runGitHubStart(t, h)
		frag = runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
	}
	if !strings.HasPrefix(frag, "oidc_error=throttled") {
		t.Fatalf("expected throttled error after repeated failures, got %q", frag)
	}
}

func TestGitHubRegisterRoutes(t *testing.T) {
	m := newGitHubMock(t)
	h, _, _ := newGitHubTestHandler(t, m, "admin@example.com")
	r := chi.NewRouter()
	h.Register(r)
	for _, p := range []string{"/auth/github/status", "/auth/github/start", "/auth/github/callback"} {
		req := httptest.NewRequest(http.MethodGet, p, http.NoBody)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Result().StatusCode == http.StatusNotFound {
			t.Fatalf("route %s not registered", p)
		}
	}
}

// CreateAuthToken failure surfaces as a generic error redirect, not a 500/panic.
func TestGitHubCreateAuthTokenError(t *testing.T) {
	m := newGitHubMock(t)
	store := newMemStore()
	store.createErr = errNoSession // CreateSession (used by CreateAuthToken) fails
	sessionMgr := webauthn.NewSessionManager(store)
	enc, err := auth.EncryptString("client-secret-value", testMasterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	fs := newFakeSettings(map[string]string{
		githubEnabledKey:       "true",
		githubClientIDKey:      githubTestClientID,
		githubClientSecretKey:  enc,
		githubAllowedEmailsKey: "admin@example.com",
		githubPublicBaseURLKey: "https://mh.example.test",
	})
	h := NewGitHubHandler(fs, sessionMgr, mockIPLimiter{}, testMasterKey)
	rt, err := h.runtime(context.Background())
	if err != nil || rt == nil || !rt.enabled {
		t.Fatalf("runtime: %v", err)
	}
	rt.apiBaseURL = m.server.URL
	rt.oauth2Config.Endpoint = oauth2.Endpoint{TokenURL: m.server.URL + "/login/oauth/access_token"}

	// CreateLoginState also uses the store; allow it by clearing createErr for Start,
	// then re-arming it so only CreateAuthToken (in Callback) fails.
	store.mu.Lock()
	store.createErr = nil
	store.mu.Unlock()
	loc, cookie := runGitHubStart(t, h)
	store.mu.Lock()
	store.createErr = errNoSession
	store.mu.Unlock()

	frag := runGitHubCallback(t, h, cookie, url.Values{"state": {loc.Query().Get("state")}, "code": {"c"}})
	if !strings.HasPrefix(frag, "oidc_error=") {
		t.Fatalf("expected error fragment on CreateAuthToken failure, got %q", frag)
	}
}

// assertCallbackDenied drives the callback with a hand-built cookie and asserts a
// generic error fragment with no leaked token. Used for the login-state validation
// branches that can't be reached through a normal Start.
func assertCallbackDenied(t *testing.T, h *GitHubHandler, cookie *http.Cookie, q url.Values) {
	t.Helper()
	frag := runGitHubCallback(t, h, cookie, q)
	if !strings.HasPrefix(frag, "oidc_error=") {
		t.Fatalf("expected error fragment, got %q", frag)
	}
	if strings.Contains(frag, "oidc_token=") {
		t.Fatalf("denied callback leaked a token: %q", frag)
	}
}

// A login-state record aged past githubLoginTTL is rejected ("expired login
// state"): ConsumeLoginState returns the record but reports it expired.
func TestGitHubExpiredLoginState(t *testing.T) {
	m := newGitHubMock(t)
	h, _, sessionMgr := newGitHubTestHandler(t, m, "admin@example.com")

	blob, err := json.Marshal(githubLoginState{State: "s1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Negative TTL => ExpiresAt already in the past.
	id, err := sessionMgr.CreateLoginState(context.Background(), blob, -time.Minute)
	if err != nil {
		t.Fatalf("create login state: %v", err)
	}
	cookie := &http.Cookie{Name: githubCookieName, Value: id.String()}
	assertCallbackDenied(t, h, cookie, url.Values{"state": {"s1"}, "code": {"c"}})
}

// A cookie whose value is not a UUID is rejected ("bad login state") before any
// store lookup.
func TestGitHubBadLoginStateCookie(t *testing.T) {
	m := newGitHubMock(t)
	h, _, _ := newGitHubTestHandler(t, m, "admin@example.com")

	cookie := &http.Cookie{Name: githubCookieName, Value: "not-a-uuid"}
	assertCallbackDenied(t, h, cookie, url.Values{"state": {"s1"}, "code": {"c"}})
}

// A login-state record whose stored blob is not valid JSON is rejected ("corrupt
// login state") at the json.Unmarshal step, before any state/token work.
func TestGitHubCorruptLoginState(t *testing.T) {
	m := newGitHubMock(t)
	h, _, sessionMgr := newGitHubTestHandler(t, m, "admin@example.com")

	id, err := sessionMgr.CreateLoginState(context.Background(), []byte("{not-json"), time.Minute)
	if err != nil {
		t.Fatalf("create login state: %v", err)
	}
	cookie := &http.Cookie{Name: githubCookieName, Value: id.String()}
	// State value is irrelevant: unmarshal fails before the state compare.
	assertCallbackDenied(t, h, cookie, url.Values{"state": {"whatever"}, "code": {"c"}})
}

// A callback that arrives after GitHub SSO is disabled is rejected
// ("unavailable") and still clears the single-use login-state cookie.
func TestGitHubCallbackDisabledClearsCookie(t *testing.T) {
	m := newGitHubMock(t)
	h, fs, _ := newGitHubTestHandler(t, m, "admin@example.com")
	fs.set(githubEnabledKey, "false") // runtime() rebuilds to a disabled runtime

	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?code=c&state=s", http.NoBody)
	req.AddCookie(&http.Cookie{Name: githubCookieName, Value: "stale"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "#oidc_error=") {
		t.Fatalf("expected error fragment, got Location %q", loc)
	}
	var cleared bool
	for _, c := range res.Cookies() {
		if c.Name == githubCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("disabled-runtime callback must still clear the login-state cookie")
	}
}
