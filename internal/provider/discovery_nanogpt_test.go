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

func TestGetNanoGPTUsage_Success(t *testing.T) {
	// Create test server with mock NanoGPT usage response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock NanoGPT usage response
		response := NanoGPTUsageResponse{
			Active:             true,
			Provider:           "nano-gpt",
			ProviderStatus:     "active",
			ProviderStatusRaw:  "active",
			StripeSubscription: "sub_test123",
			AllowOverage:       false,
			State:              "active",
			Limits: NanoGPTUsageLimits{
				WeeklyInputTokens: int64Ptr(200000),
				DailyInputTokens:  int64Ptr(50000),
				DailyImages:       int64Ptr(10),
			},
			Period: NanoGPTUsagePeriod{
				CurrentPeriodEnd: "2024-01-31T23:59:59Z",
			},
			DailyInputTokens: &NanoGPTUsageTokenInfo{
				Used:        10000,
				Remaining:   40000,
				PercentUsed: 20.0,
				ResetAt:     1704067200,
			},
			WeeklyInputTokens: &NanoGPTUsageTokenInfo{
				Used:        50000,
				Remaining:   150000,
				PercentUsed: 25.0,
				ResetAt:     1704672000,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create discovery service with test client
	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	// Create test provider with encrypted key
	masterKey := "test-master-key-for-testing-only-32bytes!"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	// Test usage fetch
	usage, err := service.GetNanoGPTUsage(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetNanoGPTUsage failed: %v", err)
	}

	// Verify results
	if !usage.Active {
		t.Error("Expected Active to be true")
	}
	if usage.Provider != "nano-gpt" {
		t.Errorf("Expected Provider 'nano-gpt', got '%s'", usage.Provider)
	}
	if usage.ProviderStatus != "active" {
		t.Errorf("Expected ProviderStatus 'active', got '%s'", usage.ProviderStatus)
	}
	if usage.Limits.DailyInputTokens == nil {
		t.Error("Expected DailyInputTokens to be non-nil")
	} else if *usage.Limits.DailyInputTokens != 50000 {
		t.Errorf("Expected DailyInputTokens 50000, got %d", *usage.Limits.DailyInputTokens)
	}
	if usage.DailyInputTokens == nil {
		t.Error("Expected DailyInputTokens to be non-nil")
	} else if usage.DailyInputTokens.Used != 10000 {
		t.Errorf("Expected DailyInputTokens.Used 10000, got %d", usage.DailyInputTokens.Used)
	}
	if usage.WeeklyInputTokens == nil {
		t.Error("Expected WeeklyInputTokens to be non-nil")
	} else if usage.WeeklyInputTokens.Used != 50000 {
		t.Errorf("Expected WeeklyInputTokens.Used 50000, got %d", usage.WeeklyInputTokens.Used)
	}
}

func TestGetNanoGPTUsage_Non200Status(t *testing.T) {
	// Create test server that returns 401 Unauthorized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-for-testing-only-32bytes!"
	keyPair, err := auth.Encrypt("wrong-api-key", masterKey)
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	_, err = service.GetNanoGPTUsage(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
}

func TestGetNanoGPTUsage_InvalidJSON(t *testing.T) {
	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-for-testing-only-32bytes!"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	_, err = service.GetNanoGPTUsage(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestGetNanoGPTUsage_DecryptionError(t *testing.T) {
	// Create discovery service - no server needed since decryption fails first
	service := &DiscoveryService{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Create provider with valid encryption structure but wrong key
	masterKey := "test-master-key-for-testing-only-32bytes!"
	wrongKeyPair, err := auth.Encrypt("test-api-key", "different-master-key-32bytes!!")
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.nano-gpt.com",
		EncryptedKey: wrongKeyPair.Ciphertext,
		KeyNonce:     wrongKeyPair.Nonce,
		KeySalt:      wrongKeyPair.Salt,
	}

	_, err = service.GetNanoGPTUsage(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for decryption failure, got nil")
	}
}

func TestGetNanoGPTUsage_ContextCancellation(t *testing.T) {
	// Create a slow test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NanoGPTUsageResponse{Active: true})
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-for-testing-only-32bytes!"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL,
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	// Create a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err = service.GetNanoGPTUsage(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for context cancellation, got nil")
	}
}

func TestDiscoverNanoGPT_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("detailed") != "true" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		response := NanoGPTDetailedResponse{
			Object: "list",
			Data: []NanoGPTModel{
				{
					ID:              "gpt-4o",
					Name:            "GPT-4o",
					Description:     "OpenAI flagship model",
					ContextLength:   intPtr(128000),
					MaxOutputTokens: intPtr(16384),
					OwnedBy:         "openai",
					Architecture: NanoGPTArchitecture{
						Modality:         "text",
						InputModalities:  []string{"text", "image"},
						OutputModalities: []string{"text"},
					},
					Capabilities: NanoGPTCapabilities{
						Vision:           true,
						Reasoning:        true,
						ToolCalling:      true,
						StructuredOutput: true,
					},
					Pricing: NanoGPTPricing{
						Prompt:     floatPtr(2.5),
						Completion: floatPtr(10.0),
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	models, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "gpt-4o" {
		t.Errorf("Expected ModelID 'gpt-4o', got '%s'", models[0].ModelID)
	}
	if models[0].Name != "GPT-4o" {
		t.Errorf("Expected Name 'GPT-4o', got '%s'", models[0].Name)
	}
	if models[0].Description != "OpenAI flagship model" {
		t.Errorf("Expected Description 'OpenAI flagship model', got '%s'", models[0].Description)
	}
	if models[0].DisplayName != "GPT-4o" {
		t.Errorf("Expected DisplayName 'GPT-4o', got '%s'", models[0].DisplayName)
	}
	if models[0].ContextLength == nil || *models[0].ContextLength != 128000 {
		t.Errorf("Expected ContextLength 128000, got %v", models[0].ContextLength)
	}
	if models[0].MaxOutputTokens == nil || *models[0].MaxOutputTokens != 16384 {
		t.Errorf("Expected MaxOutputTokens 16384, got %v", models[0].MaxOutputTokens)
	}
	if models[0].InputPricePerMillion == nil || *models[0].InputPricePerMillion != 2.5 {
		t.Errorf("Expected InputPricePerMillion 2.5, got %v", models[0].InputPricePerMillion)
	}
	if models[0].OutputPricePerMillion == nil || *models[0].OutputPricePerMillion != 10.0 {
		t.Errorf("Expected OutputPricePerMillion 10.0, got %v", models[0].OutputPricePerMillion)
	}
	// NanoGPT reports pricing/context over the wire, so these must be marked
	// live (a genuine provider change overwrites on upsert and is reported).
	if !models[0].LiveMeta.InputPrice || !models[0].LiveMeta.OutputPrice ||
		!models[0].LiveMeta.ContextLength || !models[0].LiveMeta.MaxOutputTokens {
		t.Errorf("Expected wire-sourced fields to be live, got %+v", models[0].LiveMeta)
	}
}

// A model whose pricing is omitted must leave the price fields nil and
// unmarked-live, so a partial NanoGPT response can't overwrite a stored nonzero
// price with a synthetic 0. A model that explicitly prices at 0 (free) keeps the
// real &0 and stays live.
func TestDiscoverNanoGPT_OmittedPricingStaysNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := NanoGPTDetailedResponse{
			Object: "list",
			Data: []NanoGPTModel{
				{
					ID:      "no-pricing",
					Name:    "No Pricing",
					OwnedBy: "test",
					Architecture: NanoGPTArchitecture{
						Modality:         "text",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					ContextLength: intPtr(128000),
					Pricing:       NanoGPTPricing{}, // prompt/completion omitted -> nil
				},
				{
					ID:      "free-model",
					Name:    "Free Model",
					OwnedBy: "test",
					Architecture: NanoGPTArchitecture{
						Modality:         "text",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					ContextLength: intPtr(128000),
					Pricing: NanoGPTPricing{
						Prompt:     floatPtr(0), // explicit free price -> kept as &0
						Completion: floatPtr(0),
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), Name: "nanogpt-test", BaseURL: server.URL}

	models, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(models))
	}

	// Omitted pricing -> nil, not marked live.
	noPricing := models[0]
	if noPricing.InputPricePerMillion != nil || noPricing.OutputPricePerMillion != nil {
		t.Errorf("omitted pricing must be nil, got in=%v out=%v",
			noPricing.InputPricePerMillion, noPricing.OutputPricePerMillion)
	}
	if noPricing.LiveMeta.InputPrice || noPricing.LiveMeta.OutputPrice {
		t.Errorf("omitted pricing must not be marked live, got %+v", noPricing.LiveMeta)
	}

	// Explicit 0 -> kept as a real value and marked live.
	free := models[1]
	if free.InputPricePerMillion == nil || *free.InputPricePerMillion != 0 ||
		free.OutputPricePerMillion == nil || *free.OutputPricePerMillion != 0 {
		t.Errorf("explicit free pricing must be &0, got in=%v out=%v",
			free.InputPricePerMillion, free.OutputPricePerMillion)
	}
	if !free.LiveMeta.InputPrice || !free.LiveMeta.OutputPrice {
		t.Errorf("explicit free pricing must be marked live, got %+v", free.LiveMeta)
	}
}

func TestDiscoverNanoGPT_EmptyNameUsesID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := NanoGPTDetailedResponse{
			Object: "list",
			Data: []NanoGPTModel{
				{
					ID:      "model-with-no-name",
					OwnedBy: "test",
					Architecture: NanoGPTArchitecture{
						Modality:         "text",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Capabilities: NanoGPTCapabilities{},
					Pricing:      NanoGPTPricing{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	models, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// When Name is empty, DisplayName should fall back to ID
	if models[0].DisplayName != "model-with-no-name" {
		t.Errorf("Expected DisplayName to fall back to ID 'model-with-no-name', got '%s'", models[0].DisplayName)
	}
}

func TestDiscoverNanoGPT_WithSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := NanoGPTDetailedResponse{
			Object: "list",
			Data: []NanoGPTModel{
				{
					ID:      "sub-model",
					Name:    "Subscription Model",
					OwnedBy: "test",
					Architecture: NanoGPTArchitecture{
						Modality:         "text",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Capabilities: NanoGPTCapabilities{},
					Pricing:      NanoGPTPricing{},
					Subscription: &NanoGPTSubscription{
						Included: true,
						Note:     "Included in Pro plan",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	models, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// Verify subscription params were included
	if models[0].Params == "" || models[0].Params == "{}" {
		t.Error("Expected Params to contain subscription data")
	}
}

func TestDiscoverNanoGPT_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := NanoGPTDetailedResponse{
			Object: "list",
			Data:   []NanoGPTModel{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	models, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty response, got %d", len(models))
	}
}

func TestDiscoverNanoGPT_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	_, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDiscoverNanoGPT_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	_, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for HTTP 500, got nil")
	}
}

func TestDiscoverNanoGPT_VisionCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := NanoGPTDetailedResponse{
			Object: "list",
			Data: []NanoGPTModel{
				{
					ID:      "vision-model",
					Name:    "Vision Model",
					OwnedBy: "test",
					Architecture: NanoGPTArchitecture{
						Modality:         "vision",
						InputModalities:  []string{"text", "image", "video"},
						OutputModalities: []string{"text"},
					},
					Capabilities: NanoGPTCapabilities{
						Vision:     true,
						VideoInput: true,
						Reasoning:  true,
					},
					Pricing: NanoGPTPricing{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "nanogpt-test",
		BaseURL: server.URL,
	}

	models, err := service.discoverNanoGPT(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPT failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Vision {
		t.Error("Expected Vision capability to be true")
	}
	if !caps.VideoInput {
		t.Error("Expected VideoInput capability to be true")
	}
	if !caps.Reasoning {
		t.Error("Expected Reasoning capability to be true")
	}

	if !strings.Contains(models[0].InputModalities, "image") {
		t.Errorf("Expected image in InputModalities, got %s", models[0].InputModalities)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func intPtr(v int) *int {
	return &v
}
