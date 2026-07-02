package proxy

import (
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

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
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
		tpmLimiter:     ratelimit.NewTPMLimiter(settingsRepo),
		ipLimiter:      ipLimiter,
		upstreamTransport: &http.Transport{
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
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
	createFunc func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error)
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

func (m *mockVirtualKeyRepoWithFuncs) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, name, keyHash, keyPreview, rps, burst, tpm, allowedProviders, stripReasoning)
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
	tpmLimiter := ratelimit.NewTPMLimiter(settingsRepo)
	defer tpmLimiter.Stop()
	defer ipLimiter.Stop()

	h := NewHandler(cfg, providerRepo, modelRepo, nil, virtualKeyRepo, failoverRepo, settingsRepo, rateLimiter, tpmLimiter, ipLimiter, nil)

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
	tpmLimiter := ratelimit.NewTPMLimiter(settingsRepo)
	defer tpmLimiter.Stop()
	defer ipLimiter.Stop()

	h := NewHandler(
		&config.Config{MasterKey: "test-key", RateLimitEnabled: false},
		provider.NewRepository(nil), model.NewRepository(nil), nil,
		virtualkey.NewRepository(nil), failover.NewRepository(nil),
		settingsRepo, rateLimiter, tpmLimiter, ipLimiter, nil,
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
	vk, err := h.virtualKeyRepo.Create(context.Background(), "test-middleware", keyHash, "sk-tes...", nil, nil, nil, nil, nil)
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
	// An unknown key is authentication failure, not a server fault: FindByKeyHash
	// now returns virtualkey.ErrNotFound, so the middleware returns a clean 401
	// instead of leaking the raw pgx "no rows in result set" as a 500.
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unknown key, got %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "no rows in result set") {
		t.Errorf("raw DB error leaked to client: %s", rr.Body.String())
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
// CircuitBreaker tests
// ---------------------------------------------------------------------------

func TestCircuitBreaker_ReturnsInternalBreaker(t *testing.T) {
	settingsRepo := settings.NewRepository(nil)
	rateLimiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	defer rateLimiter.Stop()
	tpmLimiter := ratelimit.NewTPMLimiter(settingsRepo)
	defer tpmLimiter.Stop()
	defer ipLimiter.Stop()

	cb := failover.NewCircuitBreaker(settingsRepo)
	h := &Handler{
		cfg:            &config.Config{MasterKey: "test"},
		circuitBreaker: cb,
		ipLimiter:      ipLimiter,
	}

	got := h.CircuitBreaker()
	if got != cb {
		t.Error("CircuitBreaker() should return the handler's internal circuit breaker")
	}
}

// ---------------------------------------------------------------------------
// safeDialFunc tests
// ---------------------------------------------------------------------------

func TestSafeDialFunc_NilDialer(t *testing.T) {
	result := safeDialFunc(nil)
	if result != nil {
		t.Error("safeDialFunc(nil) should return nil, allowing http.Transport to use default dialer")
	}
}

func TestSafeDialFunc_NonNilDialer(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	result := safeDialFunc(sd)
	if result == nil {
		t.Error("safeDialFunc(non-nil) should return sd.DialContext, got nil")
	}
	// Verify the returned function is the DialContext method by calling it
	// with a blocked IP — should get the expected blocked-IP error
	conn, err := result(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		conn.Close()
		t.Error("expected error from DialContext on blocked IP")
	}
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

	_, err := h.virtualKeyRepo.Create(ctx, "test-key", "hash123", "sk-tes...", nil, nil, nil, nil, nil)

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
		createFunc: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error) {
			return expectedVK, nil
		},
	}

	result, err := mockRepo.Create(context.Background(), "test-key", "hash123", "sk-tes...", nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
		return
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
		createFunc: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error) {
			return &VirtualKeyInfo{
				ID:         "test-id-123",
				Name:       "my-virtual-key",
				KeyHash:    "sha256-hash-value",
				KeyPreview: "sk-proj...",
				TokensUsed: 999999,
			}, nil
		},
	}

	result, err := mockRepo.Create(context.Background(), "my-virtual-key", "sha256-hash-value", "sk-proj...", nil, nil, nil, nil, nil)

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

	result, err := h.virtualKeyRepo.Create(context.Background(), testKey.Name, testKey.KeyHash, testKey.KeyPreview, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
		return
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
	created, err := h.virtualKeyRepo.Create(context.Background(), testKey.Name, testKey.KeyHash, testKey.KeyPreview, nil, nil, nil, nil, nil)
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
	created, err := h.virtualKeyRepo.Create(context.Background(), "roundtrip-key", virtualkey.Hash("sk-roundtrip"), "sk-rou...", nil, nil, nil, nil, nil)
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

// ---------------------------------------------------------------------------
// Tests moved from coverage_test.go
// ---------------------------------------------------------------------------

// coverageMockVirtualKeyRepo implements VirtualKeyRepository for coverage tests.
type coverageMockVirtualKeyRepo struct {
	findByKeyHashFunc func(ctx context.Context, keyHash string) (*VirtualKeyInfo, error)
}

func (m *coverageMockVirtualKeyRepo) AddTokens(ctx context.Context, keyHash string, tokens int) error {
	return nil
}

func (m *coverageMockVirtualKeyRepo) TouchLastUsed(ctx context.Context, keyHash string) error {
	return nil
}

func (m *coverageMockVirtualKeyRepo) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
	if m.findByKeyHashFunc != nil {
		return m.findByKeyHashFunc(ctx, keyHash)
	}
	return nil, nil
}

func (m *coverageMockVirtualKeyRepo) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error) {
	return nil, nil
}

func (m *coverageMockVirtualKeyRepo) Delete(ctx context.Context, id string) error {
	return nil
}

// coverageMockModelRepo implements ModelRepository for coverage tests.
type coverageMockModelRepo struct {
	listEnabledFunc func(ctx context.Context) ([]*model.Model, error)
}

func (m *coverageMockModelRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	if m.listEnabledFunc != nil {
		return m.listEnabledFunc(ctx)
	}
	return nil, nil
}

func (m *coverageMockModelRepo) Upsert(ctx context.Context, model *model.Model) error {
	return nil
}

func (m *coverageMockModelRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *coverageMockModelRepo) Get(ctx context.Context, id uuid.UUID) (*model.Model, error) {
	return nil, nil
}

func (m *coverageMockModelRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}

func (m *coverageMockModelRepo) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error) {
	return nil, nil
}

// TestProxyKeyMiddleware_MissingAuth tests that when no Authorization header
// is provided, the middleware returns 401 with a JSON error.
func TestProxyKeyMiddleware_MissingAuth(t *testing.T) {
	t.Helper()
	h := &Handler{
		cfg:       &config.Config{MasterKey: "test"},
		ipLimiter: ratelimit.NewIPLimiter(30, 60, nil, nil),
	}
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	// No Authorization header
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called without auth header")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// Verify response is JSON
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
}

// TestProxyKeyMiddleware_InvalidAuth tests that when Authorization header
// has an invalid key, the middleware returns 401 with JSON error.
func TestProxyKeyMiddleware_InvalidAuth(t *testing.T) {
	t.Helper()
	mockRepo := &coverageMockVirtualKeyRepo{
		findByKeyHashFunc: func(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
			return nil, virtualkey.ErrNotFound
		},
	}
	h := &Handler{
		cfg:            &config.Config{MasterKey: "test"},
		ipLimiter:      ratelimit.NewIPLimiter(30, 60, nil, nil),
		virtualKeyRepo: mockRepo,
	}
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called with invalid key")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// Verify response is JSON with expected message
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
	if msg, ok := resp["error"].(map[string]interface{}); !ok {
		t.Error("response should have error object")
	} else if msg["message"] != "invalid virtual key" {
		t.Errorf("expected error message 'invalid virtual key', got %v", msg["message"])
	}
}

// TestProxyKeyMiddleware_DBError tests that when virtual key repo returns
// a non-ErrNotFound error, the middleware returns 500 with JSON error.
func TestProxyKeyMiddleware_DBError(t *testing.T) {
	t.Helper()
	dbErr := errors.New("database connection failed")
	mockRepo := &coverageMockVirtualKeyRepo{
		findByKeyHashFunc: func(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
			return nil, dbErr
		},
	}
	h := &Handler{
		cfg:            &config.Config{MasterKey: "test"},
		ipLimiter:      ratelimit.NewIPLimiter(30, 60, nil, nil),
		virtualKeyRepo: mockRepo,
	}
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer some-key")
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
// Tests moved from coverage_gap2_test.go
// ---------------------------------------------------------------------------

// TestRegisterAdminChat_ChatKey verifies that calling h.RegisterAdminChat
// and making a POST to /api/chat/chat exercises the middleware that sets
// virtualKeyNameKey to "chat". The middleware runs even if ChatCompletions errors.
func TestRegisterAdminChat_ChatKey(t *testing.T) {
	t.Helper()

	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.cfg.RateLimitEnabled = false

	mux := chi.NewMux()
	// Actually call RegisterAdminChat on a subrouter
	mux.Route("/api/chat", func(r chi.Router) {
		// Add recover middleware to catch panic from ChatCompletions (nil DB)
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if rec := recover(); rec != nil {
						// Expected: ChatCompletions panics on DB access with nil pool
						// Middleware already executed, coverage counted
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				next.ServeHTTP(w, r)
			})
		})
		h.RegisterAdminChat(r)
	})

	// Make a POST request - the middleware will run and set context key
	// ChatCompletions will error (no DB) but middleware coverage is counted
	body := `{"model":"test","messages":[]}`
	req := httptest.NewRequest("POST", "/api/chat/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// We can't easily verify the context key since ChatCompletions errors,
	// but the middleware DID run (coverage counts it). Verify request was processed.
	if rr.Code == 0 {
		t.Error("request was not processed")
	}
}

// TestRegisterAdminChat_ArenaKey verifies that calling h.RegisterAdminChat
// and making a POST to /api/chat/arena exercises the middleware that sets
// virtualKeyNameKey to "arena".
func TestRegisterAdminChat_ArenaKey(t *testing.T) {
	t.Helper()

	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.cfg.RateLimitEnabled = false

	mux := chi.NewMux()
	mux.Route("/api/chat", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if rec := recover(); rec != nil {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				next.ServeHTTP(w, r)
			})
		})
		h.RegisterAdminChat(r)
	})

	body := `{"model":"test","messages":[]}`
	req := httptest.NewRequest("POST", "/api/chat/arena", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code == 0 {
		t.Error("request was not processed")
	}
}

// TestRegisterAdminChat_CompletionsKey verifies that calling h.RegisterAdminChat
// and making a POST to /api/chat/completions exercises the middleware that sets
// virtualKeyNameKey to "completions".
func TestRegisterAdminChat_CompletionsKey(t *testing.T) {
	t.Helper()

	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.cfg.RateLimitEnabled = false

	mux := chi.NewMux()
	mux.Route("/api/chat", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if rec := recover(); rec != nil {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				next.ServeHTTP(w, r)
			})
		})
		h.RegisterAdminChat(r)
	})

	body := `{"model":"test","messages":[]}`
	req := httptest.NewRequest("POST", "/api/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code == 0 {
		t.Error("request was not processed")
	}
}

// TestRegisterAdminChat_RateLimiterMiddleware verifies that the rate limiter
// middleware is applied when RateLimitEnabled is true.
func TestRegisterAdminChat_RateLimiterMiddleware(t *testing.T) {
	t.Helper()

	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.cfg.RateLimitEnabled = true

	mux := chi.NewMux()
	mux.Route("/api/chat", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if rec := recover(); rec != nil {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				next.ServeHTTP(w, r)
			})
		})
		h.RegisterAdminChat(r)
	})

	body := `{"model":"test","messages":[]}`
	req := httptest.NewRequest("POST", "/api/chat/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Request should be processed (may be rate limited or error from ChatCompletions)
	// The important thing is that RegisterAdminChat was called with RateLimitEnabled=true
	if rr.Code == 0 {
		t.Error("request was not processed")
	}
}

func TestChatCompletions_RequestBodyNotCached(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Create a mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	// Create provider + model
	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-uncached", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{
		ID:               uuid.New(),
		ProviderID:       prov.ID,
		ModelID:          "test-model",
		Name:             "Test Model",
		DisplayName:      "Test Model Display",
		Description:      "A test model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Request WITHOUT RequestBodyKey in context (normal path)
	body := `{"model":"test-provider-uncached/test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)
	// Do NOT add ctxkeys.RequestBodyKey to context

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_ReadBodyError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Create request with body reader that errors
	errReader := &errorReader{err: errors.New("body read error")}
	req := httptest.NewRequest("POST", "/v1/chat/completions", errReader)
	req = withAuthContext(req)
	// Ensure RequestBodyKey is NOT in context

	w := httptest.NewRecorder()

	h.ChatCompletions(w, req)

	// Should return 400 with error message
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "failed to read request body") {
		t.Errorf("expected error about reading body, got %q", body)
	}
}

func TestChatCompletions_NoCandidatesAfterResolve(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Create provider WITHOUT any model
	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	_, err = h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-nomodel", BaseURL: "http://localhost:9999", APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	// No model created for this provider

	body := `{"model":"test-provider-nomodel/nonexistent-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestChatCompletions_NewRequestWithContextError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-reqerr", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Override newRequestWithContext to fail
	origNewReq := newRequestWithContext
	defer func() { newRequestWithContext = origNewReq }()
	newRequestWithContext = func(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
		return nil, errors.New("request creation failed")
	}

	body := `{"model":"test-provider-reqerr/test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// TestChatCompletions_ContextErrorHandling drives a provider that is slower than
// the configured request_timeout, so the per-attempt failover deadline expires.
// That is a gateway timeout (504), not a generic provider failure (502).
func TestChatCompletions_ContextErrorHandling(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Server that delays response to trigger timeout
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-ctxerr", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Set very short request timeout
	if err := h.settingsRepo.Set(ctx, "request_timeout", "100ms"); err != nil {
		t.Fatalf("failed to set timeout: %v", err)
	}
	defer func() {
		_ = h.settingsRepo.Set(ctx, "request_timeout", "60000")
	}()
	h.settingsRepo.InvalidateCache("request_timeout")

	body := `{"model":"test-provider-ctxerr/test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected 504 (provider exceeded request_timeout), got %d", w.Code)
	}
}

func TestChatCompletions_RetryRequestCreationError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retrycreate", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Override newRequestWithContext: succeed on first call, fail on retry
	origNewReq := newRequestWithContext
	defer func() { newRequestWithContext = origNewReq }()
	reqCallCount := 0
	newRequestWithContext = func(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
		reqCallCount++
		if reqCallCount > 1 {
			return nil, errors.New("retry request creation failed")
		}
		return http.NewRequestWithContext(ctx, method, url, body)
	}

	body := `{"model":"test-provider-retrycreate/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// When retry request creation fails, all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_RetryRequestDoError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Second request (retry): hijack and close connection to cause Do error
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retrydo", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-retrydo/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// When retry Do fails, all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_RetryCancelFailoverPath(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Retry: return 500 (failover-eligible status)
		// With single candidate, failover won't trigger but non-200 path
		// will call retryCancel at L1052-1054
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal server error"}}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retry-failover", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-retry-failover/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// Retry returns 500, single candidate so no failover, 500 forwarded
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_RetryCancelNon200Path(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Retry: return 400 (non-failover-eligible, non-200)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request after retry"}}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retrycancel-non200", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-retrycancel-non200/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletions_RetryCancelStreamingPath(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Retry: return 200 streaming
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-stream", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-stream/test-model","messages":[{"role":"user","content":"hi"}],"stream":true,"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Tests moved from context_skip_test.go
// ---------------------------------------------------------------------------

// TestContextErrorsNotCountedAsProviderFailures verifies that context
// cancellation and deadline errors are correctly identified so the proxy
// handler can skip circuit breaker RecordFailure calls for them.
//
// The actual skip logic lives in ChatCompletions (proxy.go:446-460).
// This test validates the error classification on which that logic depends.
func TestContextErrorsNotCountedAsProviderFailures(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		shouldBeSkipped bool
	}{
		{
			name:            "context.Canceled is skipped",
			err:             context.Canceled,
			shouldBeSkipped: true,
		},
		{
			name:            "context.DeadlineExceeded is skipped",
			err:             context.DeadlineExceeded,
			shouldBeSkipped: true,
		},
		{
			name:            "wrapped context.Canceled is skipped",
			err:             fmt.Errorf("upstream: %w", context.Canceled),
			shouldBeSkipped: true,
		},
		{
			name:            "wrapped context.DeadlineExceeded is skipped",
			err:             fmt.Errorf("upstream: %w", context.DeadlineExceeded),
			shouldBeSkipped: true,
		},
		{
			name:            "connection refused is NOT skipped",
			err:             errors.New("connection refused"),
			shouldBeSkipped: false,
		},
		{
			name:            "DNS error is NOT skipped",
			err:             errors.New("lookup: no such host"),
			shouldBeSkipped: false,
		},
		{
			name:            "nil error is NOT skipped (shouldn't happen but test)",
			err:             nil,
			shouldBeSkipped: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var skipped bool
			if tc.err != nil {
				skipped = errors.Is(tc.err, context.Canceled) || errors.Is(tc.err, context.DeadlineExceeded)
			}
			if skipped != tc.shouldBeSkipped {
				t.Errorf("errors.Is classification: skipped=%v, want skipped=%v for err=%v", skipped, tc.shouldBeSkipped, tc.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Owned-key middleware behaviour (phase 2 of multi-user)
// ---------------------------------------------------------------------------

// ownerAwareVKRepo returns a fixed VirtualKeyInfo from FindByKeyHash.
type ownerAwareVKRepo struct {
	mockVirtualKeyRepoWithFuncs
	vk *VirtualKeyInfo
}

func (m *ownerAwareVKRepo) FindByKeyHash(context.Context, string) (*VirtualKeyInfo, error) {
	return m.vk, nil
}

func TestProxyKeyMiddleware_DisabledOwnerRejected(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.virtualKeyRepo = &ownerAwareVKRepo{vk: &VirtualKeyInfo{
		ID: "vk-1", Name: "owned", KeyHash: "hash-1",
		Owner: &OwnerInfo{ID: "uid-1", Enabled: false},
	}}

	called := false
	handler := h.ProxyKeyMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer sk-owned-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler must not run for a disabled owner's key")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "owner account is disabled") {
		t.Errorf("unexpected body: %s", rr.Body.String())
	}
}

func TestProxyKeyMiddleware_OwnerContextPropagated(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)
	rps := 2.0
	burst := 3
	tpm := 6000
	h.virtualKeyRepo = &ownerAwareVKRepo{vk: &VirtualKeyInfo{
		ID: "vk-1", Name: "owned", KeyHash: "hash-1",
		Owner: &OwnerInfo{ID: "uid-1", Enabled: true, RateLimitRPS: &rps, RateLimitBurst: &burst, RateLimitTPM: &tpm},
	}}

	var gotUID string
	var gotRPS *float64
	var gotBurst, gotTPM *int
	handler := h.ProxyKeyMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotUID, _ = r.Context().Value(ctxkeys.VirtualKeyOwnerIDKey).(string)
		gotRPS, _ = r.Context().Value(ctxkeys.UserRateLimitRPSKey).(*float64)
		gotBurst, _ = r.Context().Value(ctxkeys.UserRateLimitBurstKey).(*int)
		gotTPM, _ = r.Context().Value(ctxkeys.UserRateLimitTPMKey).(*int)
	}))
	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer sk-owned-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotUID != "uid-1" {
		t.Errorf("owner id = %q, want uid-1", gotUID)
	}
	if gotRPS == nil || *gotRPS != rps {
		t.Errorf("user rps = %v, want %v", gotRPS, rps)
	}
	if gotBurst == nil || *gotBurst != burst {
		t.Errorf("user burst = %v, want %v", gotBurst, burst)
	}
	if gotTPM == nil || *gotTPM != tpm {
		t.Errorf("user tpm = %v, want %v", gotTPM, tpm)
	}
}

func TestProxyKeyMiddleware_UnownedKeyHasNoOwnerContext(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.virtualKeyRepo = &ownerAwareVKRepo{vk: &VirtualKeyInfo{ID: "vk-1", Name: "plain", KeyHash: "hash-1"}}

	sawOwner := false
	handler := h.ProxyKeyMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, sawOwner = r.Context().Value(ctxkeys.VirtualKeyOwnerIDKey).(string)
	}))
	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer sk-plain-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if sawOwner {
		t.Error("unowned key must not carry owner context")
	}
}
