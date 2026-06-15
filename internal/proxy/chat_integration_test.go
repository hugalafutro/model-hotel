package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// testProxyEnv holds the test environment for proxy integration tests.
type testProxyEnv struct {
	Handler      *Handler
	Upstream     *httptest.Server
	ProviderID   uuid.UUID
	ModelID      uuid.UUID
	KeyHash      string
	ProviderName string
	ModelName    string
}

// newTestProxyHandler creates a Handler with test data for ChatCompletions testing.
// Returns a testProxyEnv struct containing all test fixtures.
func newTestProxyHandler(t *testing.T) *testProxyEnv {
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
					"prompt_tokens":     5,
					"completion_tokens": 7,
					"total_tokens":      12,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}))

	return newTestProxyEnvWithUpstream(t, upstream)
}

// newTestProxyEnvWithUpstream builds the standard single-provider test fixtures
// (provider, model, virtual key, handler) around a caller-supplied upstream.
// The provider's API key is "test-api-key".
func newTestProxyEnvWithUpstream(t *testing.T, upstream *httptest.Server) *testProxyEnv {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

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
		ID:               modelID,
		ProviderID:       providerID,
		ModelID:          modelName,
		Name:             "Test Model",
		Description:      "Test model for integration tests",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}

	if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	// Create test virtual key
	virtualKeyName := "test-key-" + uuid.New().String()[:8]
	keyHash := virtualkey.Hash(virtualKeyName)
	keyPreview := "test-" + keyHash[:8]
	if _, err := virtualKeyRepo.Create(context.Background(), virtualKeyName, keyHash, keyPreview, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	return &testProxyEnv{
		Handler:      handler,
		Upstream:     upstream,
		ProviderID:   providerID,
		ModelID:      modelID,
		KeyHash:      keyHash,
		ProviderName: providerName,
		ModelName:    modelName,
	}
}

func TestChatCompletions_NonStreaming(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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

	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	modelID := env.ModelID
	keyHash := env.KeyHash
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	keyHash := env.KeyHash
	defer env.Upstream.Close()

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
	env := newTestProxyHandler(t)
	handler := env.Handler
	keyHash := env.KeyHash
	defer env.Upstream.Close()

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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	keyHash := env.KeyHash
	providerName := env.ProviderName
	modelName := env.ModelName
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

	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

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

	virtualKey, err := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-4xx"), "sk-tes...", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "` + providerName + `/error-model", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
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

	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

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

	virtualKey, err := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-5xx"), "sk-tes...", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "` + providerName + `/error-model-5xx", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-5xx"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return error response (upstream 5xx is passed through)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["error"] == nil {
		t.Error("expected error in response")
	}
}

// TestChatCompletions_HotelModelNoAvailableProvider tests that hotel/ prefix
// with no available provider returns 404.
func TestChatCompletions_HotelModelNoAvailableProvider(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	keyHash := env.KeyHash
	defer env.Upstream.Close()

	pool := testDB.Pool()
	failoverRepo := failover.NewRepository(pool)

	// Create a failover group with no models (empty candidates)
	groupName := "empty-group-" + uuid.New().String()[:8]
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName, []uuid.UUID{}, map[string]bool{}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create empty failover group: %v", err)
	}

	body := `{"model": "hotel/` + groupName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return 404 Not Found (empty failover group returns error)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestChatCompletions_ProviderModelInvalidFormat tests model with multiple slashes
// like "a/b/c" is handled gracefully.
func TestChatCompletions_ProviderModelInvalidFormat(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	keyHash := env.KeyHash
	defer env.Upstream.Close()

	// Model with multiple slashes - should use first two parts
	body := `{"model": "provider/model/extra", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return 404 (provider not found) - handled gracefully, not a crash
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestChatCompletions_FailoverWithBackoff tests that when first provider returns
// 500, the request succeeds with the second provider after backoff.
func TestChatCompletions_FailoverWithBackoff(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	// Create first provider that returns 500
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "internal error",
				"type":    "server_error",
			},
		})
	}))
	defer upstream1.Close()

	// Create second provider that succeeds
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "success-model",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "success"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 7,
				"total_tokens":      12,
			},
		})
	}))
	defer upstream2.Close()

	// Create providers
	keyPair1, _ := auth.Encrypt("key1", "test-master-key-for-integration")
	keyPair2, _ := auth.Encrypt("key2", "test-master-key-for-integration")

	provider1Name := "fail-provider-" + uuid.New().String()[:8]
	provider1, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    provider1Name,
		BaseURL: upstream1.URL,
		APIKey:  "key1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)

	provider2Name := "success-provider-" + uuid.New().String()[:8]
	provider2, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    provider2Name,
		BaseURL: upstream2.URL,
		APIKey:  "key2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)

	// Create models
	model1 := &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider1.ID,
		ModelID:          "shared-model",
		Name:             "Shared Model 1",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     provider1Name,
		ProviderEnabled:  true,
	}
	model2 := &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider2.ID,
		ModelID:          "shared-model",
		Name:             "Shared Model 2",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     provider2Name,
		ProviderEnabled:  true,
	}
	_ = modelRepo.Upsert(context.Background(), model1)
	_ = modelRepo.Upsert(context.Background(), model2)

	// Create failover group with both models
	groupName := "failover-group-" + uuid.New().String()[:8]
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName, []uuid.UUID{model1.ID, model2.ID}, map[string]bool{model1.ID.String(): true, model2.ID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key
	virtualKey, _ := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-failover"), "sk-tes...", nil, nil, nil, nil, nil)
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "hotel/` + groupName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-failover"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should succeed with second provider after failover
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after failover, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["model"] != "success-model" {
		t.Errorf("expected model 'success-model', got %v", resp["model"])
	}
}

// TestChatCompletions_ClientDisconnectDuringBackoff tests that client disconnect
// during failover backoff returns 408.
func TestChatCompletions_ClientDisconnectDuringBackoff(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	// Create providers that are slow (to allow client disconnect during backoff)
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "error1",
			},
		})
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should not be reached because client disconnects during backoff
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"choices": []map[string]interface{}{{"message": map[string]interface{}{"content": "success"}}},
		})
	}))
	defer upstream2.Close()

	keyPair1, _ := auth.Encrypt("key1", "test-master-key-for-integration")
	keyPair2, _ := auth.Encrypt("key2", "test-master-key-for-integration")

	provider1Name := "slow-provider1-" + uuid.New().String()[:8]
	provider1, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    provider1Name,
		BaseURL: upstream1.URL,
		APIKey:  "key1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)

	provider2Name := "slow-provider2-" + uuid.New().String()[:8]
	provider2, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    provider2Name,
		BaseURL: upstream2.URL,
		APIKey:  "key2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)

	model1 := &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider1.ID,
		ModelID:          "shared-model",
		Name:             "Model 1",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     provider1Name,
		ProviderEnabled:  true,
	}
	model2 := &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider2.ID,
		ModelID:          "shared-model",
		Name:             "Model 2",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     provider2Name,
		ProviderEnabled:  true,
	}
	_ = modelRepo.Upsert(context.Background(), model1)
	_ = modelRepo.Upsert(context.Background(), model2)

	groupName := "disconnect-group-" + uuid.New().String()[:8]
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName, []uuid.UUID{model1.ID, model2.ID}, map[string]bool{model1.ID.String(): true, model2.ID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	virtualKey, _ := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-disconnect"), "sk-tes...", nil, nil, nil, nil, nil)
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "hotel/` + groupName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-disconnect"))

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	req = req.WithContext(ctx)

	// Start request in goroutine
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ChatCompletions(w, req)
		close(done)
	}()

	// Let first attempt complete
	time.Sleep(50 * time.Millisecond)

	// Cancel during backoff (before second attempt)
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ChatCompletions did not finish")
	}

	// Should get 499 (client closed request) — a client hangup is not a
	// provider failure, so it is no longer reported as 408/502 (see
	// plans/logging-and-errors-overhaul.md §7).
	if w.Code != 499 {
		t.Errorf("expected 499, got %d", w.Code)
	}
}

// TestChatCompletions_ParamRejectionAutoRetry tests that when provider returns
// 400 with parameter rejection, the proxy retries with param stripped.
func TestChatCompletions_ParamRejectionAutoRetry(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	var retryCount atomic.Int32
	// Create provider that rejects temperature on first request, succeeds on retry
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount.Add(1)
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		// First request has temperature - reject it with quoted param name
		if retryCount.Load() == 1 && body["temperature"] != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Unsupported parameter: \"temperature\" is not supported for this model",
					"type":    "invalid_request_error",
				},
			})
			return
		}

		// Retry or request without temperature - succeed
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   body["model"].(string),
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "success"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 7,
				"total_tokens":      12,
			},
		})
	}))
	defer upstream.Close()

	keyPair, _ := auth.Encrypt("test-key", "test-master-key-for-integration")
	providerName := "retry-provider-" + uuid.New().String()[:8]
	prov, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: upstream.URL,
		APIKey:  "test-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)

	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       prov.ID,
		ModelID:          "retry-model",
		Name:             "Retry Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	_ = modelRepo.Upsert(context.Background(), testModel)

	virtualKey, _ := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-retry"), "sk-tes...", nil, nil, nil, nil, nil)
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	// Request with temperature parameter
	body := `{"model": "` + providerName + `/retry-model", "messages": [{"role": "user", "content": "hello"}], "stream": false, "temperature": 0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-retry"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should succeed after auto-retry
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after auto-retry, got %d", w.Code)
	}

	// Verify retry happened
	if retryCount.Load() != 2 {
		t.Errorf("expected 2 requests (initial + retry), got %d", retryCount.Load())
	}
}

// TestChatCompletions_NonJSONErrorBody tests error handling for non-JSON
// upstream responses (e.g. HTML from CDN). With a single provider and no
// failover candidates remaining, this exercises the all-candidates-exhausted
// path (line 1708) which wraps the error in an OpenAI-compatible envelope.
// Lines 1719-1723 (non-JSON wrapping with remaining candidates) would require
// a hotel/ failover group setup.
func TestChatCompletions_NonJSONErrorBody(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Upstream returns HTML error (simulating CDN/proxy error)
	htmlUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`<html><body><h1>Bad Gateway</h1></body></html>`))
	}))
	defer htmlUpstream.Close()

	keyPair, _ := auth.Encrypt("test-key", "test-master-key-for-integration")
	providerName := "html-provider-" + uuid.New().String()[:8]
	prov, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: htmlUpstream.URL,
		APIKey:  "test-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	testModel := &model.Model{
		ID:               uuid.New(),
		ProviderID:       prov.ID,
		ModelID:          "html-model",
		Name:             "HTML Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  `["text"]`,
		OutputModalities: `["text"]`,
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	_ = modelRepo.Upsert(context.Background(), testModel)

	virtualKey, err := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-html"), "sk-tes...", nil, nil, nil, nil, nil)
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
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 5 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}

	body := `{"model": "` + providerName + `/html-model", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-html"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return error wrapped in OpenAI-compatible envelope
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}

	// Body should be valid JSON with error object (not raw HTML)
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got: %s", w.Body.String())
	}

	if _, ok := resp["error"]; !ok {
		t.Errorf("expected error object in response, got: %v", resp)
	}
}
