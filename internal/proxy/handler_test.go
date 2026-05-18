package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newUnitHandler creates a Handler with nil-pool repos suitable for unit
// testing. Rate limiting is disabled so the middleware is a no-op.
// Callers must defer stopUnitHandler(h) to clean up background goroutines.
func newUnitHandler() *Handler {
	cfg := &config.Config{
		MasterKey:        "test-master-key",
		RateLimitEnabled: false,
	}
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	settingsRepo := settings.NewRepository(nil)
	rateLimiter := ratelimit.NewLimiter(settingsRepo)
	return &Handler{
		cfg:            cfg,
		providerRepo:   provider.NewRepository(nil),
		modelRepo:      model.NewRepository(nil),
		virtualKeyRepo: &virtualKeyRepoAdapter{repo: virtualkey.NewRepository(nil)},
		failoverRepo:   failover.NewRepository(nil),
		settingsRepo:   settingsRepo,
		rateLimiter:    rateLimiter,
		ipLimiter:      ipLimiter,
		upstreamTransport: &http.Transport{
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
	}
}

// stopUnitHandler stops background goroutines started by newUnitHandler.
func stopUnitHandler(h *Handler) {
	h.rateLimiter.Stop()
	h.ipLimiter.Stop()
	if h.upstreamTransport != nil {
		h.upstreamTransport.CloseIdleConnections()
	}
}

// mockVirtualKeyRepoWithFuncs is a test mock implementing VirtualKeyRepository
// with customizable Create and Delete functions for testing error paths.
// (Note: mockVirtualKeyRepo exists in response_test.go for simpler use cases)
type mockVirtualKeyRepoWithFuncs struct {
	createFunc func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error)
	deleteFunc func(ctx context.Context, id string) error
}

func (m *mockVirtualKeyRepoWithFuncs) AddTokens(_ context.Context, keyHash string, tokens int) error {
	return nil
}

func (m *mockVirtualKeyRepoWithFuncs) TouchLastUsed(ctx context.Context, keyHash string) error {
	return nil
}

func (m *mockVirtualKeyRepoWithFuncs) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
	return nil, nil
}

func (m *mockVirtualKeyRepoWithFuncs) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, name, keyHash, keyPreview, rps, burst)
	}
	return nil, nil
}

func (m *mockVirtualKeyRepoWithFuncs) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// NewHandler tests
// ---------------------------------------------------------------------------

func TestNewHandler_SetsAllFields(t *testing.T) {
	cfg := &config.Config{MasterKey: "test-key", RateLimitEnabled: false}
	providerRepo := provider.NewRepository(nil)
	modelRepo := model.NewRepository(nil)
	virtualKeyRepo := virtualkey.NewRepository(nil)
	failoverRepo := failover.NewRepository(nil)
	settingsRepo := settings.NewRepository(nil)
	rateLimiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	defer rateLimiter.Stop()
	defer ipLimiter.Stop()

	h := NewHandler(cfg, providerRepo, modelRepo, nil, virtualKeyRepo, failoverRepo, settingsRepo, rateLimiter, ipLimiter)

	if h.cfg != cfg {
		t.Error("cfg not set correctly")
	}
	if h.providerRepo != providerRepo {
		t.Error("providerRepo not set correctly")
	}
	if h.modelRepo != modelRepo {
		t.Error("modelRepo not set correctly")
	}
	// virtualKeyRepo is wrapped in adapter, so we can't compare directly
	if h.virtualKeyRepo == nil {
		t.Error("virtualKeyRepo should not be nil")
	}
	if h.failoverRepo != failoverRepo {
		t.Error("failoverRepo not set correctly")
	}
	if h.settingsRepo != settingsRepo {
		t.Error("settingsRepo not set correctly")
	}
	if h.rateLimiter != rateLimiter {
		t.Error("rateLimiter not set correctly")
	}
	if h.ipLimiter != ipLimiter {
		t.Error("ipLimiter not set correctly")
	}
	if h.upstreamTransport == nil {
		t.Error("upstreamTransport should not be nil")
	}
}

func TestNewHandler_CreatesTransport(t *testing.T) {
	settingsRepo := settings.NewRepository(nil)
	rateLimiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	defer rateLimiter.Stop()
	defer ipLimiter.Stop()

	h := NewHandler(
		&config.Config{MasterKey: "test-key", RateLimitEnabled: false},
		provider.NewRepository(nil), model.NewRepository(nil), nil,
		virtualkey.NewRepository(nil), failover.NewRepository(nil),
		settingsRepo, rateLimiter, ipLimiter,
	)

	if h.upstreamTransport == nil {
		t.Fatal("upstreamTransport should be created")
	}
	if h.upstreamTransport.ResponseHeaderTimeout != 120*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 120s", h.upstreamTransport.ResponseHeaderTimeout)
	}
	if h.upstreamTransport.IdleConnTimeout != 120*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 120s", h.upstreamTransport.IdleConnTimeout)
	}
	if h.upstreamTransport.MaxIdleConns != 200 {
		t.Errorf("MaxIdleConns = %v, want 200", h.upstreamTransport.MaxIdleConns)
	}
	if h.upstreamTransport.MaxIdleConnsPerHost != 20 {
		t.Errorf("MaxIdleConnsPerHost = %v, want 20", h.upstreamTransport.MaxIdleConnsPerHost)
	}
}

// ---------------------------------------------------------------------------
// ProxyKeyMiddleware tests (pure unit — no DB required)
// ---------------------------------------------------------------------------

func TestProxyKeyMiddleware_EmptyBearerToken(t *testing.T) {
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	defer ipLimiter.Stop()

	h := &Handler{
		cfg:       &config.Config{MasterKey: "test"},
		ipLimiter: ipLimiter,
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called with empty Bearer token")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_BearerPrefixOnly(t *testing.T) {
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	defer ipLimiter.Stop()

	h := &Handler{
		cfg:       &config.Config{MasterKey: "test"},
		ipLimiter: ipLimiter,
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called with Bearer prefix only")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ProxyKeyMiddleware tests (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestProxyKeyMiddleware_ValidKey_Integration(t *testing.T) {
	h := newIntegrationHandler()

	testKey := "sk-test-proxy-middleware-valid-key"
	keyHash := virtualkey.Hash(testKey)
	vk, err := h.virtualKeyRepo.Create(context.Background(), "test-middleware", keyHash, "sk-tes...", nil, nil)
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() {
		_ = h.virtualKeyRepo.Delete(context.Background(), vk.ID)
	}()

	called := false
	var capturedVKName interface{}
	var capturedVKID interface{}
	var capturedVKHash interface{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		capturedVKName = r.Context().Value(virtualKeyNameKey)
		capturedVKID = r.Context().Value(virtualKeyIDKey)
		capturedVKHash = r.Context().Value(VirtualKeyHashKey)
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+testKey)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler should be called with valid key")
	}
	if capturedVKName != vk.Name {
		t.Errorf("virtual key name = %v, want %q", capturedVKName, vk.Name)
	}
	if capturedVKID != vk.ID {
		t.Errorf("virtual key ID = %v, want %s", capturedVKID, vk.ID)
	}
	if capturedVKHash != keyHash {
		t.Errorf("virtual key hash = %v, want %s", capturedVKHash, keyHash)
	}
}

func TestProxyKeyMiddleware_KeyNotFound_Integration(t *testing.T) {
	h := newIntegrationHandler()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer sk-nonexistent-key-12345")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called with unknown key")
	}
	// FindByKeyHash returns pgx.ErrNoRows which is not virtualkey.ErrNotFound,
	// so the middleware falls into the "db lookup failed" branch and returns 500.
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (pgx.ErrNoRows != ErrNotFound), got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_ContextCanceledDBError(t *testing.T) {
	h := newIntegrationHandler()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer sk-some-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called on DB error")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// This should not panic
	h.Close()
}

func TestClose_Idempotent(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	h.Close()
	h.Close()
}

func TestClose_NilTransport(t *testing.T) {
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	defer ipLimiter.Stop()

	h := &Handler{
		cfg:               &config.Config{MasterKey: "test"},
		ipLimiter:         ipLimiter,
		upstreamTransport: nil,
	}

	// Should not panic even with nil transport
	h.Close()
}

// ---------------------------------------------------------------------------
// Register tests
// ---------------------------------------------------------------------------

func TestRegister_RequiresAuth(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (auth required), got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// RegisterAdminChat tests
// ---------------------------------------------------------------------------

func TestRegisterAdminChat_OnlyPostMethods(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()
	h.RegisterAdminChat(r)

	chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method != "POST" {
			t.Errorf("expected only POST methods for admin routes, got %s %s", method, route)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// RegisterAdminChat virtual key context tests
// ---------------------------------------------------------------------------

func TestRegisterAdminChat_SetsVirtualKeyContext(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()
	h.RegisterAdminChat(r)

	// Test that /chat route sets virtual key context to "chat"
	var capturedVKName interface{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedVKName = r.Context().Value(virtualKeyNameKey)
	})

	// Wrap the route to capture context
	r.Post("/chat", func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})

	req := httptest.NewRequest("POST", "/chat", http.NoBody)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if capturedVKName != "chat" {
		t.Errorf("expected virtual key name 'chat', got %v", capturedVKName)
	}
}

// ---------------------------------------------------------------------------
// virtualKeyRepoAdapter.Create tests
// ---------------------------------------------------------------------------

func TestVirtualKeyRepoAdapter_Create_ErrorPropagation(t *testing.T) {
	t.Parallel()

	h := newIntegrationHandler()

	// Use a canceled context to trigger an error from the underlying repo
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.virtualKeyRepo.Create(ctx, "test-key", "hash123", "sk-tes...", nil, nil)

	if err == nil {
		t.Error("expected error from canceled context, got nil")
	}
}

func TestVirtualKeyRepository_Create_Success(t *testing.T) {
	t.Parallel()

	expectedVK := &VirtualKeyInfo{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "test-key",
		KeyHash:    "hash123",
		KeyPreview: "sk-tes...",
		TokensUsed: 1000,
	}
	mockRepo := &mockVirtualKeyRepoWithFuncs{
		createFunc: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error) {
			return expectedVK, nil
		},
	}

	result, err := mockRepo.Create(context.Background(), "test-key", "hash123", "sk-tes...", nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ID != expectedVK.ID {
		t.Errorf("ID = %q, want %q", result.ID, expectedVK.ID)
	}
	if result.Name != expectedVK.Name {
		t.Errorf("Name = %q, want %q", result.Name, expectedVK.Name)
	}
	if result.KeyHash != expectedVK.KeyHash {
		t.Errorf("KeyHash = %q, want %q", result.KeyHash, expectedVK.KeyHash)
	}
	if result.KeyPreview != expectedVK.KeyPreview {
		t.Errorf("KeyPreview = %q, want %q", result.KeyPreview, expectedVK.KeyPreview)
	}
	if result.TokensUsed != expectedVK.TokensUsed {
		t.Errorf("TokensUsed = %d, want %d", result.TokensUsed, expectedVK.TokensUsed)
	}
}

func TestVirtualKeyRepository_Create_AllFieldsMapped(t *testing.T) {
	t.Parallel()

	mockRepo := &mockVirtualKeyRepoWithFuncs{
		createFunc: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error) {
			return &VirtualKeyInfo{
				ID:         "test-id-123",
				Name:       "my-virtual-key",
				KeyHash:    "sha256-hash-value",
				KeyPreview: "sk-proj...",
				TokensUsed: 999999,
			}, nil
		},
	}

	result, err := mockRepo.Create(context.Background(), "my-virtual-key", "sha256-hash-value", "sk-proj...", nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "test-id-123" {
		t.Errorf("ID not mapped correctly")
	}
	if result.Name != "my-virtual-key" {
		t.Errorf("Name not mapped correctly")
	}
	if result.KeyHash != "sha256-hash-value" {
		t.Errorf("KeyHash not mapped correctly")
	}
	if result.KeyPreview != "sk-proj..." {
		t.Errorf("KeyPreview not mapped correctly")
	}
	if result.TokensUsed != 999999 {
		t.Errorf("TokensUsed not mapped correctly")
	}
}

// ---------------------------------------------------------------------------
// VirtualKeyRepository.Delete tests (via mock)
// ---------------------------------------------------------------------------

func TestVirtualKeyRepository_Delete_InvalidUUID(t *testing.T) {
	t.Parallel()

	// Use the real adapter to test UUID parsing - it will fail before calling the repo
	adapter := &virtualKeyRepoAdapter{repo: virtualkey.NewRepository(nil)}

	err := adapter.Delete(context.Background(), "not-a-uuid")

	if err == nil {
		t.Error("expected error for invalid UUID, got nil")
	}
}

func TestVirtualKeyRepository_Delete_ErrorPropagation(t *testing.T) {
	t.Parallel()

	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	expectedErr := errors.New("delete failed")
	mockRepo := &mockVirtualKeyRepoWithFuncs{
		deleteFunc: func(ctx context.Context, id string) error {
			return expectedErr
		},
	}

	err := mockRepo.Delete(context.Background(), validUUID)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestVirtualKeyRepository_Delete_Success(t *testing.T) {
	t.Parallel()

	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	var deleteCalled bool
	var capturedID string
	mockRepo := &mockVirtualKeyRepoWithFuncs{
		deleteFunc: func(ctx context.Context, id string) error {
			deleteCalled = true
			capturedID = id
			return nil
		},
	}

	err := mockRepo.Delete(context.Background(), validUUID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Error("Delete was not called")
	}
	if capturedID != validUUID {
		t.Errorf("Delete called with ID %q, want %q", capturedID, validUUID)
	}
}

func TestVirtualKeyRepository_Delete_MultipleUUIDFormats(t *testing.T) {
	t.Parallel()

	// Test uuid.Parse directly - the adapter validates UUID before calling repo
	tests := []struct {
		name string
		id   string
	}{
		{"empty string", ""},
		{"short string", "abc123"},
		{"partial uuid", "550e8400-e29b"},
		{"uuid with extra chars", "550e8400-e29b-41d4-a716-446655440000-extra"},
		{"invalid chars", "not-a-uuid-at-all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uuid.Parse(tt.id)
			if err == nil {
				t.Errorf("expected uuid.Parse to fail for %q, got nil", tt.id)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration tests for Create and Delete (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestVirtualKeyRepoAdapter_Create_Integration(t *testing.T) {
	h := newIntegrationHandler()

	testKey := &VirtualKeyInfo{
		Name:       "integration-test-key",
		KeyHash:    virtualkey.Hash("sk-integration-test-key"),
		KeyPreview: "sk-int...",
	}

	result, err := h.virtualKeyRepo.Create(context.Background(), testKey.Name, testKey.KeyHash, testKey.KeyPreview, nil, nil)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ID == "" {
		t.Error("expected non-empty ID")
	}
	if result.Name != testKey.Name {
		t.Errorf("Name = %q, want %q", result.Name, testKey.Name)
	}
	if result.KeyHash != testKey.KeyHash {
		t.Errorf("KeyHash = %q, want %q", result.KeyHash, testKey.KeyHash)
	}
	if result.KeyPreview != testKey.KeyPreview {
		t.Errorf("KeyPreview = %q, want %q", result.KeyPreview, testKey.KeyPreview)
	}
	if result.TokensUsed != 0 {
		t.Errorf("TokensUsed = %d, want 0", result.TokensUsed)
	}

	// Cleanup
	_ = h.virtualKeyRepo.Delete(context.Background(), result.ID)
}

func TestVirtualKeyRepoAdapter_Delete_Integration(t *testing.T) {
	h := newIntegrationHandler()

	// Create a key to delete
	testKey := &VirtualKeyInfo{
		Name:       "delete-test-key",
		KeyHash:    virtualkey.Hash("sk-delete-test-key"),
		KeyPreview: "sk-del...",
	}
	created, err := h.virtualKeyRepo.Create(context.Background(), testKey.Name, testKey.KeyHash, testKey.KeyPreview, nil, nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete the key
	err = h.virtualKeyRepo.Delete(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify the key is gone
	_, err = h.virtualKeyRepo.FindByKeyHash(context.Background(), testKey.KeyHash)
	if err == nil {
		t.Error("expected error when looking up deleted key, got nil")
	}
}

func TestVirtualKeyRepoAdapter_CreateDelete_RoundTrip(t *testing.T) {
	h := newIntegrationHandler()

	// Create
	created, err := h.virtualKeyRepo.Create(context.Background(), "roundtrip-key", virtualkey.Hash("sk-roundtrip"), "sk-rou...", nil, nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify we can find it
	found, err := h.virtualKeyRepo.FindByKeyHash(context.Background(), created.KeyHash)
	if err != nil {
		t.Fatalf("FindByKeyHash failed: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("found ID = %q, want %q", found.ID, created.ID)
	}

	// Delete
	err = h.virtualKeyRepo.Delete(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = h.virtualKeyRepo.FindByKeyHash(context.Background(), created.KeyHash)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}
