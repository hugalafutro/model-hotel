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

// ---------------------------------------------------------------------------
// discoverZAICoding — catalog-based discovery (no HTTP needed)
// ---------------------------------------------------------------------------

func TestDiscoverZAICoding_ReturnsModels(t *testing.T) {
	service := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	provider := &Provider{
		ID:   uuid.New(),
		Name: "test-zai",
	}

	models, err := service.discoverZAICoding(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}
	if len(models) == 0 {
		t.Error("Expected at least one model from zai-coding catalog")
	}

	for _, m := range models {
		if m.ProviderID != provider.ID {
			t.Errorf("ProviderID = %v, want %v", m.ProviderID, provider.ID)
		}
		if m.ModelID == "" {
			t.Error("ModelID should not be empty")
		}
		if m.OwnedBy != "zhipu" {
			t.Errorf("OwnedBy = %q, want %q", m.OwnedBy, "zhipu")
		}
		if !m.Enabled {
			t.Error("Expected model to be enabled")
		}
		if m.ContextLength == nil || *m.ContextLength <= 0 {
			t.Errorf("ContextLength should be > 0, got %v", m.ContextLength)
		}
		if m.MaxOutputTokens == nil || *m.MaxOutputTokens <= 0 {
			t.Errorf("MaxOutputTokens should be > 0, got %v", m.MaxOutputTokens)
		}
	}
}

func TestDiscoverZAICoding_VisionModelCapabilities(t *testing.T) {
	service := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	provider := &Provider{
		ID:   uuid.New(),
		Name: "test-zai",
	}

	models, err := service.discoverZAICoding(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverZAICoding failed: %v", err)
	}

	// Find a vision model from the catalog
	found := false
	for _, m := range models {
		if m.Modality == "vision" {
			found = true
			var caps model.Capability
			if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
				t.Fatalf("Failed to unmarshal capabilities: %v", err)
			}
			if !caps.Vision {
				t.Errorf("Expected Vision capability for model %q with modality=vision", m.ModelID)
			}
			if !caps.VideoInput {
				t.Errorf("Expected VideoInput capability for model %q with modality=vision", m.ModelID)
			}
			if !strings.Contains(m.InputModalities, "image") {
				t.Errorf("Expected InputModalities to contain 'image' for vision model %q, got %q", m.ModelID, m.InputModalities)
			}
			break
		}
	}
	if !found {
		t.Skip("No vision model in zai-coding catalog - skipping vision capability test")
	}
}

// ---------------------------------------------------------------------------
// GetZAICodingQuota — additional error paths not covered in discovery_http_test.go
// ---------------------------------------------------------------------------

func TestGetZAICodingQuota_DecryptionError(t *testing.T) {
	service := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	// Create provider with invalid encrypted key
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai",
		EncryptedKey: []byte("invalid-encrypted-key"),
		KeyNonce:     make([]byte, 12),
		KeySalt:      make([]byte, 32),
	}

	_, err := service.GetZAICodingQuota(context.Background(), provider, "wrong-master-key")
	if err == nil {
		t.Fatal("Expected error for decryption failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decrypt API key") {
		t.Errorf("Expected decryption error, got: %v", err)
	}
}

func TestGetZAICodingQuota_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestGetZAICodingQuota_CircuitBreakerOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	service := &DiscoveryService{
		httpClient: &http.Client{
			Transport: &testTransport{url: server.URL},
		},
		retryBaseDelay: time.Millisecond,
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "test-zai-cb",
		BaseURL:      "https://api.z.ai",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	// Exhaust retries to open the circuit breaker
	for i := 0; i < 5; i++ {
		_, _ = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	}

	// Now the circuit breaker should be open - next call should fail with circuit breaker error
	_, err = service.GetZAICodingQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for circuit breaker open, got nil")
	}
}
