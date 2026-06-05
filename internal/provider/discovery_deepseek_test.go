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

func TestGetDeepSeekBalance_Success(t *testing.T) {
	// Create test server with mock DeepSeek balance response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/balance" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock DeepSeek balance response
		response := DeepSeekBalanceResponse{
			IsAvailable: true,
			BalanceInfos: []DeepSeekBalanceInfo{
				{
					Currency:        "CNY",
					TotalBalance:    "200.00",
					GrantedBalance:  "150.00",
					ToppedUpBalance: "50.00",
				},
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

	// Test balance fetch
	balance, err := service.GetDeepSeekBalance(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetDeepSeekBalance failed: %v", err)
	}

	// Verify results
	if !balance.IsAvailable {
		t.Error("Expected IsAvailable to be true")
	}
	if len(balance.BalanceInfos) != 1 {
		t.Fatalf("Expected 1 balance info, got %d", len(balance.BalanceInfos))
	}
	if balance.BalanceInfos[0].Currency != "CNY" {
		t.Errorf("Expected Currency 'CNY', got '%s'", balance.BalanceInfos[0].Currency)
	}
	if balance.BalanceInfos[0].TotalBalance != "200.00" {
		t.Errorf("Expected TotalBalance '200.00', got '%s'", balance.BalanceInfos[0].TotalBalance)
	}
	if balance.BalanceInfos[0].GrantedBalance != "150.00" {
		t.Errorf("Expected GrantedBalance '150.00', got '%s'", balance.BalanceInfos[0].GrantedBalance)
	}
	if balance.BalanceInfos[0].ToppedUpBalance != "50.00" {
		t.Errorf("Expected ToppedUpBalance '50.00', got '%s'", balance.BalanceInfos[0].ToppedUpBalance)
	}
}

func TestGetDeepSeekBalance_Non200Status(t *testing.T) {
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

	_, err = service.GetDeepSeekBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
}

func TestGetDeepSeekBalance_InvalidJSON(t *testing.T) {
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

	_, err = service.GetDeepSeekBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestGetDeepSeekBalance_DecryptionError(t *testing.T) {
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
		BaseURL:      "https://api.deepseek.com",
		EncryptedKey: wrongKeyPair.Ciphertext,
		KeyNonce:     wrongKeyPair.Nonce,
		KeySalt:      wrongKeyPair.Salt,
	}

	_, err = service.GetDeepSeekBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for decryption failure, got nil")
	}
}

func TestGetDeepSeekBalance_ContextCancellation(t *testing.T) {
	// Create a slow test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeepSeekBalanceResponse{IsAvailable: true, BalanceInfos: []DeepSeekBalanceInfo{}})
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

	_, err = service.GetDeepSeekBalance(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for context cancellation, got nil")
	}
}
