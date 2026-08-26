package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// Per-family discovery: what each provider type's listing turns into.

func TestLegacyTypeFromURL_OpenAI(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"standard openai", "https://api.openai.com/v1"},
		{"openai with path", "https://api.openai.com/v1/chat/completions"},
		{"custom openai-compatible", "https://my-custom-llm.example.com/v1"},
		{"random domain", "https://some-random-host.io/api"},
		{"localhost default", "http://localhost:3000/v1"},
		{"127.0.0.1 default", "http://127.0.0.1:8000/v1"},
		{"ipv6 loopback default", "http://[::1]:4000/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "openai" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "openai")
			}
		})
	}
}

func TestLegacyTypeFromURL_Bedrock(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"mantle us-east-1", "https://bedrock-mantle.us-east-1.api.aws/v1"},
		{"mantle eu-central-1", "https://bedrock-mantle.eu-central-1.api.aws/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "bedrock" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "bedrock")
			}
		})
	}
}

func TestLegacyTypeFromURL_NotBedrock(t *testing.T) {
	// Similar-looking hosts must stay generic: detection requires both the
	// bedrock service prefix and the AWS domain suffix.
	tests := []struct {
		name string
		url  string
	}{
		{"bedrock-mantle on wrong domain", "https://bedrock-mantle.example.com/v1"},
		{"random api.aws host", "https://someservice.us-east-1.api.aws/v1"},
		// bedrock-runtime is deliberately NOT detected: it has no /models
		// listing (404, live-verified 2026-07-18), so discovery can never work
		// against it — classifying it as bedrock would only manufacture a
		// guaranteed-failing provider. Only bedrock-mantle is supported.
		{"bedrock-runtime not supported", "https://bedrock-runtime.us-east-1.amazonaws.com/v1"},
		{"bedrock-runtime openai path not supported", "https://bedrock-runtime.us-west-2.amazonaws.com/openai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "openai" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "openai")
			}
		})
	}
}

func TestLegacyTypeFromURL_Azure(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"foundry project endpoint", "https://myres-resource.services.ai.azure.com/api/projects/myproject"},
		{"foundry resource root", "https://myres-resource.services.ai.azure.com"},
		{"foundry openai v1 path", "https://myres-resource.services.ai.azure.com/openai/v1"},
		{"classic azure openai resource", "https://myres.openai.azure.com"},
		{"classic azure openai v1 path", "https://myres.openai.azure.com/openai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "azure" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "azure")
			}
		})
	}
}

func TestLegacyTypeFromURL_NotAzure(t *testing.T) {
	// Azure-looking names on unrelated domains must stay generic: detection
	// matches the two Azure AI host suffixes only.
	tests := []struct {
		name string
		url  string
	}{
		{"azure in subdomain of wrong domain", "https://services.ai.azure.com.evil.example/openai/v1"},
		{"generic azure.com host", "https://portal.azure.com/whatever"},
		{"azure-named host elsewhere", "https://openai.azure.example.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result == "azure" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want non-azure", tc.url, result)
			}
		})
	}
}

func TestLegacyTypeFromURL_Anthropic(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://api.anthropic.com/v1"},
		{"bare domain", "https://anthropic.com/v1"},
		{"subdomain", "https://custom.anthropic.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "anthropic" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "anthropic")
			}
		})
	}
}

func TestLegacyTypeFromURL_OllamaCloud(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://ollama.com/api"},
		{"subdomain", "https://custom.ollama.com/api"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "ollama-cloud" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "ollama-cloud")
			}
		})
	}
}

func TestLegacyTypeFromURL_Cohere(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"standard cohere.com", "https://api.cohere.com/v1"},
		{"standard cohere.ai", "https://api.cohere.ai/v1"},
		{"custom cohere.com subdomain", "https://custom.cohere.com/v1"},
		{"custom cohere.ai subdomain", "https://custom.cohere.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "cohere" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "cohere")
			}
		})
	}
}

func TestLegacyTypeFromURL_Google(t *testing.T) {
	// aiplatform hosts used to detect as "google" too, but the AI Studio
	// discovery/proxy surfaces never existed there — they now route to the
	// vertex-express egress adapter (see TestLegacyTypeFromURL_VertexExpress).
	tests := []struct {
		name string
		url  string
	}{
		{"generativelanguage v1beta", "https://generativelanguage.googleapis.com/v1beta"},
		{"generativelanguage custom subdomain", "https://custom-generativelanguage.googleapis.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "google" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "google")
			}
		})
	}
}

func TestLegacyTypeFromURL_OllamaCloudSubdomain(t *testing.T) {
	result := LegacyTypeFromURL("https://custom.ollama.com/v1")
	if result != "ollama-cloud" {
		t.Errorf("LegacyTypeFromURL('https://custom.ollama.com/v1') = %q, want %q", result, "ollama-cloud")
	}
}

func TestDiscoverModels_UnsupportedProviderTypeFallsBackToOpenAI(t *testing.T) {
	// Test that an unknown provider type falls back to OpenAI discovery
	// Note: There's no "unsupported" error - unknown types default to OpenAI
	mockResponse := `{
		"data": [
			{
				"id": "fallback-model",
				"object": "model",
				"owned_by": "fallback"
			}
		]
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

	svc := &DiscoveryService{httpClient: server.Client(), retryBaseDelay: time.Millisecond}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "unknown-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels should fall back to OpenAI, got error: %v", err)
	}
	// Live "fallback-model" (not in catalog) is first, backfilled from the catalog (no union).
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "fallback-model" {
		t.Errorf("expected model ID 'fallback-model', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_OpenAIProviderType(t *testing.T) {
	// Test explicit OpenAI provider type
	mockResponse := `{
		"data": [
			{
				"id": "gpt-4-test",
				"object": "model",
				"owned_by": "openai"
			}
		]
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
		Name:         "openai-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for OpenAI should succeed, got error: %v", err)
	}
	// Live "gpt-4-test" (not in catalog) is first, backfilled from the catalog (no union).
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "gpt-4-test" {
		t.Errorf("expected model ID 'gpt-4-test', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_AnthropicProviderType(t *testing.T) {
	// Test Anthropic provider type - uses different endpoint
	// Anthropic uses pagination with "data" array and has_more/last_id
	mockResponse := `{
		"data": [
			{
				"id": "claude-3-opus-20240229",
				"display_name": "Claude 3 Opus",
				"capabilities": {},
				"max_input_tokens": 200000,
				"max_tokens": 4096
			}
		],
		"has_more": false,
		"last_id": ""
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use the test server's client, but override the BaseURL to trigger anthropic detection
	// The httpClient will still connect to the test server
	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:   uuid.New(),
		Name: "anthropic-provider",
		// Use anthropic.com domain to trigger anthropic provider type detection
		// The test server's transport will handle the actual connection
		BaseURL:      "https://api.anthropic.com",
		EncryptedKey: []byte{},
	}

	// Override the transport to redirect all requests to test server
	svc.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		newURL := server.URL + req.URL.Path
		if req.URL.RawQuery != "" {
			newURL += "?" + req.URL.RawQuery
		}
		newReq := req.Clone(req.Context())
		newReq.URL, _ = url.Parse(newURL)
		newReq.Host = newReq.URL.Host
		return http.DefaultTransport.RoundTrip(newReq)
	})

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for Anthropic should succeed, got error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "claude-3-opus-20240229" {
		t.Errorf("expected model ID 'claude-3-opus-20240229', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_OllamaProviderType(t *testing.T) {
	// Test Ollama provider type - uses /api/tags endpoint
	// Ollama also calls /api/show for each model to get details
	mockTagsResponse := `{
		"models": [
			{
				"name": "llama3.2:latest",
				"model": "llama3.2:latest"
			}
		]
	}`
	mockShowResponse := `{
		"details": {
			"family": "llama3"
		},
		"model_info": {}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockTagsResponse))
			return
		}
		if r.URL.Path == "/api/show" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockShowResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use ollama.com domain to trigger ollama-cloud provider type detection
	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "ollama-provider",
		BaseURL:      "https://api.ollama.com",
		EncryptedKey: []byte{},
	}

	// Override the transport to redirect all requests to test server
	svc.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		newURL := server.URL + req.URL.Path
		if req.URL.RawQuery != "" {
			newURL += "?" + req.URL.RawQuery
		}
		newReq := req.Clone(req.Context())
		newReq.URL, _ = url.Parse(newURL)
		newReq.Host = newReq.URL.Host
		return http.DefaultTransport.RoundTrip(newReq)
	})

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for Ollama should succeed, got error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "llama3.2:latest" {
		t.Errorf("expected model ID 'llama3.2:latest', got '%s'", models[0].ModelID)
	}
}

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
	// PDF capability lands in the input modalities; the class derives chat.
	if !strings.Contains(m.InputModalities, "image") || !strings.Contains(m.InputModalities, "pdf") {
		t.Errorf("Expected input modalities to include 'image' and 'pdf', got '%s'", m.InputModalities)
	}
	NormalizeModels(models)
	if m.Modality != "chat" {
		t.Errorf("Expected derived class 'chat' for PDF-capable model, got '%s'", m.Modality)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("endpoint") != "chat" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(CohereModelsResponse{})
			return
		}
		response := CohereModelsResponse{
			Models: []CohereNativeModel{
				{
					Name:          "c4ai-aya-expanse-32b",
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
	} else if *m.InputPricePerMillion != 0.50 {
		t.Errorf("Expected input price 0.50, got %.2f", *m.InputPricePerMillion)
	}
	if m.OutputPricePerMillion == nil {
		t.Error("Expected OutputPricePerMillion to be set for model in pricing catalog")
	} else if *m.OutputPricePerMillion != 1.50 {
		t.Errorf("Expected output price 1.50, got %.2f", *m.OutputPricePerMillion)
	}
	if m.DisplayName != "Aya Expanse 32B" {
		t.Errorf("Expected DisplayName 'Aya Expanse 32B', got '%s'", m.DisplayName)
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
			// Read the request body to get model name
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "failing-model") {
				http.Error(w, "Model not found", http.StatusNotFound)
				return
			}
			// Successful response for other models
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

	// All 3 models must be returned: failing-model is listed by /api/tags, so
	// its failed detail probe keeps it with default metadata instead of
	// dropping it (which would get it disabled as "missing").
	if len(models) != 3 {
		t.Fatalf("Expected 3 models (failing-model kept with default metadata), got %d", len(models))
	}
	for _, m := range models {
		if m.ModelID == "failing-model" && m.ContextLength != nil {
			t.Errorf("expected failing-model context length nil, got %v", *m.ContextLength)
		}
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
		ModelInfo: map[string]any{
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
	if m.InputModalities != `["text","image"]` {
		t.Errorf("Expected input modalities [\"text\",\"image\"], got '%s'", m.InputModalities)
	}
	NormalizeModelClassification(m)
	if m.Modality != "chat" {
		t.Errorf("Expected derived class 'chat', got '%s'", m.Modality)
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
		ModelInfo: map[string]any{
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
