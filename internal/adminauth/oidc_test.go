package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// --- in-memory SessionStore so the OIDC suite needs no database ---

// errNoSession stands in for any store miss; SessionManager treats every
// non-nil error from the store as "not found".
var errNoSession = errors.New("no session")

type memSessionStore struct {
	mu        sync.Mutex
	byID      map[uuid.UUID]*webauthn.SessionRecord
	byHash    map[string]*webauthn.SessionRecord
	createErr error
}

func newMemStore() *memSessionStore {
	return &memSessionStore{
		byID:   make(map[uuid.UUID]*webauthn.SessionRecord),
		byHash: make(map[string]*webauthn.SessionRecord),
	}
}

func (s *memSessionStore) CreateSession(_ context.Context, rec *webauthn.SessionRecord) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *rec
	s.byID[rec.ID] = &cp
	if rec.TokenHash != nil {
		s.byHash[*rec.TokenHash] = &cp
	}
	return nil
}

func (s *memSessionStore) GetSession(_ context.Context, id uuid.UUID) (*webauthn.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return nil, errNoSession
	}
	cp := *rec
	return &cp, nil
}

func (s *memSessionStore) GetSessionByTokenHash(_ context.Context, hash string) (*webauthn.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[hash]
	if !ok {
		return nil, errNoSession
	}
	cp := *rec
	return &cp, nil
}

func (s *memSessionStore) DeleteSession(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		// Mirror the real stores (Postgres ErrNotFound / SQLite 0-rows): report a
		// miss so ConsumeLoginState's single-use claim can reject a racing reader.
		return errNoSession
	}
	if rec.TokenHash != nil {
		delete(s.byHash, *rec.TokenHash)
	}
	delete(s.byID, id)
	return nil
}

func (s *memSessionStore) CleanupExpiredSessions(context.Context) (int64, error) { return 0, nil }

// --- fake settings ---

type fakeSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func newFakeSettings(kv map[string]string) *fakeSettings {
	cp := make(map[string]string, len(kv))
	for k, v := range kv {
		cp[k] = v
	}
	return &fakeSettings{m: cp}
}

func (f *fakeSettings) set(k, v string) {
	f.mu.Lock()
	f.m[k] = v
	f.mu.Unlock()
}

func (f *fakeSettings) GetBool(_ context.Context, key string, def bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.m[key] {
	case "true":
		return true
	case "false":
		return false
	default:
		return def
	}
}

func (f *fakeSettings) GetWithDefault(_ context.Context, key, def string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.m[key]; ok {
		return v
	}
	return def
}

// --- mock OpenID Connect provider ---

type mockIDP struct {
	server   *httptest.Server
	signer   jose.Signer
	jwks     jose.JSONWebKeySet
	clientID string

	mu              sync.Mutex
	nonce           string
	email           string
	emailVerified   bool
	sub             string
	omitIDToken     bool   // token response carries no id_token
	emailInIDToken  bool   // when false, email is served only at /userinfo
	tokenError      bool   // /token returns an OAuth error (exchange fails)
	expireIDToken   bool   // id_token exp is in the past (Verify must reject)
	userinfoError   bool   // /userinfo returns 500 (UserInfo fallback fails)
	userinfoSubject string // override the /userinfo sub (default: idp.sub)
}

func newMockIDP(t *testing.T, clientID string) *mockIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	const kid = "test-key-1"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: kid}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	idp := &mockIDP{
		signer:   signer,
		clientID: clientID,
		jwks: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"},
		}},
		sub:            "idp-subject-123",
		emailInIDToken: true,
	}

	mux := http.NewServeMux()
	idp.server = httptest.NewServer(mux)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, map[string]any{
			"issuer":                                idp.server.URL,
			"authorization_endpoint":                idp.server.URL + "/auth",
			"token_endpoint":                        idp.server.URL + "/token",
			"jwks_uri":                              idp.server.URL + "/jwks",
			"userinfo_endpoint":                     idp.server.URL + "/userinfo",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, idp.jwks)
	})
	// UserInfo returns the email + email_verified as plain JSON, mirroring an IdP
	// (like Authelia by default) that keeps email out of the ID token.
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		idp.mu.Lock()
		defer idp.mu.Unlock()
		if idp.userinfoError {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		sub := idp.sub
		if idp.userinfoSubject != "" {
			sub = idp.userinfoSubject
		}
		writeTestJSON(w, map[string]any{
			"sub":            sub,
			"email":          idp.email,
			"email_verified": idp.emailVerified,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		idp.mu.Lock()
		defer idp.mu.Unlock()
		if idp.tokenError {
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{"error": "invalid_grant"})
			return
		}
		resp := map[string]any{
			"access_token": "access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if !idp.omitIDToken {
			resp["id_token"] = idp.signIDToken(t)
		}
		writeTestJSON(w, resp)
	})

	t.Cleanup(idp.server.Close)
	return idp
}

// signIDToken signs an ID token with the currently configured claims. Caller
// holds idp.mu.
func (idp *mockIDP) signIDToken(t *testing.T) string {
	t.Helper()
	exp := time.Now().Add(time.Hour)
	if idp.expireIDToken {
		exp = time.Now().Add(-time.Hour)
	}
	cl := map[string]any{
		"iss":   idp.server.URL,
		"aud":   idp.clientID,
		"sub":   idp.sub,
		"exp":   exp.Unix(),
		"iat":   time.Now().Add(-2 * time.Hour).Unix(),
		"nonce": idp.nonce,
	}
	// Some IdPs omit email from the ID token; the handler then falls back to
	// the UserInfo endpoint. When emailInIDToken is true, embed it here.
	if idp.emailInIDToken {
		cl["email"] = idp.email
		cl["email_verified"] = idp.emailVerified
	}
	raw, err := jwt.Signed(idp.signer).Claims(cl).Serialize()
	if err != nil {
		t.Fatalf("sign id_token: %v", err)
	}
	return raw
}

func (idp *mockIDP) configure(nonce, email string, verified bool) {
	idp.mu.Lock()
	idp.nonce = nonce
	idp.email = email
	idp.emailVerified = verified
	idp.mu.Unlock()
}

// behavior mutates the response-shaping flags under the lock, so the httptest
// handler goroutines (which read them under the same lock) never race the test
// goroutine when -race is enabled.
func (idp *mockIDP) behavior(fn func(*mockIDP)) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	fn(idp)
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- harness ---

const oidcTestClientID = "model-hotel-test"

func newOIDCTestHandler(t *testing.T, idp *mockIDP, allowed string) (*OIDCHandler, *fakeSettings, *webauthn.SessionManager) {
	t.Helper()
	store := newMemStore()
	sessionMgr := webauthn.NewSessionManager(store)
	enc, err := auth.EncryptString("client-secret-value", testMasterKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	fs := newFakeSettings(map[string]string{
		OIDCEnabledKey:       "true",
		OIDCIssuerURLKey:     idp.server.URL,
		OIDCClientIDKey:      oidcTestClientID,
		OIDCClientSecretKey:  enc,
		OIDCAllowedEmailsKey: allowed,
		OIDCPublicBaseURLKey: "https://mh.example.test",
	})
	h := NewOIDCHandler(fs, sessionMgr, mockIPLimiter{}, testMasterKey)
	return h, fs, sessionMgr
}

// runStart drives GET /start and returns the auth-redirect URL + the login cookie.
func runStart(t *testing.T, h *OIDCHandler) (*url.URL, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/start", http.NoBody)
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
		if c.Name == oidcCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("Start did not set the login-state cookie")
	}
	return loc, cookie
}

// runCallback drives GET /callback with the given cookie + query, returning the
// fragment of the redirect Location (the part after '#').
func runCallback(t *testing.T, h *OIDCHandler, cookie *http.Cookie, query url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?"+query.Encode(), http.NoBody)
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
	if i := strings.IndexByte(loc, '#'); i >= 0 {
		return loc[i+1:]
	}
	return ""
}

func TestOIDCStatus(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, fs, _ := newOIDCTestHandler(t, idp, "admin@example.com")

	// Enabled + configured.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/status", http.NoBody)
	rec := httptest.NewRecorder()
	h.Status(rec, req)
	var resp oidcStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !resp.Enabled {
		t.Fatal("expected enabled=true when fully configured")
	}
	if resp.DisplayName == "" {
		t.Fatal("expected a display name (IdP host)")
	}

	// Disabled.
	fs.set(OIDCEnabledKey, "false")
	rec = httptest.NewRecorder()
	h.Status(rec, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/status", http.NoBody))
	resp = oidcStatusResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Enabled {
		t.Fatal("expected enabled=false when oidc_enabled=false")
	}
}

func TestOIDCLoginRoundTrip(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, sessionMgr := newOIDCTestHandler(t, idp, "Admin@Example.com")

	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	if state == "" || nonce == "" {
		t.Fatalf("auth URL missing state/nonce: %s", loc.String())
	}
	if loc.Query().Get("code_challenge") == "" || loc.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("auth URL missing PKCE S256 challenge")
	}

	// Allowlist match is case-insensitive: configured "Admin@Example.com",
	// IdP returns lowercase.
	idp.configure(nonce, "admin@example.com", true)

	q := url.Values{"state": {state}, "code": {"auth-code"}}
	frag := runCallback(t, h, cookie, q)
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

// TestOIDCUserInfoFallback covers an IdP (e.g. Authelia 4.38+) that omits email
// from the ID token: the handler must fall back to the UserInfo endpoint.
func TestOIDCUserInfoFallback(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	idp.behavior(func(m *mockIDP) { m.emailInIDToken = false }) // email only at /userinfo
	h, _, sessionMgr := newOIDCTestHandler(t, idp, "admin@example.com")

	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)

	frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
	token, err := url.QueryUnescape(strings.TrimPrefix(frag, "oidc_token="))
	if err != nil || !strings.HasPrefix(frag, "oidc_token=") {
		t.Fatalf("expected success via userinfo fallback, got %q", frag)
	}
	if !sessionMgr.Validate(context.Background(), token) {
		t.Fatal("token from userinfo-fallback login failed Validate")
	}
}

func TestOIDCCallbackRejections(t *testing.T) {
	tests := []struct {
		name string
		// mutate sets up the per-case state and returns (cookie, query).
		run func(t *testing.T, h *OIDCHandler, idp *mockIDP) string
	}{
		{
			name: "bad state",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				_, cookie := runStart(t, h)
				idp.configure("whatever", "admin@example.com", true)
				return runCallback(t, h, cookie, url.Values{"state": {"wrong"}, "code": {"c"}})
			},
		},
		{
			name: "replayed nonce mismatch",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				// IdP returns a different nonce than the one we issued.
				idp.configure("attacker-nonce", "admin@example.com", true)
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "missing login state cookie",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, _ := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				return runCallback(t, h, nil, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "single use replay",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				_ = runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}}) // consumes
				// Second use of the same cookie must fail (record deleted).
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "unverified email denied",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", false)
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "idp error param",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				return runCallback(t, h, cookie, url.Values{"state": {state}, "error": {"access_denied"}})
			},
		},
		{
			name: "code exchange failure",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				idp.behavior(func(m *mockIDP) { m.tokenError = true }) // /token returns an OAuth error
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "missing id_token in token response",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				idp.behavior(func(m *mockIDP) { m.omitIDToken = true }) // no id_token in response
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "expired id_token fails verification",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				idp.behavior(func(m *mockIDP) { m.expireIDToken = true }) // exp in the past
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "userinfo fetch failure",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				// No email in the token -> fallback to /userinfo, which errors.
				idp.behavior(func(m *mockIDP) { m.emailInIDToken = false; m.userinfoError = true })
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "userinfo subject mismatch",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				// UserInfo returns a different sub than the verified ID token: the
				// handler must reject it (OIDC core 5.3.2), not authorize the email.
				idp.behavior(func(m *mockIDP) { m.emailInIDToken = false; m.userinfoSubject = "someone-else" })
				return runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
			},
		},
		{
			name: "bad login-state cookie",
			run: func(t *testing.T, h *OIDCHandler, _ *mockIDP) string {
				bad := &http.Cookie{Name: oidcCookieName, Value: "not-a-uuid"}
				return runCallback(t, h, bad, url.Values{"state": {"x"}, "code": {"c"}})
			},
		},
		{
			name: "missing code",
			run: func(t *testing.T, h *OIDCHandler, idp *mockIDP) string {
				loc, cookie := runStart(t, h)
				state := loc.Query().Get("state")
				idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
				// Valid state, no error param, but no code either.
				return runCallback(t, h, cookie, url.Values{"state": {state}})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idp := newMockIDP(t, oidcTestClientID)
			h, _, _ := newOIDCTestHandler(t, idp, "admin@example.com")
			frag := tc.run(t, h, idp)
			if !strings.HasPrefix(frag, "oidc_error=") {
				t.Fatalf("expected error fragment, got %q", frag)
			}
		})
	}
}

// TestOIDCCallbackThrottle verifies the per-IP backoff trips after 5 failures
// and that the throttled response is a fragment redirect (so the SPA renders
// the login screen) rather than a plaintext 429.
func TestOIDCCallbackThrottle(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, _ := newOIDCTestHandler(t, idp, "admin@example.com")

	// Cookieless callbacks fail ("missing login state") and record failures. With
	// maxFailures=5 the backoff first applies after the 6th recorded failure
	// (backoffFor: shift = failures-maxFailures-1), so the 7th attempt is the
	// first one blocked. All share one IP key (mockIPLimiter returns r.RemoteAddr,
	// constant across httptest requests); the 1s lock easily outlasts the loop.
	q := url.Values{"state": {"x"}, "code": {"c"}}
	var frag string
	for i := 0; i < 7; i++ {
		frag = runCallback(t, h, nil, q)
	}
	if !strings.HasPrefix(frag, "oidc_error=throttled") {
		t.Fatalf("seventh attempt should be throttled with a fragment redirect, got %q", frag)
	}
}

// TestOIDCCallbackDisabled covers the Callback early-out when SSO is off.
func TestOIDCCallbackDisabled(t *testing.T) {
	sm := webauthn.NewSessionManager(newMemStore())
	h := NewOIDCHandler(newFakeSettings(map[string]string{OIDCEnabledKey: "false"}), sm, mockIPLimiter{}, testMasterKey)
	frag := runCallback(t, h, nil, url.Values{"state": {"x"}, "code": {"c"}})
	if !strings.HasPrefix(frag, "oidc_error=") {
		t.Fatalf("disabled callback should redirect with an error, got %q", frag)
	}
}

// failNthCreateStore fails the Nth CreateSession call, leaving the others to the
// embedded mem store. Used to drive the CreateAuthToken failure in Callback: the
// login-state create (Start) succeeds, the auth-token create (Callback) fails.
type failNthCreateStore struct {
	*memSessionStore
	failOn int
	mu     sync.Mutex
	n      int
}

func (s *failNthCreateStore) CreateSession(ctx context.Context, rec *webauthn.SessionRecord) error {
	s.mu.Lock()
	s.n++
	nth := s.n
	s.mu.Unlock()
	if nth == s.failOn {
		return errNoSession
	}
	return s.memSessionStore.CreateSession(ctx, rec)
}

func TestOIDCCallbackCreateAuthTokenError(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	store := &failNthCreateStore{memSessionStore: newMemStore(), failOn: 2}
	sm := webauthn.NewSessionManager(store)
	enc, err := auth.EncryptString("client-secret-value", testMasterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	h := NewOIDCHandler(newFakeSettings(map[string]string{
		OIDCEnabledKey:       "true",
		OIDCIssuerURLKey:     idp.server.URL,
		OIDCClientIDKey:      oidcTestClientID,
		OIDCClientSecretKey:  enc,
		OIDCAllowedEmailsKey: "admin@example.com",
		OIDCPublicBaseURLKey: "https://h.example",
	}), sm, mockIPLimiter{}, testMasterKey)

	loc, cookie := runStart(t, h) // CreateLoginState -> CreateSession #1 (ok)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
	// Everything validates; CreateAuthToken -> CreateSession #2 -> fails.
	frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
	if !strings.HasPrefix(frag, "oidc_error=") {
		t.Fatalf("expected error fragment on session-create failure, got %q", frag)
	}
}

func TestOIDCAllowlistDenyAndFailClosed(t *testing.T) {
	t.Run("email not allowlisted", func(t *testing.T) {
		idp := newMockIDP(t, oidcTestClientID)
		h, _, _ := newOIDCTestHandler(t, idp, "someone-else@example.com")
		loc, cookie := runStart(t, h)
		state := loc.Query().Get("state")
		idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
		frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
		if !strings.HasPrefix(frag, "oidc_error=") {
			t.Fatalf("expected denial, got %q", frag)
		}
	})

	t.Run("empty allowlist fails closed", func(t *testing.T) {
		idp := newMockIDP(t, oidcTestClientID)
		h, _, _ := newOIDCTestHandler(t, idp, "   ") // whitespace only -> empty set
		loc, cookie := runStart(t, h)
		state := loc.Query().Get("state")
		idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
		frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
		if !strings.HasPrefix(frag, "oidc_error=") {
			t.Fatalf("empty allowlist must deny all, got %q", frag)
		}
	})
}

// TestOIDCProviderCacheRebuild verifies that editing settings (here the
// allowlist) takes effect without reconstructing the handler: the cached runtime
// is rebuilt when the config fingerprint changes.
func TestOIDCProviderCacheRebuild(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, fs, sessionMgr := newOIDCTestHandler(t, idp, "nobody@example.com")

	// First attempt: not allowlisted -> denied.
	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
	if frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}}); !strings.HasPrefix(frag, "oidc_error=") {
		t.Fatalf("expected initial denial, got %q", frag)
	}

	// Edit the allowlist; no handler reconstruction.
	fs.set(OIDCAllowedEmailsKey, "admin@example.com")

	loc, cookie = runStart(t, h)
	state = loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)
	frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
	token, err := url.QueryUnescape(strings.TrimPrefix(frag, "oidc_token="))
	if err != nil || !strings.HasPrefix(frag, "oidc_token=") {
		t.Fatalf("expected success after allowlist edit, got %q", frag)
	}
	if !sessionMgr.Validate(context.Background(), token) {
		t.Fatal("token after rebuild failed Validate")
	}
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "a***@example.com",
		"a@example.com":     "*@example.com",
		"not-an-email":      "***",
		"":                  "***",
	}
	for in, want := range cases {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOIDCRegisterRoutes mounts the handler on a chi router and confirms the
// status route is reachable through it (Register wiring).
func TestOIDCRegisterRoutes(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, _ := newOIDCTestHandler(t, idp, "admin@example.com")
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/status", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status via router = %d, want 200", rec.Code)
	}
	var resp oidcStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.Enabled {
		t.Fatalf("expected enabled status via router, got %q", rec.Body.String())
	}
}

// TestOIDCStartErrors covers Start's non-happy branches: disabled, under-
// configured, provider build failures, and a login-state persistence failure.
func TestOIDCStartErrors(t *testing.T) {
	newH := func(masterKey string, kv map[string]string) *OIDCHandler {
		sm := webauthn.NewSessionManager(newMemStore())
		return NewOIDCHandler(newFakeSettings(kv), sm, mockIPLimiter{}, masterKey)
	}
	call := func(h *OIDCHandler) int {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/start", http.NoBody)
		rec := httptest.NewRecorder()
		h.Start(rec, req)
		return rec.Code
	}

	t.Run("disabled -> 400", func(t *testing.T) {
		if got := call(newH(testMasterKey, map[string]string{OIDCEnabledKey: "false"})); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", got)
		}
	})

	t.Run("under-configured -> 400", func(t *testing.T) {
		// enabled but no issuer -> build returns {enabled:false}.
		h := newH(testMasterKey, map[string]string{
			OIDCEnabledKey:       "true",
			OIDCClientIDKey:      "x",
			OIDCPublicBaseURLKey: "https://h.example",
		})
		if got := call(h); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", got)
		}
	})

	t.Run("bad issuer (discovery fails) -> 503", func(t *testing.T) {
		idp := newMockIDP(t, oidcTestClientID)
		h := newH(testMasterKey, map[string]string{
			OIDCEnabledKey:       "true",
			OIDCIssuerURLKey:     idp.server.URL + "/nonexistent", // discovery 404s
			OIDCClientIDKey:      "x",
			OIDCPublicBaseURLKey: "https://h.example",
		})
		if got := call(h); got != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", got)
		}
	})

	t.Run("undecryptable secret -> 503", func(t *testing.T) {
		idp := newMockIDP(t, oidcTestClientID)
		// Encrypt under a different key so decrypt with testMasterKey fails (GCM
		// auth mismatch), exercising build's decrypt-error path.
		enc, err := auth.EncryptString("s", "some-other-master-key-1234567890")
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		h := newH(testMasterKey, map[string]string{
			OIDCEnabledKey:       "true",
			OIDCIssuerURLKey:     idp.server.URL,
			OIDCClientIDKey:      "x",
			OIDCClientSecretKey:  enc,
			OIDCPublicBaseURLKey: "https://h.example",
		})
		if got := call(h); got != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", got)
		}
	})

	t.Run("secret set but no master key -> 503", func(t *testing.T) {
		idp := newMockIDP(t, oidcTestClientID)
		enc, err := auth.EncryptString("s", testMasterKey)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		h := newH("", map[string]string{ // empty master key
			OIDCEnabledKey:       "true",
			OIDCIssuerURLKey:     idp.server.URL,
			OIDCClientIDKey:      "x",
			OIDCClientSecretKey:  enc,
			OIDCPublicBaseURLKey: "https://h.example",
		})
		if got := call(h); got != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", got)
		}
	})

	t.Run("login-state persistence failure -> 500", func(t *testing.T) {
		idp := newMockIDP(t, oidcTestClientID)
		store := newMemStore()
		store.createErr = errNoSession // CreateLoginState fails
		sm := webauthn.NewSessionManager(store)
		enc, err := auth.EncryptString("client-secret-value", testMasterKey)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		h := NewOIDCHandler(newFakeSettings(map[string]string{
			OIDCEnabledKey:       "true",
			OIDCIssuerURLKey:     idp.server.URL,
			OIDCClientIDKey:      oidcTestClientID,
			OIDCClientSecretKey:  enc,
			OIDCAllowedEmailsKey: "admin@example.com",
			OIDCPublicBaseURLKey: "https://h.example",
		}), sm, mockIPLimiter{}, testMasterKey)
		if got := call(h); got != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", got)
		}
	})
}

// claimRaceStore simulates the single-use TOCTOU: GetSession always returns the
// record (as if two callbacks both read it before either delete), but only the
// first DeleteSession succeeds. ConsumeLoginState must let only the first caller
// through.
type claimRaceStore struct {
	*memSessionStore
	rec     *webauthn.SessionRecord
	deletes int
}

func (s *claimRaceStore) GetSession(context.Context, uuid.UUID) (*webauthn.SessionRecord, error) {
	cp := *s.rec
	return &cp, nil
}

func (s *claimRaceStore) DeleteSession(context.Context, uuid.UUID) error {
	s.deletes++
	if s.deletes == 1 {
		return nil // first caller's DELETE removes the row
	}
	return errNoSession // row already gone for every later caller
}

func TestConsumeLoginStateSingleUseUnderRace(t *testing.T) {
	store := &claimRaceStore{
		memSessionStore: newMemStore(),
		rec: &webauthn.SessionRecord{
			Type:        "oidc_login",
			SessionData: []byte("blob"),
			ExpiresAt:   time.Now().Add(time.Minute),
		},
	}
	sm := webauthn.NewSessionManager(store)
	id := uuid.New()

	data, err := sm.ConsumeLoginState(context.Background(), id)
	if err != nil || string(data) != "blob" {
		t.Fatalf("first consume: data=%q err=%v, want blob/nil", data, err)
	}
	// Second caller saw the same record (GetSession still returns it) but its
	// DELETE removed nothing, so the single-use guard must reject it.
	if _, err := sm.ConsumeLoginState(context.Background(), id); err == nil {
		t.Fatal("second consume should fail (record already claimed)")
	}
}
