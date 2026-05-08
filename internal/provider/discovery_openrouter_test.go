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

func TestGetOpenRouterBalance_Success(t *testing.T) {
	// Create test server with mock OpenRouter responses
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		requestCount++
		switch r.URL.Path {
		case "/credits":
			// Mock OpenRouter credits response
			response := OpenRouterCreditsResponse{
				Data: OpenRouterCreditsData{
					TotalCredits: 100.0,
					TotalUsage:   25.0,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case "/key":
			// Mock OpenRouter key response
			limit := float64(1000.0)
			limitRemaining := float64(975.0)
			response := OpenRouterKeyResponse{
				Data: OpenRouterKeyData{
					Label:              "Test Key",
					Limit:              &limit,
					LimitReset:         "2024-02-01T00:00:00Z",
					LimitRemaining:     &limitRemaining,
					IncludeByokInLimit: false,
					Usage:              25.0,
					UsageDaily:         5.0,
					UsageWeekly:        15.0,
					UsageMonthly:       25.0,
					ByokUsage:          0.0,
					IsFreeTier:         false,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
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

	// Test balance fetch
	balance, err := service.GetOpenRouterBalance(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetOpenRouterBalance failed: %v", err)
	}

	// Verify results
	if balance.Label != "Test Key" {
		t.Errorf("Expected Label 'Test Key', got '%s'", balance.Label)
	}
	if balance.Limit == nil || *balance.Limit != 1000.0 {
		t.Errorf("Expected Limit 1000.0, got %v", balance.Limit)
	}
	if balance.LimitReset != "2024-02-01T00:00:00Z" {
		t.Errorf("Expected LimitReset '2024-02-01T00:00:00Z', got '%s'", balance.LimitReset)
	}
	if balance.LimitRemaining == nil || *balance.LimitRemaining != 975.0 {
		t.Errorf("Expected LimitRemaining 975.0, got %v", balance.LimitRemaining)
	}
	if balance.Usage != 25.0 {
		t.Errorf("Expected Usage 25.0, got %f", balance.Usage)
	}
	if balance.UsageDaily != 5.0 {
		t.Errorf("Expected UsageDaily 5.0, got %f", balance.UsageDaily)
	}
	if balance.UsageWeekly != 15.0 {
		t.Errorf("Expected UsageWeekly 15.0, got %f", balance.UsageWeekly)
	}
	if balance.UsageMonthly != 25.0 {
		t.Errorf("Expected UsageMonthly 25.0, got %f", balance.UsageMonthly)
	}
	if balance.CreditsTotal != 100.0 {
		t.Errorf("Expected CreditsTotal 100.0, got %f", balance.CreditsTotal)
	}
	if balance.CreditsUsed != 25.0 {
		t.Errorf("Expected CreditsUsed 25.0, got %f", balance.CreditsUsed)
	}
	if balance.CreditsRemaining != 75.0 {
		t.Errorf("Expected CreditsRemaining 75.0, got %f", balance.CreditsRemaining)
	}
	if balance.IsFreeTier {
		t.Error("Expected IsFreeTier to be false")
	}

	// Verify both endpoints were called
	if requestCount != 2 {
		t.Errorf("Expected 2 requests (credits + key), got %d", requestCount)
	}
}

func TestGetOpenRouterBalance_Non200StatusCredits(t *testing.T) {
	// Create test server that returns 401 for credits endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/credits" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
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

	_, err = service.GetOpenRouterBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for non-200 status on credits endpoint, got nil")
	}
}

func TestGetOpenRouterBalance_Non200StatusKey(t *testing.T) {
	// Create test server that returns 200 for credits but 401 for key
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/credits":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(OpenRouterCreditsResponse{
				Data: OpenRouterCreditsData{TotalCredits: 100.0, TotalUsage: 0.0},
			})
		case "/key":
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
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

	_, err = service.GetOpenRouterBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for non-200 status on key endpoint, got nil")
	}
}

func TestGetOpenRouterBalance_InvalidJSON(t *testing.T) {
	// Create test server with invalid JSON response for credits
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/credits" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json "))
		} else {
			http.NotFound(w, r)
		}
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

	_, err = service.GetOpenRouterBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestGetOpenRouterBalance_DecryptionError(t *testing.T) {
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
		BaseURL:      "https://openrouter.ai",
		EncryptedKey: wrongKeyPair.Ciphertext,
		KeyNonce:     wrongKeyPair.Nonce,
		KeySalt:      wrongKeyPair.Salt,
	}

	_, err = service.GetOpenRouterBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for decryption failure, got nil")
	}
}

func TestGetOpenRouterBalance_ContextCancellation(t *testing.T) {
	// Create a slow test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(OpenRouterCreditsResponse{
			Data: OpenRouterCreditsData{TotalCredits: 100.0, TotalUsage: 0.0},
		})
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

	_, err = service.GetOpenRouterBalance(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for context cancellation, got nil")
	}
}
