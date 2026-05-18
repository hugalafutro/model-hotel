package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

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

func (m *coverageMockVirtualKeyRepo) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error) {
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
	} else if msg["message"] != "Invalid virtual key" {
		t.Errorf("expected error message 'Invalid virtual key', got %v", msg["message"])
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

	// Verify response is JSON with expected message
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
	if msg, ok := resp["error"].(map[string]interface{}); !ok {
		t.Error("response should have error object")
	} else if msg["message"] != "Internal error" {
		t.Errorf("expected error message 'Internal error', got %v", msg["message"])
	}
}

// TestParseAccumulatedError_Nil tests that parseAccumulatedError with nil
// error returns nil.
func TestParseAccumulatedError_Nil(t *testing.T) {
	t.Helper()
	result := parseAccumulatedError(nil)
	if result != "" {
		t.Errorf("expected empty string for nil input, got %q", result)
	}
}

// TestParseAccumulatedError_NonAccumulated tests that parseAccumulatedError
// with a regular error (not from accumulation) handles various inputs.
func TestParseAccumulatedError_NonAccumulated(t *testing.T) {
	t.Helper()
	// Regular error that doesn't match OpenAI or Anthropic error formats
	data := []byte("some random error message")
	result := parseAccumulatedError(data)
	// Should return empty string since it doesn't start with {
	if result != "" {
		t.Errorf("expected empty string for non-JSON error, got %q", result)
	}

	// Test with JSON that doesn't match error formats - returns raw JSON
	jsonData := []byte(`{"foo":"bar"}`)
	result = parseAccumulatedError(jsonData)
	// Function returns raw bytes if they start with { (heuristic for truncated JSON)
	if result != `{"foo":"bar"}` {
		t.Errorf("expected raw JSON string, got %q", result)
	}
}

// TestWaitForInsert_Timeout tests that WaitForInsert returns after timeout
// when the insert goroutine never completes.
func TestWaitForInsert_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	t.Helper()
	h := &Handler{}

	// Create a requestLogData with a WaitGroup that never gets Done()
	logData := &requestLogData{
		id:             "test-timeout-id",
		modelID:        "test-model",
		streaming:      false,
		virtualKeyName: "test-key",
		state:          "pending",
	}
	// Add 1 to the WaitGroup but never call Done()
	logData.insertWg.Add(1)

	start := time.Now()
	h.WaitForInsert(logData)
	elapsed := time.Since(start)

	// Should return within ~6 seconds (5s timeout + small margin)
	if elapsed < 5*time.Second {
		t.Errorf("WaitForInsert returned too early: %v (expected ~5s timeout)", elapsed)
	}
	if elapsed > 7*time.Second {
		t.Errorf("WaitForInsert took too long: %v (expected ~5s timeout)", elapsed)
	}
}

// TestWaitForInsert_Completes tests that WaitForInsert returns immediately
// when the insert completes.
func TestWaitForInsert_Completes(t *testing.T) {
	t.Helper()
	h := &Handler{}

	logData := &requestLogData{
		id:             "test-complete-id",
		modelID:        "test-model",
		streaming:      false,
		virtualKeyName: "test-key",
		state:          "pending",
	}
	logData.insertWg.Add(1)

	// Call Done() in a goroutine after a brief delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		logData.insertWg.Done()
	}()

	start := time.Now()
	h.WaitForInsert(logData)
	elapsed := time.Since(start)

	// Should return quickly (within ~100ms, not the 5s timeout)
	if elapsed > 100*time.Millisecond {
		t.Errorf("WaitForInsert took too long: %v (expected ~10ms)", elapsed)
	}
}

// TestListModels_DBError tests that when modelRepo.ListEnabled returns error,
// ListModels returns 500 with JSON error.
func TestListModels_DBError(t *testing.T) {
	t.Helper()
	dbErr := errors.New("database query failed")
	mockRepo := &coverageMockModelRepo{
		listEnabledFunc: func(ctx context.Context) ([]*model.Model, error) {
			return nil, dbErr
		},
	}
	h := &Handler{
		modelRepo: mockRepo,
	}

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	// Verify response is JSON with expected message
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
	if msg, ok := resp["error"].(map[string]interface{}); !ok {
		t.Error("response should have error object")
	} else if msg["message"] != "failed to list models" {
		t.Errorf("expected error message 'failed to list models', got %v", msg["message"])
	}
}
