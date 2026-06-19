package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	otptotp "github.com/pquerna/otp/totp"

	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// mockIPLimiter is defined in handler_webauthn_test.go.

// truncateTOTPTables cleans TOTP state between tests (newTestHandler does NOT).
func truncateTOTPTables(t *testing.T) {
	t.Helper()
	if apiTestDB == nil {
		t.Skip("skipping: test database not available")
	}
	_, err := apiTestDB.Pool().Exec(context.Background(),
		`TRUNCATE admin_totp, admin_totp_recovery`)
	if err != nil {
		t.Fatalf("failed to truncate totp tables: %v", err)
	}
}

// mockStubTotpStatus is a stub TotpStatus for the AuthMiddleware tests.
type stubTotpStatus struct {
	enabled bool
	err     error
}

func (s *stubTotpStatus) IsEnabled(context.Context) (bool, error) {
	return s.enabled, s.err
}

// newTotpTestHandler builds a Handler + TOTP handler wired over the test DB.
// The shared totpEnabled cache is driven through the real refresh path.
func newTotpTestHandler(t *testing.T) (*Handler, *TotpHandler) {
	t.Helper()
	truncateTOTPTables(t)

	h := newTestHandler(t)
	totpRepo := totpsvc.NewRepository(apiTestDB.Pool(), testMasterKey)
	h.SetTotpStatus(totpRepo)
	// Force a synchronous seed so tests don't race the goroutine.
	h.totpEnabled.Store(false)

	// Clean up TOTP state after the test too.
	t.Cleanup(func() { truncateTOTPTables(t) })

	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	// Create a fresh session manager over the test DB and wire it on the
	// Handler so AuthMiddleware's session-token fallback can validate tokens
	// minted by /totp/login (which uses the TotpHandler.sessionMgr).
	wrepo := webauthn.NewRepository(apiTestDB.Pool())
	sessionMgr := webauthn.NewSessionManager(wrepo)
	h.SetWebAuthnSessionManager(sessionMgr)

	th := NewTotpHandler(totpRepo, adminMgr, sessionMgr, mockIPLimiter{}, false, h.TotpEnabled, h.RefreshTotpEnabled)
	return h, th
}

// enrollAndEnable drives an enrollment via the repo (faster than HTTP for
// tests that only need TOTP "on"). Returns the secret so callers can generate
// valid codes.
func enrollAndEnable(t *testing.T, repo *totpsvc.Repository) string {
	t.Helper()
	_, secret, err := repo.Enroll(context.Background())
	if err != nil {
		t.Fatalf("Enroll failed: %v", err)
	}
	if err := repo.Enable(context.Background()); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}
	return secret
}

// validCode generates a TOTP code valid at the current time. It retries up to
// 3 times in case of a TOTP-window boundary collision.
func validCode(t *testing.T, secret string) string {
	t.Helper()
	for range 3 {
		code, err := otptotp.GenerateCode(secret, time.Now())
		if err == nil {
			return code
		}
	}
	t.Fatal("otptotp.GenerateCode failed after 3 attempts")
	return ""
}

// codeForStep returns a TOTP code offset by `steps` 30-second windows from now.
// Single-use enforcement (one accepted code per step) means a test that chains
// enroll + login + disable must use distinct, increasing steps within the
// skew=1 window: enroll -1, login 0 (validCode), disable +1.
func codeForStep(t *testing.T, secret string, steps int) string {
	t.Helper()
	c, err := otptotp.GenerateCode(secret, time.Now().Add(time.Duration(steps)*30*time.Second))
	if err != nil {
		t.Fatalf("GenerateCode(step %d): %v", steps, err)
	}
	return c
}

// --- Helper: HTTP-driver through a router ---

func serveTotpRouter(th *TotpHandler) chi.Router {
	r := chi.NewRouter()
	th.Register(r)
	return r
}

// --- Status tests ---

func TestTotpStatus_Disabled(t *testing.T) {
	h, th := newTotpTestHandler(t)
	_ = h

	r := serveTotpRouter(th)
	req := httptest.NewRequest(http.MethodGet, "/totp/status", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", resp["enabled"])
	}
}

func TestTotpStatus_Enabled(t *testing.T) {
	h, th := newTotpTestHandler(t)
	enrollAndEnable(t, th.totpRepo)
	// Refresh the cache so the handler sees TOTP as enabled.
	h.RefreshTotpEnabled(context.Background())

	r := serveTotpRouter(th)
	req := httptest.NewRequest(http.MethodGet, "/totp/status", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", resp["enabled"])
	}
}

// --- EnrollStart tests ---

func TestTotpEnrollStart_AdminAuth(t *testing.T) {
	_, th := newTotpTestHandler(t)

	body := bytes.NewReader(nil)
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", body)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	serveTotpRouter(th).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["uri"] == "" || resp["secret"] == "" {
		t.Errorf("expected non-empty uri+secret, got %+v", resp)
	}
}

func TestTotpEnrollStart_DemoReadOnly(t *testing.T) {
	truncateTOTPTables(t)
	t.Cleanup(func() { truncateTOTPTables(t) })

	totpRepo := totpsvc.NewRepository(apiTestDB.Pool(), testMasterKey)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	wrepo := webauthn.NewRepository(apiTestDB.Pool())
	sessionMgr := webauthn.NewSessionManager(wrepo)
	th := NewTotpHandler(totpRepo, adminMgr, sessionMgr, mockIPLimiter{}, true, func() bool { return false }, func(context.Context) {})

	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 (read-only), got %d: %s", w.Code, w.Body.String())
	}
}

// --- EnrollVerify tests ---

func TestTotpEnrollVerify_HappyPath(t *testing.T) {
	_, th := newTotpTestHandler(t)

	// Enroll start.
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var startResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	secret := startResp["secret"]

	// Enroll verify with a valid code.
	code := validCode(t, secret)
	vreq := httptest.NewRequest(http.MethodPost, "/totp/enroll/verify",
		bytes.NewReader([]byte(`{"code":"`+code+`"}`)))
	vreq.Header.Set("Authorization", "Bearer admin-token")
	vreq.Header.Set("Content-Type", "application/json")
	vw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(vw, vreq)

	if vw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", vw.Code, vw.Body.String())
	}
	var verifyResp struct {
		RecoveryCodes []string `json:"recovery_codes"`
		Token         string   `json:"token"`
	}
	if err := json.Unmarshal(vw.Body.Bytes(), &verifyResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(verifyResp.RecoveryCodes) != 10 {
		t.Errorf("expected 10 recovery codes, got %d", len(verifyResp.RecoveryCodes))
	}
	// Enabling 2FA invalidates the raw admin token, so enroll/verify mints a
	// session token (kept by the UI) to avoid logging the admin out.
	if verifyResp.Token == "" {
		t.Error("expected a session token in the enroll/verify response")
	}
}

func TestTotpEnrollVerify_InvalidCode(t *testing.T) {
	_, th := newTotpTestHandler(t)

	// Enroll start.
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify with "000000" -- retry on collision (unusual but possible).
	for range 3 {
		vreq := httptest.NewRequest(http.MethodPost, "/totp/enroll/verify",
			bytes.NewReader([]byte(`{"code":"000000"}`)))
		vreq.Header.Set("Authorization", "Bearer admin-token")
		vreq.Header.Set("Content-Type", "application/json")
		vw := httptest.NewRecorder()
		serveTotpRouter(th).ServeHTTP(vw, vreq)

		if vw.Code == http.StatusBadRequest {
			return // expected outcome
		}
		// If the code happened to be valid (rare jackpot), just skip.
		if vw.Code == http.StatusOK {
			t.Skip("000000 accidentally matched; skipping")
		}
	}
	t.Errorf("expected 400 for invalid code, got non-400 after 3 tries")
}

// --- Disable tests ---

// doEnrollVerify drives through enroll/start + enroll/verify and returns the
// secret + recovery_codes. After EnrollVerify TOTP is enabled, so subsequent
// mutation requests must use a SESSION token (raw admin is rejected by the
// TOTP gate).
func doEnrollVerify(t *testing.T, th *TotpHandler) (string, []string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var startResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	secret := startResp["secret"]

	// enroll uses step -1 so a follow-up login (step 0) / disable (step +1) in
	// the same test get distinct, increasing single-use steps.
	code := codeForStep(t, secret, -1)
	vreq := httptest.NewRequest(http.MethodPost, "/totp/enroll/verify",
		bytes.NewReader([]byte(`{"code":"`+code+`"}`)))
	vreq.Header.Set("Authorization", "Bearer admin-token")
	vreq.Header.Set("Content-Type", "application/json")
	vw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(vw, vreq)
	if vw.Code != http.StatusOK {
		t.Fatalf("enroll/verify: expected 200, got %d: %s", vw.Code, vw.Body.String())
	}
	var verifyResp struct {
		RecoveryCodes []string `json:"recovery_codes"`
		Token         string   `json:"token"`
	}
	if err := json.Unmarshal(vw.Body.Bytes(), &verifyResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return secret, verifyResp.RecoveryCodes
}

// sessionTokenAfterEnroll logs in via /totp/login to obtain a session token
// for the post-EnrollVerify state (where raw admin is rejected by the gate).
func sessionTokenAfterEnroll(t *testing.T, th *TotpHandler, secret string) string {
	t.Helper()
	code := validCode(t, secret)
	body := []byte(`{"token":"admin-token","code":"` + code + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("totp/login: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp["token"]
}

func TestTotpDisable_WithTotpCode(t *testing.T) {
	_, th := newTotpTestHandler(t)
	secret, _ := doEnrollVerify(t, th)
	// TOTP is now on: raw admin token is rejected by the gate, so obtain a
	// session token via /totp/login.
	sessionToken := sessionTokenAfterEnroll(t, th, secret)

	// step +1: enroll used -1 and the login above used 0 (single-use steps).
	code := codeForStep(t, secret, 1)
	dreq := httptest.NewRequest(http.MethodPost, "/totp/disable",
		bytes.NewReader([]byte(`{"code":"`+code+`"}`)))
	dreq.Header.Set("Authorization", "Bearer "+sessionToken)
	dreq.Header.Set("Content-Type", "application/json")
	dw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(dw, dreq)

	if dw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", dw.Code, dw.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(dw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["disabled"] != true {
		t.Errorf("expected disabled=true, got %v", resp["disabled"])
	}
}

func TestTotpDisable_WithRecoveryCode(t *testing.T) {
	_, th := newTotpTestHandler(t)
	secret, recoveryCodes := doEnrollVerify(t, th)
	if len(recoveryCodes) == 0 {
		t.Fatal("expected non-empty recovery codes")
	}
	sessionToken := sessionTokenAfterEnroll(t, th, secret)

	rc := recoveryCodes[0]
	dreq := httptest.NewRequest(http.MethodPost, "/totp/disable",
		bytes.NewReader([]byte(`{"code":"`+rc+`"}`)))
	dreq.Header.Set("Authorization", "Bearer "+sessionToken)
	dreq.Header.Set("Content-Type", "application/json")
	dw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(dw, dreq)

	if dw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", dw.Code, dw.Body.String())
	}
}

func TestTotpDisable_InvalidCode(t *testing.T) {
	_, th := newTotpTestHandler(t)
	secret, _ := doEnrollVerify(t, th)
	sessionToken := sessionTokenAfterEnroll(t, th, secret)

	dreq := httptest.NewRequest(http.MethodPost, "/totp/disable",
		bytes.NewReader([]byte(`{"code":"BOGUS-CODE-NOT-VALID"}`)))
	dreq.Header.Set("Authorization", "Bearer "+sessionToken)
	dreq.Header.Set("Content-Type", "application/json")
	dw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(dw, dreq)

	if dw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", dw.Code, dw.Body.String())
	}
}

// --- Login tests ---

func TestTotpLogin_HappyPath(t *testing.T) {
	h, th := newTotpTestHandler(t)
	secret, _ := doEnrollVerify(t, th)
	// After enroll/verify, the cache should be refreshed (true).
	if !h.TotpEnabled() {
		t.Fatal("expected TOTP enabled after enroll/verify")
	}

	code := validCode(t, secret)
	body := []byte(`{"token":"admin-token","code":"` + code + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sessionToken := resp["token"]
	if sessionToken == "" {
		t.Fatal("expected non-empty session token")
	}

	// Assert the session token passes AuthMiddleware on a protected route.
	mw := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	protectedReq := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	protectedReq.Header.Set("Authorization", "Bearer "+sessionToken)
	pw := httptest.NewRecorder()
	mw.ServeHTTP(pw, protectedReq)
	if pw.Code != http.StatusOK {
		t.Errorf("session token should pass AuthMiddleware, got %d", pw.Code)
	}
}

func TestTotpLogin_RecoveryCode(t *testing.T) {
	h, th := newTotpTestHandler(t)
	_, recoveryCodes := doEnrollVerify(t, th)
	if !h.TotpEnabled() {
		t.Fatal("expected TOTP enabled after enroll/verify")
	}
	if len(recoveryCodes) == 0 {
		t.Fatal("expected recovery codes")
	}

	rc := recoveryCodes[1]
	body := []byte(`{"token":"admin-token","code":"` + rc + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["token"] == "" {
		t.Error("expected non-empty session token")
	}

	// session token must pass AuthMiddleware.
	mw := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	protectedReq := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	protectedReq.Header.Set("Authorization", "Bearer "+resp["token"])
	pw := httptest.NewRecorder()
	mw.ServeHTTP(pw, protectedReq)
	if pw.Code != http.StatusOK {
		t.Errorf("recovery-code session token should pass AuthMiddleware, got %d", pw.Code)
	}
}

func TestTotpLogin_BadToken(t *testing.T) {
	_, th := newTotpTestHandler(t)
	secret, _ := doEnrollVerify(t, th)

	code := validCode(t, secret)
	body := []byte(`{"token":"wrong","code":"` + code + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTotpLogin_BadCode(t *testing.T) {
	_, th := newTotpTestHandler(t)
	doEnrollVerify(t, th)

	body := []byte(`{"token":"admin-token","code":"000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	for range 3 {
		w := httptest.NewRecorder()
		serveTotpRouter(th).ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized {
			return
		}
		if w.Code == http.StatusOK {
			t.Skip("000000 accidentally matched; skipping")
		}
	}
	t.Errorf("expected 401 for bad code, got non-401 after 3 tries")
}

func TestTotpLogin_BadBoth(t *testing.T) {
	_, th := newTotpTestHandler(t)
	doEnrollVerify(t, th)

	for range 3 {
		body := []byte(`{"token":"wrong","code":"000000"}`)
		req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveTotpRouter(th).ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized {
			return
		}
	}
	t.Errorf("expected 401 for bad token+code, got non-401 after 3 tries")
}

func TestTotpLogin_Disabled(t *testing.T) {
	_, th := newTotpTestHandler(t)
	// TOTP not enabled (fresh state).

	body := []byte(`{"token":"admin-token","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/totp/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (TOTP not enabled), got %d: %s", w.Code, w.Body.String())
	}
}

// --- adminOrSessionAuth TOTP gate test ---

func TestTotpAdminOrSessionAuth_TotpOn_RejectsRawToken(t *testing.T) {
	truncateTOTPTables(t)
	t.Cleanup(func() { truncateTOTPTables(t) })

	totpRepo := totpsvc.NewRepository(apiTestDB.Pool(), testMasterKey)
	enrollAndEnable(t, totpRepo)

	wrepo := webauthn.NewRepository(apiTestDB.Pool())
	sessionMgr := webauthn.NewSessionManager(wrepo)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}

	th := NewTotpHandler(totpRepo, adminMgr, sessionMgr, mockIPLimiter{}, false, func() bool { return true }, func(context.Context) {})

	// Raw admin token: with TOTP on, enroll/start must 401.
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (raw token rejected with TOTP on), got %d: %s", w.Code, w.Body.String())
	}

	// Session token: should pass.
	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w2, req2)
	// enroll/start over an already-enabled config resets to provisional, but
	// the auth gate is what we care about: anything != 401 means the session
	// token passed the gate.
	if w2.Code == http.StatusUnauthorized {
		t.Errorf("session token should pass adminOrSessionAuth (TOTP on), got 401: %s", w2.Body.String())
	}
}

// TestTotpLogin_Throttled drives repeated failed logins from one IP and asserts
// the per-IP backoff kicks in with a 429 + Retry-After once the threshold is
// exceeded (covers the throttle branch in Login).
func TestTotpLogin_Throttled(t *testing.T) {
	_, th := newTotpTestHandler(t)
	doEnrollVerify(t, th) // enables TOTP so /totp/login is active

	got429 := false
	for i := 0; i < 8; i++ {
		req := httptest.NewRequest(http.MethodPost, "/totp/login",
			bytes.NewReader([]byte(`{"token":"wrong","code":"000000"}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveTotpRouter(th).ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			if w.Header().Get("Retry-After") == "" {
				t.Error("429 response missing Retry-After header")
			}
			break
		}
	}
	if !got429 {
		t.Error("expected a 429 after repeated failed logins")
	}
}

// TestTotpLogin_RecoveryNotBurnedOnBadToken asserts a recovery code is NOT
// consumed when the admin token is invalid (recovery-code DoS guard): a code
// rejected with a bad token must still work with a valid token afterwards.
func TestTotpLogin_RecoveryNotBurnedOnBadToken(t *testing.T) {
	_, th := newTotpTestHandler(t)
	_, codes := doEnrollVerify(t, th)
	recovery := codes[0]

	login := func(token, code string) int {
		body := `{"token":"` + token + `","code":"` + code + `"}`
		req := httptest.NewRequest(http.MethodPost, "/totp/login",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		serveTotpRouter(th).ServeHTTP(w, req)
		return w.Code
	}

	// Bad token + valid recovery code: must fail and must NOT burn the code.
	if got := login("wrong-token", recovery); got != http.StatusUnauthorized {
		t.Fatalf("bad-token login: expected 401, got %d", got)
	}
	// Same recovery code with the real token must still succeed (not burned).
	if got := login("admin-token", recovery); got != http.StatusOK {
		t.Errorf("recovery code was burned by the bad-token attempt: expected 200, got %d", got)
	}
}

// TestTotpEnrollStart_RefreshesEnabledCache asserts that re-enrolling while TOTP
// is active refreshes the in-memory gate to match the DB (enabled=false),
// preventing a lockout if the re-enroll is abandoned.
func TestTotpEnrollStart_RefreshesEnabledCache(t *testing.T) {
	h, th := newTotpTestHandler(t)
	secret, _ := doEnrollVerify(t, th)
	if !h.TotpEnabled() {
		t.Fatal("precondition: TOTP should be enabled after enroll/verify")
	}
	// With TOTP enabled the raw admin token is rejected, so re-enroll uses a
	// session token (as the real UI does after /totp/login).
	sessionTok := sessionTokenAfterEnroll(t, th, secret)
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+sessionTok)
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if h.TotpEnabled() {
		t.Error("EnrollStart reset enabled=false in the DB but the cache still reports enabled")
	}
}

// doTotpPost POSTs a body to a TOTP route through the full router (auth gate
// included) and returns the recorder. Empty auth omits the Authorization header.
func doTotpPost(th *TotpHandler, path, auth, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	return w
}

func TestTotpEnrollStart_NoAuthRejected(t *testing.T) {
	_, th := newTotpTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("enroll/start without auth: expected 401, got %d", w.Code)
	}
}

func TestTotpHandlers_BadJSONBody(t *testing.T) {
	// TOTP disabled: the raw admin token passes the gate, so a malformed body
	// reaches the 400 path on the admin/session-gated mutation handlers.
	_, th := newTotpTestHandler(t)
	if w := doTotpPost(th, "/totp/enroll/verify", "admin-token", "{not json"); w.Code != http.StatusBadRequest {
		t.Errorf("enroll/verify bad body: expected 400, got %d", w.Code)
	}
	if w := doTotpPost(th, "/totp/disable", "admin-token", "{not json"); w.Code != http.StatusBadRequest {
		t.Errorf("disable bad body: expected 400, got %d", w.Code)
	}
}

func TestTotpLogin_BadJSONBody(t *testing.T) {
	_, th := newTotpTestHandler(t)
	doEnrollVerify(t, th) // login requires TOTP enabled
	if w := doTotpPost(th, "/totp/login", "", "{not json"); w.Code != http.StatusBadRequest {
		t.Errorf("login bad body: expected 400, got %d", w.Code)
	}
}
