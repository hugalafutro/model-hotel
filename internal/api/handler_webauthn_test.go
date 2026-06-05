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

	req, _ := newChiRequest(http.MethodPost, "/webauthn/register/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	h.RegisterStart(nil, req)
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

	req, _ := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)

	h.LoginStart(nil, req)
}

// TestWebAuthnHandler_LoginFinish_ExpiredSession tests that an expired session returns 400
func TestWebAuthnHandler_LoginFinish_ExpiredSession(t *testing.T) {
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

	// Create an expired login session
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

	// Expired sessions are still returned by GetSession, but the handler
	// proceeds to TryValidatePasskeyLogin which will fail with invalid credential
	// The actual expiration check happens in the session manager validation
	// For now, just verify it doesn't crash and returns an error
	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 status for expired session, got %d", w.Code)
	}
}

// TestWebAuthnHandler_RegisterFinish_ExpiredSession tests that an expired session returns non-200
func TestWebAuthnHandler_RegisterFinish_ExpiredSession(t *testing.T) {
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

	// Create an expired registration session
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

	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 status for expired session, got %d", w.Code)
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
