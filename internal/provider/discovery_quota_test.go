package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
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
