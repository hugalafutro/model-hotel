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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
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
	createFn func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*virtualkey.VirtualKey, error)
	listFn   func(ctx context.Context) ([]*virtualkey.VirtualKey, error)
	getFn    func(ctx context.Context, id uuid.UUID) (*virtualkey.VirtualKey, error)
	deleteFn func(ctx context.Context, id uuid.UUID) error
	updateFn func(ctx context.Context, id uuid.UUID, name string, rps *float64, burst *int) (*virtualkey.VirtualKey, error)
}

func (m *mockVirtualKeyStore) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*virtualkey.VirtualKey, error) {
	if m.createFn != nil {
		return m.createFn(ctx, name, keyHash, keyPreview, rps, burst)
	}
	return nil, errors.New("mock: Create not implemented")
}
func (m *mockVirtualKeyStore) List(ctx context.Context) ([]*virtualkey.VirtualKey, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, errors.New("mock: List not implemented")
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
func (m *mockVirtualKeyStore) Update(ctx context.Context, id uuid.UUID, name string, rps *float64, burst *int) (*virtualkey.VirtualKey, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, name, rps, burst)
	}
	return nil, errors.New("mock: Update not implemented")
}

type mockSettingsStore struct {
	getWithDefaultFn  func(ctx context.Context, key string, defaultValue string) string
	setFn             func(ctx context.Context, key string, value string) error
	getAllFn          func(ctx context.Context) (map[string]string, error)
	setTxFn           func(ctx context.Context, tx pgx.Tx, key, value string) error
	invalidateCacheFn func(key string)
}

func (m *mockSettingsStore) GetWithDefault(ctx context.Context, key, defaultValue string) string {
	if m.getWithDefaultFn != nil {
		return m.getWithDefaultFn(ctx, key, defaultValue)
	}
	return defaultValue
}
func (m *mockSettingsStore) Set(ctx context.Context, key, value string) error {
	if m.setFn != nil {
		return m.setFn(ctx, key, value)
	}
	return errors.New("mock: Set not implemented")
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
func (m *mockSettingsStore) InvalidateCache(key string) {
	if m.invalidateCacheFn != nil {
		m.invalidateCacheFn(key)
	}
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
	t.Skip("requires real database connection for model count query")
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
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*virtualkey.VirtualKey, error) {
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

// --- Pure function tests ---

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
			want:    false,
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
// The ListProviders handler requires real DB connection for model/token count queries.
// Integration tests cover: TestListProviders_Empty, TestListProviders_AfterCreate,
// TestListProviders_WithPagination, TestListProviders_WithSearchFilter,
// TestListProviders_WithPaginationAndModelCounts, TestListProviders_SearchFilter_Integration
// ---------------------------------------------------------------------------

// setChiURLParam sets a chi URL parameter on the request context.
func setChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
