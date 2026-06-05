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

func TestGetNeuralWattQuota_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quota" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		response := NeuralWattQuotaResponse{
			SnapshotAt: "2026-06-02T17:42:29Z",
			Balance: NeuralWattQuotaBalance{
				CreditsRemainingUSD: 23.9,
				TotalCreditsUSD:     23.9,
				CreditsUsedUSD:      0.0,
				AccountingMethod:    "energy",
			},
			Usage: NeuralWattQuotaUsage{
				Lifetime: NeuralWattQuotaUsagePeriod{
					CostUSD:   21.2366,
					Requests:  10870,
					Tokens:    1135360843,
					EnergyKWh: 4.2473,
				},
				CurrentMonth: NeuralWattQuotaUsagePeriod{
					CostUSD:   6.905,
					Requests:  3699,
					Tokens:    418956390,
					EnergyKWh: 1.381,
				},
			},
			Limits: NeuralWattQuotaLimits{
				OverageLimitUSD: nil,
				RateLimitTier:   "standard",
			},
			Subscription: NeuralWattQuotaSubscription{
				Plan:               "standard",
				Status:             "active",
				BillingInterval:    "month",
				CurrentPeriodStart: "2026-05-28T00:43:33Z",
				CurrentPeriodEnd:   "2026-06-28T00:43:33Z",
				AutoRenew:          true,
				KWhIncluded:        16.0,
				KWhUsed:            2.0283,
				KWhRemaining:       13.9717,
				InOverage:          false,
			},
			Key: NeuralWattQuotaKey{
				Name:      "testing",
				Allowance: nil,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
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

	quota, err := service.GetNeuralWattQuota(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("GetNeuralWattQuota failed: %v", err)
	}

	if quota.Balance.CreditsRemainingUSD != 23.9 {
		t.Errorf("Expected CreditsRemainingUSD 23.9, got %f", quota.Balance.CreditsRemainingUSD)
	}
	if quota.Balance.AccountingMethod != "energy" {
		t.Errorf("Expected AccountingMethod 'energy', got '%s'", quota.Balance.AccountingMethod)
	}
	if quota.Usage.Lifetime.Requests != 10870 {
		t.Errorf("Expected Lifetime Requests 10870, got %d", quota.Usage.Lifetime.Requests)
	}
	if quota.Usage.CurrentMonth.EnergyKWh != 1.381 {
		t.Errorf("Expected CurrentMonth EnergyKWh 1.381, got %f", quota.Usage.CurrentMonth.EnergyKWh)
	}
	if quota.Limits.OverageLimitUSD != nil {
		t.Errorf("Expected OverageLimitUSD nil, got %v", quota.Limits.OverageLimitUSD)
	}
	if quota.Limits.RateLimitTier != "standard" {
		t.Errorf("Expected RateLimitTier 'standard', got '%s'", quota.Limits.RateLimitTier)
	}
	if !quota.Subscription.AutoRenew {
		t.Error("Expected AutoRenew true")
	}
	if quota.Subscription.KWhRemaining != 13.9717 {
		t.Errorf("Expected KWhRemaining 13.9717, got %f", quota.Subscription.KWhRemaining)
	}
	if quota.Subscription.InOverage {
		t.Error("Expected InOverage false")
	}
	if quota.Key.Name != "testing" {
		t.Errorf("Expected Key Name 'testing', got '%s'", quota.Key.Name)
	}
	if quota.Key.Allowance != nil {
		t.Errorf("Expected Key Allowance nil, got %v", quota.Key.Allowance)
	}
}

func TestGetNeuralWattQuota_FreeTier404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Free tier returns 404 on /v1/quota
		w.WriteHeader(http.StatusNotFound)
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

	quota, err := service.GetNeuralWattQuota(context.Background(), provider, masterKey)
	if err != nil {
		t.Fatalf("Expected nil error for 404 (free tier), got: %v", err)
	}
	if quota != nil {
		t.Errorf("Expected nil quota for 404 (free tier), got non-nil")
	}
}

func TestGetNeuralWattQuota_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient:     server.Client(),
		retryBaseDelay: time.Millisecond,
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

	_, err = service.GetNeuralWattQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
}

func TestGetNeuralWattQuota_InvalidJSON(t *testing.T) {
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

	_, err = service.GetNeuralWattQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestGetNeuralWattQuota_DecryptionError(t *testing.T) {
	service := &DiscoveryService{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	masterKey := "test-master-key-for-testing-only-32bytes!"
	wrongKeyPair, err := auth.Encrypt("test-api-key", "different-master-key-32bytes!!")
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.neuralwatt.com",
		EncryptedKey: wrongKeyPair.Ciphertext,
		KeyNonce:     wrongKeyPair.Nonce,
		KeySalt:      wrongKeyPair.Salt,
	}

	_, err = service.GetNeuralWattQuota(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for decryption failure, got nil")
	}
}

func TestGetNeuralWattQuota_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NeuralWattQuotaResponse{})
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err = service.GetNeuralWattQuota(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for context cancellation, got nil")
	}
}
