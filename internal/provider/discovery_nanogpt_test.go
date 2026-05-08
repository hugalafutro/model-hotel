package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
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
		time.Sleep(500 * time.Millisecond)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = service.GetNanoGPTUsage(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for context cancellation, got nil")
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
