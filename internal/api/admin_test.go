package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/provider"
	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// --- Mock types ---

type mockProviderStore struct {
	createFn    func(ctx context.Context, req provider.CreateProviderRequest, ek, kn, ks []byte) (*provider.Provider, error)
	listFn      func(ctx context.Context) ([]*provider.Provider, error)
	getFn       func(ctx context.Context, id uuid.UUID) (*provider.Provider, error)
	getByNameFn func(ctx context.Context, name string) (*provider.Provider, error)
	updateFn    func(ctx context.Context, id uuid.UUID, req provider.UpdateProviderRequest, ek, kn, ks []byte) (*provider.Provider, error)
	deleteFn    func(ctx context.Context, id uuid.UUID) error
}

func (m *mockProviderStore) Create(ctx context.Context, req provider.CreateProviderRequest, ek, kn, ks []byte) (*provider.Provider, error) {
	if m.createFn != nil {
		return m.createFn(ctx, req, ek, kn, ks)
	}
	return nil, errors.New("mock: Create not implemented")
}
func (m *mockProviderStore) List(ctx context.Context) ([]*provider.Provider, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, errors.New("mock: List not implemented")
}
func (m *mockProviderStore) Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, errors.New("mock: Get not implemented")
}
func (m *mockProviderStore) GetByName(ctx context.Context, name string) (*provider.Provider, error) {
	if m.getByNameFn != nil {
		return m.getByNameFn(ctx, name)
	}
	return nil, errors.New("mock: GetByName not implemented")
}
func (m *mockProviderStore) Update(ctx context.Context, id uuid.UUID, req provider.UpdateProviderRequest, ek, kn, ks []byte) (*provider.Provider, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, req, ek, kn, ks)
	}
	return nil, errors.New("mock: Update not implemented")
}
func (m *mockProviderStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return errors.New("mock: Delete not implemented")
}

type mockVirtualKeyStore struct {
	createFn func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error)
	listFn   func(ctx context.Context) ([]*virtualkey.VirtualKey, error)
	getFn    func(ctx context.Context, id uuid.UUID) (*virtualkey.VirtualKey, error)
	deleteFn func(ctx context.Context, id uuid.UUID) error
	updateFn func(ctx context.Context, id uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error)
}

func (m *mockVirtualKeyStore) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
	if m.createFn != nil {
		return m.createFn(ctx, name, keyHash, keyPreview, rps, burst, tpm, allowedProviders, stripReasoning, owner)
	}
	return nil, errors.New("mock: Create not implemented")
}
func (m *mockVirtualKeyStore) List(ctx context.Context) ([]*virtualkey.VirtualKey, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, errors.New("mock: List not implemented")
}
func (m *mockVirtualKeyStore) ListByOwner(ctx context.Context, _ uuid.UUID) ([]*virtualkey.VirtualKey, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, errors.New("mock: ListByOwner not implemented")
}

func (m *mockVirtualKeyStore) Get(ctx context.Context, id uuid.UUID) (*virtualkey.VirtualKey, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, errors.New("mock: Get not implemented")
}
func (m *mockVirtualKeyStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return errors.New("mock: Delete not implemented")
}
func (m *mockVirtualKeyStore) Update(ctx context.Context, id uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, name, rps, burst, tpm, allowedProviders, stripReasoning, owner)
	}
	return nil, errors.New("mock: Update not implemented")
}

type mockSettingsStore struct {
	getWithDefaultFn  func(ctx context.Context, key string, defaultValue string) string
	getCheckedFn      func(ctx context.Context, key string) (string, bool, error)
	setFn             func(ctx context.Context, key string, value string) error
	getAllFn          func(ctx context.Context) (map[string]string, error)
	setTxFn           func(ctx context.Context, tx pgx.Tx, key, value string) error
	deleteKeysTxFn    func(ctx context.Context, tx pgx.Tx, keys []string) error
	deleteKeyFn       func(ctx context.Context, key string) error
	invalidateCacheFn func(key string)
	getBoolFn         func(ctx context.Context, key string, defaultValue bool) bool
	getDurationFn     func(ctx context.Context, key string, defaultValue time.Duration) time.Duration
	getIntFn          func(ctx context.Context, key string, defaultValue int) int
}

func (m *mockSettingsStore) GetWithDefault(ctx context.Context, key, defaultValue string) string {
	if m.getWithDefaultFn != nil {
		return m.getWithDefaultFn(ctx, key, defaultValue)
	}
	return defaultValue
}
func (m *mockSettingsStore) GetChecked(ctx context.Context, key string) (string, bool, error) {
	if m.getCheckedFn != nil {
		return m.getCheckedFn(ctx, key)
	}
	return "", false, nil
}
func (m *mockSettingsStore) Set(ctx context.Context, key, value string) error {
	if m.setFn != nil {
		return m.setFn(ctx, key, value)
	}
	return errors.New("mock: Set not implemented")
}
func (m *mockSettingsStore) SetMany(ctx context.Context, kvs [][2]string) error {
	if m.setFn != nil {
		for _, kv := range kvs {
			if err := m.setFn(ctx, kv[0], kv[1]); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("mock: SetMany not implemented")
}
func (m *mockSettingsStore) GetAll(ctx context.Context) (map[string]string, error) {
	if m.getAllFn != nil {
		return m.getAllFn(ctx)
	}
	return nil, errors.New("mock: GetAll not implemented")
}
func (m *mockSettingsStore) SetTx(ctx context.Context, tx pgx.Tx, key, value string) error {
	if m.setTxFn != nil {
		return m.setTxFn(ctx, tx, key, value)
	}
	return errors.New("mock: SetTx not implemented")
}
func (m *mockSettingsStore) DeleteKeysTx(ctx context.Context, tx pgx.Tx, keys []string) error {
	if m.deleteKeysTxFn != nil {
		return m.deleteKeysTxFn(ctx, tx, keys)
	}
	return errors.New("mock: DeleteKeysTx not implemented")
}
func (m *mockSettingsStore) DeleteKey(ctx context.Context, key string) error {
	if m.deleteKeyFn != nil {
		return m.deleteKeyFn(ctx, key)
	}
	return errors.New("mock: DeleteKey not implemented")
}
func (m *mockSettingsStore) InvalidateCache(key string) {
	if m.invalidateCacheFn != nil {
		m.invalidateCacheFn(key)
	}
}

func (m *mockSettingsStore) NotifyDeleted(key string) {
}

func (m *mockSettingsStore) GetBool(ctx context.Context, key string, defaultValue bool) bool {
	if m.getBoolFn != nil {
		return m.getBoolFn(ctx, key, defaultValue)
	}
	return defaultValue
}

func (m *mockSettingsStore) GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration {
	if m.getDurationFn != nil {
		return m.getDurationFn(ctx, key, defaultValue)
	}
	return defaultValue
}

func (m *mockSettingsStore) GetInt(ctx context.Context, key string, defaultValue int) int {
	if m.getIntFn != nil {
		return m.getIntFn(ctx, key, defaultValue)
	}
	return defaultValue
}

type mockAdminAuth struct {
	validateFn func(token string) bool
}

func (m *mockAdminAuth) Validate(token string) bool {
	if m.validateFn != nil {
		return m.validateFn(token)
	}
	return false
}

// testHandler creates a Handler with mock dependencies.
func testHandler(provStore *mockProviderStore, vkStore *mockVirtualKeyStore, setsStore *mockSettingsStore, auth *mockAdminAuth, dbPool *db.DB) *Handler {
	return &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   true,
			AllowedProviderHosts: []string{"api.example.com", "localhost"},
		},
		providerRepo:   provStore,
		dbPool:         dbPool,
		adminMgr:       auth,
		virtualKeyRepo: vkStore,
		settingsRepo:   setsStore,
		appVersion:     "test",
		ghReleasesURL:  githubReleasesURL,
		ghTagsURL:      githubTagsURL,
	}
}

func newChiRequest(method, path string, body io.Reader) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	// The admin auth middleware injects an identity on every authenticated
	// request (AdminIdentity for the admin token). These handler tests call the
	// handlers directly, so carry the same admin identity to mirror production;
	// authorization predicates like canTouchKey fail closed on a nil identity.
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
	return req, httptest.NewRecorder()
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
}

// --- Provider endpoint tests ---

func TestGetSettings_Success(t *testing.T) {
	mockSets := &mockSettingsStore{
		getAllFn: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"theme": "dark", "rate_limit_enabled": "true"}, nil
		},
	}
	h := testHandler(nil, nil, mockSets, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodGet, "/settings", nil)

	h.GetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var settings map[string]string
	parseJSON(t, w, &settings)
	if settings["theme"] != "dark" {
		t.Errorf("expected theme 'dark', got %q", settings["theme"])
	}
}

func TestGetSettings_RepoError(t *testing.T) {
	mockSets := &mockSettingsStore{
		getAllFn: func(ctx context.Context) (map[string]string, error) {
			return nil, errors.New("db failure")
		},
	}
	h := testHandler(nil, nil, mockSets, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodGet, "/settings", nil)

	h.GetSettings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// --- Virtual key endpoint tests ---

func TestCreateVirtualKey_Success(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			if name == "" {
				t.Error("expected non-empty name")
			}
			return &virtualkey.VirtualKey{
				ID:         uuid.New(),
				Name:       name,
				KeyHash:    keyHash,
				KeyPreview: keyPreview,
				CreatedAt:  time.Now(),
			}, nil
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"my-key"}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var resp virtualkey.VirtualKeyResponse
	parseJSON(t, w, &resp)
	if resp.Name != "my-key" {
		t.Errorf("expected name 'my-key', got %q", resp.Name)
	}
	if resp.Key == "" {
		t.Error("expected key to be returned on creation")
	}
	// The preview is the "sk-" prefix plus the key's last four characters,
	// the same tail width the provider card shows; checked against the
	// returned key so a wrong slice offset cannot pass on shape alone.
	if want := "sk-..." + resp.Key[len(resp.Key)-4:]; resp.KeyPreview != want {
		t.Errorf("key_preview = %q, want %q", resp.KeyPreview, want)
	}
}

func TestCreateVirtualKey_MissingName(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":""}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestListVirtualKeys_Success(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		listFn: func(ctx context.Context) ([]*virtualkey.VirtualKey, error) {
			now := time.Now()
			return []*virtualkey.VirtualKey{
				{ID: uuid.New(), Name: "key-1", KeyPreview: "sk-...ab", CreatedAt: now},
				{ID: uuid.New(), Name: "key-2", KeyPreview: "sk-...cd", CreatedAt: now},
			}, nil
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodGet, "/virtual-keys", nil)

	h.ListVirtualKeys(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp []virtualkey.VirtualKeyResponse
	parseJSON(t, w, &resp)
	if len(resp) != 2 {
		t.Errorf("expected 2 keys, got %d", len(resp))
	}
	// Key field must be empty for list (not includeKey)
	if resp[0].Key != "" {
		t.Errorf("expected empty key for list, got %q", resp[0].Key)
	}
}

func TestGetVirtualKey_Success(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		getFn: func(ctx context.Context, vid uuid.UUID) (*virtualkey.VirtualKey, error) {
			return &virtualkey.VirtualKey{
				ID:        vid,
				Name:      "test-key",
				CreatedAt: time.Now(),
			}, nil
		},
	}
	h := testHandler(nil, mockVK, nil, nil, nil)
	req, w := newChiRequest(http.MethodGet, "/virtual-keys/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.GetVirtualKey(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestDeleteVirtualKey_Success(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		deleteFn: func(ctx context.Context, vid uuid.UUID) error { return nil },
	}
	h := testHandler(nil, mockVK, nil, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/virtual-keys/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteVirtualKey(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

func TestDeleteVirtualKey_NotFound(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		deleteFn: func(ctx context.Context, vid uuid.UUID) error { return virtualkey.ErrNotFound },
	}
	h := testHandler(nil, mockVK, nil, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/virtual-keys/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteVirtualKey(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// --- Auth middleware tests ---

func TestAuthMiddleware_NoToken(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_BearerToken(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_MalformedHeader_NoBearerPrefix(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Basic abc123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d for non-Bearer prefix, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_EmptyAuthorizationHeader(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d for empty Authorization header, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_BearerWithEmptyToken(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d for Bearer with empty token, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_TokenWithLeadingWhitespace(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// "Bearer  valid-token" (double space) — ParseBearerToken returns " valid-token"
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer  valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Token has leading space, so admin validation should fail
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d for token with leading space, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_WebAuthnSessionFallback(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "session-token" },
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer session-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d when webAuthn fallback succeeds, got %d", http.StatusOK, rec.Code)
	}
}

func TestAuthMiddleware_WebAuthnSessionFallbackFails(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, _ string) bool { return false },
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d when both admin and webAuthn fail, got %d", http.StatusUnauthorized, rec.Code)
	}
}

// --- Cookie/CSRF AuthMiddleware tests ---

// TestAuthMiddleware_SessionCookie_Get_Works verifies a browser session
// carried on the HttpOnly cookie authenticates a safe (GET) request without
// any Authorization header.
func TestAuthMiddleware_SessionCookie_Get_Works(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "valid-session" },
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/system", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "valid-session"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET with session cookie = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAuthMiddleware_SessionCookie_Post_RequiresCSRF verifies that a cookie-authenticated
// unsafe method (POST) is rejected with 403 when no matching CSRF header is present.
func TestAuthMiddleware_SessionCookie_Post_RequiresCSRF(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "valid-session" },
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/providers", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "valid-session"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST cookie without CSRF = %d, want 403", rec.Code)
	}
}

// TestAuthMiddleware_SessionCookie_Post_WithCSRF_Works verifies that a cookie-authenticated
// POST succeeds when the CSRF header matches the CSRF cookie (double-submit).
func TestAuthMiddleware_SessionCookie_Post_WithCSRF_Works(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "valid-session" },
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/providers", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "valid-session"})
	req.AddCookie(&http.Cookie{Name: authcookie.CSRFCookie, Value: "csrf-xyz"})
	req.Header.Set(authcookie.CSRFHeader, "csrf-xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST cookie with CSRF = %d, want 200", rec.Code)
	}
}

// TestAuthMiddleware_AdminTokenHeader_StillWorks_NoCSRF verifies that the
// existing admin-token bearer path (TOTP off) is untouched by the cookie
// branch: a header POST needs no CSRF token.
func TestAuthMiddleware_AdminTokenHeader_StillWorks_NoCSRF(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin-token header POST = %d, want 200 (CSRF-exempt)", rec.Code)
	}
}

// --- TOTP/AuthMiddleware enforcement tests ---

// TestAuthMiddleware_TotpEnabled_RejectsRawToken verifies that with TOTP
// enabled (via a stub TotpStatus), a bare admin token is rejected so the
// second factor cannot be bypassed.
func TestAuthMiddleware_TotpEnabled_RejectsRawToken(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.SetTotpStatus(&stubTotpStatus{enabled: true})
	// Wait for the async seed goroutine in SetTotpStatus; in a pinch just set
	// the cache synchronously.
	h.totpEnabled.Store(true)

	handler := h.AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		w := httptest.NewRecorder()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with TOTP on, got %d", rec.Code)
	}
}

// TestAuthMiddleware_TotpEnabled_SessionTokenWorks verifies that a session
// token passes AuthMiddleware when TOTP is enabled.
func TestAuthMiddleware_TotpEnabled_SessionTokenWorks(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.SetTotpStatus(&stubTotpStatus{enabled: true})
	h.totpEnabled.Store(true)

	// Build a real session manager to mint a token the AuthMiddleware will accept.
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("test database not available: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	h.SetWebAuthnSessionManager(sessionMgr)

	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	t.Cleanup(func() {
		sessionMgr.RevokeAuthToken(context.Background(), token)
	})

	protected := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for session token with TOTP on, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthMiddleware_TotpDisabled_RawTokenWorks verifies that TOTP off (stub
// returns false) allows the raw admin token.
func TestAuthMiddleware_TotpDisabled_RawTokenWorks(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.SetTotpStatus(&stubTotpStatus{enabled: false})
	h.totpEnabled.Store(false)

	protected := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with TOTP off, got %d", rec.Code)
	}
}

// TestAuthMiddleware_TotpDbError_FailsClosed verifies that when RefreshTotpEnabled
// hits a DB error, it fails closed (cache becomes true) so a DB blip does not
// silently disable 2FA.
func TestAuthMiddleware_TotpDbError_FailsClosed(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.SetTotpStatus(&stubTotpStatus{enabled: false, err: errors.New("db down")})
	// Force a refresh; the DB error path should set the cache to true.
	h.RefreshTotpEnabled(context.Background())

	if !h.TotpEnabled() {
		t.Fatal("expected fail-closed: TOTP cache should be true after DB error")
	}

	protected := h.AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (fail closed) on DB error, got %d", rec.Code)
	}
}

// TestAuthMiddleware_TotpNilStatus_RawTokenWorks verifies that a Handler
// with no TOTP wired (nil source) behaves like today: raw admin token passes.
func TestAuthMiddleware_TotpNilStatus_RawTokenWorks(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	// No SetTotpStatus call; h.totpStatus is nil.

	protected := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (nil TOTP -> treated as off), got %d", rec.Code)
	}
}

// TestAuthMiddleware_DisableRevertsToRawToken drives through a real
// enroll+enable (cache true, raw token 401) then disable (cache false, raw
// token 200 again), verifying the full lifecycle.
func TestAuthMiddleware_DisableRevertsToRawToken(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	truncateTOTPTables(t)
	t.Cleanup(func() { truncateTOTPTables(t) })

	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-token" }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	pool := apiTestDB.Pool()
	totpRepo := totpsvc.NewRepository(pool, testMasterKey)
	h.SetTotpStatus(totpRepo)
	h.totpEnabled.Store(false)

	// Enroll only (we don't need a valid code for the repo-level Disable).
	_, _, err := totpRepo.Enroll(context.Background())
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if err := totpRepo.Enable(context.Background()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	h.RefreshTotpEnabled(context.Background())
	if !h.TotpEnabled() {
		t.Fatal("expected TOTP enabled after Enable")
	}

	// Raw admin token should now be 401.
	protected := h.AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with TOTP on, got %d", rec.Code)
	}

	// Disable. The repo Disable does not itself check a code (the handler does);
	// here we only exercise the cache-refresh lifecycle.
	_ = totpRepo.Disable(context.Background())
	h.RefreshTotpEnabled(context.Background())
	if h.TotpEnabled() {
		t.Fatal("expected TOTP disabled after Disable")
	}
	// Raw admin token should now be 200.
	protected2 := h.AuthMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req2 := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req2.Header.Set("Authorization", "Bearer valid-token")
	rec2 := httptest.NewRecorder()
	protected2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 after Disable (raw token enabled again), got %d", rec2.Code)
	}
}

// setChiURLParam sets a chi URL parameter on the request context.
func setChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- Coverage tests for uncovered lines ---

// mintedCSRF has the exact shape authcookie mints (43 base64url chars), so a
// refresh keeps it rather than replacing a foreign-looking value.
const mintedCSRF = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// TestAuthMiddleware_SessionCookie_ReissuedWhenSlid: when validation slid the
// session's expiry, the middleware hands the browser the new lifetime by
// re-issuing the cookie pair (same token, MaxAge to the new expiry). The
// browser enforces MaxAge on its own, so a server-only extension would still
// log the operator out on the original schedule.
func TestAuthMiddleware_SessionCookie_ReissuedWhenSlid(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	newExpiry := time.Now().Add(webauthn.AuthTokenTTL)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		authFn: func(_ context.Context, token string) (webauthn.AuthResult, bool) {
			if token != "valid-session" {
				return webauthn.AuthResult{}, false
			}
			return webauthn.AuthResult{UserID: []byte("admin"), ExpiresAt: newExpiry, Extended: true}, true
		},
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/system", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "valid-session"})
	req.AddCookie(&http.Cookie{Name: authcookie.CSRFCookie, Value: mintedCSRF})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	var sess, csrf *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case authcookie.SessionCookie:
			sess = c
		case authcookie.CSRFCookie:
			csrf = c
		}
	}
	if sess == nil || csrf == nil {
		t.Fatalf("cookie pair not re-issued after a slid session: %+v", rec.Result().Cookies())
	}
	if sess.Value != "valid-session" || csrf.Value != mintedCSRF {
		t.Errorf("re-issued values changed: session=%q csrf=%q", sess.Value, csrf.Value)
	}
	want := int(webauthn.AuthTokenTTL.Seconds())
	if sess.MaxAge < want-5 || sess.MaxAge > want || csrf.MaxAge < want-5 || csrf.MaxAge > want {
		t.Errorf("MaxAge = %d/%d, want about %d (the new expiry)", sess.MaxAge, csrf.MaxAge, want)
	}
}

// TestAuthMiddleware_SessionCookie_NotReissuedWhenNotSlid: an ordinary
// validated request writes no cookies, so the hot path stays free of
// Set-Cookie headers.
func TestAuthMiddleware_SessionCookie_NotReissuedWhenNotSlid(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	h.webauthnSessionMgr = &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "valid-session" },
	}
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/system", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "valid-session"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Errorf("cookies re-issued although the session did not slide: %+v", got)
	}
}

// TestResolveCredentials_ReauthDoesNotSlide: the SSE heartbeat re-check calls
// resolveCredentials with use=false and must go through Verify (a pure lookup),
// never Authenticate: it is the server's own tick, not the person using the
// session, and its headers are long gone so it could not carry a re-issued
// cookie anyway. A real request (use=true) goes through Authenticate and gets
// the refresh hint.
func TestResolveCredentials_ReauthDoesNotSlide(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := testHandler(nil, nil, nil, mockAuth, nil)
	authCalls := 0
	mgr := &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "valid-session" },
		authFn: func(_ context.Context, token string) (webauthn.AuthResult, bool) {
			authCalls++
			if token != "valid-session" {
				return webauthn.AuthResult{}, false
			}
			return webauthn.AuthResult{UserID: []byte("admin"), ExpiresAt: time.Now().Add(webauthn.AuthTokenTTL), Extended: true}, true
		},
	}
	h.webauthnSessionMgr = mgr

	req := httptest.NewRequest(http.MethodGet, "/api/events", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "valid-session"})

	id, cookieAuth, ok, refresh := h.resolveCredentials(req, false)
	if !ok || !cookieAuth || id == nil {
		t.Fatalf("re-check: ok=%v cookieAuth=%v id=%v, want an admitted cookie session", ok, cookieAuth, id)
	}
	if refresh != nil {
		t.Error("re-check produced a cookie refresh hint")
	}
	if authCalls != 0 || mgr.verifyCalls.Load() != 1 {
		t.Errorf("re-check used Authenticate %d times and Verify %d times, want 0 and 1", authCalls, mgr.verifyCalls.Load())
	}

	_, _, ok, refresh = h.resolveCredentials(req, true)
	if !ok || refresh == nil {
		t.Errorf("real request: ok=%v refresh=%v, want an admitted session with a refresh hint", ok, refresh)
	}
	if authCalls != 1 {
		t.Errorf("real request used Authenticate %d times, want 1", authCalls)
	}
}
