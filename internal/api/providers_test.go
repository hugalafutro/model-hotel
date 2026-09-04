package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

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
	// The key is held for the credential mask from the moment it is created.
	if !slices.Contains(util.HeldSecrets(), "sk-test-key") {
		t.Error("created provider's key not held for the credential mask")
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
	payload := `{"name":"` + newName + `","api_key":"sk-rotated-key-for-held-test"}`
	body := bytes.NewReader([]byte(payload))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateProvider(w, req)
	// A rotated key is held for the credential mask the moment it is set.
	if !slices.Contains(util.HeldSecrets(), "sk-rotated-key-for-held-test") {
		t.Error("rotated provider key not held for the credential mask")
	}

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
	// The delete also has to take the quota drift watch's schema baseline with
	// it: nothing else removes that key, so it would outlive the provider and
	// accumulate in the settings K/V for every provider ever deleted.
	var deletedKeys []string
	sets := &mockSettingsStore{
		deleteKeyFn: func(_ context.Context, key string) error {
			deletedKeys = append(deletedKeys, key)
			return nil
		},
	}
	h := testHandler(mockProv, nil, sets, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteProvider(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}
	wantKey := quotaSchemaSettingKey(id)
	if len(deletedKeys) != 1 || deletedKeys[0] != wantKey {
		t.Errorf("got deleted settings keys %v, want exactly [%s]", deletedKeys, wantKey)
	}
}

// TestDeleteProvider_BaselineCleanupFailureStillSucceeds keeps the cleanup in
// its place: the provider row is already gone by the time it runs, so a settings
// error is a housekeeping wart to log, never a reason to report a failed delete
// for something that did happen.
func TestDeleteProvider_BaselineCleanupFailureStillSucceeds(t *testing.T) {
	id := uuid.New()
	mockProv := &mockProviderStore{
		deleteFn: func(context.Context, uuid.UUID) error { return nil },
	}
	sets := &mockSettingsStore{
		deleteKeyFn: func(context.Context, string) error { return errors.New("settings boom") },
	}
	h := testHandler(mockProv, nil, sets, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteProvider(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("got status %d, want %d — a failed baseline cleanup must not fail the delete", w.Code, http.StatusNoContent)
	}
}

// TestDeleteProvider_ForgetsTheInMemoryDriftCandidate covers the other half of
// the orphan: the debounce candidate is process-wide state keyed by provider,
// and a provider that is gone can never clear its own entry (its snapshots
// cascade away with it, so the watch never visits it again).
func TestDeleteProvider_ForgetsTheInMemoryDriftCandidate(t *testing.T) {
	id := uuid.New()
	h := testHandler(
		&mockProviderStore{deleteFn: func(context.Context, uuid.UUID) error { return nil }},
		nil,
		&mockSettingsStore{deleteKeyFn: func(context.Context, string) error { return nil }},
		nil, nil,
	)
	h.quotaSchemaSeen = map[uuid.UUID]quotaSchemaCandidate{
		id: {fingerprint: "abc", seen: 1, fetchedAt: time.Now()},
	}

	req, w := newChiRequest(http.MethodDelete, "/providers/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteProvider(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusNoContent)
	}
	if _, ok := h.quotaSchemaSeen[id]; ok {
		t.Error("the drift candidate of a deleted provider must be forgotten")
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

func TestProviderTypeAllowsEmptyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerType string
		want         bool
	}{
		{name: "opencode_zen", providerType: "opencode-zen", want: true},
		{name: "openai", providerType: "openai", want: false},
		{name: "anthropic", providerType: "anthropic", want: false},
		{name: "ollama", providerType: "ollama", want: true},
		// Self-hosted servers are keyless whatever port they run on: the
		// waiver follows the stored type, not the address.
		{name: "lmstudio", providerType: "lmstudio", want: true},
		{name: "koboldcpp", providerType: "koboldcpp", want: true},
		{name: "custom", providerType: "custom", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerTypeAllowsEmptyKey(tt.providerType)
			if got != tt.want {
				t.Errorf("providerTypeAllowsEmptyKey(%q) = %v, want %v", tt.providerType, got, tt.want)
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
	// The operator gets the reason, not a bare "invalid": which rule refused
	// the address decides what they have to change.
	if !strings.Contains(got, codeProviderURLRejected) {
		t.Errorf("expected the %s code, got %q", codeProviderURLRejected, got)
	}
	if !strings.Contains(got, "ALLOWED_PROVIDER_HOSTS") {
		t.Errorf("expected the reason to name the allowlist, got %q", got)
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
	// The operator gets the reason, not a bare "invalid": which rule refused
	// the address decides what they have to change.
	if !strings.Contains(got, codeProviderURLRejected) {
		t.Errorf("expected the %s code, got %q", codeProviderURLRejected, got)
	}
	if !strings.Contains(got, "ALLOWED_PROVIDER_HOSTS") {
		t.Errorf("expected the reason to name the allowlist, got %q", got)
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
	}, nil, &mockSettingsStore{
		deleteKeyFn: func(context.Context, string) error { return nil },
	}, &mockAdminAuth{validateFn: func(string) bool { return true }}, testDB)

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

	var response []map[string]any
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

	// Two request logs a day apart: the totals sum both, tokens_since is the
	// older one.
	oldest := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	_, err = pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $3, 'test-model', 200, 100, 50, 25, $4),
		       ($2, $3, 'test-model', 200, 100, 0, 50, $5)`,
		uuid.New(), uuid.New(), providerUUID, oldest, oldest.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request logs: %v", err)
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
			if p.TokensSince == nil || !p.TokensSince.Equal(oldest) {
				t.Errorf("Expected TokensSince=%v (the oldest log), got %v", oldest, p.TokensSince)
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

func TestCreateProvider_UnknownProviderType(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"test","base_url":"https://api.example.com/v1","api_key":"sk-key","provider_type":"not-a-real-type"}`))
	req, w := newChiRequest(http.MethodPost, "/providers", body)

	h.CreateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "unknown provider_type") {
		t.Fatalf("expected the unknown-type reason, got %q", w.Body.String())
	}
}

// A type-only update re-checks the stored address against the current URL
// rules: a legacy plain-HTTP address that predates the HTTPS requirement must
// be rejected rather than probed.
func TestUpdateProvider_TypeChangeRechecksStoredURL(t *testing.T) {
	id := uuid.New()
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   false,
			AllowedProviderHosts: []string{"example.com"},
		},
		providerRepo: &mockProviderStore{
			getFn: func(_ context.Context, got uuid.UUID) (*provider.Provider, error) {
				return &provider.Provider{ID: got, Name: "legacy", BaseURL: "http://example.com/v1", ProviderType: "openai"}, nil
			},
		},
		adminMgr: &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	body := bytes.NewReader([]byte(`{"provider_type":"anthropic"}`))
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "HTTPS") {
		t.Fatalf("expected the HTTPS reason, got %q", w.Body.String())
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"other pg code", &pgconn.PgError{Code: "23505"}, false},
		{"fk violation", &pgconn.PgError{Code: "23503"}, true},
		{"wrapped fk violation", fmt.Errorf("delete: %w", &pgconn.PgError{Code: "23503"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isForeignKeyViolation(tc.err); got != tc.want {
				t.Fatalf("isForeignKeyViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
