package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	otptotp "github.com/pquerna/otp/totp"

	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

const userTotpTestMasterKey = "usertotp-test-master-key"

// setupUserTotpTest wires the multi-user stack plus the per-user TOTP factory,
// mirroring main.go.
func setupUserTotpTest(t *testing.T) (chi.Router, *webauthn.SessionManager) {
	t.Helper()
	h, r := newTestHandlerWithRouter(t)

	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, webauthn_sessions CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sessionMgr)
	h.SetUserAuth(userRepo, webauthnRepo)
	h.SetUserTotp(func(id uuid.UUID) *totpsvc.Repository {
		return totpsvc.NewRepositoryWithStore(totpsvc.NewUserPostgresStore(pool, id), userTotpTestMasterKey)
	})
	return r, sessionMgr
}

// userSession creates a user via the admin API and returns its id + session token.
func userSession(t *testing.T, r chi.Router, sm *webauthn.SessionManager, username string) (string, string) {
	t.Helper()
	id := createUserViaAPI(t, r, username, "password123", "user", []string{"chat"})
	token, err := sm.CreateAuthToken(context.Background(), []byte(id), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	return id, token
}

// totpCodeAt generates a code for the enrolled secret offset by `steps`
// 30-second windows, so chained operations never collide on the single-use
// step guard.
func totpCodeAt(t *testing.T, secret string, steps int) string {
	t.Helper()
	code, err := otptotp.GenerateCode(secret, time.Now().Add(time.Duration(steps)*30*time.Second))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	return code
}

// enrollUserTotp drives enroll/start + enroll/verify over HTTP and returns the
// secret and recovery codes.
func enrollUserTotp(t *testing.T, r chi.Router, token string) (string, []string) {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/auth/totp/enroll/start", token, "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/start: %d %s", w.Code, w.Body.String())
	}
	var start struct {
		URI    string `json:"uri"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &start); err != nil {
		t.Fatalf("bad start body: %v", err)
	}
	w = doJSON(t, r, http.MethodPost, "/auth/totp/enroll/verify", token,
		`{"code":"`+totpCodeAt(t, start.Secret, -1)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/verify: %d %s", w.Code, w.Body.String())
	}
	var verify struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &verify); err != nil {
		t.Fatalf("bad verify body: %v", err)
	}
	if len(verify.RecoveryCodes) == 0 {
		t.Fatal("no recovery codes returned")
	}
	return start.Secret, verify.RecoveryCodes
}

func TestUserTotp_EnvAdminRejected(t *testing.T) {
	r, _ := setupUserTotpTest(t)
	// The env-token admin has no users row; every self-service surface must
	// point it at /api/totp instead of silently operating on nothing. Each
	// handler returns early on the callerTotpRepo !ok result.
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/auth/totp/status"},
		{http.MethodPost, "/auth/totp/enroll/start"},
		{http.MethodPost, "/auth/totp/enroll/verify"},
		{http.MethodPost, "/auth/totp/disable"},
	} {
		w := doJSON(t, r, tc.method, tc.path, envAdminToken, "{}")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400 (body %s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestUserTotp_ResetInvalidID(t *testing.T) {
	r, _ := setupUserTotpTest(t)
	// A malformed id never reaches the store: parseUUIDParam answers 400.
	if w := doJSON(t, r, http.MethodPost, "/users/not-a-uuid/totp/reset", envAdminToken, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("reset bad id = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}

func TestUserTotp_DisableThrottlesGuessing(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	_, token := userSession(t, r, sm, "totp-throttle")
	enrollUserTotp(t, r, token)

	// Repeated wrong disable codes must back off (429) before the code is even
	// checked, so a hijacked session cannot brute-force the 6-digit window and
	// switch the second factor off.
	var got429 bool
	for range 8 {
		w := doJSON(t, r, http.MethodPost, "/auth/totp/disable", token, `{"code":"000000"}`)
		if w.Code == http.StatusTooManyRequests {
			if w.Header().Get("Retry-After") == "" {
				t.Error("429 without Retry-After header")
			}
			got429 = true
			break
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status %d (body %s)", w.Code, w.Body.String())
		}
	}
	if !got429 {
		t.Fatal("disable throttle never engaged after repeated wrong codes")
	}
}

func TestUserTotp_EnrollFlow(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	_, token := userSession(t, r, sm, "alice")

	// Fresh account: disabled.
	w := doJSON(t, r, http.MethodGet, "/auth/totp/status", token, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("initial status: %d %s", w.Code, w.Body.String())
	}

	// Start carries the username in the otpauth URI.
	w = doJSON(t, r, http.MethodPost, "/auth/totp/enroll/start", token, "{}")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "alice") {
		t.Fatalf("enroll/start: %d %s", w.Code, w.Body.String())
	}

	// A wrong code does not enable anything.
	w = doJSON(t, r, http.MethodPost, "/auth/totp/enroll/verify", token, `{"code":"000000"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad-code verify: %d, want 400", w.Code)
	}

	secret, _ := enrollUserTotp(t, r, token)

	// Status now reports enabled + recovery counts.
	w = doJSON(t, r, http.MethodGet, "/auth/totp/status", token, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":true`) ||
		!strings.Contains(w.Body.String(), "recovery_remaining") {
		t.Fatalf("enabled status: %d %s", w.Code, w.Body.String())
	}

	// Re-enroll while active is refused (the gate never moves under live 2FA).
	w = doJSON(t, r, http.MethodPost, "/auth/totp/enroll/start", token, "{}")
	if w.Code != http.StatusConflict {
		t.Fatalf("re-enroll: %d, want 409", w.Code)
	}

	// Disable requires a valid code.
	w = doJSON(t, r, http.MethodPost, "/auth/totp/disable", token, `{"code":"000000"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad-code disable: %d, want 401", w.Code)
	}
	w = doJSON(t, r, http.MethodPost, "/auth/totp/disable", token,
		`{"code":"`+totpCodeAt(t, secret, 1)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet, "/auth/totp/status", token, "")
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("post-disable status: %s", w.Body.String())
	}
}

func TestUserTotp_DisableWithRecoveryCode(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	_, token := userSession(t, r, sm, "alice")
	_, codes := enrollUserTotp(t, r, token)

	w := doJSON(t, r, http.MethodPost, "/auth/totp/disable", token, `{"code":"`+codes[0]+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("recovery disable: %d %s", w.Code, w.Body.String())
	}
}

func TestUserTotp_AdminReset(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	aliceID, aliceToken := userSession(t, r, sm, "alice")
	_, bobToken := userSession(t, r, sm, "bob")
	enrollUserTotp(t, r, aliceToken)

	// The list shows the badge flag.
	w := doJSON(t, r, http.MethodGet, "/users", envAdminToken, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totp_enabled":true`) {
		t.Fatalf("list totp_enabled: %d %s", w.Code, w.Body.String())
	}

	// Non-admin cannot reset anyone.
	if w := doJSON(t, r, http.MethodPost, "/users/"+aliceID+"/totp/reset", bobToken, ""); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin reset: %d, want 403", w.Code)
	}
	// Unknown target is a 404, not a silent no-op.
	if w := doJSON(t, r, http.MethodPost, "/users/"+uuid.NewString()+"/totp/reset", envAdminToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown reset: %d, want 404", w.Code)
	}
	// Admin reset kills the second factor without a code.
	if w := doJSON(t, r, http.MethodPost, "/users/"+aliceID+"/totp/reset", envAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("admin reset: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet, "/auth/totp/status", aliceToken, "")
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("post-reset status: %s", w.Body.String())
	}
}

func TestUserTotp_SurfaceNotWiredAnd404s(t *testing.T) {
	// Handler without SetUserTotp: both the self-service surface and the
	// admin reset answer 404 instead of nil-panicking.
	_, r := newTestHandlerWithRouter(t)
	if w := doJSON(t, r, http.MethodGet, "/auth/totp/status", envAdminToken, ""); w.Code != http.StatusNotFound {
		t.Errorf("unwired status: %d, want 404", w.Code)
	}
	if w := doJSON(t, r, http.MethodPost, "/users/"+uuid.NewString()+"/totp/reset", envAdminToken, ""); w.Code != http.StatusNotFound {
		t.Errorf("unwired reset: %d, want 404", w.Code)
	}
}

func TestUserTotp_EdgeResponses(t *testing.T) {
	r, sm := setupUserTotpTest(t)
	_, token := userSession(t, r, sm, "totp-edges")
	enrollUserTotp(t, r, token)

	// Re-enrolling over a live second factor is refused.
	if w := doJSON(t, r, http.MethodPost, "/auth/totp/enroll/start", token, ""); w.Code != http.StatusConflict {
		t.Errorf("enroll while enabled: %d, want 409", w.Code)
	}
	// Malformed bodies are 400s.
	if w := doJSON(t, r, http.MethodPost, "/auth/totp/enroll/verify", token, `{not json`); w.Code != http.StatusBadRequest {
		t.Errorf("verify bad body: %d, want 400", w.Code)
	}
	if w := doJSON(t, r, http.MethodPost, "/auth/totp/disable", token, `{not json`); w.Code != http.StatusBadRequest {
		t.Errorf("disable bad body: %d, want 400", w.Code)
	}
	// A wrong disable code is a 401 and leaves 2FA on.
	if w := doJSON(t, r, http.MethodPost, "/auth/totp/disable", token, `{"code":"000000"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("disable wrong code: %d, want 401", w.Code)
	}
	// Admin reset of a nonexistent user is a 404, not a silent no-op.
	if w := doJSON(t, r, http.MethodPost, "/users/"+uuid.NewString()+"/totp/reset", envAdminToken, ""); w.Code != http.StatusNotFound {
		t.Errorf("reset unknown user: %d, want 404", w.Code)
	}
}
