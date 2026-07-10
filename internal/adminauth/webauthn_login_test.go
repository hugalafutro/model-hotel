package adminauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	webauthnx "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// TestWebAuthnHandler_LoginFinish_InvalidJSONBody tests that malformed JSON returns 400
func TestWebAuthnHandler_LoginFinish_InvalidJSONBody(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"invalid"`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_InvalidSessionID tests that invalid UUID returns 400
func TestWebAuthnHandler_LoginFinish_InvalidSessionID(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"session_id": "not-a-uuid", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_WrongSessionType tests that using a registration session for login returns 400
func TestWebAuthnHandler_LoginFinish_WrongSessionType(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Create a registration session (wrong type for login endpoint)
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		repo.DeleteSession(ctx, sessionID)
	})

	body := `{"session_id": "` + sessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid session type") {
		t.Errorf("expected 'invalid session type' error, got: %s", w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_SessionNotFound tests that non-existent session returns 400
func TestWebAuthnHandler_LoginFinish_SessionNotFound(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Use a valid UUID that doesn't exist in DB
	fakeSessionID := uuid.New()
	body := `{"session_id": "` + fakeSessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginStart_NilRelyingParty tests that LoginStart panics when relyingParty is nil
// This is expected behavior - relyingParty should never be nil in production
func TestWebAuthnHandler_LoginStart_NilRelyingParty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil relyingParty, but did not panic")
		}
	}()

	h := newTestWebAuthnHandler(nil, nil, nil, nil)
	w := httptest.NewRecorder()

	req, _ := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)

	h.LoginStart(w, req)
}

// TestWebAuthnHandler_LoginStart_CreateSessionError tests that LoginStart
// returns 500 when CreateSession fails after a successful BeginDiscoverableLogin.
func TestWebAuthnHandler_LoginStart_CreateSessionError(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	pool.Close() // close immediately so CreateSession fails

	repo := webauthn.NewRepository(pool)

	rp, err := webauthn.NewRelyingParty("localhost", "Test App", []string{"https://localhost:8081"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}

	h := newTestWebAuthnHandler(repo, rp, nil, nil)

	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)

	h.LoginStart(w, req)

	// BeginDiscoverableLogin succeeds, but CreateSession fails → 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to create session") {
		t.Errorf("expected 'failed to create session' error, got: %s", w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_UpdateSignCountError tests the path where
// ValidatePasskeyLogin succeeds but UpdateSignCount fails. Since we can't
// easily produce valid WebAuthn signatures, we document the expected behavior.
// The UpdateSignCount error path returns 500 "failed to update credential".
// This path is exercised in production when the DB becomes unavailable after
// a successful login verification.

// TestWebAuthnHandler_RegisterStart_MarshalSessionError tests the rare case
// where json.Marshal fails for the session data. Since json.Marshal only
// fails for unsupported types (channels, functions), and the webauthn
// library's SessionData is always marshalable, this path is effectively
// unreachable in practice. We document this for coverage awareness.

// TestWebAuthnHandler_LoginStart_MarshalSessionError tests the rare case
// where json.Marshal fails for the login session data. Same reasoning as
// RegisterStart_MarshalSessionError - effectively unreachable.

// TestWebAuthnHandler_LoginFinish_InvalidCredential tests that a login finish
// with a malformed credential body returns a non-200 error. The session is
// expired only to avoid accidental reuse — the handler does NOT check
// ExpiresAt; it fails at ParseCredentialRequestResponseBody.
func TestWebAuthnHandler_LoginFinish_InvalidCredential(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Create an expired login session (expiry is irrelevant — the malformed
	// credential below causes the failure before any time-based check)
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"type":"login"}`),
		Type:        "login",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(-5 * time.Minute), // Expired
	}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		repo.DeleteSession(ctx, sessionID)
	})

	body := `{"session_id": "` + sessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	// The handler does not check ExpiresAt — it proceeds to
	// TryValidatePasskeyLogin which fails parsing the empty credential.
	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 status for invalid credential, got %d", w.Code)
	}
}

// TestWebAuthnHandler_LoginFinish_EmptySessionID tests that empty session_id returns 400
func TestWebAuthnHandler_LoginFinish_EmptySessionID(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"session_id": "", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// --- LoginStart error path tests ---

// TestWebAuthnHandler_LoginStart_NilRepoWithRP tests that LoginStart panics
// when repo is nil but relyingParty is set (repo.CreateSession called on nil)
func TestWebAuthnHandler_LoginStart_NilRepoWithRP(t *testing.T) {
	rp := &webauthnx.WebAuthn{}
	h := newTestWebAuthnHandler(nil, rp, nil, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil repo and non-nil rp, but did not panic")
		}
	}()

	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	h.LoginStart(w, req)
}

// --- LoginFinish error path tests ---

// TestWebAuthnHandler_LoginFinish_InvalidSessionData tests that corrupt session
// data (not valid SessionData JSON) returns 500 after successful session lookup
func TestWebAuthnHandler_LoginFinish_InvalidSessionData(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Create a login session with invalid JSON session data
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`not-valid-json`),
		Type:        "login",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() {
		repo.DeleteSession(ctx, sessionID)
	})

	body := `{"session_id": "` + sessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginStart_Success tests the full success path through
// LoginStart: BeginDiscoverableLogin → json.Marshal → CreateSession → writeJSON.
func TestWebAuthnHandler_LoginStart_Success(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)

	h.LoginStart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	sessionID, ok := resp["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatal("expected non-empty session_id in response")
	}

	options, ok := resp["options"]
	if !ok || options == nil {
		t.Fatal("expected options in response")
	}

	// Verify session was persisted in DB
	parsedID, err := uuid.Parse(sessionID)
	if err != nil {
		t.Fatalf("failed to parse session_id: %v", err)
	}
	session, err := repo.GetSession(context.Background(), parsedID)
	if err != nil {
		t.Fatalf("failed to get session from DB: %v", err)
	}
	if session.Type != "login" {
		t.Errorf("expected session type 'login', got %q", session.Type)
	}

	// Cleanup
	repo.DeleteSession(context.Background(), parsedID)
}

// TestWebAuthnHandler_LoginStart_CancelledContext tests that LoginStart
// returns 500 when the request context is cancelled (CreateSession fails;
// BeginDiscoverableLogin does not use the context).
func TestWebAuthnHandler_LoginStart_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	// LoginStart: BeginDiscoverableLogin (no context) then CreateSession (uses context).
	// Cancel context so CreateSession fails after BeginDiscoverableLogin succeeds.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	req = req.WithContext(ctx)

	h.LoginStart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginStart_WriteJSONError tests that LoginStart
// handles writeJSON failures gracefully.
func TestWebAuthnHandler_LoginStart_WriteJSONError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	fw := &failingResponseWriter{}

	h.LoginStart(fw, req)

	// The key assertion: the handler did not panic.
}

// TestWebAuthnHandler_LoginFinish_EmptyBody tests that an empty request body
// returns 400 (json.NewDecoder fails on empty input).
func TestWebAuthnHandler_LoginFinish_EmptyBody(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// --- LoginFinish deep error path tests ---

// TestWebAuthnHandler_LoginFinish_WithSyntacticallyValidCredential tests the path
// through LoginFinish where ParseCredentialRequestResponseBody succeeds but
// ValidatePasskeyLogin fails (cryptographically invalid data).
// This covers the userLookup + ValidatePasskeyLogin error paths.
func TestWebAuthnHandler_LoginFinish_WithSyntacticallyValidCredential(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	// Step 1: Call LoginStart to create a valid session
	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	h.LoginStart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LoginStart failed: %d %s", w.Code, w.Body.String())
	}

	var startResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&startResp); err != nil {
		t.Fatalf("failed to decode LoginStart response: %v", err)
	}
	sessionID, _ := startResp["session_id"].(string)
	if sessionID == "" {
		t.Fatal("expected non-empty session_id from LoginStart")
	}
	defer repo.DeleteSession(ctx, uuid.MustParse(sessionID))

	// Step 2: Build a syntactically valid but cryptographically invalid credential
	// using the helper that constructs proper authenticatorData.
	fakeCred := buildFakeLoginCredential(t, "dGVzdA")

	// Step 3: Call LoginFinish with the valid session + fake credential
	finishBody := map[string]interface{}{
		"session_id": sessionID,
		"credential": fakeCred,
	}
	finishBytes, _ := json.Marshal(finishBody)

	req2 := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(string(finishBytes)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	h.LoginFinish(w2, req2)

	// ValidatePasskeyLogin should fail, returning 400
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w2.Code, w2.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_CancelledContext tests that LoginFinish returns
// an error when the request context is cancelled (GetSession fails).
func TestWebAuthnHandler_LoginFinish_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fakeSessionID := uuid.New()
	body := `{"session_id": "` + fakeSessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_ClosedPool tests that LoginFinish returns
// an error when the database pool is closed (GetSession fails, mapped to 400).
func TestWebAuthnHandler_LoginFinish_ClosedPool(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	workingRepo := webauthn.NewRepository(pool)
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"challenge":"test"}`),
		Type:        "login",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	if err := workingRepo.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	t.Cleanup(func() { workingRepo.DeleteSession(ctx, sessionID) })

	closedPool := newClosedPool(t)
	closedRepo := webauthn.NewRepository(closedPool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(closedRepo, nil, nil, adminMgr)

	body := `{"session_id": "` + sessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginStart_BeginDiscoverableLoginError tests that LoginStart
// returns 500 when BeginDiscoverableLogin fails. A WebAuthn RP with an empty RPID
// causes BeginDiscoverableLogin to return an error.
func TestWebAuthnHandler_LoginStart_BeginDiscoverableLoginError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)

	misconfiguredRP, rpErr := webauthn.NewRelyingParty("", "Test", []string{"http://localhost"})
	if rpErr != nil {
		t.Fatalf("failed to create misconfigured RP: %v", rpErr)
	}

	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, misconfiguredRP, nil, adminMgr)

	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	w := httptest.NewRecorder()

	h.LoginStart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to begin login") {
		t.Errorf("expected 'failed to begin login' error, got: %s", w.Body.String())
	}
}

// --- LoginFinish with stored credential for userLookup callback ---

// TestWebAuthnHandler_LoginFinish_WithStoredCredential tests the LoginFinish path
// where the userLookup callback finds a matching credential via GetCredentialByID,
// but ValidatePasskeyLogin still fails (signature is fake).
func TestWebAuthnHandler_LoginFinish_WithStoredCredential(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	// Step 1: Create a credential in the DB so that GetCredentialByID finds it
	credID := []byte("login-finish-test-cred")
	credRecord := &webauthn.CredentialRecord{
		ID:                credID,
		Name:              "Test Key",
		PublicKey:         make([]byte, 64), // fake ECDSA public key
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{"internal"},
		FlagsByte:         0x41,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}
	if err := repo.StoreCredential(ctx, credRecord); err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}
	t.Cleanup(func() { repo.DeleteCredential(ctx, credID) })

	// Step 2: Call LoginStart to create a valid session
	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	h.LoginStart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LoginStart failed: %d %s", w.Code, w.Body.String())
	}

	var startResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&startResp); err != nil {
		t.Fatalf("failed to decode LoginStart response: %v", err)
	}
	sessionID, _ := startResp["session_id"].(string)
	if sessionID == "" {
		t.Fatal("expected non-empty session_id from LoginStart")
	}
	defer repo.DeleteSession(ctx, uuid.MustParse(sessionID))

	// Step 3: Build a fake assertion credential with the stored credential's ID
	credIDB64 := base64.RawURLEncoding.EncodeToString(credID)
	fakeCred := buildFakeLoginCredential(t, credIDB64)

	// Step 4: Call LoginFinish - userLookup finds the credential via
	// GetCredentialByID, but ValidatePasskeyLogin fails (fake signature)
	finishBody := map[string]interface{}{
		"session_id": sessionID,
		"credential": fakeCred,
	}
	finishBytes, _ := json.Marshal(finishBody)

	req2 := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(string(finishBytes)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	h.LoginFinish(w2, req2)

	// ValidatePasskeyLogin should fail because the signature is fake,
	// returning 400 "passkey login verification failed"
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "passkey login verification failed") {
		t.Errorf("expected 'passkey login verification failed' error, got: %s", w2.Body.String())
	}
}
