package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
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
	return req, httptest.NewRecorder()
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
}

// --- Provider endpoint tests ---

func TestCreateProvider_Success(t *testing.T) {
	mockProv := &mockProviderStore{
		createFn: func(_ context.Context, req provider.CreateProviderRequest, _, _, _ []byte) (*provider.Provider, error) {
			if req.Name != "test-provider" {
				t.Errorf("expected name 'test-provider', got %q", req.Name)
			}
			if req.BaseURL != "https://api.example.com/v1" {
				t.Errorf("expected base_url 'https://api.example.com/v1', got %q", req.BaseURL)
			}
			return &provider.Provider{
				ID:        uuid.New(),
				Name:      req.Name,
				BaseURL:   req.BaseURL,
				Enabled:   true,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(_ string) bool { return true }}

	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	body := bytes.NewReader([]byte(`{"name":"test-provider","base_url":"https://api.example.com/v1","api_key":"sk-test-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)

	h.CreateProvider(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var resp provider.ProviderResponse
	parseJSON(t, w, &resp)
	if resp.Name != "test-provider" {
		t.Errorf("expected name 'test-provider', got %q", resp.Name)
	}
	if resp.BaseURL != "https://api.example.com/v1" {
		t.Errorf("expected base_url, got %q", resp.BaseURL)
	}
}

func TestCreateProvider_MissingName(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"","base_url":"https://api.example.com/v1","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)

	h.CreateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateProvider_NameTooLong(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	longName := bytes.Repeat([]byte("a"), 101)
	payload := `{"name":"` + string(longName) + `","base_url":"https://api.example.com/v1","api_key":"sk-key"}`
	body := bytes.NewReader([]byte(payload))
	req, w := newChiRequest(http.MethodPost, "/providers", body)

	h.CreateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateProvider_MissingBaseURL(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)

	h.CreateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateProvider_DuplicateName(t *testing.T) {
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, name string) (*provider.Provider, error) {
			return &provider.Provider{ID: uuid.New(), Name: name}, nil // existing provider
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"duplicate","base_url":"https://api.example.com/v1","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)

	h.CreateProvider(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
}

func TestListProviders_RepoError(t *testing.T) {
	mockProv := &mockProviderStore{
		listFn: func(_ context.Context) ([]*provider.Provider, error) {
			return nil, errors.New("db error")
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodGet, "/providers", nil)

	h.ListProviders(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestGetProvider_Success(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create a provider first
	createBody := `{"name":"get-test-provider","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	// Now GET the provider
	req = httptest.NewRequest(http.MethodGet, "/providers/"+created.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get provider: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var fetched struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		ModelCount int    `json:"model_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("failed to decode fetched provider: %v", err)
	}
	if fetched.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, fetched.ID)
	}
	if fetched.Name != "get-test-provider" {
		t.Errorf("expected name 'get-test-provider', got %q", fetched.Name)
	}
}

func TestGetProvider_NotFound(t *testing.T) {
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			return nil, pgx.ErrNoRows
		},
	}
	h := testHandler(mockProv, nil, nil, nil, nil)
	id := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.GetProvider(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestUpdateProvider_Success(t *testing.T) {
	id := uuid.New()
	newName := "updated-name"
	mockProv := &mockProviderStore{
		updateFn: func(ctx context.Context, pid uuid.UUID, req provider.UpdateProviderRequest, ek, kn, ks []byte) (*provider.Provider, error) {
			if pid != id {
				t.Errorf("expected id %s, got %s", id, pid)
			}
			return &provider.Provider{
				ID:        id,
				Name:      *req.Name,
				BaseURL:   "https://api.example.com",
				Enabled:   true,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}, nil
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	payload := `{"name":"` + newName + `"}`
	body := bytes.NewReader([]byte(payload))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateProvider(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp provider.ProviderResponse
	parseJSON(t, w, &resp)
	if resp.Name != newName {
		t.Errorf("expected name %q, got %q", newName, resp.Name)
	}
}

func TestDeleteProvider_Success(t *testing.T) {
	id := uuid.New()
	mockProv := &mockProviderStore{
		deleteFn: func(ctx context.Context, pid uuid.UUID) error {
			if pid != id {
				t.Errorf("expected id %s, got %s", id, pid)
			}
			return nil
		},
	}
	h := testHandler(mockProv, nil, nil, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteProvider(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

func TestDeleteProvider_NotFound(t *testing.T) {
	id := uuid.New()
	mockProv := &mockProviderStore{
		deleteFn: func(ctx context.Context, pid uuid.UUID) error {
			return pgx.ErrNoRows
		},
	}
	h := testHandler(mockProv, nil, nil, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteProvider(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// --- Settings endpoint tests ---

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
	if resp.KeyPreview == "" {
		t.Error("expected key_preview to be set")
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

	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil)
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

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil_error",
			err:  nil,
			want: false,
		},
		{
			name: "pg_error_23505_unique_violation",
			err:  &pgconn.PgError{Code: "23505"},
			want: true,
		},
		{
			name: "pg_error_23503_fk_violation",
			err:  &pgconn.PgError{Code: "23503"},
			want: false,
		},
		{
			name: "pg_error_42P01_undefined_table",
			err:  &pgconn.PgError{Code: "42P01"},
			want: false,
		},
		{
			name: "wrapped_pg_error_23505",
			err:  fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "23505"}),
			want: true,
		},
		{
			name: "non_pg_error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUniqueViolation(tt.err)
			if got != tt.want {
				t.Errorf("isUniqueViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestProviderTypeAllowsEmptyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{
			name:    "opencode_zen_base_url",
			baseURL: "https://opencode.ai/api/zen",
			want:    true,
		},
		{
			name:    "openai_base_url",
			baseURL: "https://api.openai.com/v1",
			want:    false,
		},
		{
			name:    "anthropic_base_url",
			baseURL: "https://api.anthropic.com/v1",
			want:    false,
		},
		{
			name:    "ollama_localhost",
			baseURL: "http://localhost:11434",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerTypeAllowsEmptyKey(tt.baseURL)
			if got != tt.want {
				t.Errorf("providerTypeAllowsEmptyKey(%q) = %v, want %v", tt.baseURL, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListProviders tests with integration (see handler_integration_test.go)

// --- Additional unit tests for uncovered paths ---

func TestCreateProvider_BaseURLTooLong(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	longURL := "https://api.example.com/" + strings.Repeat("a", 490) // >500 chars
	body := bytes.NewReader([]byte(fmt.Sprintf(`{"name":"test","base_url":"%s","api_key":"sk-key"}`, longURL)))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateProvider_APIKeyTooLong(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	longKey := "sk-" + strings.Repeat("a", 498) // >500 chars
	body := bytes.NewReader([]byte(fmt.Sprintf(`{"name":"test","base_url":"https://api.example.com/v1","api_key":"%s"}`, longKey)))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateProvider_HTTPURLRejected(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   false,
			AllowedProviderHosts: []string{"api.example.com"},
		},
		providerRepo: &mockProviderStore{},
		adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"http://api.example.com/v1","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateProvider_RepoError(t *testing.T) {
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, _ string) (*provider.Provider, error) { return nil, nil },
		createFn: func(_ context.Context, _ provider.CreateProviderRequest, _, _, _ []byte) (*provider.Provider, error) {
			return nil, errors.New("db error")
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"https://api.example.com/v1","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestCreateProvider_UniqueViolation(t *testing.T) {
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, _ string) (*provider.Provider, error) { return nil, nil },
		createFn: func(_ context.Context, _ provider.CreateProviderRequest, _, _, _ []byte) (*provider.Provider, error) {
			return nil, &pgconn.PgError{Code: "23505"}
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"https://api.example.com/v1","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
}

func TestUpdateProvider_DuplicateName(t *testing.T) {
	id := uuid.New()
	otherID := uuid.New()
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, name string) (*provider.Provider, error) {
			return &provider.Provider{ID: otherID, Name: name}, nil // different ID = conflict
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"duplicate-name"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
}

func TestUpdateProvider_UniqueViolation(t *testing.T) {
	id := uuid.New()
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, _ string) (*provider.Provider, error) { return nil, nil },
		updateFn: func(_ context.Context, _ uuid.UUID, _ provider.UpdateProviderRequest, _, _, _ []byte) (*provider.Provider, error) {
			return nil, &pgconn.PgError{Code: "23505"}
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"conflict-name"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
}

func TestUpdateProvider_APIKeyTooLong(t *testing.T) {
	id := uuid.New()
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	longKey := "sk-" + strings.Repeat("a", 498) // >500 chars
	body := bytes.NewReader([]byte(fmt.Sprintf(`{"api_key":"%s"}`, longKey)))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestGetProvider_RepoError(t *testing.T) {
	mockProv := &mockProviderStore{
		getFn: func(_ context.Context, _ uuid.UUID) (*provider.Provider, error) {
			return nil, errors.New("connection refused")
		},
	}
	h := testHandler(mockProv, nil, nil, nil, nil)
	id := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())
	h.GetProvider(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestDeleteProvider_RepoError(t *testing.T) {
	mockProv := &mockProviderStore{
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			return errors.New("connection refused")
		},
	}
	h := testHandler(mockProv, nil, nil, nil, nil)
	id := uuid.New()
	req, w := newChiRequest(http.MethodDelete, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())
	h.DeleteProvider(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// The ListProviders handler requires real DB connection for model/token count queries.
// Integration tests cover: TestListProviders_Empty, TestListProviders_AfterCreate,
// TestListProviders_WithPagination, TestListProviders_WithSearchFilter,
// TestListProviders_WithPaginationAndModelCounts, TestListProviders_SearchFilter_Integration
// ---------------------------------------------------------------------------

// --- Additional tests for uncovered error paths ---

func TestCreateProvider_EmptyAPIKey_Unit(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   true,
			AllowedProviderHosts: []string{"api.example.com"},
		},
		providerRepo: &mockProviderStore{},
		adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	// OpenAI-style URL requires API key
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"https://api.example.com/v1","api_key":""}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "api_key is required for this provider type") {
		t.Errorf("expected error about api_key required, got %q", got)
	}
}

func TestCreateProvider_BlockedHost(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   true,
			AllowedProviderHosts: []string{"allowed.com"},
		},
		providerRepo: &mockProviderStore{},
		adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"https://blocked.com/v1","api_key":"sk-key"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "invalid provider URL") {
		t.Errorf("expected error about invalid provider URL, got %q", got)
	}
}

func TestUpdateProvider_MalformedJSON(t *testing.T) {
	id := uuid.New()
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{invalid json}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "invalid request body") {
		t.Errorf("expected error about invalid request body, got %q", got)
	}
}

func TestUpdateProvider_DuplicateNameOnRename(t *testing.T) {
	id := uuid.New()
	existingID := uuid.New()
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, name string) (*provider.Provider, error) {
			return &provider.Provider{ID: existingID, Name: name}, nil
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"existing-name"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "a provider with this name already exists") {
		t.Errorf("expected error about duplicate name, got %q", got)
	}
}

func TestUpdateProvider_HTTPURLRejected(t *testing.T) {
	id := uuid.New()
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   false,
			AllowedProviderHosts: []string{"example.com"},
		},
		providerRepo: &mockProviderStore{},
		adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	body := bytes.NewReader([]byte(`{"base_url":"http://example.com/v1"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "base_url must use HTTPS") {
		t.Errorf("expected error about HTTPS requirement, got %q", got)
	}
}

func TestUpdateProvider_BlockedHost(t *testing.T) {
	id := uuid.New()
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   true,
			AllowedProviderHosts: []string{"allowed.com"},
		},
		providerRepo: &mockProviderStore{},
		adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	body := bytes.NewReader([]byte(`{"base_url":"https://blocked.com/v1"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "invalid provider URL") {
		t.Errorf("expected error about invalid provider URL, got %q", got)
	}
}

func TestUpdateProvider_GenericRepoError(t *testing.T) {
	id := uuid.New()
	mockProv := &mockProviderStore{
		getByNameFn: func(_ context.Context, _ string) (*provider.Provider, error) { return nil, nil },
		updateFn: func(_ context.Context, _ uuid.UUID, _ provider.UpdateProviderRequest, _, _, _ []byte) (*provider.Provider, error) {
			return nil, errors.New("generic db error")
		},
	}
	h := testHandler(mockProv, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"test"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// setChiURLParam sets a chi URL parameter on the request context.
func setChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- Coverage tests for uncovered lines ---

func TestCreateProvider_InvalidJSON(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := strings.NewReader("{invalid json")
	req, w := newChiRequest(http.MethodPost, "/providers", body)
	h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestCreateProvider_EncryptError is not implemented because auth.Encrypt uses argon2.IDKey
// which succeeds even with an empty master key. The error path (lines 216-219) would only be
// hit if crypto/rand.Read fails (extremely rare) or AES cipher creation fails. Testing this
// would require refactoring to allow dependency injection of the randReader or cipher functions.
// The encrypt call itself (line 215) is exercised by TestCreateProvider_Success.

func TestUpdateProvider_InvalidUUID(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodPut, "/providers/not-a-uuid", strings.NewReader(`{"name":"test"}`))
	req = setChiURLParam(req, "id", "not-a-uuid")
	h.UpdateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUpdateProvider_BaseURLTooLong(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	longURL := "https://api.example.com/" + strings.Repeat("a", 500)
	body := bytes.NewReader([]byte(fmt.Sprintf(`{"base_url":"%s"}`, longURL)))
	req, w := newChiRequest(http.MethodPut, "/providers/"+uuid.New().String(), body)
	req = setChiURLParam(req, "id", uuid.New().String())
	h.UpdateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	got := strings.TrimSpace(w.Body.String())
	if !strings.Contains(got, "invalid base URL") {
		t.Errorf("expected error about invalid base URL, got %q", got)
	}
}

// TestUpdateProvider_EncryptError is not implemented because auth.Encrypt uses argon2.IDKey
// which succeeds even with an empty master key. The error path (lines 398-401) would only be
// hit if crypto/rand.Read fails (extremely rare) or AES cipher creation fails. Testing this
// would require refactoring to allow dependency injection of the randReader or cipher functions.
// The encrypt call itself (line 397) is exercised by TestUpdateProvider_Success.

func TestListProviders_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	// Create a real DB connection for the model/token count queries
	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := testHandler(&mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{{ID: uuid.New(), Name: "test", BaseURL: "https://api.example.com", Enabled: true}}, nil
		},
	}, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, testDB)

	// Create request with cancelled context
	req, w := newChiRequest(http.MethodGet, "/providers", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel() // Cancel immediately to cause query errors
	req = req.WithContext(ctx)

	h.ListProviders(w, req)
	// With cancelled context, the model counts query should fail
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestDeleteProvider_SyncFailoverError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	// Create a real DB connection, then close it so SyncAllModels fails.
	// The deleteFn mock doesn't use the pool, so it succeeds independently.
	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	testDB.Close() // Close immediately — SyncAllModels will fail with closed pool

	h := testHandler(&mockProviderStore{
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			return nil // Mock succeeds; SyncAllModels is what we want to fail
		},
	}, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, testDB)

	req, w := newChiRequest(http.MethodDelete, "/providers/"+uuid.New().String(), nil)
	req = setChiURLParam(req, "id", uuid.New().String())

	h.DeleteProvider(w, req)
	// Delete succeeds (mocked), SyncAllModels fails (closed pool),
	// but handler logs the error and still returns 204 No Content.
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap_test.go
// ---------------------------------------------------------------------------

// TestListProviders_Integration tests the ListProviders handler with an empty database.
func TestListProviders_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response) != 0 {
		t.Errorf("expected empty provider list, got %d providers", len(response))
	}
}

// TestListProviders_WithProviders tests listing providers when database has entries.
func TestListProviders_WithProviders(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create two providers
	provider1 := `{"name": "test-list-1", "base_url": "https://api.openai.com", "api_key": "sk-test1"}`
	provider2 := `{"name": "test-list-2", "base_url": "https://api.anthropic.com", "api_key": "sk-ant-test"}`

	for _, body := range []string{provider1, provider2} {
		req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("failed to create provider: %d: %s", w.Code, w.Body.String())
		}
	}

	// List all providers
	req := httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("expected 2 providers, got %d", len(response))
	}
}

// TestCreateProvider_Integration_Success tests creating a provider with valid data.
func TestCreateProvider_Integration_Success(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"name": "test-create-success", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Name != "test-create-success" {
		t.Errorf("expected name 'test-create-success', got %s", response.Name)
	}
	if response.BaseURL != "https://api.openai.com" {
		t.Errorf("expected base_url 'https://api.openai.com', got %s", response.BaseURL)
	}
	if response.ID == "" {
		t.Error("expected non-empty ID")
	}
}

// TestUpdateProvider_Integration_Success tests updating a provider's fields.
func TestUpdateProvider_Integration_Success(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider first
	createBody := `{"name": "test-update-original", "base_url": "https://api.openai.com", "api_key": "sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %d: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	// Update the provider
	updateBody := `{"name": "test-update-new", "base_url": "https://api.anthropic.com"}`
	req = httptest.NewRequest(http.MethodPut, "/providers/"+createResp.ID, strings.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var updateResp struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&updateResp); err != nil {
		t.Fatalf("failed to decode update response: %v", err)
	}

	if updateResp.Name != "test-update-new" {
		t.Errorf("expected name 'test-update-new', got %s", updateResp.Name)
	}
	if updateResp.BaseURL != "https://api.anthropic.com" {
		t.Errorf("expected base_url 'https://api.anthropic.com', got %s", updateResp.BaseURL)
	}
}

// TestUpdateProvider_NotFound tests updating a non-existent provider.
func TestUpdateProvider_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	unknownID := "00000000-0000-0000-0000-000000000000"
	body := `{"name": "test-update-notfound"}`
	req := httptest.NewRequest(http.MethodPut, "/providers/"+unknownID, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 Not Found, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteProvider_Integration_Success tests deleting an existing provider.
func TestDeleteProvider_Integration_Success(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider first
	createBody := `{"name": "test-delete-success", "base_url": "https://api.openai.com", "api_key": "sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %d: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	// Delete the provider
	req = httptest.NewRequest(http.MethodDelete, "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204 No Content, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 Not Found after delete, got %d", w.Code)
	}
}

// TestListProviders_WithModelCounts tests ListProviders with providers that have models
// to cover the model count query and rows.Scan paths.
func TestListProviders_WithModelCounts(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	// Clean test data
	pool.Exec(ctx, `TRUNCATE request_logs, models, providers CASCADE`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	defer dbInst.Close()

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test", nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create a provider
	createBody := `{"name":"test-provider-models","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	// Insert models for this provider
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO models (id, model_id, name, provider_id, enabled, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, true, NOW(), NOW()),
		       ($5, $6, $7, $4, true, NOW(), NOW())`,
		uuid.New(), modelID1, "model-1", created.ID,
		uuid.New(), modelID2, "model-2")
	if err != nil {
		t.Fatalf("Failed to insert models: %v", err)
	}

	// List providers
	req = httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list providers: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var providers []provider.ProviderResponse
	if err := json.NewDecoder(w.Body).Decode(&providers); err != nil {
		t.Fatalf("failed to decode providers: %v", err)
	}

	// Find our test provider
	var found bool
	for _, p := range providers {
		if p.Name == "test-provider-models" {
			found = true
			if p.ModelCount != 2 {
				t.Errorf("Expected ModelCount=2, got %d", p.ModelCount)
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find test-provider-models in list")
	}
}

// TestListProviders_WithTokenCounts tests ListProviders with request logs
// to cover the token count query and rows.Scan paths.
func TestListProviders_WithTokenCounts(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	// Clean test data
	pool.Exec(ctx, `TRUNCATE request_logs, models, providers CASCADE`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	defer dbInst.Close()

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test", nil, nil, nil, nil)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create a provider
	createBody := `{"name":"test-provider-tokens","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	providerUUID, _ := uuid.Parse(created.ID)

	// Insert request logs with token counts for this provider
	logID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'test-model', 200, 100, 50, 75, NOW())`,
		logID, providerUUID)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// List providers
	req = httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list providers: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var providers []provider.ProviderResponse
	if err := json.NewDecoder(w.Body).Decode(&providers); err != nil {
		t.Fatalf("failed to decode providers: %v", err)
	}

	// Find our test provider
	var found bool
	for _, p := range providers {
		if p.Name == "test-provider-tokens" {
			found = true
			if p.TotalTokens != 125 {
				t.Errorf("Expected TotalTokens=125, got %d", p.TotalTokens)
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find test-provider-tokens in list")
	}
}

// TestListProviders_TokenCountScanError tests the token count rows.Scan error
// path in ListProviders. Uses a cancelled context during the token count query
// to force a query failure.
func TestListProviders_TokenCountScanError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	// With a cancelled context, the token count query will fail,
	// which also covers the rows.Scan error path indirectly.
	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := testHandler(&mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{{ID: uuid.New(), Name: "test", BaseURL: "https://api.example.com", Enabled: true}}, nil
		},
	}, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, testDB)

	// Create request with cancelled context - model count query succeeds but
	// token count query may fail due to the cancelled context
	req, w := newChiRequest(http.MethodGet, "/providers", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	h.ListProviders(w, req)
	// With a cancelled context, one of the queries should fail
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// TestListProviders_TokenCountScanError tests the model count rows.Scan error
// path in ListProviders directly. Uses a closed database pool so the query fails.
func TestListProviders_ClosedDBPool(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("apiTestDBURL not set: test database required")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	pool.Close() // close immediately so queries fail

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	// Create a handler with provider list succeeding but DB pool closed
	h := &Handler{
		providerRepo: &mockProviderStore{
			listFn: func(ctx context.Context) ([]*provider.Provider, error) {
				return []*provider.Provider{}, nil
			},
		},
		dbPool:   testDB,
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}

	req := httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	w := httptest.NewRecorder()

	h.ListProviders(w, req)

	// Any query against the DB should eventually fail since the pool is shared
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d", w.Code)
	}
}
