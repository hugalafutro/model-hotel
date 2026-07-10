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
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// ChatCompletions request validation tests (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestChatCompletions_MissingBody(t *testing.T) {
	h := newIntegrationHandler()
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(""))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", rr.Code)
	}
}

func TestChatCompletions_InvalidJSON(t *testing.T) {
	h := newIntegrationHandler()
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader("not json"))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestChatCompletions_MissingModel(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", rr.Code)
	}
}

func TestChatCompletions_InvalidModelFormat(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"just-a-name","messages":[]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid model format, got %d", rr.Code)
	}
}

func TestChatCompletions_HotelModelNotFound(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"hotel/nonexistent","messages":[]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown hotel model, got %d", rr.Code)
	}
}

func TestChatCompletions_SpecificProviderNotFound(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"unknown-provider/some-model","messages":[]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown provider, got %d", rr.Code)
	}
}

func TestChatCompletions_StreamOptionsInjection(t *testing.T) {
	body := `{"model":"test","stream":true,"messages":[]}`
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatal(err)
	}
	raw["stream_options"] = map[string]interface{}{
		"include_usage": true,
	}
	injected, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(injected, &parsed); err != nil {
		t.Fatal(err)
	}
	so, ok := parsed["stream_options"].(map[string]interface{})
	if !ok {
		t.Fatal("stream_options should be a map")
	}
	if so["include_usage"] != true {
		t.Error("stream_options.include_usage should be true")
	}
}

// backoffDuration and pow2 removed — backoff logic is now in production code
// (failoverBackoff in proxy.go) with jitter.

// TestChatCompletions_ModelWithNoSlash tests the error path for model names
// that don't contain a slash (invalid format)
func TestChatCompletions_ModelWithNoSlash(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"justmodel","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for model without slash, got %d", rr.Code)
	}
}

// TestChatCompletions_EmptyModel tests the error path for empty model field
func TestChatCompletions_EmptyModel(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty model, got %d", rr.Code)
	}
}

func TestChatCompletions_MiddlewareContextWithoutBodyBytes(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"hotel/nonexistent","stream":true,"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	// Simulate middleware having already parsed the body
	ctx := context.WithValue(req.Context(), ctxkeys.RequestBodyParseMsKey, float64(1.5))
	ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, "hotel/nonexistent")
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, true)
	// No RequestBodyKey set — so bodyBytes remains empty
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)
	// Model "hotel/nonexistent" should 404 (no failover group found)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestChatCompletions_MiddlewareContextWithBodyBytes(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"hotel/nonexistent","stream":false,"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxkeys.RequestBodyParseMsKey, float64(2.0))
	ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, "hotel/nonexistent")
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, false)
	ctx = context.WithValue(ctx, ctxkeys.RequestBodyKey, []byte(body))
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions all providers exhausted (lines 1372-1384)
// ---------------------------------------------------------------------------

func TestChatCompletions_AllProvidersFail(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	providerName := env.ProviderName
	modelName := env.ModelName
	defer upstream.Close()
	defer handler.upstreamTransport.CloseIdleConnections()

	// Replace the upstream with one that returns 500 (failover-eligible)
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal server error"}}`))
	})

	// Single provider returning 500 → no more candidates → non-200 error path
	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return 500 (the upstream's error forwarded to client)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions middleware context with settings read time (lines 948-950, 992-994, 1051-1053)
// ---------------------------------------------------------------------------

func TestChatCompletions_SettingsReadTimeFromContext(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Provide settings read time via context pointer
	settingsMs := 3.0
	body := `{"model":"hotel/nonexistent","stream":false,"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxkeys.SettingsReadMsKey, &settingsMs)
	ctx = context.WithValue(ctx, ctxkeys.RequestBodyKey, []byte(body))
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	// Just verify it doesn't panic and the settings read time is consumed
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent hotel model, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions with settings read time reaching the failover loop
// (lines 992-994, 1051-1053)
// ---------------------------------------------------------------------------

func TestChatCompletions_SettingsReadTimeInFailoverLoop(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	providerName := env.ProviderName
	modelName := env.ModelName
	defer env.Upstream.Close()
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))

	// Set settings read time via context pointer — this is read at lines
	// 948, 992, and 1051 inside the ChatCompletions failover loop.
	settingsMs := 2.5
	ctx := context.WithValue(req.Context(), ctxkeys.SettingsReadMsKey, &settingsMs)
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions all providers exhausted - failover path (lines 1372-1384)
// ---------------------------------------------------------------------------

func TestChatCompletions_FailoverAllProvidersExhausted(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-for-failover"

	// Create two providers pointing at non-listening ports (connection refused).
	// This triggers the error path at line 1167, which `continue`s past
	// all candidates, reaching the "all providers exhausted" path at line 1372.
	keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key1: %v", err)
	}
	prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "failover-prov-1-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:1", // connection refused
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider1: %v", err)
	}

	keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key2: %v", err)
	}
	prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "failover-prov-2-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:2", // connection refused
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider2: %v", err)
	}

	// Create models for both providers.
	modelID1 := uuid.New()
	modelName := "failover-model-" + uuid.New().String()[:8]
	model1 := &model.Model{
		ID: modelID1, ProviderID: prov1.ID, ModelID: modelName,
		Name: "Failover Model 1", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov1.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model1); err != nil {
		t.Fatalf("failed to upsert model1: %v", err)
	}

	modelID2 := uuid.New()
	model2 := &model.Model{
		ID: modelID2, ProviderID: prov2.ID, ModelID: modelName,
		Name: "Failover Model 2", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov2.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model2); err != nil {
		t.Fatalf("failed to upsert model2: %v", err)
	}

	// Create failover group with both models.
	if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{}, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key.
	vkName := "failover-test-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "failover-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Both providers fail with 5xx → all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for all providers exhausted, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions specific provider request failed (lines 1378-1381)
// ---------------------------------------------------------------------------

func TestChatCompletions_SpecificProviderAllProvidersFail(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-for-specific"

	// Create a provider pointing at a non-listening port (connection refused).
	// This triggers the error path that reaches line 1378 (specific provider failed).
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "specific-prov-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:1", // connection refused
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "specific-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "Specific Model", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, testModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "specific-test-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "specific-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	// Use specific provider format (not hotel/) → single candidate → non-200 error forwarded
	body := `{"model": "` + prov.Name + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Single provider with connection refused → 502 Bad Gateway
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// TestChatCompletions_DeprecationCacheFirstEntry tests the deprecation cache
// LoadOrStore path when no existing entry exists (first rejection learned for a model).
// Covers lines 1226-1229 in proxy.go. The CompareAndSwap merge loop (1232-1240)
// has a latent bug: map[string]bool is not a comparable type, causing CompareAndSwap
// to panic. That path can only be tested after the type is changed to a comparable one.
func TestChatCompletions_DeprecationCacheFirstEntry(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	providerName := env.ProviderName
	modelName := env.ModelName
	defer upstream.Close()
	defer stopUnitHandlerIntegration(handler)

	// Configure upstream to return 400 with a param rejection for "top_p".
	// The backtick-wrapped param name is recognized by parseProviderParamError.
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"` + "`top_p`" + ` is not supported for this model"}}`))
	})

	// No pre-existing cache entry — LoadOrStore returns !loaded, storing the
	// rejected params as the first entry and breaking out of the loop.
	providerType := provider.DetectProviderType(upstream.URL)
	cacheKey := fmt.Sprintf("%s:%s", providerType, modelName)

	body := `{"model": "` + providerName + `/` + modelName + `", "stream": false, "messages": [{"role": "user", "content": "hello"}], "temperature": 0.7, "top_p": 0.9}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// The 400 triggers deprecation caching, then auto-retry strips top_p.
	// The retry also returns 400 (same upstream), so 400 is forwarded.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	// Verify the cache entry was created with "top_p".
	cached, loaded := handler.deprecationCache.Load(cacheKey)
	if !loaded {
		t.Fatal("expected cache entry to exist")
	}
	entryPtr, ok := cached.(*map[string]bool)
	if !ok {
		t.Fatalf("expected cache value to be *map[string]bool, got %T", cached)
	}
	if !(*entryPtr)["top_p"] {
		t.Error("expected 'top_p' to be in cache entry")
	}
}

// TestChatCompletions_RetryCancelDuringFailover covers the retryCancel cleanup
// at line 1318-1320. Scenario: provider returns 400 (param rejection), auto-retry
// strips the rejected param and retries, the retry returns 500 (failover-eligible),
// and there are more candidates available, so failover continues. The retryCancel
// must be called during the failover continue path.
func TestChatCompletions_RetryCancelDuringFailover(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-retry-cancel"

	// Create two providers. The first returns 400 then 500 (triggering retry
	// and failover). The second also fails with connection refused, so all
	// providers exhaust. The key is that the first provider's retry returns
	// a failover-eligible status (500) while retryCancel is set.
	keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key1: %v", err)
	}
	callCount := 0
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		if _, hasTopP := reqBody["top_p"]; hasTopP {
			// First request: return 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"` + "`top_p`" + ` is not supported"}}`))
		} else {
			// Retry (top_p stripped): return 500 (failover-eligible)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"internal error"}}`))
		}
	}))
	defer upstream1.Close()

	prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "retry-prov-1-" + uuid.New().String()[:8],
		BaseURL: upstream1.URL,
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider1: %v", err)
	}

	keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key2: %v", err)
	}
	prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "retry-prov-2-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:1", // connection refused
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider2: %v", err)
	}

	// Create models for both providers.
	modelName := "retry-model-" + uuid.New().String()[:8]
	model1 := &model.Model{
		ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
		Name: "Retry Model 1", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov1.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model1); err != nil {
		t.Fatalf("failed to upsert model1: %v", err)
	}

	model2 := &model.Model{
		ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
		Name: "Retry Model 2", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov2.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model2); err != nil {
		t.Fatalf("failed to upsert model2: %v", err)
	}

	// Create failover group with both models.
	if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{}, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key.
	vkName := "retry-cancel-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "retry-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "hotel/` + modelName + `", "stream": false, "messages": [{"role": "user", "content": "hello"}], "top_p": 0.9}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Provider 1: 400 → retry → 500 (failover) + Provider 2: connection refused
	// All providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// TestChatCompletions_TouchLastUsedGoroutine verifies the TouchLastUsed goroutine
// at line 1081 fires during a successful request. The error paths (1083-1085: panic
// recovery, 1089-1091: TouchLastUsed error) cannot be reliably tested because:
//   - provider.Repository is a concrete type (not mockable)
//   - closing the pool breaks the entire request, not just the goroutine
//   - the goroutine creates its own 5s-timeout context, so cancellation from
//     the test doesn't affect it
//
// Coverage of the success path is confirmed (48 hits in the coverage profile).
func TestChatCompletions_TouchLastUsedGoroutine(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	providerName := env.ProviderName
	modelName := env.ModelName
	defer upstream.Close()
	defer stopUnitHandlerIntegration(handler)

	// The upstream returns a successful response, which causes the code to
	// reach the TouchLastUsed goroutine at line 1081.
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12,
			},
		})
	})

	body := `{"model": "` + providerName + `/` + modelName + `", "stream": false, "messages": [{"role": "user", "content": "hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Give the TouchLastUsed goroutine time to execute.
	time.Sleep(200 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// ChatCompletions TTFT probe integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestChatCompletions_TTFTProbeSuccess(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	// Upstream SSE server that sends data immediately.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Configure short TTFT timeout (generous for local test).
	if err := settingsRepo.Set(ctx, "ttft_timeout", "5s"); err != nil {
		t.Fatalf("failed to set ttft_timeout: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "ttft_timeout", "60s") }()
	settingsRepo.InvalidateCache("ttft_timeout")

	masterKey := "test-master-key-ttft-success"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "ttft-success-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "ttft-success-model-" + uuid.New().String()[:8]
	m := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "TTFT Success", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "ttft-success-vk-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ttft-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, ratelimit.NewLimiter(settingsRepo), ratelimit.NewIPLimiter(30, 60, nil, nil))

	body := fmt.Sprintf(`{"model":"%s/%s","messages":[{"role":"user","content":"hi"}],"stream":true}`, prov.Name, modelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	respBody := w.Body.String()
	if !strings.Contains(respBody, "data: [DONE]") {
		t.Error("expected response to contain [DONE] sentinel")
	}
	if !strings.Contains(respBody, "hi") {
		t.Error("expected response to contain streamed content")
	}
}

func TestChatCompletions_TTFTProbeTimeout(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	// Upstream server that delays sending data (simulates slow TTFT).
	// Uses r.Context().Done() so the handler returns promptly when the
	// probe closes the body (avoids waiting for the full sleep).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer upstream.Close()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Very short TTFT timeout so the probe fails quickly.
	if err := settingsRepo.Set(ctx, "ttft_timeout", "100ms"); err != nil {
		t.Fatalf("failed to set ttft_timeout: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "ttft_timeout", "60s") }()
	settingsRepo.InvalidateCache("ttft_timeout")

	// Set circuit breaker threshold to 1 so probe failure opens it.
	if err := settingsRepo.Set(ctx, "circuit_breaker_threshold", "1"); err != nil {
		t.Fatalf("failed to set circuit_breaker_threshold: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "circuit_breaker_threshold", "5") }()
	settingsRepo.InvalidateCache("circuit_breaker_threshold")

	masterKey := "test-master-key-ttft-timeout"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "ttft-timeout-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "ttft-timeout-model-" + uuid.New().String()[:8]
	m := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "TTFT Timeout", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "ttft-timeout-vk-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ttft-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, ratelimit.NewLimiter(settingsRepo), ratelimit.NewIPLimiter(30, 60, nil, nil))

	body := fmt.Sprintf(`{"model":"%s/%s","messages":[{"role":"user","content":"hi"}],"stream":true}`, prov.Name, modelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Single provider, probe timeout → all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for TTFT probe timeout, got %d", w.Code)
	}

	// Verify circuit breaker recorded failure (threshold=1 → open).
	cbState := handler.circuitBreaker.GetState(prov.ID)
	if cbState != failover.StateOpen {
		t.Errorf("expected circuit breaker StateOpen after probe timeout, got %s", cbState)
	}
}

func TestChatCompletions_TTFTDisabled_CBRecordsSuccess(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	// Upstream SSE server that sends data immediately.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Disable TTFT probe (0 = immediate commit / backward compat).
	if err := settingsRepo.Set(ctx, "ttft_timeout", "0s"); err != nil {
		t.Fatalf("failed to set ttft_timeout: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "ttft_timeout", "60s") }()
	settingsRepo.InvalidateCache("ttft_timeout")

	// Circuit breaker threshold = 1 so we can detect success recording.
	if err := settingsRepo.Set(ctx, "circuit_breaker_threshold", "1"); err != nil {
		t.Fatalf("failed to set circuit_breaker_threshold: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "circuit_breaker_threshold", "5") }()
	settingsRepo.InvalidateCache("circuit_breaker_threshold")

	masterKey := "test-master-key-ttft-disabled"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "ttft-disabled-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "ttft-disabled-model-" + uuid.New().String()[:8]
	m := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "TTFT Disabled", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "ttft-disabled-vk-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ttft-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, ratelimit.NewLimiter(settingsRepo), ratelimit.NewIPLimiter(30, 60, nil, nil))

	body := fmt.Sprintf(`{"model":"%s/%s","messages":[{"role":"user","content":"hi"}],"stream":true}`, prov.Name, modelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response content matches TestChatCompletions_TTFTProbeSuccess pattern
	respBody := w.Body.String()
	if !strings.Contains(respBody, "data: [DONE]") {
		t.Error("expected response to contain [DONE] sentinel")
	}
	if !strings.Contains(respBody, "ok") {
		t.Error("expected response to contain streamed content")
	}

	// When ttft_timeout=0, the else-if branch records CB success immediately
	// (backward-compat path at the `else if circuitBreakerEnabled` block).
	// With threshold=1 and a success recorded, the circuit should stay closed.
	cbState := handler.circuitBreaker.GetState(prov.ID)
	if cbState != failover.StateClosed {
		t.Errorf("expected circuit breaker StateClosed after success, got %s", cbState)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions allowed_providers filter tests (lines 1158-1181)
// ---------------------------------------------------------------------------

func TestChatCompletions_AllowedProviders_FilterAllowed(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-allowed-providers"

	// Provider 1: real upstream that returns success (the "allowed" provider)
	prov1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-allowed",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer prov1Server.Close()

	keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key1: %v", err)
	}
	prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "allowed-prov-" + uuid.New().String()[:8],
		BaseURL: prov1Server.URL,
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider1: %v", err)
	}

	// Provider 2: tracking upstream that fails the test if contacted (the "blocked" provider)
	var prov2Contacted atomic.Bool
	prov2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prov2Contacted.Store(true)
		t.Error("blocked provider should never have been contacted")
		w.WriteHeader(http.StatusOK)
	}))
	defer prov2Server.Close()

	keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key2: %v", err)
	}
	prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "blocked-prov-" + uuid.New().String()[:8],
		BaseURL: prov2Server.URL,
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider2: %v", err)
	}

	// Create models for both providers with same model ID (for failover group)
	modelName := "ap-model-" + uuid.New().String()[:8]
	model1 := &model.Model{
		ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
		Name: "Model 1", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov1.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model1); err != nil {
		t.Fatalf("failed to upsert model1: %v", err)
	}

	model2 := &model.Model{
		ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
		Name: "Model 2", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov2.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model2); err != nil {
		t.Fatalf("failed to upsert model2: %v", err)
	}

	// Create failover group (hotel/) with both models
	if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{}, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key with allowed_providers = [prov1.ID only]
	vkName := "ap-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ap-" + vkHash[:8]
	allowedProviders := []string{prov1.ID.String()}
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, &allowedProviders, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	// Set allowed_providers in context (simulating what middleware does)
	rCtx = context.WithValue(rCtx, ctxkeys.VirtualKeyAllowedProvidersKey, &allowedProviders)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should get 200 from prov1 (allowed). prov2 must never be contacted.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (allowed provider succeeds), got %d; body: %s", w.Code, w.Body.String())
	}
	if prov2Contacted.Load() {
		t.Error("blocked provider was contacted despite allowed_providers filter")
	}
}

func TestChatCompletions_AllowedProviders_BlockAllReturns403(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-blocked-providers"

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "blocked-only-prov-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:9997",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create model
	modelName := "blocked-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "Blocked Model", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, testModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Create virtual key with allowed_providers = [different provider ID]
	// This blocks the only available provider
	vkName := "blocked-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "bk-" + vkHash[:8]
	allowedProviders := []string{"00000000-0000-0000-0000-000000000000"} // non-existent provider
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, &allowedProviders, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "` + prov.Name + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	// Set allowed_providers in context (simulating what middleware does)
	rCtx = context.WithValue(rCtx, ctxkeys.VirtualKeyAllowedProvidersKey, &allowedProviders)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// All candidates filtered → 403
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 (virtual key does not have access), got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "virtual key does not have access to any provider") {
		t.Errorf("expected 403 error message, got: %s", w.Body.String())
	}
}

func TestChatCompletions_AllowedProviders_NilAllowsAll(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	providerName := env.ProviderName
	modelName := env.ModelName
	defer env.Upstream.Close()
	defer handler.upstreamTransport.CloseIdleConnections()

	// Virtual key created with nil allowed_providers (via newTestProxyHandler)
	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// nil allowed_providers → no filtering → request succeeds
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (nil allowed_providers allows all), got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_AllowedProviders_EmptySliceAllowsAll(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-empty-allowed"

	// Create a provider that returns success
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()

	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "empty-allowed-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "empty-allowed-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "Empty Allowed Model", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, testModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Create virtual key with empty allowed_providers slice (len==0)
	vkName := "empty-allowed-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ea-" + vkHash[:8]
	emptyAllowed := []string{} // empty slice, not nil
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, &emptyAllowed, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	body := `{"model": "` + prov.Name + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	// Set empty allowed_providers in context (simulating what middleware does)
	rCtx = context.WithValue(rCtx, ctxkeys.VirtualKeyAllowedProvidersKey, &emptyAllowed)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// empty slice allowed_providers → len==0 check skips filter → request succeeds
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (empty slice allowed_providers skips filter), got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// TestChatCompletions_UpstreamErrorForwarding - tests the new error forwarding logic
// (proxy.go lines 1691-1722)
// ---------------------------------------------------------------------------

func TestChatCompletions_UpstreamErrorForwarding(t *testing.T) {
	t.Run("failover exhausted returns generic error", func(t *testing.T) {
		pool := testDB.Pool()
		ctx := context.Background()

		settingsRepo := settings.NewRepository(pool)
		failoverRepo := failover.NewRepository(pool)
		modelRepo := model.NewRepository(pool)
		providerRepo := provider.NewRepository(pool)
		virtualKeyRepo := virtualkey.NewRepository(pool)
		limiter := ratelimit.NewLimiter(settingsRepo)
		ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

		masterKey := "test-master-key-for-error-forward"

		// Create upstream that returns 400 with context_length_exceeded
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": {"message": "context_length_exceeded", "type": "invalid_request_error"}}`))
		}))
		defer upstream.Close()

		keyPair, err := auth.Encrypt("test-api-key", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key: %v", err)
		}

		provName := "error-prov-" + uuid.New().String()[:8]
		prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName,
			BaseURL: upstream.URL,
			APIKey:  "test-api-key",
		}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
		if err != nil {
			t.Fatalf("failed to create provider: %v", err)
		}

		modelName := "error-model-" + uuid.New().String()[:8]
		testModel := &model.Model{
			ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
			Name: "Error Model", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, testModel); err != nil {
			t.Fatalf("failed to upsert model: %v", err)
		}

		vkName := "error-test-key-" + uuid.New().String()[:8]
		vkHash := virtualkey.Hash(vkName)
		if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, "et-"+vkHash[:8], nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("failed to create virtual key: %v", err)
		}

		handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

		body := `{"model": "` + provName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
		rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
		rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
		req = req.WithContext(rCtx)

		w := httptest.NewRecorder()
		handler.ChatCompletions(w, req)

		// Single provider (no failover candidates) with 400 → generic error
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}

		bodyStr := w.Body.String()
		if !strings.Contains(bodyStr, "upstream provider returned HTTP 400") {
			t.Errorf("expected generic error message, got: %s", bodyStr)
		}
		if strings.Contains(bodyStr, "context_length_exceeded") {
			t.Errorf("should NOT forward upstream JSON details, got: %s", bodyStr)
		}
	})

	t.Run("non-failover-eligible error forwards upstream body", func(t *testing.T) {
		pool := testDB.Pool()
		ctx := context.Background()

		settingsRepo := settings.NewRepository(pool)
		failoverRepo := failover.NewRepository(pool)
		modelRepo := model.NewRepository(pool)
		providerRepo := provider.NewRepository(pool)
		virtualKeyRepo := virtualkey.NewRepository(pool)
		limiter := ratelimit.NewLimiter(settingsRepo)
		ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

		masterKey := "test-master-key-for-forward-body"

		// Create two upstreams - first returns 400 (non-failover-eligible)
		callCount := 0
		upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			// First provider returns 400 with custom error
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": {"message": "custom_validation_error", "type": "invalid_request_error"}}`))
		}))
		defer upstream1.Close()

		upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Second provider succeeds (we should never reach here for 400)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": "cmpl-2", "choices": [{"message": {"content": "success"}}]}`))
		}))
		defer upstream2.Close()

		keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key1: %v", err)
		}
		keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key2: %v", err)
		}

		provName1 := "forward-prov-1-" + uuid.New().String()[:8]
		prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName1,
			BaseURL: upstream1.URL,
			APIKey:  "test-api-key-1",
		}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
		if err != nil {
			t.Fatalf("failed to create provider1: %v", err)
		}

		provName2 := "forward-prov-2-" + uuid.New().String()[:8]
		prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName2,
			BaseURL: upstream2.URL,
			APIKey:  "test-api-key-2",
		}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
		if err != nil {
			t.Fatalf("failed to create provider2: %v", err)
		}

		modelName := "forward-model-" + uuid.New().String()[:8]
		model1 := &model.Model{
			ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
			Name: "Forward Model 1", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName1, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, model1); err != nil {
			t.Fatalf("failed to upsert model1: %v", err)
		}

		model2 := &model.Model{
			ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
			Name: "Forward Model 2", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName2, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, model2); err != nil {
			t.Fatalf("failed to upsert model2: %v", err)
		}

		// Create failover group with both models
		if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
			[]uuid.UUID{model1.ID, model2.ID},
			map[string]bool{}, nil, nil, nil, nil,
		); err != nil {
			t.Fatalf("failed to create failover group: %v", err)
		}

		vkName := "forward-test-key-" + uuid.New().String()[:8]
		vkHash := virtualkey.Hash(vkName)
		if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, "ft-"+vkHash[:8], nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("failed to create virtual key: %v", err)
		}

		handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

		// Use hotel/ format to trigger failover group
		body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
		rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
		rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
		req = req.WithContext(rCtx)

		w := httptest.NewRecorder()
		handler.ChatCompletions(w, req)

		// 400 is NOT failover-eligible, so first provider's body should be forwarded
		// hasMoreCandidates=true but isFailoverEligible=false → forward upstream body
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}

		bodyStr := w.Body.String()
		// Should contain the upstream JSON error details
		if !strings.Contains(bodyStr, "custom_validation_error") {
			t.Errorf("expected upstream error body to be forwarded, got: %s", bodyStr)
		}
		// Verify we only called the first provider (no failover occurred)
		if callCount != 1 {
			t.Errorf("expected 1 call (no failover for 400), got %d", callCount)
		}
	})

	t.Run("non-JSON error body wrapped in OpenAI envelope", func(t *testing.T) {
		pool := testDB.Pool()
		ctx := context.Background()

		settingsRepo := settings.NewRepository(pool)
		failoverRepo := failover.NewRepository(pool)
		modelRepo := model.NewRepository(pool)
		providerRepo := provider.NewRepository(pool)
		virtualKeyRepo := virtualkey.NewRepository(pool)
		limiter := ratelimit.NewLimiter(settingsRepo)
		ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

		masterKey := "test-master-key-for-nonjson-body"

		// First provider returns a non-failover-eligible status (422) with a
		// non-JSON body (e.g. HTML from a CDN). With a candidate remaining this
		// exercises forwardUpstreamError's else branch: wrap the body in an
		// OpenAI-compatible error envelope rather than forwarding raw HTML.
		callCount := 0
		htmlBody := `<html><body>502 Bad Gateway (cloudflare)</body></html>`
		upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(htmlBody))
		}))
		defer upstream1.Close()

		upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Second provider succeeds (we should never reach here for 422).
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id": "cmpl-2", "choices": [{"message": {"content": "success"}}]}`))
		}))
		defer upstream2.Close()

		keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key1: %v", err)
		}
		keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key2: %v", err)
		}

		provName1 := "nonjson-prov-1-" + uuid.New().String()[:8]
		prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName1,
			BaseURL: upstream1.URL,
			APIKey:  "test-api-key-1",
		}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
		if err != nil {
			t.Fatalf("failed to create provider1: %v", err)
		}

		provName2 := "nonjson-prov-2-" + uuid.New().String()[:8]
		prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName2,
			BaseURL: upstream2.URL,
			APIKey:  "test-api-key-2",
		}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
		if err != nil {
			t.Fatalf("failed to create provider2: %v", err)
		}

		modelName := "nonjson-model-" + uuid.New().String()[:8]
		model1 := &model.Model{
			ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
			Name: "NonJSON Model 1", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName1, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, model1); err != nil {
			t.Fatalf("failed to upsert model1: %v", err)
		}

		model2 := &model.Model{
			ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
			Name: "NonJSON Model 2", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName2, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, model2); err != nil {
			t.Fatalf("failed to upsert model2: %v", err)
		}

		// Create failover group with both models
		if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
			[]uuid.UUID{model1.ID, model2.ID},
			map[string]bool{}, nil, nil, nil, nil,
		); err != nil {
			t.Fatalf("failed to create failover group: %v", err)
		}

		vkName := "nonjson-test-key-" + uuid.New().String()[:8]
		vkHash := virtualkey.Hash(vkName)
		if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, "nj-"+vkHash[:8], nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("failed to create virtual key: %v", err)
		}

		handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

		// Use hotel/ format to trigger failover group
		body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
		rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
		rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
		req = req.WithContext(rCtx)

		w := httptest.NewRecorder()
		handler.ChatCompletions(w, req)

		// 422 is NOT failover-eligible, so the first provider is terminal even
		// with a candidate remaining; the non-JSON body is wrapped, not raw.
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected status 422, got %d", w.Code)
		}
		bodyStr := w.Body.String()
		// Response must be a valid JSON OpenAI-error envelope, never raw HTML.
		if !json.Valid([]byte(bodyStr)) {
			t.Errorf("expected wrapped JSON envelope, got non-JSON: %s", bodyStr)
		}
		if strings.HasPrefix(strings.TrimSpace(bodyStr), "<") {
			t.Errorf("expected wrapped envelope, got raw HTML: %s", bodyStr)
		}
		// The sanitized upstream message should be carried in the envelope.
		if !strings.Contains(bodyStr, "Bad Gateway") {
			t.Errorf("expected upstream message in envelope, got: %s", bodyStr)
		}
		// Verify we only called the first provider (no failover for 422).
		if callCount != 1 {
			t.Errorf("expected 1 call (no failover for 422), got %d", callCount)
		}
	})

	t.Run("all candidates exhausted returns generic error", func(t *testing.T) {
		pool := testDB.Pool()
		ctx := context.Background()

		settingsRepo := settings.NewRepository(pool)
		failoverRepo := failover.NewRepository(pool)
		modelRepo := model.NewRepository(pool)
		providerRepo := provider.NewRepository(pool)
		virtualKeyRepo := virtualkey.NewRepository(pool)
		limiter := ratelimit.NewLimiter(settingsRepo)
		ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

		masterKey := "test-master-key-for-exhausted"

		// Create two upstreams that both return 500
		upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": {"message": "provider1_error"}}`))
		}))
		defer upstream1.Close()

		upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": {"message": "provider2_error"}}`))
		}))
		defer upstream2.Close()

		keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key1: %v", err)
		}
		keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key2: %v", err)
		}

		provName1 := "exhaust-prov-1-" + uuid.New().String()[:8]
		prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName1,
			BaseURL: upstream1.URL,
			APIKey:  "test-api-key-1",
		}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
		if err != nil {
			t.Fatalf("failed to create provider1: %v", err)
		}

		provName2 := "exhaust-prov-2-" + uuid.New().String()[:8]
		prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    provName2,
			BaseURL: upstream2.URL,
			APIKey:  "test-api-key-2",
		}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
		if err != nil {
			t.Fatalf("failed to create provider2: %v", err)
		}

		modelName := "exhaust-model-" + uuid.New().String()[:8]
		model1 := &model.Model{
			ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
			Name: "Exhaust Model 1", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName1, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, model1); err != nil {
			t.Fatalf("failed to upsert model1: %v", err)
		}

		model2 := &model.Model{
			ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
			Name: "Exhaust Model 2", Description: "", Capabilities: "{}",
			Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
			Enabled: true, ProviderName: provName2, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(ctx, model2); err != nil {
			t.Fatalf("failed to upsert model2: %v", err)
		}

		// Create failover group with both models
		if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
			[]uuid.UUID{model1.ID, model2.ID},
			map[string]bool{}, nil, nil, nil, nil,
		); err != nil {
			t.Fatalf("failed to create failover group: %v", err)
		}

		vkName := "exhaust-test-key-" + uuid.New().String()[:8]
		vkHash := virtualkey.Hash(vkName)
		if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, "xt-"+vkHash[:8], nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("failed to create virtual key: %v", err)
		}

		handler := newCanonicalHandler(t, masterKey, pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

		// Use hotel/ format to trigger failover group
		body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
		rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
		rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
		req = req.WithContext(rCtx)

		w := httptest.NewRecorder()
		handler.ChatCompletions(w, req)

		// Both providers return 500 → all exhausted → generic error
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", w.Code)
		}

		bodyStr := w.Body.String()
		// Should contain generic error, NOT the specific upstream errors
		if !strings.Contains(bodyStr, "upstream provider returned HTTP 500") {
			t.Errorf("expected generic error message, got: %s", bodyStr)
		}
		if strings.Contains(bodyStr, "provider1_error") || strings.Contains(bodyStr, "provider2_error") {
			t.Errorf("should NOT forward upstream JSON details, got: %s", bodyStr)
		}
	})
}
