package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// =============================================================================
// Anthropic Discovery Tests
// =============================================================================

func TestAnthropicDiscovery_Non200Status(t *testing.T) {
	t.Parallel()

	// Create test server that returns 403
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := svc.discoverAnthropic(context.Background(), provider, "test-key")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status code") {
		t.Errorf("Expected 'unexpected status code' error, got: %v", err)
	}
}

func TestAnthropicDiscovery_JSONDecodeError(t *testing.T) {
	t.Parallel()

	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := svc.discoverAnthropic(context.Background(), provider, "test-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected 'failed to decode response' error, got: %v", err)
	}
}

func TestAnthropicDiscovery_RequestCreationError(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.anthropic.com",
	}

	// Create cancelled context to trigger request creation error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := svc.discoverAnthropic(ctx, provider, "test-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestAnthropicDiscovery_PDFWithoutVision(t *testing.T) {
	t.Parallel()

	// Model with PDF capability but NOT vision - should trigger the modality switch
	pageResponse := `{
		"data": [
			{
				"id": "claude-pdf-only",
				"type": "model",
				"display_name": "Claude PDF Only",
				"created_at": "2025-01-01T00:00:00Z",
				"max_input_tokens": 200000,
				"max_tokens": 32768,
				"capabilities": {
					"image_input": {"supported": false},
					"pdf_input": {"supported": true},
					"structured_outputs": {"supported": false},
					"batch": {"supported": false},
					"citations": {"supported": false},
					"code_execution": {"supported": false}
				}
			}
		],
		"has_more": false,
		"first_id": "claude-pdf-only",
		"last_id": "claude-pdf-only"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(pageResponse))
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := svc.discoverAnthropic(context.Background(), provider, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	m := models[0]
	// Should have PDFUpload capability
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.PDFUpload {
		t.Error("Expected PDFUpload=true for model with pdf_input capability")
	}
	// Modality should be switched to "vision" even though image_input is false
	if m.Modality != "vision" {
		t.Errorf("Expected modality 'vision' for PDF-capable model, got '%s'", m.Modality)
	}
	// Input modalities should include image
	if !strings.Contains(m.InputModalities, "image") {
		t.Errorf("Expected input modalities to include 'image', got '%s'", m.InputModalities)
	}
}

// =============================================================================
// Cohere Discovery Tests
// =============================================================================

func TestDiscoverCohere_RequestCreationError(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.cohere.ai/compatibility/v1",
	}

	// Create cancelled context to trigger request creation error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := svc.discoverCohere(ctx, provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestDiscoverCohere_ReadBodyError(t *testing.T) {
	t.Parallel()

	// Create a custom RoundTripper that returns a response with failing body
	errorRoundTripper := &errorBodyRoundTripper{}
	client := &http.Client{
		Transport: errorRoundTripper,
	}

	svc := &DiscoveryService{
		httpClient: client,
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.cohere.ai/compatibility/v1",
	}

	_, err := svc.discoverCohere(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for body read failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read response") {
		t.Errorf("Expected 'failed to read response' error, got: %v", err)
	}
}

// errorBodyRoundTripper returns a response with a body that fails on read
type errorBodyRoundTripper struct{}

func (e *errorBodyRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(&failingReader{}),
	}, nil
}

// failingReader returns data once, then errors
type failingReader struct{}

func (f *failingReader) Read(p []byte) (int, error) {
	// Write valid JSON first
	data := []byte(`{"models":[],"next_page_token":""}`)
	if len(p) >= len(data) {
		copy(p, data)
		return len(data), nil
	}
	// On second read, return error
	return 0, io.ErrUnexpectedEOF
}

func TestDiscoverCohere_JSONDecodeError(t *testing.T) {
	t.Parallel()

	// Create test server with 200 status but invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json for cohere "))
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := svc.discoverCohere(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected 'failed to decode response' error, got: %v", err)
	}
}

func TestDiscoverCohere_ModelWithPricing(t *testing.T) {
	t.Parallel()

	// Create test server with a model that matches the pricing catalog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := CohereModelsResponse{
			Models: []CohereNativeModel{
				{
					Name:          "command-r-plus-08-2024",
					Endpoints:     []string{"chat"},
					ContextLength: 128000,
					Features:      []string{"tools", "vision"},
					IsDeprecated:  false,
				},
			},
			NextPageToken: "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := svc.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	m := models[0]
	// Should have pricing from catalog
	if m.InputPricePerMillion == nil {
		t.Error("Expected InputPricePerMillion to be set for model in pricing catalog")
	} else if *m.InputPricePerMillion != 2.50 {
		t.Errorf("Expected input price 2.50, got %.2f", *m.InputPricePerMillion)
	}
	if m.OutputPricePerMillion == nil {
		t.Error("Expected OutputPricePerMillion to be set for model in pricing catalog")
	} else if *m.OutputPricePerMillion != 10.00 {
		t.Errorf("Expected output price 10.00, got %.2f", *m.OutputPricePerMillion)
	}
	if m.DisplayName != "Command R+" {
		t.Errorf("Expected DisplayName 'Command R+', got '%s'", m.DisplayName)
	}
	if m.MaxOutputTokens == nil {
		t.Error("Expected MaxOutputTokens to be set")
	} else if *m.MaxOutputTokens != 4096 {
		t.Errorf("Expected MaxOutputTokens 4096, got %d", *m.MaxOutputTokens)
	}
}

// =============================================================================
// Ollama Discovery Tests
// =============================================================================

func TestDiscoverOllama_ShowModelFailure(t *testing.T) {
	t.Parallel()

	// Create test server where /api/tags succeeds but /api/show fails for one model
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/tags" && r.Method == "GET":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "llama3.2"},
					{Name: "failing-model"},
					{Name: "mistral"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case r.URL.Path == "/api/show" && r.Method == "POST":
			callCount++
			// Read the request body to get model name
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "failing-model") {
				http.Error(w, "Model not found", http.StatusNotFound)
				return
			}
			// Successful response for other models
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo: map[string]interface{}{
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

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := svc.discoverOllama(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOllama failed: %v", err)
	}

	// Should have 2 models (1 skipped due to show failure)
	if len(models) != 2 {
		t.Errorf("Expected 2 models (1 skipped), got %d", len(models))
	}
}

func TestBuildOllamaModel_ThinkingCapability(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{}

	provider := &Provider{
		ID: uuid.New(),
	}

	showResponse := &OllamaShowResponse{
		Capabilities: []string{"tools", "thinking", "vision"},
		ModelInfo: map[string]interface{}{
			"llama.context_length": float64(32768),
		},
		Details: OllamaShowDetails{
			Family: "llama",
		},
	}

	m := svc.buildOllamaModel(provider, "test-model-thinking", showResponse)

	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}

	if !caps.Reasoning {
		t.Error("Expected Reasoning=true for 'thinking' capability")
	}
	if !caps.ToolCalling {
		t.Error("Expected ToolCalling=true for 'tools' capability")
	}
	if !caps.Vision {
		t.Error("Expected Vision=true for 'vision' capability")
	}
	if m.Modality != "vision" {
		t.Errorf("Expected modality 'vision', got '%s'", m.Modality)
	}
	if m.OwnedBy != "llama" {
		t.Errorf("Expected ownedBy 'llama', got '%s'", m.OwnedBy)
	}
}

func TestBuildOllamaModel_EmptyFamily(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{}

	provider := &Provider{
		ID: uuid.New(),
	}

	showResponse := &OllamaShowResponse{
		Capabilities: []string{"tools"},
		ModelInfo: map[string]interface{}{
			"llama.context_length": float64(8192),
		},
		Details: OllamaShowDetails{
			Family: "", // Empty family should default to "ollama"
		},
	}

	m := svc.buildOllamaModel(provider, "test-model-empty-family", showResponse)

	if m.OwnedBy != "ollama" {
		t.Errorf("Expected ownedBy 'ollama' for empty family, got '%s'", m.OwnedBy)
	}
}

func TestGetOllamaCloudAccount_RequestCreationError(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://ollama.com/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	// Create cancelled context to trigger request creation error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = svc.GetOllamaCloudAccount(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestGetOllamaCloudAccount_JSONDecodeError(t *testing.T) {
	t.Parallel()

	// Create test server that returns 200 with invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json for ollama cloud "))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{
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

	_, err = svc.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode account response") {
		t.Errorf("Expected 'failed to decode account response' error, got: %v", err)
	}
}
