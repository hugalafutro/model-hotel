package adminauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// fakeUserStore implements UserLoginStore in memory.
type fakeUserStore struct {
	byUsername map[string]*user.User
	touched    []uuid.UUID
	hasEnabled bool
	statusErr  error
	getErr     error // GetByUsername returns this (non-nil) instead of a lookup
	touchErr   error // TouchLastLogin returns this
}

func (s *fakeUserStore) GetByUsername(_ context.Context, username string) (*user.User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if u, ok := s.byUsername[username]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}

func (s *fakeUserStore) TouchLastLogin(_ context.Context, id uuid.UUID) error {
	s.touched = append(s.touched, id)
	return s.touchErr
}

func (s *fakeUserStore) HasEnabled(_ context.Context) (bool, error) {
	return s.hasEnabled, s.statusErr
}

func newLoginFixture(t *testing.T, users ...*user.User) (*UserLoginHandler, *fakeUserStore, *webauthn.SessionManager, chi.Router) {
	t.Helper()
	store := &fakeUserStore{byUsername: map[string]*user.User{}, hasEnabled: true}
	for _, u := range users {
		store.byUsername[u.Username] = u
	}
	sm := webauthn.NewSessionManager(newMemStore())
	h := NewUserLoginHandler(store, sm, mockIPLimiter{}, nil)
	r := chi.NewRouter()
	h.Register(r)
	return h, store, sm, r
}

func testUser(t *testing.T, username, password string, enabled bool) *user.User {
	t.Helper()
	hash, err := user.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return &user.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: hash,
		Role:         user.RoleUser,
		Grants:       []string{"chat"},
		Enabled:      enabled,
	}
}

func doLogin(t *testing.T, r chi.Router, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte(body)))
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUserLogin_Success(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	_, store, sm, r := newLoginFixture(t, u)

	w := doLogin(t, r, `{"username":"alice","password":"correct-horse"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	handle, ok := sm.TokenUser(context.Background(), resp["token"])
	if !ok {
		t.Fatal("minted token does not validate")
	}
	if string(handle) != u.ID.String() {
		t.Errorf("session handle = %q, want user uuid %q", handle, u.ID)
	}
	if len(store.touched) != 1 || store.touched[0] != u.ID {
		t.Errorf("TouchLastLogin not recorded: %v", store.touched)
	}
}

func TestUserLogin_Failures(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	disabled := testUser(t, "mallory", "correct-horse", false)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"wrong password", `{"username":"alice","password":"wrong"}`, http.StatusUnauthorized},
		{"unknown user", `{"username":"nobody","password":"whatever1"}`, http.StatusUnauthorized},
		{"disabled user", `{"username":"mallory","password":"correct-horse"}`, http.StatusUnauthorized},
		{"empty password", `{"username":"alice","password":""}`, http.StatusBadRequest},
		{"empty username", `{"username":"","password":"x"}`, http.StatusBadRequest},
		{"malformed body", `{"username":`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, r := newLoginFixture(t, u, disabled)
			w := doLogin(t, r, tc.body)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestUserLogin_UniformUnauthorizedBody(t *testing.T) {
	// Unknown-user and wrong-password responses must be indistinguishable.
	u := testUser(t, "alice", "correct-horse", true)
	_, _, _, r := newLoginFixture(t, u)

	w1 := doLogin(t, r, `{"username":"alice","password":"wrong"}`)
	w2 := doLogin(t, r, `{"username":"nobody","password":"wrong"}`)
	if w1.Code != http.StatusUnauthorized || w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401s, got %d and %d", w1.Code, w2.Code)
	}
	if w1.Body.String() != w2.Body.String() {
		t.Errorf("bodies differ: %q vs %q", w1.Body.String(), w2.Body.String())
	}
}

func TestUserLogin_ThrottleAfterFailures(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	_, _, _, r := newLoginFixture(t, u)

	var last *httptest.ResponseRecorder
	// The throttle locks the key once failures accumulate; a correct
	// password from the same IP must then be refused too.
	for range 10 {
		last = doLogin(t, r, `{"username":"alice","password":"wrong"}`)
		if last.Code == http.StatusTooManyRequests {
			break
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("never throttled, last status = %d", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After header")
	}
	w := doLogin(t, r, `{"username":"alice","password":"correct-horse"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("throttle bypassed by correct password: %d", w.Code)
	}
}

func TestUserLogin_PerUsernameThrottle(t *testing.T) {
	// Failures spread across source IPs never trip the per-IP throttle (one
	// failure each) but must still lock the targeted account.
	u := testUser(t, "alice", "correct-horse", true)
	_, _, _, r := newLoginFixture(t, u)

	attempt := func(ip, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte(body)))
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	var last *httptest.ResponseRecorder
	for i := range 10 {
		last = attempt(fmt.Sprintf("10.0.%d.1:1234", i), `{"username":"alice","password":"wrong"}`)
		if last.Code == http.StatusTooManyRequests {
			break
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("distributed brute force never throttled, last status = %d", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After header")
	}
	// The lock follows the username, not the IP: a fresh IP is refused too.
	if w := attempt("172.16.0.1:1234", `{"username":"alice","password":"correct-horse"}`); w.Code != http.StatusTooManyRequests {
		t.Errorf("account throttle bypassed from fresh IP: %d", w.Code)
	}
	// A different username from a fresh IP is unaffected by alice's lock.
	if w := attempt("172.16.0.2:1234", `{"username":"nobody","password":"whatever1"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("other username caught by alice's throttle: %d", w.Code)
	}
}

func TestUserLogin_LookupErrorIs500(t *testing.T) {
	// A GetByUsername failure that is not "not found" is an infrastructure
	// error, distinct from bad credentials: it must surface as 500, not 401.
	store := &fakeUserStore{byUsername: map[string]*user.User{}, hasEnabled: true, getErr: errors.New("db down")}
	sm := webauthn.NewSessionManager(newMemStore())
	h := NewUserLoginHandler(store, sm, mockIPLimiter{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	w := doLogin(t, r, `{"username":"alice","password":"whatever1"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("lookup error status = %d, want 500", w.Code)
	}
}

func TestUserLogin_MalformedStoredHashDenies(t *testing.T) {
	// A corrupt stored hash (DB tamper / foreign write) must deny the login as
	// 401, never 500 or a panic, and never authenticate.
	u := testUser(t, "alice", "correct-horse", true)
	u.PasswordHash = "not-a-valid-argon2id-hash"
	_, _, _, r := newLoginFixture(t, u)

	w := doLogin(t, r, `{"username":"alice","password":"correct-horse"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("malformed hash status = %d, want 401", w.Code)
	}
}

func TestUserLogin_TouchLastLoginFailureStillSucceeds(t *testing.T) {
	// Recording the last-login timestamp is best-effort: a failure there must
	// not break an otherwise-valid login.
	u := testUser(t, "alice", "correct-horse", true)
	store := &fakeUserStore{byUsername: map[string]*user.User{"alice": u}, hasEnabled: true, touchErr: errors.New("write failed")}
	sm := webauthn.NewSessionManager(newMemStore())
	h := NewUserLoginHandler(store, sm, mockIPLimiter{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	w := doLogin(t, r, `{"username":"alice","password":"correct-horse"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite TouchLastLogin failure", w.Code)
	}
}

func TestUserLogin_Status(t *testing.T) {
	_, store, _, r := newLoginFixture(t)

	get := func() map[string]bool {
		req := httptest.NewRequest(http.MethodGet, "/auth/status", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var resp map[string]bool
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("bad body: %v", err)
		}
		return resp
	}

	if resp := get(); !resp["enabled"] {
		t.Error("expected enabled=true")
	}
	store.hasEnabled = false
	if resp := get(); resp["enabled"] {
		t.Error("expected enabled=false")
	}
	// DB errors fail quiet: the form hides, other login paths still work.
	store.statusErr = errors.New("db down")
	if resp := get(); resp["enabled"] {
		t.Error("expected enabled=false on store error")
	}
}

// --- Per-user TOTP second factor ---

// fakeTotpStore is an in-memory totp.Store so login-time enforcement can be
// tested without Postgres. The crypto/policy layer on top is the real
// totp.Repository.
type fakeTotpStore struct {
	sec      totpsvc.EncryptedSecret
	enrolled bool
	enabled  bool
	lastStep *int64
	recovery map[string]bool // hash -> used
	err      error           // when set, every method fails with it
}

func (s *fakeTotpStore) UpsertEnrollment(_ context.Context, cipher, nonce, salt []byte) error {
	if s.err != nil {
		return s.err
	}
	s.sec = totpsvc.EncryptedSecret{Cipher: cipher, Nonce: nonce, Salt: salt}
	s.enrolled, s.enabled, s.lastStep = true, false, nil
	return nil
}

func (s *fakeTotpStore) LoadSecret(_ context.Context) (totpsvc.EncryptedSecret, bool, error) {
	if s.err != nil {
		return totpsvc.EncryptedSecret{}, false, s.err
	}
	return s.sec, s.enrolled, nil
}

func (s *fakeTotpStore) RecordUsedStep(_ context.Context, step int64) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if s.lastStep != nil && *s.lastStep >= step {
		return false, nil
	}
	s.lastStep = &step
	return true, nil
}

func (s *fakeTotpStore) Enable(_ context.Context) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if !s.enrolled {
		return false, nil
	}
	s.enabled = true
	return true, nil
}

func (s *fakeTotpStore) Disable(_ context.Context) error {
	if s.err != nil {
		return s.err
	}
	*s = fakeTotpStore{recovery: map[string]bool{}}
	return nil
}

func (s *fakeTotpStore) DisableIfAuthorized(_ context.Context, authorize totpsvc.DisableAuthorizer) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if !s.enrolled {
		return false, nil
	}
	unused := func(h string) (bool, error) {
		used, exists := s.recovery[h]
		return exists && !used, nil
	}
	ok, err := authorize(s.sec, s.lastStep, unused)
	if err != nil || !ok {
		return false, err
	}
	*s = fakeTotpStore{recovery: map[string]bool{}}
	return true, nil
}

func (s *fakeTotpStore) IsEnabled(_ context.Context) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.enrolled && s.enabled, nil
}

func (s *fakeTotpStore) EnabledAt(_ context.Context) (time.Time, bool, error) {
	if s.err != nil {
		return time.Time{}, false, s.err
	}
	if !s.enrolled || !s.enabled {
		return time.Time{}, false, nil
	}
	return time.Now(), true, nil
}

func (s *fakeTotpStore) RecoveryCounts(_ context.Context) (int, int, error) {
	if s.err != nil {
		return 0, 0, s.err
	}
	remaining := 0
	for _, used := range s.recovery {
		if !used {
			remaining++
		}
	}
	return remaining, len(s.recovery), nil
}

func (s *fakeTotpStore) LastUsedStep(_ context.Context) (*int64, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	return s.lastStep, s.enrolled, nil
}

func (s *fakeTotpStore) ReplaceRecoveryCodes(_ context.Context, hashes []string) error {
	if s.err != nil {
		return s.err
	}
	s.recovery = map[string]bool{}
	for _, h := range hashes {
		s.recovery[h] = false
	}
	return nil
}

func (s *fakeTotpStore) ConsumeRecoveryCode(_ context.Context, hash string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if used, exists := s.recovery[hash]; exists && !used {
		s.recovery[hash] = true
		return true, nil
	}
	return false, nil
}

// newTotpLoginFixture wires a login handler whose TOTP factory hands every
// user the same fake-store-backed repository, enrolled+enabled with the
// returned secret.
func newTotpLoginFixture(t *testing.T, u *user.User) (*fakeTotpStore, string, chi.Router) {
	t.Helper()
	store := &fakeUserStore{byUsername: map[string]*user.User{u.Username: u}, hasEnabled: true}
	fake := &fakeTotpStore{recovery: map[string]bool{}}
	repo := totpsvc.NewRepositoryWithStore(fake, testMasterKey)
	_, secret, err := repo.EnrollAs(context.Background(), u.Username)
	if err != nil {
		t.Fatalf("EnrollAs: %v", err)
	}
	if err := repo.Enable(context.Background()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	sm := webauthn.NewSessionManager(newMemStore())
	h := NewUserLoginHandler(store, sm, mockIPLimiter{}, func(uuid.UUID) *totpsvc.Repository { return repo })
	r := chi.NewRouter()
	h.Register(r)
	return fake, secret, r
}

func TestUserLogin_TotpRequired(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	_, _, r := newTotpLoginFixture(t, u)

	// Correct password, no code: told to supply the second factor.
	w := doLogin(t, r, `{"username":"alice","password":"correct-horse"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if !resp["totp_required"] {
		t.Errorf("missing totp_required flag: %s", w.Body.String())
	}

	// Wrong password with TOTP enabled must NOT reveal totp_required.
	w = doLogin(t, r, `{"username":"alice","password":"wrong"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("totp_required")) {
		t.Errorf("totp_required leaked on wrong password: %s", w.Body.String())
	}
}

func TestUserLogin_TotpValidCode(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	_, secret, r := newTotpLoginFixture(t, u)

	w := doLogin(t, r, `{"username":"alice","password":"correct-horse","code":"`+validCode(t, secret)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad body: %v", err)
	}
	if resp["token"] == "" {
		t.Error("no session token in response")
	}
}

func TestUserLogin_TotpWrongCodeThrottles(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	_, _, r := newTotpLoginFixture(t, u)

	var last *httptest.ResponseRecorder
	for range 10 {
		last = doLogin(t, r, `{"username":"alice","password":"correct-horse","code":"000000"}`)
		if last.Code == http.StatusTooManyRequests {
			break
		}
		if last.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", last.Code)
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("wrong codes never throttled, last status = %d", last.Code)
	}
}

func TestUserLogin_TotpRecoveryCode(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	fake, _, r := newTotpLoginFixture(t, u)

	repo := totpsvc.NewRepositoryWithStore(fake, testMasterKey)
	codes, err := repo.GenerateRecoveryCodes(context.Background())
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	body := `{"username":"alice","password":"correct-horse","code":"` + codes[0] + `"}`
	w := doLogin(t, r, body)
	if w.Code != http.StatusOK {
		t.Fatalf("recovery code login status = %d, body = %s", w.Code, w.Body.String())
	}
	// Single use: the same code is refused the second time.
	if w := doLogin(t, r, body); w.Code != http.StatusUnauthorized {
		t.Errorf("recovery code reuse status = %d, want 401", w.Code)
	}
}

func TestUserLogin_TotpStateErrorFailsClosed(t *testing.T) {
	u := testUser(t, "alice", "correct-horse", true)
	fake, secret, r := newTotpLoginFixture(t, u)
	fake.err = errors.New("db down")

	w := doLogin(t, r, `{"username":"alice","password":"correct-horse","code":"`+validCode(t, secret)+`"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (fail closed)", w.Code)
	}
}

// TestUserLogin_TotpPostgresStoreRoundTrip exercises the real UserPostgresStore
// (user_totp / user_totp_recovery, migration 052) end to end through the login
// handler: enroll, enable, code-gated login, recovery-code consumption, and
// the ON DELETE CASCADE cleanup when the user goes away.
func TestUserLogin_TotpPostgresStoreRoundTrip(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("test database not available")
	}
	ctx := context.Background()
	userRepo := user.NewRepository(apiTestDB.Pool())
	hash, err := user.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u, err := userRepo.Create(ctx, "totp-roundtrip", "", nil, hash, user.RoleUser, []string{"chat"}, user.Limits{})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = userRepo.Delete(context.Background(), u.ID) })

	factory := func(id uuid.UUID) *totpsvc.Repository {
		return totpsvc.NewRepositoryWithStore(totpsvc.NewUserPostgresStore(apiTestDB.Pool(), id), testMasterKey)
	}
	repo := factory(u.ID)
	uri, secret, err := repo.EnrollAs(ctx, u.Username)
	if err != nil {
		t.Fatalf("EnrollAs: %v", err)
	}
	if !bytes.Contains([]byte(uri), []byte("totp-roundtrip")) {
		t.Errorf("otpauth URI does not carry the username: %s", uri)
	}
	if enabled, err := repo.IsEnabled(ctx); err != nil || enabled {
		t.Fatalf("provisional enrollment must not be enabled (enabled=%v err=%v)", enabled, err)
	}
	if err := repo.Enable(ctx); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	codes, err := repo.GenerateRecoveryCodes(ctx)
	if err != nil || len(codes) == 0 {
		t.Fatalf("GenerateRecoveryCodes: %v (%d codes)", err, len(codes))
	}

	store := &fakeUserStore{byUsername: map[string]*user.User{u.Username: u}, hasEnabled: true}
	sm := webauthn.NewSessionManager(newMemStore())
	h := NewUserLoginHandler(store, sm, mockIPLimiter{}, factory)
	r := chi.NewRouter()
	h.Register(r)

	// No code: asked for the second factor.
	w := doLogin(t, r, `{"username":"totp-roundtrip","password":"correct-horse"}`)
	if w.Code != http.StatusUnauthorized || !bytes.Contains(w.Body.Bytes(), []byte("totp_required")) {
		t.Fatalf("no-code response: %d %s", w.Code, w.Body.String())
	}
	// Valid code (step -1 so the +0 recovery/login below never collides).
	w = doLogin(t, r, `{"username":"totp-roundtrip","password":"correct-horse","code":"`+codeForStep(t, secret, -1)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code login: %d %s", w.Code, w.Body.String())
	}
	// Recovery code works once.
	body := `{"username":"totp-roundtrip","password":"correct-horse","code":"` + codes[0] + `"}`
	if w := doLogin(t, r, body); w.Code != http.StatusOK {
		t.Fatalf("recovery login: %d %s", w.Code, w.Body.String())
	}
	if w := doLogin(t, r, body); w.Code != http.StatusUnauthorized {
		t.Fatalf("recovery reuse: %d, want 401", w.Code)
	}

	// Deleting the user cascades both TOTP tables away.
	if err := userRepo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var n int
	if err := apiTestDB.Pool().QueryRow(ctx,
		`SELECT (SELECT COUNT(*) FROM user_totp WHERE user_id = $1) + (SELECT COUNT(*) FROM user_totp_recovery WHERE user_id = $1)`,
		u.ID).Scan(&n); err != nil {
		t.Fatalf("cascade check: %v", err)
	}
	if n != 0 {
		t.Errorf("user_totp rows survived user deletion: %d", n)
	}
}
