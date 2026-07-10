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

	"github.com/go-chi/chi/v5"
	webauthnx "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

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

// TestAvailable_WithNonNilRP tests that Available reports enabled=true (RP set)
// but has_credentials=false when no passkey is registered, so the login screen
// does not advertise a passkey button that cannot work.
func TestWebAuthnHandler_Available_WithNonNilRP(t *testing.T) {
	// We can't easily construct a real webauthnx.WebAuthn, so we use a non-nil placeholder
	// In practice, this is set when WebAuthn is configured with HTTPS + proper config
	rp := &webauthnx.WebAuthn{} // non-nil but not fully initialized
	h := newTestWebAuthnHandler(nil, rp, nil, nil)
	h.webauthnRepo = availStubStore{} // no credentials registered

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
	if resp["has_credentials"] != false {
		t.Errorf("expected has_credentials=false with no passkeys, got %v", resp["has_credentials"])
	}
}

// TestAvailable_WithCredentials tests that Available reports has_credentials=true
// once at least one passkey is registered, which is what unlocks the login
// screen's passkey button.
func TestWebAuthnHandler_Available_WithCredentials(t *testing.T) {
	rp := &webauthnx.WebAuthn{}
	h := newTestWebAuthnHandler(nil, rp, nil, nil)
	h.webauthnRepo = availStubStore{creds: []*webauthn.CredentialRecord{{Name: "yubikey"}}}

	req, w := newChiRequest(http.MethodGet, "/webauthn/available", nil)
	h.Available(w, req)

	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["enabled"] != true || resp["has_credentials"] != true {
		t.Errorf("expected enabled=true has_credentials=true, got %v", resp)
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
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
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

// TestWebAuthnHandler_DeleteCredential_Success tests deleting an existing credential
func TestWebAuthnHandler_DeleteCredential_Success(t *testing.T) {
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

// --- DeleteCredential with closed pool ---

// TestWebAuthnHandler_DeleteCredential_RepoError tests that DeleteCredential
// returns 500 when the repository's database pool is closed (repo returns
// an error). This covers the repo error path differently from the
// ValidButNonExistent test: the error is a connection error, not ErrNotFound.
func TestWebAuthnHandler_DeleteCredential_RepoError(t *testing.T) {
	closedPool := newClosedPool(t)
	repo := webauthn.NewRepository(closedPool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	credID := base64.RawURLEncoding.EncodeToString([]byte("any-id"))
	req := httptest.NewRequest(http.MethodDelete, "/webauthn/credentials/"+credID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", credID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.DeleteCredential(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// --- RenameCredential with closed pool ---

// TestWebAuthnHandler_RenameCredential_RepoError tests that RenameCredential
// returns 500 when the repository database pool is closed (repo.RenameCredential
// returns a connection error rather than ErrNotFound).
func TestWebAuthnHandler_RenameCredential_RepoError(t *testing.T) {
	closedPool := newClosedPool(t)
	repo := webauthn.NewRepository(closedPool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	credID := base64.RawURLEncoding.EncodeToString([]byte("any-id"))
	body := renameCredentialRequest{Name: "New Name"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/webauthn/credentials/"+credID, strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", credID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.RenameCredential(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}
