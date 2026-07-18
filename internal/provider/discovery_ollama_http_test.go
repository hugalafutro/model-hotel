package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestDiscoverOllama_HTTP(t *testing.T) {
	// Create test server with mock Ollama tags response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/tags" && r.Method == "GET":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "llama3.2"},
					{Name: "mistral"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case r.URL.Path == "/api/show" && r.Method == "POST":
			// Mock show response
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo: map[string]any{
					"llama.context_length": float64(8192),
				},
				Details: OllamaShowDetails{
					Family: "llama",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverOllama(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOllama failed: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "llama3.2" {
		t.Errorf("Expected model ID 'llama3.2', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "llama" {
		t.Errorf("Expected ownedBy 'llama', got '%s'", models[0].OwnedBy)
	}

	if *models[0].ContextLength != 8192 {
		t.Errorf("Expected context length 8192, got %d", *models[0].ContextLength)
	}
	// Context length is a live /api/show probe, so it must be marked live (a
	// larger-context pull propagates and is reported); prices stay fill-only.
	if !models[0].LiveMeta.ContextLength {
		t.Error("Expected LiveMeta.ContextLength=true for the live /api/show context length")
	}
	if models[0].LiveMeta.InputPrice || models[0].LiveMeta.OutputPrice {
		t.Errorf("Expected no live price flags for Ollama (no wire pricing), got %+v", models[0].LiveMeta)
	}

	// Check capabilities
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities: %v", err)
	} else {
		if !caps.Streaming {
			t.Error("Expected streaming capability to be true")
		}
		if !caps.ToolCalling {
			t.Error("Expected tool calling capability to be true")
		}
	}
}

func TestDiscoverOllama_Non200Status(t *testing.T) {
	assertDiscoverHTTPError(t, "non-200 status", errorStatusHandler(http.StatusInternalServerError),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverOllama(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverOllama_InvalidJSON(t *testing.T) {
	assertDiscoverHTTPError(t, "invalid JSON", invalidJSONHandler(),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverOllama(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverOllama_ContextCancelled(t *testing.T) {
	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		switch r.URL.Path {
		case "/api/tags":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "llama3.2"},
				},
			}
			json.NewEncoder(w).Encode(response)
		case "/api/show":
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo: map[string]any{
					"llama.context_length": float64(8192),
				},
				Details: OllamaShowDetails{
					Family: "llama",
				},
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := service.discoverOllama(ctx, provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestOllamaShowModel_Non200Status(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Model not found", http.StatusNotFound)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	_, err := service.ollamaShowModel(context.Background(), server.URL, "test-api-key", "nonexistent-model")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
}

func TestOllamaShowModel_InvalidJSON(t *testing.T) {
	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	_, err := service.ollamaShowModel(context.Background(), server.URL, "test-api-key", "test-model")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestOllamaShowModel_Success(t *testing.T) {
	// Create test server with valid response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OllamaShowResponse{
			Capabilities: []string{"tools", "vision"},
			ModelInfo: map[string]any{
				"llama.context_length": float64(16384),
			},
			Details: OllamaShowDetails{
				Family: "mistral",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	show, err := service.ollamaShowModel(context.Background(), server.URL, "test-api-key", "test-model")
	if err != nil {
		t.Fatalf("ollamaShowModel failed: %v", err)
	}

	if len(show.Capabilities) != 2 {
		t.Errorf("Expected 2 capabilities, got %d", len(show.Capabilities))
	}

	if show.Details.Family != "mistral" {
		t.Errorf("Expected family 'mistral', got '%s'", show.Details.Family)
	}

	ctxLen := show.ModelInfo["llama.context_length"]
	if ctxLen != float64(16384) {
		t.Errorf("Expected context length 16384, got %v", ctxLen)
	}
}

func TestGetOllamaCloudAccount_Success(t *testing.T) {
	t.Parallel()

	// Create test server with mock Ollama Cloud account response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" && r.Method == "POST" {
			response := OllamaCloudAccount{
				ID:    "test-user-id",
				Email: "test@example.com",
				Name:  "Test User",
				Plan:  "pro",
				CustomerID: OllamaCloudNullableString{
					String: "cus_test123",
					Valid:  true,
				},
				SubscriptionID: OllamaCloudNullableString{
					String: "sub_test456",
					Valid:  true,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
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
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	account, err := service.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetOllamaCloudAccount failed: %v", err)
	}

	if account.ID != "test-user-id" {
		t.Errorf("Expected ID 'test-user-id', got '%s'", account.ID)
	}
	if account.Email != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got '%s'", account.Email)
	}
	if account.Name != "Test User" {
		t.Errorf("Expected name 'Test User', got '%s'", account.Name)
	}
	if account.Plan != "pro" {
		t.Errorf("Expected plan 'pro', got '%s'", account.Plan)
	}
	if !account.CustomerID.Valid || account.CustomerID.String != "cus_test123" {
		t.Errorf("Expected customer ID 'cus_test123', got %+v", account.CustomerID)
	}
}

func TestGetOllamaCloudAccount_DecryptionFailure(t *testing.T) {
	t.Parallel()

	service := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	// Create provider with invalid encrypted key
	// Use properly sized nonce (12 bytes) and salt (32 bytes) for AES-GCM
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://ollama.com/v1",
		EncryptedKey: []byte("invalid-encrypted-key"),
		KeyNonce:     make([]byte, 12), // Proper nonce length
		KeySalt:      make([]byte, 32), // Proper salt length
	}

	_, err := service.GetOllamaCloudAccount(context.Background(), provider, "wrong-master-key")
	if err == nil {
		t.Fatal("Expected error for invalid encrypted key, got nil")
		return
	}
	if !strings.Contains(err.Error(), "failed to decrypt API key") {
		t.Errorf("Expected decryption error, got: %v", err)
	}
}

func TestGetOllamaCloudAccount_Non200Status(t *testing.T) {
	t.Parallel()

	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient:     server.Client(),
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
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("Expected error for non-200 status, got nil")
		return
	}
}

func TestGetOllamaCloudAccount_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json "))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
		return
	}
	if !strings.Contains(err.Error(), "failed to decode account response") {
		t.Errorf("Expected decode error, got: %v", err)
	}
}

func TestGetOllamaCloudAccount_V1SuffixStripped(t *testing.T) {
	t.Parallel()

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.URL.Path == "/api/me" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OllamaCloudAccount{ID: "user-1", Plan: "free"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetOllamaCloudAccount failed: %v", err)
	}
	// The /v1 suffix should be stripped so the request goes to /api/me not /v1/api/me
	if requestedPath != "/api/me" {
		t.Errorf("Expected request to /api/me, got %s", requestedPath)
	}
}

func TestDiscoverOllama_ShowModelFails(t *testing.T) {
	// Test that a failed show request results in the model being skipped rather than erroring
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "good-model"},
					{Name: "bad-model"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case "/api/show":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] == "bad-model" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo:    map[string]any{"llama.context_length": float64(8192)},
				Details:      OllamaShowDetails{Family: "llama"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverOllama(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOllama should not fail when show fails for individual models: %v", err)
	}
	// Both models must be returned: bad-model is listed by /api/tags, so a
	// failed detail probe keeps it with default metadata instead of dropping
	// it (a dropped model would be disabled as "missing" by the scan).
	if len(models) != 2 {
		t.Fatalf("Expected 2 models (bad-model kept with default metadata), got %d", len(models))
	}
	byID := map[string]*model.Model{}
	for _, m := range models {
		byID[m.ModelID] = m
	}
	good, bad := byID["good-model"], byID["bad-model"]
	if good == nil || bad == nil {
		t.Fatalf("expected good-model and bad-model, got %v", models)
	}
	if good.ContextLength == nil || *good.ContextLength != 8192 {
		t.Errorf("expected good-model context length 8192, got %v", good.ContextLength)
	}
	if bad.ContextLength != nil {
		t.Errorf("expected bad-model context length nil (fill-only), got %v", *bad.ContextLength)
	}
	if !bad.Enabled {
		t.Error("expected bad-model to stay enabled")
	}
}

func TestDiscoverOllama_VisionCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{{Name: "vision-model"}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case "/api/show":
			response := OllamaShowResponse{
				Capabilities: []string{"vision", "tools"},
				ModelInfo:    map[string]any{"llama.context_length": float64(16384)},
				Details:      OllamaShowDetails{Family: "llama"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverOllama(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOllama failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Vision {
		t.Error("Expected Vision capability to be true for vision model")
	}
	if !caps.ToolCalling {
		t.Error("Expected ToolCalling capability to be true")
	}
	NormalizeModels(models)
	if models[0].Modality != "chat" {
		t.Errorf("Expected derived class 'chat', got '%s'", models[0].Modality)
	}
	if models[0].InputModalities != `["text","image"]` {
		t.Errorf("Expected input modalities [\"text\",\"image\"], got '%s'", models[0].InputModalities)
	}
}

func TestBuildOllamaModel_EmptyFamilyHTTP(t *testing.T) {
	service := &DiscoveryService{}
	show := &OllamaShowResponse{
		Capabilities: []string{},
		ModelInfo:    map[string]any{},
		Details:      OllamaShowDetails{Family: ""},
	}
	provider := &Provider{ID: uuid.New()}

	m := service.buildOllamaModel(provider, "test-model", show)
	if m.OwnedBy != "ollama" {
		t.Errorf("Expected OwnedBy 'ollama' when family is empty, got '%s'", m.OwnedBy)
	}
}

func TestBuildOllamaModel_ContextLengthFromModelInfoHTTP(t *testing.T) {
	service := &DiscoveryService{}
	show := &OllamaShowResponse{
		Capabilities: []string{},
		ModelInfo: map[string]any{
			"llama.context_length": float64(32768),
		},
		Details: OllamaShowDetails{Family: "llama"},
	}
	provider := &Provider{ID: uuid.New()}

	m := service.buildOllamaModel(provider, "test-model", show)
	if m.ContextLength == nil || *m.ContextLength != 32768 {
		t.Errorf("Expected ContextLength 32768, got %v", m.ContextLength)
	}
}

func TestBuildOllamaModel_ThinkingCapabilityHTTP(t *testing.T) {
	service := &DiscoveryService{}
	show := &OllamaShowResponse{
		Capabilities: []string{"thinking"},
		ModelInfo:    map[string]any{},
		Details:      OllamaShowDetails{Family: "llama"},
	}
	provider := &Provider{ID: uuid.New()}

	m := service.buildOllamaModel(provider, "test-model", show)
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Reasoning {
		t.Error("Expected Reasoning capability to be true for thinking model")
	}
}

func TestBuildOllamaModel_EmbeddingCapabilityHTTP(t *testing.T) {
	service := &DiscoveryService{}
	// Ollama reports an embedding-only model as capabilities:["embedding"] with
	// no "completion" — it must be classified as embedding, not text, so it is
	// hidden from the chat picker.
	show := &OllamaShowResponse{
		Capabilities: []string{"embedding"},
		ModelInfo:    map[string]any{},
		Details:      OllamaShowDetails{Family: "nomic-bert"},
	}
	provider := &Provider{ID: uuid.New()}

	m := service.buildOllamaModel(provider, "nomic-embed-text", show)
	NormalizeModelClassification(m)
	if m.Modality != "embedding" {
		t.Errorf("Expected Modality 'embedding', got '%s'", m.Modality)
	}
	if m.OutputModalities != `["embedding"]` {
		t.Errorf("Expected OutputModalities '[\"embedding\"]', got '%s'", m.OutputModalities)
	}
}

func TestBuildOllamaModel_CompletionStaysTextHTTP(t *testing.T) {
	service := &DiscoveryService{}
	// A normal chat model reports "completion"; even if its name happened to
	// contain an embedding-ish token, an authoritative completion capability
	// keeps it as a chat model.
	show := &OllamaShowResponse{
		Capabilities: []string{"completion", "tools"},
		ModelInfo:    map[string]any{},
		Details:      OllamaShowDetails{Family: "llama"},
	}
	provider := &Provider{ID: uuid.New()}

	m := service.buildOllamaModel(provider, "llama3-embed-tutor", show)
	NormalizeModelClassification(m)
	if m.Modality != "chat" {
		t.Errorf("Expected class 'chat' for a completion model, got '%s'", m.Modality)
	}
}

func TestBuildOllamaModel_EmbeddingByNameFallbackHTTP(t *testing.T) {
	service := &DiscoveryService{}
	// Older Ollama returns no capabilities at all; fall back to the name
	// heuristic so an embedding model is still caught.
	show := &OllamaShowResponse{
		Capabilities: []string{},
		ModelInfo:    map[string]any{},
		Details:      OllamaShowDetails{Family: "bert"},
	}
	provider := &Provider{ID: uuid.New()}

	m := service.buildOllamaModel(provider, "mxbai-embed-large", show)
	NormalizeModelClassification(m)
	if m.Modality != "embedding" {
		t.Errorf("Expected Modality 'embedding' from name fallback, got '%s'", m.Modality)
	}
}

func TestGetOllamaCloudAccount_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(OllamaCloudAccount{ID: "user-1", Plan: "free"})
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err = service.GetOllamaCloudAccount(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for context cancellation, got nil")
	}
}

func TestGetOllamaCloudAccount_SuccessWithoutV1Suffix(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.URL.Path == "/api/me" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OllamaCloudAccount{ID: "user-1", Plan: "pro"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Test without /v1 suffix - should still hit /api/me
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetOllamaCloudAccount failed: %v", err)
	}
	if requestedPath != "/api/me" {
		t.Errorf("Expected request to /api/me, got %s", requestedPath)
	}
}

func TestGetOllamaCloudAccount_403Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient:     server.Client(),
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
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for 403 response, got nil")
	}
}
