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
	svc := NewDiscoveryService()
	masterKey := "test-master-key-1234567890123456"

	t.Run("unknown_provider_type", func(t *testing.T) {
		// Mock OpenAI server for fallback
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
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
			if r.URL.Path == "/v1/models" {
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
		// Test provider type detection
		provider := &Provider{
			ID:      uuid.New(),
			BaseURL: "https://ollama.com",
		}
		providerType := DetectProviderType(provider.BaseURL)
		assert.Equal(t, "ollama", providerType)
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
}

// TestGetZAICodingQuota is defined in discovery_http_test.go with live API testing

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
	// Rewrite the URL to point to our mock server
	req.URL, _ = url.Parse(m.url + req.URL.Path)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
