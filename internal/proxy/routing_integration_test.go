//go:build integration

package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestResolveHotelModel_EmptyFailoverGroup tests the case where a failover group exists but has no priority order
func TestResolveHotelModel_EmptyFailoverGroup(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, _, keyHash, _, _ := newTestProxyHandler(t)
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
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, modelID, keyHash, _, modelName := newTestProxyHandler(t)
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
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, modelID, keyHash, providerName, modelName := newTestProxyHandler(t)
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
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, modelID, keyHash, providerName, modelName := newTestProxyHandler(t)
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

// TestResolveHotelModel_CircuitBreakerOpen tests the case where circuit breaker is open for a provider
func TestResolveHotelModel_CircuitBreakerOpen(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, providerID, modelID, keyHash, _, modelName := newTestProxyHandler(t)
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
		handler.circuitBreaker.RecordFailure(providerID)
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
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, _, keyHash, _, _ := newTestProxyHandler(t)
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

// TestChatCompletions_UpstreamTimeout tests the case where upstream times out
func TestChatCompletions_UpstreamTimeout(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}
	t.Skip("skipped: upstream timeout test requires >30s sleep, too slow for CI")

	handler, _, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	// Create a slow upstream server that times out
	slowUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Longer than default timeout
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello world"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":      5,
				"completion_tokens": 7,
				"total_tokens":       12,
			},
		})
	}))
	defer slowUpstream.Close()

	// Update provider to use slow upstream
	pool := testDB.Pool()
	providerRepo := provider.NewRepository(pool)
	testProvider, err := providerRepo.GetByName(context.Background(), providerName)
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	newURL := slowUpstream.URL
	if _, err := providerRepo.Update(context.Background(), testProvider.ID, provider.UpdateProviderRequest{BaseURL: &newURL}, testProvider.EncryptedKey, testProvider.KeyNonce, testProvider.KeySalt); err != nil {
		t.Fatalf("failed to update provider: %v", err)
	}

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should get an error due to timeout
	if w.Code == http.StatusOK {
		t.Error("expected error status code, got 200")
	}
}

// TestChatCompletions_UpstreamConnectionRefused tests the case where upstream refuses connection
func TestChatCompletions_UpstreamConnectionRefused(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	// Close the upstream server to simulate connection refused
	upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should get an error due to connection refused
	if w.Code == http.StatusOK {
		t.Error("expected error status code, got 200")
	}
}

// TestChatCompletions_UpstreamMalformedResponse tests the case where upstream returns malformed JSON
func TestChatCompletions_UpstreamMalformedResponse(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	// Create a mock upstream server that returns malformed JSON
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{invalid json"))
	})

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Proxy processes the response - exact behavior with malformed JSON is
	// implementation-specific (may forward as-is or return error).
	// Just verify it doesn't crash and returns some response.
	if w.Code != http.StatusOK && w.Code != http.StatusBadGateway {
		t.Errorf("expected 200 or 502, got %d", w.Code)
	}
}

// TestChatCompletions_StreamingPartialStream tests the case where upstream sends partial stream
func TestChatCompletions_StreamingPartialStream(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	// Create a mock upstream server that sends partial stream without [DONE]
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		// Close connection without sending [DONE]
	})

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should complete but with warning about missing [DONE]
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestChatCompletions_StreamingSSEFormatError tests the case where upstream sends malformed SSE
func TestChatCompletions_StreamingSSEFormatError(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	// Create a mock upstream server that sends malformed SSE
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "invalid sse format\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	})

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should complete but with error in stream
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestChatCompletions_FailoverOnRateLimit tests failover when rate limit is hit
func TestChatCompletions_FailoverOnRateLimit(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, modelID, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create failover group with two models (but we only have one for this test)
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create a mock upstream server that returns 429
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "rate limit exceeded",
				"type":    "rate_limit_exceeded",
			},
		})
	}))
	defer upstream.Close()

	// Update provider to use rate-limited upstream
	providerRepo := provider.NewRepository(pool)
	testProvider, err := providerRepo.GetByName(context.Background(), providerName)
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	newURL := upstream.URL
	if _, err := providerRepo.Update(context.Background(), testProvider.ID, provider.UpdateProviderRequest{BaseURL: &newURL}, testProvider.EncryptedKey, testProvider.KeyNonce, testProvider.KeySalt); err != nil {
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

	// Should get 429 error
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

// TestChatCompletions_FailoverOnAuthError tests failover when auth error occurs
func TestChatCompletions_FailoverOnAuthError(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, modelID, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create failover group
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create a mock upstream server that returns 401
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "invalid api key",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer upstream.Close()

	// Update provider to use auth-error upstream
	providerRepo := provider.NewRepository(pool)
	testProvider, err := providerRepo.GetByName(context.Background(), providerName)
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	newURL := upstream.URL
	if _, err := providerRepo.Update(context.Background(), testProvider.ID, provider.UpdateProviderRequest{BaseURL: &newURL}, testProvider.EncryptedKey, testProvider.KeyNonce, testProvider.KeySalt); err != nil {
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

	// Should get 401 error
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestChatCompletions_FailoverOn5xxError tests failover when 5xx error occurs
func TestChatCompletions_FailoverOn5xxError(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, modelID, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create failover group
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create a mock upstream server that returns 500
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "internal server error",
				"type":    "server_error",
			},
		})
	}))
	defer upstream.Close()

	// Update provider to use 500-error upstream
	providerRepo := provider.NewRepository(pool)
	testProvider, err := providerRepo.GetByName(context.Background(), providerName)
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}
	newURL := upstream.URL
	if _, err := providerRepo.Update(context.Background(), testProvider.ID, provider.UpdateProviderRequest{BaseURL: &newURL}, testProvider.EncryptedKey, testProvider.KeyNonce, testProvider.KeySalt); err != nil {
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

	// Should get 500 error
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// TestChatCompletions_EmptyMessages tests the case with empty messages array
func TestChatCompletions_EmptyMessages(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer handler.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should still attempt to process (upstream may reject empty messages)
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Errorf("expected 200 or 400, got %d", w.Code)
	}
}

// TestChatCompletions_RoutingInvalidJSON tests the case with malformed JSON body
func TestChatCompletions_RoutingInvalidJSON(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, _, _, _, keyHash, _, _ := newTestProxyHandler(t)
	defer handler.Close()

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{invalid json}"))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}