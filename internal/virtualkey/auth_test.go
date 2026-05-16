package virtualkey

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestGenerateReturnsKeyWithPrefix(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	if !strings.HasPrefix(key, "sk-") {
		t.Errorf("Generated key should have 'sk-' prefix, got %q", key)
	}
}

func TestGenerateReturnsNonEmptyKey(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	// "sk-" (3 chars) + 16 bytes hex-encoded (32 chars) = 35 chars
	if len(key) != 35 {
		t.Errorf("Generated key should be 35 chars (sk- + 32 hex), got %d chars: %q", len(key), key)
	}
}

func TestGenerateProducesUniqueKeys(t *testing.T) {
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key, err := Generate()
		if err != nil {
			t.Fatalf("Generate() failed on iteration %d: %v", i, err)
		}
		if keys[key] {
			t.Errorf("Duplicate key generated: %q", key)
		}
		keys[key] = true
	}
}

func TestGenerateKeyIsHexEncoded(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	hexPart := strings.TrimPrefix(key, "sk-")
	for _, c := range hexPart {
		if !isHexChar(c) {
			t.Errorf("Key part after 'sk-' should be hex-encoded, found non-hex char %q in %q", c, key)
			break
		}
	}
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

func TestHashReturnsSHA256Hex(t *testing.T) {
	input := "sk-test-key-12345"
	hash := Hash(input)

	// Compute expected hash manually
	expected := sha256.Sum256([]byte(input))
	expectedHex := hex.EncodeToString(expected[:])

	if hash != expectedHex {
		t.Errorf("Hash(%q) = %q, want %q", input, hash, expectedHex)
	}
}

func TestHashReturns64CharString(t *testing.T) {
	hash := Hash("any-input")
	if len(hash) != 64 {
		t.Errorf("Hash should return 64-char hex string (SHA-256), got %d chars", len(hash))
	}
}

func TestHashIsDeterministic(t *testing.T) {
	input := "sk-same-key"
	h1 := Hash(input)
	h2 := Hash(input)
	if h1 != h2 {
		t.Errorf("Hash should be deterministic: got %q then %q for same input", h1, h2)
	}
}

func TestHashDifferentInputsProduceDifferentOutputs(t *testing.T) {
	h1 := Hash("key-one")
	h2 := Hash("key-two")
	if h1 == h2 {
		t.Error("Hash should produce different outputs for different inputs")
	}
}

func TestHashEmptyString(t *testing.T) {
	hash := Hash("")
	if hash == "" {
		t.Error("Hash of empty string should not be empty")
	}
	// SHA-256 of empty string is well-known
	expected := sha256.Sum256([]byte(""))
	expectedHex := hex.EncodeToString(expected[:])
	if hash != expectedHex {
		t.Errorf("Hash('') = %q, want %q", hash, expectedHex)
	}
}

func TestHashAndGenerateCompatible(t *testing.T) {
	// Generate a key and verify we can hash it
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	hash := Hash(key)
	if len(hash) != 64 {
		t.Errorf("Hash of generated key should be 64 chars, got %d", len(hash))
	}
}

func TestGenerate_RandReaderError(t *testing.T) {
	orig := randReader
	defer func() { randReader = orig }()
	randReader = &failReader{}
	_, err := Generate()
	if err == nil {
		t.Error("expected error when rand reader fails")
	}
}

type failReader struct{}

func (f *failReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("mock rand error")
}

func TestHashIsOneWay(t *testing.T) {
	// The hash should not contain the original input
	input := "sk-secret-key-value"
	hash := Hash(input)
	if strings.Contains(hash, input) {
		t.Error("Hash output should not contain the original input string")
	}
}
