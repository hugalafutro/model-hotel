package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// Test discoverOllama with mock server
func TestDiscoverOllama(t *testing.T) {
	tagsResponse := `{
		"models": [
			{"name": "llama3"},
			{"name": "mistral"}
		]
	}`

	showResponse := `{
		"name": "llama3",
		"details": {"family": "llama3"},
		"parameters": "temperature",
		"template": "chat",
		"capabilities": ["vision"],
		"model_info": {"llama3.context_length": 8192}
	}`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(tagsResponse))
		case "/api/show":
			w.Write([]byte(showResponse))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverOllama(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOllama failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "llama3" {
		t.Errorf("expected model ID 'llama3', got '%s'", models[0].ModelID)
	}

	var caps model.Capability
	json.Unmarshal([]byte(models[0].Capabilities), &caps)
	if !caps.Vision {
		t.Error("expected Vision=true for llama3")
	}

	if models[0].ContextLength == nil || *models[0].ContextLength != 8192 {
		t.Errorf("expected ContextLength 8192, got %v", models[0].ContextLength)
	}
}

// Test discoverNanoGPT with mock server
func TestDiscoverNanoGPT(t *testing.T) {
	mockResponse := `{
		"data": [
			{
				"id": "gpt-4",
				"name": "GPT-4",
				"description": "Test model",
				"owned_by": "test",
				"capabilities": {
					"vision": true,
					"reasoning": true,
					"tool_calling": true
				},
				"architecture": {
					"modality": "text",
					"input_modalities": ["text"],
					"output_modalities": ["text"]
				},
				"pricing": {
					"prompt": 0.03,
					"completion": 0.06
				},
				"context_length": 8192,
				"max_output_tokens": 4096
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" && r.URL.Query().Get("detailed") == "true" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverNanoGPT(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	m := models[0]
	if m.ModelID != "gpt-4" {
		t.Errorf("expected model ID 'gpt-4', got '%s'", m.ModelID)
	}

	if m.DisplayName != "GPT-4" {
		t.Errorf("expected DisplayName 'GPT-4', got '%s'", m.DisplayName)
	}

	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 0.03 {
		t.Errorf("expected InputPricePerMillion 0.03, got %v", m.InputPricePerMillion)
	}

	if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 0.06 {
		t.Errorf("expected OutputPricePerMillion 0.06, got %v", m.OutputPricePerMillion)
	}

	var caps model.Capability
	json.Unmarshal([]byte(m.Capabilities), &caps)
	if !caps.Vision {
		t.Error("expected Vision=true")
	}
	if !caps.Reasoning {
		t.Error("expected Reasoning=true")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true")
	}
}

// Test discoverOpenCodeGo with mock server
// Test discoverOpenCodeGo with 404 fallback to catalog
func TestDiscoverOpenCodeGo_404Fallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo should not fail on 404 (should fallback to catalog): %v", err)
	}

	// Should return catalog models on 404
	if len(models) == 0 {
		t.Error("expected catalog models on 404, got 0")
	}
}

// Test discoverOpenCodeGo with non-200 status
func TestDiscoverOpenCodeGo_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	_, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
		return
	}
}

// Test discoverOpenCodeGo with invalid JSON response
func TestDiscoverOpenCodeGo_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json }"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	_, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
		return
	}
}

// Test discoverOpenCodeGo with empty response
func TestDiscoverOpenCodeGo_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data": [], "object": "list"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}
	// Empty-but-successful listing returns empty (no catalog union), so the
	// discovered set stays empty and DisableMissingModels is a no-op.
	if len(models) != 0 {
		t.Errorf("expected 0 models for empty live response, got %d", len(models))
	}
}

func TestDiscoverOpenCodeGo(t *testing.T) {
	mockResponse := `{
		"data": [
			{
				"id": "gpt-4",
				"object": "model",
				"owned_by": "test",
				"created": 1700000000
			}
		],
		"object": "list"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}

	// Live "gpt-4" (not in catalog) unions with the catalog.
	if len(models) != len(GetOpenCodeGoCatalog())+1 {
		t.Fatalf("expected catalog+1 merged models, got %d", len(models))
	}

	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "gpt-4" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected live 'gpt-4' present in merged results")
	}
	if m.OwnedBy != "test" {
		t.Errorf("expected OwnedBy 'test', got '%s'", m.OwnedBy)
	}
}

// Test discoverOpenCodeGo with a model that's not in the catalog
func TestDiscoverOpenCodeGo_UnknownModelNotInCatalog(t *testing.T) {
	mockResponse := `{
		"data": [
			{
				"id": "totally-unknown-model-not-in-catalog",
				"object": "model",
				"owned_by": "unknown-vendor",
				"created": 1700000000
			}
		],
		"object": "list"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}

	// Unknown live model unions with the catalog.
	if len(models) != len(GetOpenCodeGoCatalog())+1 {
		t.Fatalf("expected catalog+1 merged models, got %d", len(models))
	}

	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "totally-unknown-model-not-in-catalog" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected unknown live model present in merged results")
	}
	if m.OwnedBy != "unknown-vendor" {
		t.Errorf("expected OwnedBy 'unknown-vendor', got '%s'", m.OwnedBy)
	}
	if !m.Enabled {
		t.Error("expected model to be enabled")
	}
}

// Test discoverOpenCodeGo with a mix of catalog and unknown models
func TestDiscoverOpenCodeGo_MixedCatalogAndUnknown(t *testing.T) {
	mockResponse := `{
		"data": [
			{
				"id": "totally-unknown-model-xyz",
				"object": "model",
				"owned_by": "unknown",
				"created": 1700000000
			}
		],
		"object": "list"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeGo(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}

	// Unknown live model unions with the catalog.
	if len(models) != len(GetOpenCodeGoCatalog())+1 {
		t.Fatalf("expected catalog+1 merged models, got %d", len(models))
	}
	var unknown *model.Model
	for _, mm := range models {
		if mm.ModelID == "totally-unknown-model-xyz" {
			unknown = mm
		}
	}
	if unknown == nil {
		t.Fatal("expected unknown live model present in merged results")
	}
	// Unknown model should get a minimal entry with streaming capability
	var caps model.Capability
	if err := json.Unmarshal([]byte(unknown.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("Expected Streaming capability to be true for unknown model")
	}
}

// Test discoverOpenCodeZen with non-200 status
func TestDiscoverOpenCodeZen_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	_, err := svc.discoverOpenCodeZen(ctx, provider, "test-key")
	if err == nil {
		t.Fatal("expected error for 400 status, got nil")
		return
	}
}

// Test discoverOpenCodeZen with invalid JSON response
func TestDiscoverOpenCodeZen_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json }"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	_, err := svc.discoverOpenCodeZen(ctx, provider, "test-key")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
		return
	}
}

// Test discoverOpenCodeZen with keyless mode filtering paid models
func TestDiscoverOpenCodeZen_KeylessFiltersPaidModels(t *testing.T) {
	// Mock response with a paid model (non-zero pricing) and a catalog model
	// big-pickle is in the OpenCode Zen catalog as a free model
	mockResponse := `{
		"data": [
			{
				"id": "paid-model",
				"object": "model",
				"owned_by": "opencode",
				"created": 1700000000,
				"pricing": {
					"prompt": "0.01",
					"completion": "0.02"
				}
			},
			{
				"id": "big-pickle",
				"object": "model",
				"owned_by": "opencode",
				"created": 1700000000,
				"pricing": {
					"prompt": "0.00",
					"completion": "0.00"
				}
			}
		],
		"object": "list"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{}, // Empty key = keyless mode
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeZen(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeZen failed: %v", err)
	}

	// Should only return free catalog models in keyless mode
	if len(models) != 1 {
		t.Errorf("expected 1 free model in keyless mode, got %d", len(models))
	}
	if len(models) > 0 && models[0].ModelID != "big-pickle" {
		t.Errorf("expected big-pickle, got %s", models[0].ModelID)
	}
}

// Test discoverOpenCodeZen with empty response
func TestDiscoverOpenCodeZen_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data": [], "object": "list"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeZen(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeZen failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models for empty response, got %d", len(models))
	}
}

func TestDiscoverOpenCodeZen(t *testing.T) {
	mockResponse := `{
		"data": [
			{
				"id": "big-pickle",
				"object": "model",
				"owned_by": "opencode",
				"created": 1700000000,
				"pricing": {
					"prompt": "0.00",
					"completion": "0.00"
				}
			}
		],
		"object": "list"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{}, // Empty key to trigger keyless mode
	}

	ctx := context.Background()
	models, err := svc.discoverOpenCodeZen(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeZen failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	m := models[0]
	if m.ModelID != "big-pickle" {
		t.Errorf("expected model ID 'big-pickle', got '%s'", m.ModelID)
	}
	if m.OwnedBy != "opencode" {
		t.Errorf("expected OwnedBy 'opencode', got '%s'", m.OwnedBy)
	}
}

// Test discoverZAICoding against a mock live /models endpoint: live models are
// merged with the embedded catalog (live wins, catalog backfills + unions).
func TestDiscoverZAICoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-5.1","object":"model","owned_by":"z-ai"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: &http.Client{Transport: &testTransport{url: server.URL}}}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.z.ai/api/coding/paas/v4",
	}

	ctx := context.Background()
	models, err := svc.discoverZAICoding(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("expected at least one model from ZAI discovery")
	}

	// Find the live-listed glm-5.1 and verify catalog backfill + owner mapping.
	var live51 *model.Model
	for _, m := range models {
		if m.ModelID == "glm-5.1" {
			live51 = m
		}
	}
	if live51 == nil {
		t.Fatal("expected glm-5.1 from live listing")
	}
	if live51.OwnedBy != "zhipu" {
		t.Errorf("expected OwnedBy 'zhipu' (z-ai normalized), got '%s'", live51.OwnedBy)
	}
	if live51.ContextLength == nil || *live51.ContextLength <= 0 {
		t.Error("expected ContextLength backfilled from catalog")
	}
	var caps model.Capability
	json.Unmarshal([]byte(live51.Capabilities), &caps)
	if !caps.Streaming {
		t.Error("expected Streaming=true after catalog backfill")
	}

	// glm-5.2 is catalog-only (not in the live list) and must be unioned in.
	var found52 bool
	for _, m := range models {
		if m.ModelID == "glm-5.2" {
			found52 = true
		}
	}
	if !found52 {
		t.Error("expected catalog-only glm-5.2 to be unioned into discovery results")
	}
}

// Test GetNanoGPTUsage with mock server
func TestGetNanoGPTUsage(t *testing.T) {
	mockResponse := `{
		"active": true,
		"provider": "test-provider",
		"providerStatus": "active",
		"providerStatusRaw": "active",
		"stripeSubscriptionId": "sub_123",
		"cancellationReason": null,
		"canceledAt": null,
		"endedAt": null,
		"cancelAt": null,
		"cancelAtPeriodEnd": false,
		"limits": {
			"weeklyInputTokens": 1000000,
			"dailyInputTokens": 100000,
			"dailyImages": 100
		},
		"allowOverage": false,
		"period": {
			"currentPeriodEnd": "2024-12-31"
		},
		"dailyImages": {
			"used": 10,
			"remaining": 90,
			"percentUsed": 10.0,
			"resetAt": 1735689600
		},
		"dailyInputTokens": {
			"used": 1000,
			"remaining": 99000,
			"percentUsed": 1.0,
			"resetAt": 1735689600
		},
		"weeklyInputTokens": {
			"used": 5000,
			"remaining": 995000,
			"percentUsed": 0.5,
			"resetAt": 1735689600
		},
		"state": "active",
		"graceUntil": null
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/usage" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}

	// Create properly encrypted key for testing
	masterKey := "test-master-key"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	ctx := context.Background()
	usage, err := svc.GetNanoGPTUsage(ctx, provider, masterKey)
	if err != nil {
		t.Fatalf("GetNanoGPTUsage failed: %v", err)
	}

	if usage == nil {
		t.Fatal("expected non-nil usage")
		return
	}

	if usage.DailyInputTokens == nil || usage.DailyInputTokens.Used != 1000 {
		t.Errorf("expected DailyInputTokens.Used 1000, got %v", usage.DailyInputTokens)
	}
}

// Test GetZAICodingQuota - requires live API call since the function uses hardcoded URL
// Test GetZAICodingQuota - success path with mocked server
// Test GetZAICodingQuota_DecryptionFailure tests the decryption error path
func TestGetZAICodingQuota_DecryptionFailure(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}

	// Create provider with encrypted key but wrong master key (will fail decryption)
	masterKey := "test-master-key"
	apiKey := "test-api-key"

	// Encrypt with one key
	keyPair, err := auth.Encrypt(apiKey, "different-master-key")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.z.ai",
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	ctx := context.Background()
	_, err = svc.GetZAICodingQuota(ctx, provider, masterKey)
	if err == nil {
		t.Fatal("expected error for wrong master key, got nil")
		return
	}
	if !strings.Contains(err.Error(), "failed to decrypt API key") {
		t.Errorf("expected decryption error, got: %v", err)
	}
}

// Note: GetZAICodingQuota always requires an encrypted API key - ZAI is not a keyless provider.
// Empty encrypted keys are not a valid use case for this function.

func TestGetZAICodingQuota(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live API test in short mode")
	}
	if os.Getenv("LIVE_API_TESTS") == "" {
		t.Skip("skipping live API test (set LIVE_API_TESTS=1 to enable)")
	}

	apiKey := os.Getenv("ZAI_CODING_API_KEY")
	if apiKey == "" {
		t.Skip("ZAI_CODING_API_KEY environment variable is required for live API tests")
	}

	svc := &DiscoveryService{httpClient: http.DefaultClient}

	// Create properly encrypted key for testing
	masterKey := "test-master-key"

	// Encrypt the API key
	keyPair, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.z.ai",
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	ctx := context.Background()
	quota, err := svc.GetZAICodingQuota(ctx, provider, masterKey)
	if err != nil {
		t.Fatalf("GetZAICodingQuota failed: %v", err)
	}

	if quota == nil {
		t.Fatal("expected non-nil quota")
		return
	}

	if len(quota.Data.Limits) == 0 {
		t.Error("expected at least one limit in quota response")
	}

	t.Logf("ZAI Coding quota test passed - %d limits found", len(quota.Data.Limits))
}

func TestGetZAICodingQuota_MockServer(t *testing.T) {
	t.Parallel()

	// Create test server with mock ZAI Coding quota response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/monitor/usage/quota/limit" && r.Method == "GET" {
			response := ZAICodingQuotaResponse{
				Code:    0,
				Msg:     "success",
				Success: true,
				Data: ZAICodingQuotaData{
					Level: "level-1",
					Limits: []ZAICodingQuotaLimit{
						{
							Type:          "daily",
							Unit:          1,
							Number:        1000000,
							Usage:         10000,
							CurrentValue:  990000,
							Remaining:     990000,
							Percentage:    1.0,
							NextResetTime: 1735689600,
							UsageDetails: []ZAICodingQuotaUsageDetail{
								{ModelCode: "glm-4-flash", Usage: 5000},
								{ModelCode: "glm-4-air", Usage: 5000},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use custom transport to redirect requests to mock server
	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}

	// Create provider with encrypted key
	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	quota, err := service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetZAICodingQuota failed: %v", err)
	}

	if quota == nil {
		t.Fatal("expected non-nil quota")
		return
	}

	if !quota.Success {
		t.Error("expected Success=true")
	}
	if quota.Code != 0 {
		t.Errorf("expected Code=0, got %d", quota.Code)
	}
	if quota.Msg != "success" {
		t.Errorf("expected Msg='success', got '%s'", quota.Msg)
	}
	if len(quota.Data.Limits) != 1 {
		t.Errorf("expected 1 limit, got %d", len(quota.Data.Limits))
	}
	if quota.Data.Limits[0].Type != "daily" {
		t.Errorf("expected Type='daily', got '%s'", quota.Data.Limits[0].Type)
	}
	if quota.Data.Limits[0].Remaining != 990000 {
		t.Errorf("expected Remaining=990000, got %d", quota.Data.Limits[0].Remaining)
	}
	if len(quota.Data.Limits[0].UsageDetails) != 2 {
		t.Errorf("expected 2 usage details, got %d", len(quota.Data.Limits[0].UsageDetails))
	}
}

func TestGetZAICodingQuota_Non200Status(t *testing.T) {
	t.Parallel()

	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/monitor/usage/quota/limit" {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use custom transport to redirect requests to mock server
	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
		retryBaseDelay: time.Millisecond,
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("Expected error for non-200 status, got nil")
		return
	}
}

func TestGetZAICodingQuota_InvalidJSON(t *testing.T) {
	t.Parallel()

	// Create test server that returns 200 with invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/monitor/usage/quota/limit" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("this is not valid json"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
		return
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("expected decode error, got: %v", err)
	}
}
