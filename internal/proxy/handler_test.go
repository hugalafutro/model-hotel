package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

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
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
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
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

// stopUnitHandler stops background goroutines started by newUnitHandler.
func stopUnitHandler(h *Handler) {
	h.rateLimiter.Stop()
	h.ipLimiter.Stop()
}

func containsMethod(methods []string, method string) bool {
	for _, m := range methods {
		if m == method {
			return true
		}
	}
	return false
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
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
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
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
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
	if h.upstreamTransport.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", h.upstreamTransport.IdleConnTimeout)
	}
}

// ---------------------------------------------------------------------------
// ProxyKeyMiddleware tests (pure unit — no DB required)
// ---------------------------------------------------------------------------

func TestProxyKeyMiddleware_EmptyBearerToken(t *testing.T) {
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
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

func TestProxyKeyMiddleware_BearerPrefixOnly(t *testing.T) {
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
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

	req := httptest.NewRequest("POST", "/chat/completions", nil)
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
	if h == nil {
		t.Skip("database not available")
	}

	testKey := "sk-test-proxy-middleware-valid-key"
	keyHash := virtualkey.Hash(testKey)
	vk, err := h.virtualKeyRepo.Create(context.Background(), "test-middleware", keyHash, "sk-tes...")
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

	req := httptest.NewRequest("POST", "/chat/completions", nil)
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
	if h == nil {
		t.Skip("database not available")
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
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
	if h == nil {
		t.Skip("database not available")
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("POST", "/chat/completions", nil).WithContext(ctx)
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
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
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

func TestRegister(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()

	// This should not panic
	h.Register(r)
}

func TestRegister_SetsUpRoutes(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()
	h.Register(r)

	routes := make(map[string][]string)
	chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routes[route] = append(routes[route], method)
		return nil
	})

	if !containsMethod(routes["/models"], "GET") {
		t.Error("expected GET /models route to be registered")
	}
	if !containsMethod(routes["/chat/completions"], "POST") {
		t.Error("expected POST /chat/completions route to be registered")
	}
}

func TestRegister_RequiresAuth(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("GET", "/models", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (auth required), got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// RegisterAdminChat tests
// ---------------------------------------------------------------------------

func TestRegisterAdminChat(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()

	// This should not panic
	h.RegisterAdminChat(r)
}

func TestRegisterAdminChat_SetsUpRoutes(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	r := chi.NewRouter()
	h.RegisterAdminChat(r)

	routes := make(map[string][]string)
	chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routes[route] = append(routes[route], method)
		return nil
	})

	tests := []struct {
		method string
		route  string
	}{
		{"POST", "/chat"},
		{"POST", "/arena"},
		{"POST", "/completions"},
	}
	for _, tt := range tests {
		if !containsMethod(routes[tt.route], tt.method) {
			t.Errorf("expected %s %s route to be registered", tt.method, tt.route)
		}
	}
}

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

	req := httptest.NewRequest("POST", "/chat", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if capturedVKName != "chat" {
		t.Errorf("expected virtual key name 'chat', got %v", capturedVKName)
	}
}
