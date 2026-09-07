package provider

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

func TestFetchQuotaJSON_InvalidBaseURL(t *testing.T) {
	masterKey := "test-master-key-for-testing-only-32bytes!"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("Failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		Name:         "bad-url-provider",
		BaseURL:      "http://example.com/\x7f",
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	service := &DiscoveryService{}

	_, err = service.GetDeepSeekBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("expected error for base URL with control character, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("error = %q, want it to mention request creation failure", err)
	}
}

// TestGetNeuralWattQuota_FreeTier404_NoErrorLog covers the quota poll of a
// free-tier NeuralWatt key: /quota answers 404 on every poll, which is the
// expected shape of that plan and must not produce an ERROR line, or a fleet
// with such a key emits a permanent error stream into the app log.
func TestGetNeuralWattQuota_FreeTier404_NoErrorLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
	}))
	defer server.Close()

	var logged strings.Builder
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { debuglog.SetHandler(debuglog.StdoutHandler()) })

	masterKey := "test-master-key-for-testing-only-32bytes!"
	provider, err := newQuotaTestProvider(server.URL, "free-tier-key", masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := &DiscoveryService{httpClient: server.Client()}

	quota, err := service.GetNeuralWattQuota(context.Background(), provider, masterKey)
	if err != nil || quota != nil {
		t.Fatalf("GetNeuralWattQuota = (%v, %v), want (nil, nil)", quota, err)
	}
	if strings.Contains(logged.String(), "level=ERROR") {
		t.Errorf("free-tier 404 logged at ERROR: %s", logged.String())
	}
	if !strings.Contains(logged.String(), "likely free tier") {
		t.Errorf("expected the free-tier INFO line, got: %s", logged.String())
	}
}

// TestFetchQuotaJSON_ErrorNamesResource keeps the endpoint that failed in the
// operator-visible error: OpenRouter reads two of them per balance fetch, so a
// bare status cannot say which one broke.
func TestFetchQuotaJSON_ErrorNamesResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/key") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"total_credits":5,"total_usage":1}}`))
	}))
	defer server.Close()

	masterKey := "test-master-key-for-testing-only-32bytes!"
	provider, err := newQuotaTestProvider(server.URL, "or-key", masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := &DiscoveryService{httpClient: server.Client()}

	_, err = service.GetOpenRouterBalance(context.Background(), provider, masterKey)
	if err == nil {
		t.Fatal("expected an error for a 500 on the key endpoint")
	}
	if !strings.Contains(err.Error(), "key info") {
		t.Errorf("error = %q, want it to name the key info resource", err)
	}
}

// TestDecryptProviderKey_NoStoredKey covers a provider saved without a
// credential: the fetch that follows must fail on what the upstream says about
// the missing key, not on a crypto error about an empty salt.
func TestDecryptProviderKey_NoStoredKey(t *testing.T) {
	key, err := decryptProviderKey(&Provider{Name: "keyless"}, "test-master-key-for-testing-only-32bytes!", "openai")
	if err != nil {
		t.Fatalf("decryptProviderKey = %v, want no error for an absent key", err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

func newQuotaTestProvider(baseURL, apiKey, masterKey string) (*Provider, error) {
	keyPair, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		return nil, err
	}
	return &Provider{
		ID:           uuid.New(),
		Name:         "quota-test",
		BaseURL:      baseURL,
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}, nil
}
