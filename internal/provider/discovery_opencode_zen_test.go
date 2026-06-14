package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// TestDiscoverOpenCodeZen_ModelNotInCatalog tests that models not in the
// OpenCode Zen catalog are still included with default capabilities.
func TestDiscoverOpenCodeZen_ModelNotInCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	masterKey := "test-master-key-1234567890123456"

	// Mock server returning a model not in the OpenCode Zen catalog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"data": [
					{"id": "big-pickle", "object": "model", "owned_by": "opencode", "created": 1234567890},
					{"id": "unknown-custom-model", "object": "model", "owned_by": "opencode", "created": 1234567891}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create properly encrypted key so keyless mode is disabled
	// This allows testing the "model not in catalog" code path
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

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	models, err := svc.discoverOpenCodeZen(ctx, provider, masterKey)

	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	// Keyed provider: the two live models union with the catalog. big-pickle is
	// already a catalog entry; unknown-custom-model is a new union member.
	assert.Len(t, models, len(GetOpenCodeZenCatalog())+1)

	// Find the unknown model
	var unknownModel *model.Model
	for _, m := range models {
		if m.ModelID == "unknown-custom-model" {
			unknownModel = m
			break
		}
	}
	assert.NotNil(t, unknownModel)
	assert.Equal(t, "unknown-custom-model", unknownModel.ModelID)
	assert.Equal(t, "unknown-custom-model", unknownModel.DisplayName)
	assert.Equal(t, "opencode", unknownModel.OwnedBy)
	// Should have default capabilities with streaming enabled
	assert.Contains(t, unknownModel.Capabilities, `"streaming":true`)
}

// TestDiscoverOpenCodeZen_WithAuthKey tests that the Authorization header
// is correctly set when making requests to the OpenCode Zen API.
func TestDiscoverOpenCodeZen_WithAuthKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	apiKey := "test-api-key"

	// Mock server that checks for Authorization header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer "+apiKey {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte("Unauthorized"))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"data": [
					{"id": "big-pickle", "object": "model", "owned_by": "opencode", "created": 1234567890}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// discoverOpenCodeZen takes the decrypted apiKey directly (not masterKey)
	// Use empty EncryptedKey to indicate keyless mode, but pass apiKey directly
	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: []byte{}, // Keyless - apiKey passed directly
	}

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	// Test with API key passed directly
	models, err := svc.discoverOpenCodeZen(ctx, provider, apiKey)
	assert.NoError(t, err)
	assert.NotEmpty(t, models)
	assert.Len(t, models, 1)
	assert.Equal(t, "big-pickle", models[0].ModelID)
}
