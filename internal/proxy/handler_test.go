package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// mock implementations for testing
type mockVirtualKeyRepo struct {
	findByKeyHashFunc func(ctx context.Context, keyHash string) (*virtualkey.VirtualKey, error)
	touchLastUsedFunc func(ctx context.Context, keyHash string) error
}

func (m *mockVirtualKeyRepo) FindByKeyHash(ctx context.Context, keyHash string) (*virtualkey.VirtualKey, error) {
	if m.findByKeyHashFunc != nil {
		return m.findByKeyHashFunc(ctx, keyHash)
	}
	return nil, virtualkey.ErrNotFound
}

func (m *mockVirtualKeyRepo) TouchLastUsed(ctx context.Context, keyHash string) error {
	if m.touchLastUsedFunc != nil {
		return m.touchLastUsedFunc(ctx, keyHash)
	}
	return nil
}

type mockProviderRepo struct {
	getByNameFunc func(ctx context.Context, name string) (*provider.Provider, error)
	getByIDsFunc  func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*provider.Provider, error)
}

func (m *mockProviderRepo) GetByName(ctx context.Context, name string) (*provider.Provider, error) {
	if m.getByNameFunc != nil {
		return m.getByNameFunc(ctx, name)
	}
	return nil, errors.New("not found")
}

func (m *mockProviderRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*provider.Provider, error) {
	if m.getByIDsFunc != nil {
		return m.getByIDsFunc(ctx, ids)
	}
	return make(map[uuid.UUID]*provider.Provider), nil
}

type mockModelRepo struct {
	listEnabledFunc            func(ctx context.Context) ([]*model.Model, error)
	getByIDsFunc               func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
	getByProviderAndModelIDFunc func(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error)
}

func (m *mockModelRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	if m.listEnabledFunc != nil {
		return m.listEnabledFunc(ctx)
	}
	return []*model.Model{}, nil
}

func (m *mockModelRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	if m.getByIDsFunc != nil {
		return m.getByIDsFunc(ctx, ids)
	}
	return make(map[uuid.UUID]*model.Model), nil
}

func (m *mockModelRepo) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error) {
	if m.getByProviderAndModelIDFunc != nil {
		return m.getByProviderAndModelIDFunc(ctx, providerID, modelID)
	}
	return nil, errors.New("not found")
}

type mockFailoverRepo struct {
	getByModelFunc func(ctx context.Context, modelID string) (*failover.FailoverGroup, error)
	getEnabledFunc func(ctx context.Context) ([]*failover.FailoverGroup, error)
}

func (m *mockFailoverRepo) GetByModel(ctx context.Context, modelID string) (*failover.FailoverGroup, error) {
	if m.getByModelFunc != nil {
		return m.getByModelFunc(ctx, modelID)
	}
	return nil, errors.New("not found")
}

func (m *mockFailoverRepo) GetEnabled(ctx context.Context) ([]*failover.FailoverGroup, error) {
	if m.getEnabledFunc != nil {
		return m.getEnabledFunc(ctx)
	}
	return []*failover.FailoverGroup{}, nil
}

type mockSettingsRepo struct {
	getBoolFunc func(ctx context.Context, key string, defaultValue bool) bool
}

func (m *mockSettingsRepo) GetBool(ctx context.Context, key string, defaultValue bool) bool {
	if m.getBoolFunc != nil {
		return m.getBoolFunc(ctx, key, defaultValue)
	}
	return defaultValue
}

func newTestHandler() *Handler {
	cfg := &config.Config{
		MasterKey:         "test-master-key",
		RateLimitEnabled:  false,
		DebugProxyHeaders: false,
	}

	return &Handler{
		cfg:            cfg,
		providerRepo:   &mockProviderRepo{},
		modelRepo:      &mockModelRepo{},
		virtualKeyRepo: &mockVirtualKeyRepo{},
		failoverRepo:   &mockFailoverRepo{},
		settingsRepo:   &mockSettingsRepo{},
		rateLimiter:    ratelimit.NewLimiter(nil),
		ipLimiter:      ratelimit.NewIPLimiter(30, 60),
		upstreamTransport: &http.Transport{
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// ProxyKeyMiddleware tests
// ---------------------------------------------------------------------------

func TestProxyKeyMiddleware_MissingHeader(t *testing.T) {
	h := newTestHandler()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called without auth header")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_InvalidScheme(t *testing.T) {
	h := newTestHandler()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called with Basic auth")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_EmptyBearerToken(t *testing.T) {
	h := newTestHandler()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
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

func TestProxyKeyMiddleware_KeyNotFound(t *testing.T) {
	h := newTestHandler()
	h.virtualKeyRepo = &mockVirtualKeyRepo{
		findByKeyHashFunc: func(ctx context.Context, keyHash string) (*virtualkey.VirtualKey, error) {
			return nil, virtualkey.ErrNotFound
		},
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called when key not found")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_DBError(t *testing.T) {
	h := newTestHandler()
	h.virtualKeyRepo = &mockVirtualKeyRepo{
		findByKeyHashFunc: func(ctx context.Context, keyHash string) (*virtualkey.VirtualKey, error) {
			return nil, errors.New("database error")
		},
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called when DB error occurs")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_Success(t *testing.T) {
	h := newTestHandler()
	testVK := &virtualkey.VirtualKey{
		ID:          uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Name:        "test-key",
		KeyHash:     "abc123",
		KeyPreview:  "sk-abc...",
		TokensUsed:  100,
		LastUsedAt:  time.Now(),
		CreatedAt:   time.Now(),
	}
	h.virtualKeyRepo = &mockVirtualKeyRepo{
		findByKeyHashFunc: func(ctx context.Context, keyHash string) (*virtualkey.VirtualKey, error) {
			return testVK, nil
		},
	}

	var capturedCtx context.Context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-test-key-123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Check that context values are set correctly
	if capturedCtx == nil {
		t.Fatal("context should not be nil")
	}

	vkName := capturedCtx.Value(virtualKeyNameKey)
	if vkName != "test-key" {
		t.Errorf("virtual key name in context = %v, want %s", vkName, "test-key")
	}

	vkID := capturedCtx.Value(virtualKeyIDKey)
	if vkID != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("virtual key ID in context = %v, want %s", vkID, "00000000-0000-0000-0000-000000000001")
	}

	vkHash := capturedCtx.Value(virtualkey.VirtualKeyHashKey)
	if vkHash != "abc123" {
		t.Errorf("virtual key hash in context = %v, want %s", vkHash, "abc123")
	}
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	h := newTestHandler()
	// This should not panic
	h.Close()
}

// ---------------------------------------------------------------------------
// Register tests
// ---------------------------------------------------------------------------

func TestRegister(t *testing.T) {
	h := newTestHandler()
	router := http.NewServeMux()
	
	// This should not panic
	h.Register(router)
}

// ---------------------------------------------------------------------------
// RegisterAdminChat tests
// ---------------------------------------------------------------------------

func TestRegisterAdminChat(t *testing.T) {
	h := newTestHandler()
	router := http.NewServeMux()
	
	// This should not panic
	h.RegisterAdminChat(router)
}