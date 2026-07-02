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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	h := NewUserLoginHandler(store, sm, mockIPLimiter{})
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
	h := NewUserLoginHandler(store, sm, mockIPLimiter{})
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
	h := NewUserLoginHandler(store, sm, mockIPLimiter{})
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
