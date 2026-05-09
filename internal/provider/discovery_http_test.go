package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
	if len(models) != 0 {
		t.Errorf("expected 0 models for empty response, got %d", len(models))
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

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	m := models[0]
	if m.ModelID != "gpt-4" {
		t.Errorf("expected model ID 'gpt-4', got '%s'", m.ModelID)
	}
	if m.OwnedBy != "test" {
		t.Errorf("expected OwnedBy 'test', got '%s'", m.OwnedBy)
	}
}

// Test discoverOpenCodeZen with mock server
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

// Test discoverZAICoding (uses static catalog, no HTTP needed)
func TestDiscoverZAICoding(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.z.ai",
	}

	ctx := context.Background()
	models, err := svc.discoverZAICoding(ctx, provider, "test-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("expected at least one model from ZAI catalog")
	}

	// Check first model has required fields
	m := models[0]
	if m.ModelID == "" {
		t.Error("expected ModelID to be set")
	}
	if m.OwnedBy != "zhipu" {
		t.Errorf("expected OwnedBy 'zhipu', got '%s'", m.OwnedBy)
	}

	var caps model.Capability
	json.Unmarshal([]byte(m.Capabilities), &caps)
	if !caps.Streaming {
		t.Error("expected Streaming=true")
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

	// Use v1 encryption (no salt) for simplicity in tests
	keyV1 := auth.DeriveKey(masterKey)
	block, _ := aes.NewCipher(keyV1)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	copy(nonce, "test-nonce-12") // Fixed nonce for test

	ciphertext := gcm.Seal(nil, nonce, []byte(apiKey), nil)

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: ciphertext,
		KeyNonce:     nonce,
		KeySalt:      nil, // Use v1 (no salt)
	}

	ctx := context.Background()
	usage, err := svc.GetNanoGPTUsage(ctx, provider, masterKey)
	if err != nil {
		t.Fatalf("GetNanoGPTUsage failed: %v", err)
	}

	if usage == nil {
		t.Fatal("expected non-nil usage")
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
	}

	if len(quota.Data.Limits) == 0 {
		t.Error("expected at least one limit in quota response")
	}

	t.Logf("ZAI Coding quota test passed - %d limits found", len(quota.Data.Limits))
}
