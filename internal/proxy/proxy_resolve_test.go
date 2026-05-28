package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Use testDB from proxy_test.go

func TestResolveHotelModel_EmptyFailoverGroup(t *testing.T) {

	env := newTestProxyHandler(t)
	handler := env.Handler
	keyHash := env.KeyHash
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create an empty failover group
	modelName := "empty-failover-model"
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{},
		map[string]bool{}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create empty failover group: %v", err)
	}

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestResolveHotelModel_DisabledFailoverGroup tests the case where a failover group is disabled

func TestResolveHotelModel_DisabledFailoverGroup(t *testing.T) {

	env := newTestProxyHandler(t)
	handler := env.Handler
	modelID := env.ModelID
	keyHash := env.KeyHash
	modelName := env.ModelName
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create a failover group with disabled flag
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Disable the failover group
	fg, err := failoverRepo.GetByModel(context.Background(), modelName)
	if err != nil {
		t.Fatalf("failed to get failover group: %v", err)
	}
	fg.GroupEnabled = false
	if _, err := failoverRepo.Update(context.Background(), fg.ID, fg.PriorityOrder, fg.EntryEnabled, &fg.GroupEnabled, nil, nil); err != nil {
		t.Fatalf("failed to update failover group: %v", err)
	}

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestResolveHotelModel_DisabledModel tests the case where a model in failover group is disabled

func TestResolveHotelModel_DisabledModel(t *testing.T) {

	env := newTestProxyHandler(t)
	handler := env.Handler
	modelID := env.ModelID
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)

	// Create a failover group
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Disable the model
	testProvider, err := providerRepo.GetByName(context.Background(), providerName)
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	testModel, err := modelRepo.GetByProviderAndModelID(context.Background(), testProvider.ID, modelName)
	if err != nil {
		t.Fatalf("failed to get model: %v", err)
	}
	// Disable the model via direct SQL
	if _, err := pool.Exec(context.Background(),
		"UPDATE models SET enabled = false, disabled_manually = true WHERE id = $1", testModel.ID); err != nil {
		t.Fatalf("failed to disable model: %v", err)
	}
	// Invalidate the model cache so the handler reads fresh data from DB
	model.InvalidateModelCache()

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestResolveHotelModel_DisabledProvider tests the case where a provider in failover group is disabled

func TestResolveHotelModel_DisabledProvider(t *testing.T) {

	env := newTestProxyHandler(t)
	handler := env.Handler
	modelID := env.ModelID
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)

	// Create a failover group
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Disable the provider
	testProvider, err := providerRepo.GetByName(context.Background(), providerName)
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	disabled := false
	if _, err := providerRepo.Update(context.Background(), testProvider.ID, provider.UpdateProviderRequest{Enabled: &disabled}, testProvider.EncryptedKey, testProvider.KeyNonce, testProvider.KeySalt); err != nil {
		t.Fatalf("failed to update provider: %v", err)
	}

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestResolveHotelModel_CircuitBreakerOpen_Integration tests the case where circuit breaker is open for a provider

func TestResolveHotelModel_CircuitBreakerOpen_Integration(t *testing.T) {

	env := newTestProxyHandler(t)
	handler := env.Handler
	providerID := env.ProviderID
	modelID := env.ModelID
	keyHash := env.KeyHash
	modelName := env.ModelName
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create a failover group
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Open the circuit breaker for the provider (threshold=5 by default)
	for i := 0; i < 5; i++ {
		handler.circuitBreaker.RecordFailure(providerID, "test-provider")
	}

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// TestResolveSpecificProvider_InvalidFormat tests specific provider resolution with invalid format

func TestResolveSpecificProvider_InvalidFormat(t *testing.T) {

	env := newTestProxyHandler(t)
	handler := env.Handler
	keyHash := env.KeyHash
	defer handler.Close()

	// Test with malformed provider/model format (multiple slashes)
	body := `{"model": "provider/sub/model/extra", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
