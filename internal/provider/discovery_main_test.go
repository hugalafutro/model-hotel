package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDiscoverModels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := NewDiscoveryService(nil, nil)
	masterKey := "test-master-key-1234567890123456"

	t.Run("unknown_provider_type", func(t *testing.T) {
		// Mock OpenAI server for fallback
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/models" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"data": [
						{
							"id": "gpt-3.5-turbo",
							"object": "model",
							"created": 1234567890,
							"owned_by": "openai"
						}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: server.URL,
		}
		models, err := svc.DiscoverModels(ctx, provider, masterKey)
		// Unknown provider should fall back to OpenAI discovery
		assert.NoError(t, err)
		assert.NotEmpty(t, models)
		assert.Equal(t, "gpt-3.5-turbo", models[0].Name)
	})

	t.Run("openai", func(t *testing.T) {
		// Mock OpenAI server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/models" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"data": [
						{
							"id": "gpt-4",
							"object": "model",
							"created": 1234567890,
							"owned_by": "openai"
						}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: server.URL,
		}
		models, err := svc.DiscoverModels(ctx, provider, masterKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, models)
		// Should return OpenAI's static catalog
		assert.Equal(t, "gpt-4", models[0].Name)
	})

	t.Run("anthropic", func(t *testing.T) {
		// Mock Anthropic server - use a URL that will be detected as Anthropic
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"data": [
						{
							"id": "claude-3-opus-20240229",
							"object": "model",
							"created": 1234567890,
							"owned_by": "anthropic"
						}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		// Create a provider with a URL that will be detected as Anthropic
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.anthropic.com",
		}
		// Replace the discovery service's HTTP client to intercept requests
		svc := &DiscoveryService{
			httpClient: &http.Client{
				Transport: &testTransport{url: server.URL},
			},
		}
		models, err := svc.DiscoverModels(ctx, provider, masterKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, models)
		assert.Contains(t, models[0].Name, "claude")
	})

	t.Run("google", func(t *testing.T) {
		// For now, just test that Google provider type is detected correctly
		// The actual discovery requires complex mocking due to URL transformations
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://generativelanguage.googleapis.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "google", providerType)
	})

	t.Run("cohere", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.cohere.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "cohere", providerType)
	})

	t.Run("deepseek", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.deepseek.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "deepseek", providerType)
	})

	t.Run("xai", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.x.ai",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "xai", providerType)
	})

	t.Run("openrouter", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://openrouter.ai",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "openrouter", providerType)
	})

	t.Run("ollama", func(t *testing.T) {
		// Test provider type detection for local Ollama
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "http://localhost:11434",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "ollama", providerType)
	})

	t.Run("ollama_cloud", func(t *testing.T) {
		// Test provider type detection for Ollama Cloud
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://ollama.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "ollama-cloud", providerType)
	})

	t.Run("opencode_go", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://opencode.ai/zen/go",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "opencode-go", providerType)
	})

	t.Run("opencode_zen", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://opencode.ai/zen",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "opencode-zen", providerType)
	})

	t.Run("zai_coding", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.z.ai",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "zai-coding", providerType)
	})

	t.Run("nanogpt", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.nano-gpt.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "nanogpt", providerType)
	})

	t.Run("koboldcpp", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "http://localhost:5001",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "koboldcpp", providerType)
	})

	t.Run("lmstudio", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "http://localhost:1234",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "lmstudio", providerType)
	})

	t.Run("neuralwatt", func(t *testing.T) {
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://api.neuralwatt.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "neuralwatt", providerType)
	})

	t.Run("neuralwatt_subdomain", func(t *testing.T) {
		// Test provider type detection for subdomain
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://custom.neuralwatt.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "neuralwatt", providerType)
	})
}

// TestGetZAICodingQuota is defined in discovery_http_test.go with live API testing

func TestDiscoverModels_KeylessProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "gpt-4", "object": "model", "created": 1234567890, "owned_by": "openai"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Provider with empty EncryptedKey (keyless)
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{}, // keyless provider
	}

	svc := NewDiscoveryService(nil, nil)
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "gpt-4", models[0].Name)
}

func TestDiscoverModels_DecryptionFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Provider with invalid encrypted key bytes (will fail decryption)
	// Use properly sized nonce (12 bytes) and salt (32 bytes) for AES-GCM
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.openai.com",
		EncryptedKey: []byte("invalid-encrypted-key-bytes"),
		KeyNonce:     make([]byte, 12), // Proper nonce length for AES-GCM
		KeySalt:      make([]byte, 32), // Proper salt length
	}

	svc := NewDiscoveryService(nil, nil)
	_, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt API key")
}

func TestDiscoverModels_DeepSeekDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock DeepSeek server (uses /models endpoint)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "deepseek-chat", "object": "model", "created": 1234567890, "owned_by": "deepseek"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.deepseek.com",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Contains(t, models[0].Name, "deepseek")
}

func TestDiscoverModels_OllamaDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models": [{"name": "llama3.2"}]}`))
		case "/api/show":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"capabilities": [], "model_info": {}, "details": {"family": "llama"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "http://localhost:11434",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "llama3.2", models[0].ModelID)
}

func TestDiscoverModels_OllamaCloudDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock Ollama Cloud server (same API as Ollama)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models": [{"name": "llama3.1"}]}`))
		case "/api/show":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"capabilities": [], "model_info": {}, "details": {"family": "llama"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://ollama.com",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "llama3.1", models[0].ModelID)
}

func TestDiscoverModels_OpenCodeZenDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock OpenCode Zen server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle both /models and /zen/models paths
		if r.URL.Path == "/models" || r.URL.Path == "/zen/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "big-pickle", "object": "model", "owned_by": "opencode", "created": 1234567890, "pricing": {"prompt": "0.00", "completion": "0.00"}}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://opencode.ai/zen",
		EncryptedKey: []byte{}, // Keyless
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "big-pickle", models[0].ModelID)
}

func TestDiscoverModels_OpenCodeGoDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock OpenCode Go server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle both /models and /zen/go/models paths
		if r.URL.Path == "/models" || r.URL.Path == "/zen/go/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "gpt-4", "object": "model", "owned_by": "opencode", "created": 1234567890}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Use opencode.ai/zen/go which will be detected as opencode-go
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://opencode.ai/zen/go",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "gpt-4", models[0].ModelID)
}

func TestDiscoverModels_XAIDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock xAI server (uses /language-models endpoint)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models": [{"id": "grok-2", "name": "Grok 2", "capabilities": {"chat": true}}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.x.ai",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Contains(t, models[0].Name, "grok")
}

func TestDiscoverModels_GoogleDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock Google AI Studio server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models": [{"name": "models/gemini-2.0-flash", "displayName": "Gemini 2.0 Flash", "description": "Test", "inputTokenLimit": 1000000, "outputTokenLimit": 8192, "supportedGenerationMethods": ["generateContent"], "thinking": false}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Contains(t, models[0].ModelID, "gemini")
}

func TestDiscoverModels_CohereDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock Cohere server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.URL.Query().Get("endpoint") == "chat" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models": [{"name": "command-r", "endpoints": ["chat"], "context_length": 128000, "features": ["tools"]}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Use api.cohere.com which will be detected as cohere
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.cohere.com",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "command-r", models[0].ModelID)
}

func TestDiscoverModels_OpenRouterDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock OpenRouter server (uses /models endpoint)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" || r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "meta-llama/llama-3-8b-instruct", "object": "model", "created": 1234567890, "owned_by": "openrouter"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://openrouter.ai",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	if err != nil {
		t.Logf("OpenRouter dispatch error: %v", err)
	}
	assert.NoError(t, err)
	if len(models) > 0 {
		assert.Contains(t, models[0].Name, "llama")
	}
}

func TestDiscoverModels_KoboldCPPDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock KoboldCPP server (requires /api/extra/version check and /models endpoint)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/extra/version":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result": "KoboldCpp", "version": "1.0.0"}`))
		case "/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "kobold-model", "object": "model", "created": 1234567890, "owned_by": "koboldcpp"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "http://localhost:5001",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "kobold-model", models[0].Name)
}

func TestDiscoverModels_LMStudioDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock LMStudio server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "lmstudio-model", "object": "model", "created": 1234567890, "owned_by": "lmstudio"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "http://localhost:1234",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "lmstudio-model", models[0].Name)
}

func TestDiscoverModels_NanoGPTDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock NanoGPT server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" && r.URL.Query().Get("detailed") == "true" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "nano-gpt-model", "name": "NanoGPT Model", "description": "Test", "owned_by": "nano-gpt", "capabilities": {"vision": false, "reasoning": false, "tool_calling": false}, "architecture": {"modality": "text", "input_modalities": ["text"], "output_modalities": ["text"]}, "pricing": {"prompt": 0.01, "completion": 0.02}, "context_length": 8192, "max_output_tokens": 4096}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.nano-gpt.com",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Equal(t, "nano-gpt-model", models[0].ModelID)
}

func TestDiscoverModels_ZAICodingDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"glm-5.1","object":"model","owned_by":"z-ai"}]}`))
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.z.ai/api/coding/paas/v4",
	}

	svc := &DiscoveryService{httpClient: &http.Client{Transport: &testTransport{url: server.URL}}}
	models, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	// Live glm-5.1 (owned_by "z-ai") is first and normalized to "zhipu".
	assert.Equal(t, "zhipu", models[0].OwnedBy)
}

func TestDiscoverModels_ErrorPropagation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock server that returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.deepseek.com",
	}

	svc := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}
	_, err := svc.DiscoverModels(ctx, provider, masterKey)
	assert.Error(t, err)
}

func TestLoadModelsDev(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Mock models.dev API - it returns a map of provider IDs to provider specs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"test-provider": {
				"id": "test-provider",
				"name": "Test Provider",
				"models": {
					"test-model": {
						"id": "test-model",
						"name": "Test Model",
						"context_length": 4096
					}
				}
			}
		}`))
	}))
	defer server.Close()

	// Test with custom client that overrides the transport to redirect to our mock server
	client := &http.Client{
		Transport: &testTransport{url: server.URL},
	}
	err := LoadModelsDevWithClient(ctx, client)
	assert.NoError(t, err)

	// Verify cache was populated
	cache := GetModelsDevCache()
	assert.NotNil(t, cache)

	// Verify we can look up the model
	spec := cache.Lookup("test-model")
	assert.NotNil(t, spec)
	assert.Equal(t, "test-model", spec.ID)
}

// testTransport is a simple http.RoundTripper that redirects requests to a mock server
type testTransport struct {
	url string
}

func (m *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point to our mock server, preserving query parameters
	newURL, _ := url.Parse(m.url + req.URL.Path)
	newURL.RawQuery = req.URL.RawQuery
	req.URL = newURL
	req.Host = "" // Clear Host header to avoid conflicts
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
