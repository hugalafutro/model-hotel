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
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// A provider that refuses response_format json_schema the way DeepSeek does
// ("This response_format type is unavailable now") is retried in JSON mode
// with the schema folded into the prompt, and the refusal is learned: the
// next request to the same model goes out in JSON mode without a 400.
func TestChatCompletions_JSONSchemaRefusalFallsBackToJSONMode(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	var calls atomic.Int32
	var schemaRefusals atomic.Int32
	var lastSystem atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rf, _ := body["response_format"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		if rf["type"] == "json_schema" {
			schemaRefusals.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"message": "This response_format type is unavailable now", "type": "invalid_request_error",
			}})
			return
		}
		if rf["type"] != "json_object" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "expected JSON mode"}})
			return
		}
		msgs, _ := body["messages"].([]any)
		if len(msgs) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no messages"}})
			return
		}
		if first, ok := msgs[0].(map[string]any); ok {
			lastSystem.Store(first)
		}
		if !strings.Contains(strings.ToLower(fmt.Sprint(msgs)), "json") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"message": "Prompt must contain the word 'json' in some form to use 'response_format' of type 'json_object'.",
			}})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-schema", "object": "chat.completion", "created": time.Now().Unix(), "model": body["model"],
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": `{"city":"Tokyo"}`}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
		})
	}))
	defer upstream.Close()

	keyPair, _ := auth.Encrypt("test-key", "test-master-key-for-integration")
	providerName := "schema-provider-" + uuid.New().String()[:8]
	prov, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name: providerName, BaseURL: upstream.URL, APIKey: "test-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	_ = modelRepo.Upsert(context.Background(), &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: "schema-model", Name: "Schema Model",
		Capabilities: "{}", Params: "{}", Modality: "chat", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Enabled: true, ProviderName: providerName, ProviderEnabled: true,
	})
	virtualKey, _ := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("test-vk-schema"), "sk-tes...", nil, nil, nil, nil, nil, nil)
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model":"` + providerName + `/schema-model","messages":[{"role":"user","content":"Facts about Tokyo."}],"stream":false,` +
		`"response_format":{"type":"json_schema","json_schema":{"name":"city","strict":true,"schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}}`
	send := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("test-vk-schema"))
		w := httptest.NewRecorder()
		handler.ChatCompletions(w, req.WithContext(ctx))
		return w
	}

	if w := send(body); w.Code != http.StatusOK {
		t.Fatalf("first request: %d %s, want 200 after the JSON-mode retry", w.Code, w.Body.String())
	}
	if calls.Load() != 2 || schemaRefusals.Load() != 1 {
		t.Fatalf("calls = %d refusals = %d, want one refusal and one retry", calls.Load(), schemaRefusals.Load())
	}
	first, _ := lastSystem.Load().(map[string]any)
	content, _ := first["content"].(string)
	if first["role"] != "system" || !strings.Contains(content, `"required":["city"]`) {
		t.Errorf("retry's leading message = %v, want a system turn carrying the schema", first)
	}

	if w := send(body); w.Code != http.StatusOK {
		t.Fatalf("second request: %d %s, want 200", w.Code, w.Body.String())
	}
	if calls.Load() != 3 || schemaRefusals.Load() != 1 {
		t.Errorf("calls = %d refusals = %d, want the learned fallback to skip the 400", calls.Load(), schemaRefusals.Load())
	}

	// A JSON-mode request refused for its prompt is the caller's 400: the
	// fallback has nothing to rewrite, so no retry is spent on it.
	plain := `{"model":"` + providerName + `/schema-model","messages":[{"role":"user","content":"Facts about Tokyo."}],"stream":false,"response_format":{"type":"json_object"}}`
	if w := send(plain); w.Code != http.StatusBadRequest {
		t.Fatalf("json_object refusal: %d %s, want the provider's 400", w.Code, w.Body.String())
	}
	if calls.Load() != 4 {
		t.Errorf("calls = %d, want no retry for a refusal the fallback cannot fix", calls.Load())
	}
}
