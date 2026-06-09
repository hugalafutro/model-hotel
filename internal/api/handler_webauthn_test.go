package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	webauthnx "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newTestWebAuthnHandler creates a WebAuthnHandler with the given dependencies
func newTestWebAuthnHandler(
	repo *webauthn.Repository,
	rp *webauthnx.WebAuthn,
	sessionMgr *webauthn.SessionManager,
	adminMgr AdminAuthenticator,
) *WebAuthnHandler {
	return &WebAuthnHandler{
		webauthnRepo: repo,
		relyingParty: rp,
		sessionMgr:   sessionMgr,
		adminMgr:     adminMgr,
		ipLimiter:    mockIPLimiter{},
	}
}

// mockIPLimiter implements IPLimiterMiddleware for tests
type mockIPLimiter struct{}

func (m mockIPLimiter) Middleware(next http.Handler) http.Handler {
	return next
}

// TestAvailable_WithNilRP tests that Available returns enabled=false when RP is nil
func TestWebAuthnHandler_Available_WithNilRP(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	req, w := newChiRequest(http.MethodGet, "/webauthn/available", nil)
	h.Available(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", resp["enabled"])
	}
}

// TestAvailable_WithNonNilRP tests that Available returns enabled=true when RP is set
func TestWebAuthnHandler_Available_WithNonNilRP(t *testing.T) {
	// We can't easily construct a real webauthnx.WebAuthn, so we use a non-nil placeholder
	// In practice, this is set when WebAuthn is configured with HTTPS + proper config
	rp := &webauthnx.WebAuthn{} // non-nil but not fully initialized
	h := newTestWebAuthnHandler(nil, rp, nil, nil)

	req, w := newChiRequest(http.MethodGet, "/webauthn/available", nil)
	h.Available(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", resp["enabled"])
	}
}

// TestDeleteCredential_InvalidBase64URL tests that invalid base64url ID returns 400
func TestWebAuthnHandler_DeleteCredential_InvalidBase64URL(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	req, w := newChiRequest(http.MethodDelete, "/webauthn/credentials/!invalid!", nil)
	// Set chi URL param manually
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "!invalid!")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.DeleteCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestDeleteCredential_ValidButNonExistent tests that a non-existent credential returns 500
func TestWebAuthnHandler_DeleteCredential_ValidButNonExistent(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	req, w := newChiRequest(http.MethodDelete, "/webauthn/credentials/"+base64.RawURLEncoding.EncodeToString([]byte("nonexistent")), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", base64.RawURLEncoding.EncodeToString([]byte("nonexistent")))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Authorization", "Bearer test-token")

	h.DeleteCredential(w, req)

	// The handler returns 500 when repo.DeleteCredential returns an error
	// Since the credential doesn't exist, repo returns ErrNotFound
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestRenameCredential_EmptyName tests that empty name returns 400
func TestWebAuthnHandler_RenameCredential_EmptyName(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := renameCredentialRequest{Name: ""}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/test", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "dGVzdA") // base64url for "test"
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestRenameCredential_WhitespaceOnlyName tests that whitespace-only name returns 400
func TestWebAuthnHandler_RenameCredential_WhitespaceOnlyName(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := renameCredentialRequest{Name: "   "}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/test", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "dGVzdA")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestRenameCredential_TooLongName tests that name > 128 chars returns 400
func TestWebAuthnHandler_RenameCredential_TooLongName(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := renameCredentialRequest{Name: strings.Repeat("a", 129)}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/test", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "dGVzdA")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestRenameCredential_InvalidBase64URL tests that invalid base64url ID returns 400
func TestWebAuthnHandler_RenameCredential_InvalidBase64URL(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := renameCredentialRequest{Name: "New Name"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/!invalid!", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "!invalid!")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestLogout_NoAuthHeader tests that missing auth header returns 401
func TestWebAuthnHandler_Logout_NoAuthHeader(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	w := httptest.NewRecorder()

	h.Logout(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

// TestAdminOrSessionAuth_NoAuth tests that missing auth returns 401
func TestAdminOrSessionAuth_NoAuth(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	sessionMgr := webauthn.NewSessionManager(nil)
	h := newTestWebAuthnHandler(nil, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := h.adminOrSessionAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminOrSessionAuth_InvalidToken tests that invalid token returns 401
func TestAdminOrSessionAuth_InvalidToken(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	sessionMgr := webauthn.NewSessionManager(repo)
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := h.adminOrSessionAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminOrSessionAuth_AdminToken tests that admin token passes auth
func TestAdminOrSessionAuth_AdminToken(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	sessionMgr := webauthn.NewSessionManager(nil)
	h := newTestWebAuthnHandler(nil, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := h.adminOrSessionAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestAdminOrSessionAuth_SessionToken tests that session token passes auth
func TestAdminOrSessionAuth_SessionToken(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := h.adminOrSessionAuth(handler)

	// Create a session token
	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil)
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_Logout_WithValidToken tests logout with valid session token
func TestWebAuthnHandler_Logout_WithValidToken(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	// Create a session token
	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil)
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}

	// Verify token is valid before logout
	if !sessionMgr.Validate(context.Background(), token) {
		t.Fatal("token should be valid before logout")
	}

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify token is revoked after logout
	if sessionMgr.Validate(context.Background(), token) {
		t.Error("token should be invalid after logout")
	}
}

// TestListCredentials_Success tests listing credentials with valid auth
func TestListCredentials_Success(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	req := httptest.NewRequest(http.MethodGet, "/webauthn/credentials", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	h.ListCredentials(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp []credentialResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should be empty array since no credentials exist
	if len(resp) != 0 {
		t.Errorf("expected 0 credentials, got %d", len(resp))
	}
}

// TestWebAuthnHandler_RegisterFinish_InvalidJSONBody tests that malformed JSON returns 400
func TestWebAuthnHandler_RegisterFinish_InvalidJSONBody(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"invalid"`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_RegisterFinish_InvalidSessionID tests that invalid UUID returns 400
func TestWebAuthnHandler_RegisterFinish_InvalidSessionID(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"session_id": "not-a-uuid", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

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

// TestWebAuthnHandler_DeleteCredential_EmptyID tests that empty ID returns 400
func TestWebAuthnHandler_DeleteCredential_EmptyID(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/webauthn/credentials/", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.DeleteCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_RenameCredential_ValidName tests that a valid rename succeeds
func TestWebAuthnHandler_RenameCredential_ValidName(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Store a test credential
	testCredID := []byte("test-credential-id")
	testCred := webauthn.CredentialRecord{
		Name:                      "Original Name",
		ID:                        testCredID,
		PublicKey:                 []byte("public-key"),
		AttestationType:           "none",
		AttestationFormat:         "none",
		Transport:                 []string{"internal"},
		FlagsByte:                 0,
		SignCount:                 0,
		AAGUID:                    uuid.Nil,
		AttestationObject:         []byte("attested"),
		AttestationClientData:     []byte{},
		AttestationClientDataHash: []byte{},
		AttestationPublicKeyAlgo:  -7,
		AuthenticatorData:         []byte{},
		CreatedAt:                 time.Now().UTC(),
		UpdatedAt:                 time.Now().UTC(),
	}
	if err := repo.StoreCredential(context.Background(), &testCred); err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}
	t.Cleanup(func() {
		if err := repo.DeleteCredential(context.Background(), testCredID); err != nil {
			t.Errorf("failed to cleanup credential: %v", err)
		}
	})

	body := renameCredentialRequest{Name: "My Key"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/"+base64.RawURLEncoding.EncodeToString(testCredID), strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", base64.RawURLEncoding.EncodeToString(testCredID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify the name was changed
	updatedCred, err := repo.GetCredentialByID(context.Background(), testCredID)
	if err != nil {
		t.Fatalf("failed to get updated credential: %v", err)
	}
	if updatedCred.Name != "My Key" {
		t.Errorf("expected name 'My Key', got '%s'", updatedCred.Name)
	}
}

// TestWebAuthnHandler_ListCredentials_WithStoredCredential tests listing with stored credential
func TestWebAuthnHandler_ListCredentials_WithStoredCredential(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Store a test credential
	testCredID := []byte("test-credential-id-list")
	testCred := webauthn.CredentialRecord{
		Name:                      "Test Key",
		ID:                        testCredID,
		PublicKey:                 []byte("public-key"),
		AttestationType:           "none",
		AttestationFormat:         "none",
		Transport:                 []string{"internal"},
		FlagsByte:                 0,
		SignCount:                 0,
		AAGUID:                    uuid.Nil,
		AttestationObject:         []byte("attested"),
		AttestationClientData:     []byte{},
		AttestationClientDataHash: []byte{},
		AttestationPublicKeyAlgo:  -7,
		AuthenticatorData:         []byte{},
		CreatedAt:                 time.Now().UTC(),
		UpdatedAt:                 time.Now().UTC(),
	}
	if err := repo.StoreCredential(context.Background(), &testCred); err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}
	t.Cleanup(func() {
		if err := repo.DeleteCredential(context.Background(), testCredID); err != nil {
			t.Errorf("failed to cleanup credential: %v", err)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/webauthn/credentials", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	h.ListCredentials(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp []credentialResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Find the credential we stored rather than asserting exact count,
	// since other tests running in the same DB could leave credentials behind.
	var found *credentialResponse
	for i := range resp {
		if resp[i].Name == "Test Key" {
			found = &resp[i]
			break
		}
	}
	if found == nil {
		t.Error("expected to find stored credential 'Test Key' in response")
	}
}

// TestWebAuthnHandler_RenameCredential_InvalidJSONBody tests that malformed JSON returns 400
func TestWebAuthnHandler_RenameCredential_InvalidJSONBody(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"invalid"`
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/dGVzdA", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "dGVzdA")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_RenameCredential_EmptyID tests that empty ID in URL param returns 400
func TestWebAuthnHandler_RenameCredential_EmptyID(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := renameCredentialRequest{Name: "New Name"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_RegisterFinish_WrongSessionType tests that using a login session for register returns 400
func TestWebAuthnHandler_RegisterFinish_WrongSessionType(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Create a login session (wrong type for register endpoint)
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"type":"login"}`),
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
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid session type") {
		t.Errorf("expected 'invalid session type' error, got: %s", w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_WrongSessionType tests that using a registration session for login returns 400
func TestWebAuthnHandler_LoginFinish_WrongSessionType(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
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

// TestWebAuthnHandler_DeleteCredential_Success tests deleting an existing credential
func TestWebAuthnHandler_DeleteCredential_Success(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Store a test credential
	testCredID := []byte("test-credential-to-delete")
	testCred := webauthn.CredentialRecord{
		Name:                      "To Delete",
		ID:                        testCredID,
		PublicKey:                 []byte("public-key"),
		AttestationType:           "none",
		AttestationFormat:         "none",
		Transport:                 []string{"internal"},
		FlagsByte:                 0,
		SignCount:                 0,
		AAGUID:                    uuid.Nil,
		AttestationObject:         []byte("attested"),
		AttestationClientData:     []byte{},
		AttestationClientDataHash: []byte{},
		AttestationPublicKeyAlgo:  -7,
		AuthenticatorData:         []byte{},
		CreatedAt:                 time.Now().UTC(),
		UpdatedAt:                 time.Now().UTC(),
	}
	if err := repo.StoreCredential(ctx, &testCred); err != nil {
		t.Fatalf("failed to store credential: %v", err)
	}

	// Verify it exists
	_, err = repo.GetCredentialByID(ctx, testCredID)
	if err != nil {
		t.Fatalf("credential should exist before delete: %v", err)
	}

	// Delete via handler
	req := httptest.NewRequest(http.MethodDelete, "/webauthn/credentials/"+base64.RawURLEncoding.EncodeToString(testCredID), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", base64.RawURLEncoding.EncodeToString(testCredID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.DeleteCredential(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNoContent, w.Code, w.Body.String())
	}

	// Verify it's deleted
	_, err = repo.GetCredentialByID(ctx, testCredID)
	if err == nil {
		t.Error("credential should not exist after delete")
	}
}

// TestWebAuthnHandler_RegisterFinish_SessionNotFound tests that non-existent session returns 400
func TestWebAuthnHandler_RegisterFinish_SessionNotFound(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Use a valid UUID that doesn't exist in DB
	fakeSessionID := uuid.New()
	body := `{"session_id": "` + fakeSessionID.String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginFinish_SessionNotFound tests that non-existent session returns 400
func TestWebAuthnHandler_LoginFinish_SessionNotFound(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
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

// TestWebAuthnHandler_RegisterStart_NilRepo tests that RegisterStart panics when repo is nil
// This is expected behavior - repo should never be nil in production
func TestWebAuthnHandler_RegisterStart_NilRepo(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil repo, but did not panic")
		}
	}()

	h := newTestWebAuthnHandler(nil, nil, nil, nil)
	w := httptest.NewRecorder()

	req, _ := newChiRequest(http.MethodPost, "/webauthn/register/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	h.RegisterStart(w, req)
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

// TestWebAuthnHandler_LoginFinish_InvalidCredential tests that a login finish
// with a malformed credential body returns a non-200 error. The session is
// expired only to avoid accidental reuse — the handler does NOT check
// ExpiresAt; it fails at ParseCredentialRequestResponseBody.
func TestWebAuthnHandler_LoginFinish_InvalidCredential(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
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

// TestWebAuthnHandler_RegisterFinish_InvalidCredential tests that a registration
// finish with a malformed credential body returns a non-200 error. The session
// is expired only to avoid accidental reuse — the handler does NOT check
// ExpiresAt; it fails at ParseCredentialCreationResponseBody.
func TestWebAuthnHandler_RegisterFinish_InvalidCredential(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Create an expired registration session (expiry is irrelevant — the
	// malformed credential below causes the failure before any time-based check)
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
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
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	// The handler does not check ExpiresAt — it proceeds to
	// TryValidatePasskeyRegistration which fails parsing the empty credential.
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

// TestWebAuthnHandler_RegisterFinish_EmptySessionID tests that empty session_id returns 400
func TestWebAuthnHandler_RegisterFinish_EmptySessionID(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{"session_id": "", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// --- NewWebAuthnHandler constructor tests ---

// TestWebAuthnHandler_NewWebAuthnHandler_NilParams tests the constructor with nil params
func TestWebAuthnHandler_NewWebAuthnHandler_NilParams(t *testing.T) {
	h := NewWebAuthnHandler(nil, nil, nil, nil, nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.webauthnRepo != nil {
		t.Error("expected nil webauthnRepo")
	}
	if h.relyingParty != nil {
		t.Error("expected nil relyingParty")
	}
	if h.sessionMgr != nil {
		t.Error("expected nil sessionMgr")
	}
	if h.adminMgr != nil {
		t.Error("expected nil adminMgr")
	}
	if h.ipLimiter != nil {
		t.Error("expected nil ipLimiter")
	}
}

// TestWebAuthnHandler_NewWebAuthnHandler_NonNilParams tests the constructor with provided params
func TestWebAuthnHandler_NewWebAuthnHandler_NonNilParams(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	limiter := mockIPLimiter{}
	h := NewWebAuthnHandler(nil, nil, nil, adminMgr, limiter)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.adminMgr == nil {
		t.Error("expected non-nil adminMgr")
	}
	if h.ipLimiter == nil {
		t.Error("expected non-nil ipLimiter")
	}
}

// --- Register route mounting tests ---

// TestWebAuthnHandler_Register_MountsRoutes tests that Register mounts the expected routes
func TestWebAuthnHandler_Register_MountsRoutes(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(nil, nil, nil, adminMgr)

	r := chi.NewRouter()
	h.Register(r)

	// Walk the routes and collect them
	routePaths := map[string]bool{}
	walkFn := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routePaths[method+" "+route] = true
		return nil
	}
	if err := chi.Walk(r, walkFn); err != nil {
		t.Fatalf("failed to walk routes: %v", err)
	}

	expectedRoutes := []string{
		"GET /webauthn/available",
		"POST /webauthn/register/start",
		"POST /webauthn/register/finish",
		"GET /webauthn/credentials",
		"DELETE /webauthn/credentials/{id}",
		"PATCH /webauthn/credentials/{id}",
		"POST /webauthn/logout",
		"POST /webauthn/login/start",
		"POST /webauthn/login/finish",
	}

	for _, expected := range expectedRoutes {
		if !routePaths[expected] {
			t.Errorf("expected route %q to be mounted", expected)
		}
	}
}

// TestWebAuthnHandler_Register_ProtectedRoutesRequireAuth tests that protected routes reject unauthenticated requests
func TestWebAuthnHandler_Register_ProtectedRoutesRequireAuth(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	sessionMgr := webauthn.NewSessionManager(nil)
	h := newTestWebAuthnHandler(nil, nil, sessionMgr, adminMgr)

	r := chi.NewRouter()
	h.Register(r)

	protectedRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/webauthn/register/start"},
		{http.MethodPost, "/webauthn/register/finish"},
		{http.MethodGet, "/webauthn/credentials"},
		{http.MethodDelete, "/webauthn/credentials/test"},
		{http.MethodPatch, "/webauthn/credentials/test"},
		{http.MethodPost, "/webauthn/logout"},
	}

	for _, tc := range protectedRoutes {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected status %d for %s %s, got %d; body: %s",
					http.StatusUnauthorized, tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}
}

// TestWebAuthnHandler_Register_PublicRoutesAccessible tests that public routes don't require auth
func TestWebAuthnHandler_Register_PublicRoutesAccessible(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	h := newTestWebAuthnHandler(nil, nil, nil, adminMgr)

	r := chi.NewRouter()
	h.Register(r)

	// /webauthn/available is public and should return 200
	req := httptest.NewRequest(http.MethodGet, "/webauthn/available", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d for GET /webauthn/available, got %d; body: %s",
			http.StatusOK, w.Code, w.Body.String())
	}
}

// --- RegisterStart error path tests ---

// TestWebAuthnHandler_RegisterStart_NilRelyingPartyWithDB tests that RegisterStart
// panics when relyingParty is nil even with a valid repo (rp.BeginRegistration is called on nil)
func TestWebAuthnHandler_RegisterStart_NilRelyingPartyWithDB(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	h := newTestWebAuthnHandler(repo, nil, nil, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil relyingParty, but did not panic")
		}
	}()

	req, w := newChiRequest(http.MethodPost, "/webauthn/register/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	h.RegisterStart(w, req)
}

// --- RegisterFinish error path tests ---

// TestWebAuthnHandler_RegisterFinish_InvalidSessionData tests that corrupt session
// data (not valid SessionData JSON) returns 500 after successful session lookup
func TestWebAuthnHandler_RegisterFinish_InvalidSessionData(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Create a registration session with invalid JSON session data
	sessionID := uuid.New()
	session := &webauthn.SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`not-valid-json`),
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
	req := httptest.NewRequest(http.MethodPost, "/webauthn/register/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.RegisterFinish(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
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
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
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

// --- ListCredentials error path tests ---

// TestWebAuthnHandler_ListCredentials_NilRepo tests that ListCredentials with nil repo panics
func TestWebAuthnHandler_ListCredentials_NilRepo(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil repo, but did not panic")
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/webauthn/credentials", http.NoBody)
	w := httptest.NewRecorder()

	h.ListCredentials(w, req)
}

// --- SetWebAuthnSessionManager tests ---

// TestWebAuthnHandler_SetWebAuthnSessionManager_Nil tests that SetWebAuthnSessionManager
// sets the field even with a nil Handler (via the Handler type, not WebAuthnHandler)
func TestSetWebAuthnSessionManager_SetsField(t *testing.T) {
	h := newTestHandler(t)

	// Initially nil
	if h.webauthnSessionMgr != nil {
		t.Error("expected nil webauthnSessionMgr before SetWebAuthnSessionManager")
	}

	// Create a mock WebAuthnSessionManager
	mockMgr := &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, _ string) bool { return true },
		revokeFn:   func(_ context.Context, _ string) bool { return true },
	}
	h.SetWebAuthnSessionManager(mockMgr)

	if h.webauthnSessionMgr == nil {
		t.Error("expected non-nil webauthnSessionMgr after SetWebAuthnSessionManager")
	}

	// Verify it actually works through the interface
	if !h.webauthnSessionMgr.Validate(context.Background(), "any-token") {
		t.Error("expected Validate to return true via mock")
	}
}

// TestSetWebAuthnSessionManager_NilArg tests that SetWebAuthnSessionManager
// can be called with a nil argument (clears the field)
func TestSetWebAuthnSessionManager_NilArg(t *testing.T) {
	h := newTestHandler(t)

	mockMgr := &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, _ string) bool { return true },
		revokeFn:   func(_ context.Context, _ string) bool { return true },
	}
	h.SetWebAuthnSessionManager(mockMgr)
	if h.webauthnSessionMgr == nil {
		t.Fatal("expected non-nil after set")
	}

	// Clear it
	h.SetWebAuthnSessionManager(nil)
	if h.webauthnSessionMgr != nil {
		t.Error("expected nil webauthnSessionMgr after SetWebAuthnSessionManager(nil)")
	}
}

// TestWebAuthnHandler_RegisterStart_RepoListError tests that RegisterStart
// returns 500 when the repo fails to list credentials.
func TestWebAuthnHandler_RegisterStart_RepoListError(t *testing.T) {
	closedPool := newClosedPool(t)
	repo := webauthn.NewRepository(closedPool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	req, w := newChiRequest(http.MethodPost, "/webauthn/register/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	h.RegisterStart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_ListCredentials_RepoError tests that ListCredentials
// returns 500 when the repo fails to list credentials.
func TestWebAuthnHandler_ListCredentials_RepoError(t *testing.T) {
	closedPool := newClosedPool(t)
	repo := webauthn.NewRepository(closedPool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	req := httptest.NewRequest(http.MethodGet, "/webauthn/credentials", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	h.ListCredentials(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_RenameCredential_MissingNameField tests that a request
// with valid JSON but no "name" field (zero-value string) returns 400.
func TestWebAuthnHandler_RenameCredential_MissingNameField(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	body := `{}`
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/dGVzdA", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "dGVzdA")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "name must be 1-128 characters") {
		t.Errorf("expected error about name length, got: %s", w.Body.String())
	}
}

// TestWebAuthnHandler_RenameCredential_NonExistentCredential tests that
// renaming a valid base64url ID that does not exist in the repo returns 500.
func TestWebAuthnHandler_RenameCredential_NonExistentCredential(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	// Use a valid base64url-encoded ID that was never stored
	credID := []byte("nonexistent-rename-id")
	encodedID := base64.RawURLEncoding.EncodeToString(credID)

	body := renameCredentialRequest{Name: "New Name"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/"+encodedID, strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", encodedID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// mockWebAuthnSessionMgr implements WebAuthnSessionManager for testing
type mockWebAuthnSessionMgr struct {
	validateFn func(ctx context.Context, token string) bool
	revokeFn   func(ctx context.Context, token string) bool
}

func (m *mockWebAuthnSessionMgr) Validate(ctx context.Context, token string) bool {
	if m.validateFn != nil {
		return m.validateFn(ctx, token)
	}
	return false
}

func (m *mockWebAuthnSessionMgr) RevokeAuthToken(ctx context.Context, token string) bool {
	if m.revokeFn != nil {
		return m.revokeFn(ctx, token)
	}
	return false
}

// --- RegisterStart / LoginStart success path tests (require DB) ---

// TestWebAuthnHandler_RegisterStart_Success tests the full success path through
// RegisterStart: ListCredentials → BeginRegistration → json.Marshal → CreateSession → writeJSON.
func TestWebAuthnHandler_RegisterStart_Success(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	req, w := newChiRequest(http.MethodPost, "/webauthn/register/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	h.RegisterStart(w, req)

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
	if session.Type != "registration" {
		t.Errorf("expected session type 'registration', got %q", session.Type)
	}

	// Cleanup
	repo.DeleteSession(context.Background(), parsedID)
}

// TestWebAuthnHandler_LoginStart_Success tests the full success path through
// LoginStart: BeginDiscoverableLogin → json.Marshal → CreateSession → writeJSON.
func TestWebAuthnHandler_LoginStart_Success(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
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

// TestWebAuthnHandler_RegisterStart_CancelledContext tests that RegisterStart
// returns 500 when the request context is cancelled (ListCredentials fails).
func TestWebAuthnHandler_RegisterStart_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("failed to create relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, nil, adminMgr)

	// Cancel context to trigger DB errors
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, w := newChiRequest(http.MethodPost, "/webauthn/register/start", http.NoBody)
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-token")

	h.RegisterStart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_LoginStart_CancelledContext tests that LoginStart
// returns 500 when the request context is cancelled (CreateSession fails;
// BeginDiscoverableLogin does not use the context).
func TestWebAuthnHandler_LoginStart_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
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
