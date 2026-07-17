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
	assertDiscoverHTTPError(t, "invalid JSON", invalidJSONHandler(),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverNanoGPT(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverNanoGPT_HTTPError(t *testing.T) {
	assertDiscoverHTTPError(t, "HTTP 500", errorStatusHandler(http.StatusInternalServerError),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverNanoGPT(context.Background(), p, "test-api-key")
		})
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

// nanoGPTImageCatalogBody is a minimal NanoGPT image-catalog payload with two
// subscription-included models (one text-to-image, one editing) and one
// pay-as-you-go model, used to exercise base-dependent filtering.
const nanoGPTImageCatalogBody = `{"models":{"image":{
  "chroma":{"name":"Chroma","model":"chroma","provider":"chroma","iconLabel":"text-to-image","description":"Uncensored text-to-image model.","cost":{"1024x1024":0.0255,"512x512":0.0255},"subscription":{"included":true}},
  "step-image-edit-2":{"name":"Step Image Edit 2","model":"step-image-edit-2","provider":"stepfun","iconLabel":"both","description":"Editing model.","cost":{"1024x1024":0.003},"subscription":{"included":true}},
  "premium-paint":{"name":"Premium Paint","model":"premium-paint","provider":"acme","iconLabel":"text-to-image","description":"Pay-as-you-go only.","cost":{"1024x1024":0.07},"subscription":{"included":false}}
}}}`

func newNanoGPTImageCatalogServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/image" || r.Method != "GET" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nanoGPTImageCatalogBody))
	}))
}

func findModelByID(models []*model.Model, id string) *model.Model {
	for _, m := range models {
		if m.ModelID == id {
			return m
		}
	}
	return nil
}

func TestDiscoverNanoGPTImageModels_SubscriptionBaseFiltersIncluded(t *testing.T) {
	server := newNanoGPTImageCatalogServer(t)
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), Name: "nanogpt-test", BaseURL: server.URL + "/api/subscription/v1"}

	models, err := service.discoverNanoGPTImageModels(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPTImageModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 subscription-included image models, got %d", len(models))
	}
	if findModelByID(models, "premium-paint") != nil {
		t.Error("pay-as-you-go model must not be registered on a subscription base")
	}

	chroma := findModelByID(models, "chroma")
	if chroma == nil {
		t.Fatal("expected chroma to be registered")
	}
	if !strings.Contains(chroma.OutputModalities, "image") {
		t.Errorf("chroma output modalities = %q, want to contain image", chroma.OutputModalities)
	}
	if chroma.Modality != "text->image" {
		t.Errorf("chroma modality = %q, want text->image", chroma.Modality)
	}
	if !strings.Contains(chroma.Params, `"subscription_included":true`) {
		t.Errorf("chroma params = %q, want subscription_included true", chroma.Params)
	}
	if !strings.Contains(chroma.Params, `"image_generation":true`) {
		t.Errorf("chroma params = %q, want image_generation true", chroma.Params)
	}
	if chroma.InputPricePerMillion != nil || chroma.OutputPricePerMillion != nil {
		t.Error("image models must not carry token pricing")
	}
	if !chroma.Enabled {
		t.Error("discovered image model should be enabled")
	}
}

func TestDiscoverNanoGPTImageModels_NonSubscriptionBaseRegistersAll(t *testing.T) {
	server := newNanoGPTImageCatalogServer(t)
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), Name: "nanogpt-custom", BaseURL: server.URL + "/api/v1"}

	models, err := service.discoverNanoGPTImageModels(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPTImageModels failed: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected all 3 image models on a non-subscription base, got %d", len(models))
	}
	paid := findModelByID(models, "premium-paint")
	if paid == nil {
		t.Fatal("expected pay-as-you-go model to be registered on a non-subscription base")
	}
	if !strings.Contains(paid.Params, `"subscription_included":false`) {
		t.Errorf("premium-paint params = %q, want subscription_included false", paid.Params)
	}
}

func TestDiscoverNanoGPTImageModels_EditModelAcceptsImageInput(t *testing.T) {
	server := newNanoGPTImageCatalogServer(t)
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), Name: "nanogpt-test", BaseURL: server.URL + "/api/subscription/v1"}

	models, err := service.discoverNanoGPTImageModels(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverNanoGPTImageModels failed: %v", err)
	}
	edit := findModelByID(models, "step-image-edit-2")
	if edit == nil {
		t.Fatal("expected step-image-edit-2 to be registered")
	}
	if !strings.Contains(edit.InputModalities, "image") {
		t.Errorf("edit model input modalities = %q, want to contain image", edit.InputModalities)
	}
	if edit.Modality != "text+image->image" {
		t.Errorf("edit model modality = %q, want text+image->image", edit.Modality)
	}
}

func TestNanoGPTImageCatalogURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"subscription base", "https://nano-gpt.com/api/subscription/v1", "https://nano-gpt.com/api/models/image"},
		{"bare v1 base", "https://api.nano-gpt.com/v1", "https://api.nano-gpt.com/api/models/image"},
		{"trailing slash", "https://nano-gpt.com/api/subscription/v1/", "https://nano-gpt.com/api/models/image"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nanoGPTImageCatalogURL(tt.baseURL)
			if err != nil {
				t.Fatalf("nanoGPTImageCatalogURL(%q) error: %v", tt.baseURL, err)
			}
			if got != tt.want {
				t.Errorf("nanoGPTImageCatalogURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
