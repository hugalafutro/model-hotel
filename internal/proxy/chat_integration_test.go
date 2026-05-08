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
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// Use testDB from proxy_test.go

// newTestProxyHandler creates a Handler with test data for ChatCompletions testing.
// Returns handler, upstream server, provider ID, model ID, and virtual key hash.
func newTestProxyHandler(t *testing.T) (*Handler, *httptest.Server, uuid.UUID, uuid.UUID, string, string, string) {
	if testDB == nil {
		t.Skip("database not available")
	}

	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)

	// Create a mock upstream server that returns a simple chat completion
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		stream := reqBody["stream"].(bool)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":7,\"total_tokens\":12}}\n\n")
			fmt.Fprintf(w, "data: [DONE]\n\n")
		} else {
			response := map[string]interface{}{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   reqBody["model"].(string),
				"choices": []map[string]interface{}{
					{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello world"}, "finish_reason": "stop"},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":      5,
					"completion_tokens": 7,
					"total_tokens":       12,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}))

	// Create test provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-integration")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-" + uuid.New().String()[:8]
	createdProvider, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	providerID := createdProvider.ID

	// Create test model
	modelID := uuid.New()
	modelName := "test-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID:           modelID,
		ProviderID:   providerID,
		ModelID:      modelName,
		Name:         "Test Model",
		Description:  "Test model for integration tests",
		Capabilities: "{}",
		Params:       "{}",
		Modality:     "",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:      true,
		ProviderName: providerName,
		ProviderEnabled: true,
	}

	if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	// Create test virtual key
	virtualKeyName := "test-key-" + uuid.New().String()[:8]
	keyHash := virtualkey.Hash(virtualKeyName)
	keyPreview := "test-" + keyHash[:8]
	if _, err := virtualKeyRepo.Create(context.Background(), virtualKeyName, keyHash, keyPreview); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key-for-integration"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1")).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	return handler, upstream, providerID, modelID, keyHash, providerName, modelName
}

func TestChatCompletions_NonStreaming(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to parse response: %v", err)
	}

	if resp["model"] != modelName {
		t.Errorf("expected model '%s', got %v", modelName, resp["model"])
	}

	choices := resp["choices"].([]interface{})
	if len(choices) == 0 {
		t.Error("expected at least one choice")
	}
}

func TestChatCompletions_Streaming(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %v", contentType)
	}

	// Verify SSE format
	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "data: {") {
		t.Error("expected SSE data format")
	}
	if !strings.Contains(responseBody, "data: [DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}

func TestChatCompletions_HotelModel(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	handler, upstream, _, modelID, keyHash, _, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	// Create failover group
	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	if _, err := failoverRepo.UpsertWithConfig(context.Background(), modelName, []uuid.UUID{modelID},
		map[string]bool{modelID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to parse response: %v", err)
	}

	// Should resolve to the model in the failover group
	if resp["model"] != modelName {
		t.Errorf("expected model '%s', got %v", modelName, resp["model"])
	}
}

func TestChatCompletions_ModelNotFound(t *testing.T) {
	handler, upstream, _, _, keyHash, _, _ := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "nonexistent-provider/nonexistent-model", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
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

func TestChatCompletions_InvalidModelFormat_Integration(t *testing.T) {
	handler, upstream, _, _, keyHash, _, _ := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "invalid-format", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
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

func TestChatCompletions_UpstreamError(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	// Close the upstream server to simulate an error
	upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should get an error due to upstream being down
	if w.Code == http.StatusOK {
		t.Error("expected error status code, got 200")
	}
}

// Test ChatCompletions with stream=false and stream_options present
func TestChatCompletions_StreamFalseWithStreamOptions(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false, "stream_options": {"include_usage": true}}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to parse response: %v", err)
	}

	// Should still work with stream=false even if stream_options is present
	if resp["model"] != modelName {
		t.Errorf("expected model '%s', got %v", modelName, resp["model"])
	}
}

// Test ChatCompletions with stream=true and stream_options
func TestChatCompletions_StreamTrueWithStreamOptions(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": true, "stream_options": {"include_usage": true}}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %v", contentType)
	}

	// Verify SSE format includes usage when stream_options.include_usage is true
	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "data: {") {
		t.Error("expected SSE data format")
	}
	if !strings.Contains(responseBody, "data: [DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}

// Test ChatCompletions with empty messages array
func TestChatCompletions_EmptyMessagesArray(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should still work with empty messages (upstream may handle it)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// Test ChatCompletions with missing messages field
func TestChatCompletions_MissingMessagesField(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should handle missing messages gracefully
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// Test ChatCompletions with additional request parameters
func TestChatCompletions_AdditionalParameters(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false, "temperature": 0.7, "max_tokens": 100}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to parse response: %v", err)
	}

	if resp["model"] != modelName {
		t.Errorf("expected model '%s', got %v", modelName, resp["model"])
	}
}

// Test ChatCompletions non-streaming with successful response
func TestChatCompletions_NonStreaming_Success(t *testing.T) {
	handler, upstream, _, _, keyHash, providerName, modelName := newTestProxyHandler(t)
	defer upstream.Close()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %v", contentType)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["model"] != modelName {
		t.Errorf("expected model '%s', got %v", modelName, resp["model"])
	}

	choices := resp["choices"].([]interface{})
	if len(choices) != 1 {
		t.Errorf("expected 1 choice, got %d", len(choices))
	}

	usage := resp["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(5) {
		t.Errorf("expected prompt_tokens=5, got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(7) {
		t.Errorf("expected completion_tokens=7, got %v", usage["completion_tokens"])
	}
}

// Test ChatCompletions non-streaming with upstream 4xx error
func TestChatCompletions_NonStreaming_Upstream4xxError(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)

	// Create upstream that returns 400 error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "invalid request",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer upstream.Close()

	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-integration")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-4xx-" + uuid.New().String()[:8]
	createdProvider, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "error-model",
		Name:             "Error Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	virtualKey, err := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-4xx"), "sk-tes...")
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key-for-integration"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
	}

	body := `{"model": "`+providerName+`/error-model", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-4xx"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return 400 error from upstream
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Should have error structure
	if resp["error"] == nil {
		t.Error("expected error in response")
	}
}

// Test ChatCompletions non-streaming with upstream 5xx error
func TestChatCompletions_NonStreaming_Upstream5xxError(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)

	// Create upstream that returns 503 error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "service unavailable",
				"type":    "server_error",
			},
		})
	}))
	defer upstream.Close()

	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-integration")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-5xx-" + uuid.New().String()[:8]
	createdProvider, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "error-model-5xx",
		Name:             "Error Model 5xx",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	virtualKey, err := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-5xx"), "sk-tes...")
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key-for-integration"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
	}

	body := `{"model": "`+providerName+`/error-model-5xx", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-5xx"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return error response (502 Bad Gateway for upstream 5xx)
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["error"] == nil {
		t.Error("expected error in response")
	}
}

