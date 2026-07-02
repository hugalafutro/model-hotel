package adminauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
}

func (s *fakeUserStore) GetByUsername(_ context.Context, username string) (*user.User, error) {
	if u, ok := s.byUsername[username]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}

func (s *fakeUserStore) TouchLastLogin(_ context.Context, id uuid.UUID) error {
	s.touched = append(s.touched, id)
	return nil
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
