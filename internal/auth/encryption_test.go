package auth

import (
	"bytes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	masterKey := "test-master-key-123"
	plaintext := "my-secret-api-key-sk-test123"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if len(encrypted.Ciphertext) == 0 {
		t.Fatal("Ciphertext is empty")
	}

	if len(encrypted.Nonce) != nonceLength {
		t.Fatalf("Expected nonce length %d, got %d", nonceLength, len(encrypted.Nonce))
	}

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match original. Expected %q, got %q", plaintext, decrypted)
	}
}

func TestDifferentMasterKeys(t *testing.T) {
	plaintext := "my-api-key"
	masterKey1 := "master-key-1"
	masterKey2 := "master-key-2"

	encrypted1, err := Encrypt(plaintext, masterKey1)
	if err != nil {
		t.Fatalf("Encrypt with key 1 failed: %v", err)
	}

	encrypted2, err := Encrypt(plaintext, masterKey2)
	if err != nil {
		t.Fatalf("Encrypt with key 2 failed: %v", err)
	}

	if bytes.Equal(encrypted1.Ciphertext, encrypted2.Ciphertext) {
		t.Error("Different master keys should produce different ciphertexts")
	}

	_, err = Decrypt(encrypted1.Ciphertext, encrypted1.Nonce, encrypted1.Salt, masterKey2)
	if err == nil {
		t.Error("Decrypting with wrong master key should fail")
	}
}

func TestGenerateRandomKey(t *testing.T) {
	key1, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	if key1 == "" {
		t.Fatal("Generated key is empty")
	}

	key2, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	if key1 == key2 {
		t.Error("Generated keys should be different")
	}
}

func TestConstantTimeCompare(t *testing.T) {
	a := "secret-key"
	b := "secret-key"
	c := "different-key"

	if !ConstantTimeCompare(a, b) {
		t.Error("ConstantTimeCompare should return true for matching strings")
	}

	if ConstantTimeCompare(a, c) {
		t.Error("ConstantTimeCompare should return false for different strings")
	}
}

func TestEncryptWithKey_InvalidKey(t *testing.T) {
	// AES requires exactly 16, 24, or 32 byte keys
	_, err := encryptWithKey("test", []byte{1, 2, 3}) // 3 bytes - invalid
	if err == nil {
		t.Error("expected error with invalid key size")
	}
}

func TestEncryptWithKey_ShortKey(t *testing.T) {
	// 16-byte key should work for AES-128
	kp, err := encryptWithKey("hello", make([]byte, 16))
	if err != nil {
		t.Errorf("expected no error with 16-byte key, got: %v", err)
	}
	if kp == nil {
		t.Error("expected non-nil KeyPair")
	}
}
func TestDecryptWithKey_InvalidKeySize(t *testing.T) {
	// AES requires exactly 16, 24, or 32 byte keys
	_, err := decryptWithKey([]byte("ciphertext"), []byte("123456789012"), []byte{1, 2, 3})
	if err == nil {
		t.Error("expected error with invalid key size")
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	masterKey := "test-master-key-123"
	plaintext := ""

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match original. Expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncrypt_NonceRandomization(t *testing.T) {
	masterKey := "test-master-key-123"
	plaintext := "same-plaintext"

	encrypted1, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt 1 failed: %v", err)
	}

	encrypted2, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}

	// Ciphertexts should be different due to random nonce
	if bytes.Equal(encrypted1.Ciphertext, encrypted2.Ciphertext) {
		t.Error("Encrypt should produce different ciphertexts each call (nonce randomization)")
	}

	// Both should decrypt to the same plaintext
	decrypted1, err := Decrypt(encrypted1.Ciphertext, encrypted1.Nonce, encrypted1.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt 1 failed: %v", err)
	}

	decrypted2, err := Decrypt(encrypted2.Ciphertext, encrypted2.Nonce, encrypted2.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt 2 failed: %v", err)
	}

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Errorf("Both should decrypt to original. Got %q and %q", decrypted1, decrypted2)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	masterKey := "correct-master-key"
	wrongKey := "wrong-master-key"
	plaintext := "secret-data"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, wrongKey)
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestDecrypt_InvalidCiphertext(t *testing.T) {
	masterKey := "test-master-key-123"

	// Random bytes that weren't produced by Encrypt
	randomCiphertext := []byte("not-valid-gcm-ciphertext")
	randomNonce := make([]byte, nonceLength)
	randomSalt := make([]byte, 32)

	_, err := Decrypt(randomCiphertext, randomNonce, randomSalt, masterKey)
	if err == nil {
		t.Error("Decrypt with invalid ciphertext should fail")
	}
}

func TestDecrypt_WrongNonceLength(t *testing.T) {
	masterKey := "test-master-key-123"
	salt := make([]byte, 32)
	ciphertext := []byte("some-ciphertext")

	// A nonce of the wrong length must return an error, not panic the GCM Open.
	for _, n := range []int{0, 1, nonceLength - 1, nonceLength + 1} {
		if _, err := Decrypt(ciphertext, make([]byte, n), salt, masterKey); err == nil {
			t.Errorf("Decrypt with nonce length %d should fail, got nil", n)
		}
	}
}

func TestDecrypt_MissingSalt(t *testing.T) {
	masterKey := "test-master-key-123"
	ciphertext := []byte("some-ciphertext")
	nonce := make([]byte, nonceLength)

	_, err := Decrypt(ciphertext, nonce, nil, masterKey)
	if err == nil {
		t.Error("Decrypt with nil salt should fail")
	}

	_, err = Decrypt(ciphertext, nonce, []byte{}, masterKey)
	if err == nil {
		t.Error("Decrypt with empty salt should fail")
	}
}

func TestGenerateRandomKey_Length(t *testing.T) {
	key, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	// base64.RawURLEncoding of 32 bytes = 43 characters
	expectedLen := 43
	if len(key) != expectedLen {
		t.Errorf("Expected key length %d, got %d", expectedLen, len(key))
	}
}

func TestGenerateRandomKey_Randomness(t *testing.T) {
	keys := make(map[string]bool)
	for range 100 {
		key, err := GenerateRandomKey()
		if err != nil {
			t.Fatalf("GenerateRandomKey failed: %v", err)
		}
		if keys[key] {
			t.Fatal("GenerateRandomKey produced duplicate key")
		}
		keys[key] = true
	}
}

func TestDecryptWithKey_WrongCiphertext(t *testing.T) {
	// Valid key size but wrong ciphertext should fail GCM authentication
	_, err := decryptWithKey([]byte("wrong-ciphertext"), make([]byte, 12), make([]byte, 32))
	if err == nil {
		t.Error("expected error with wrong ciphertext")
	}
}

func TestEncryptDecrypt_LargePlaintext(t *testing.T) {
	t.Parallel()
	masterKey := "test-master-key-123"
	// Generate 100KB of data
	data := make([]byte, 100*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	plaintext := string(data)

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match original. Lengths: expected %d, got %d", len(plaintext), len(decrypted))
	}
}

func TestEncryptDecrypt_UnicodeAndSpecialChars(t *testing.T) {
	t.Parallel()
	masterKey := "test-master-key-123"
	// Test with emojis, CJK characters, and null bytes
	plaintext := "Hello 世界 🌍🎉\x00null\x00bytes 日本語 Ελληνικά"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match original. Expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptDecrypt_EmptyMasterKey(t *testing.T) {
	t.Parallel()
	masterKey := ""
	plaintext := "test-data"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt with empty master key failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt with empty master key failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match original. Expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptDecrypt_Concurrent(t *testing.T) {
	t.Parallel()
	masterKey := "test-master-key-123"
	plaintext := "concurrent-test-data"
	iterations := 100

	results := make(chan string, iterations)
	for range iterations {
		go func() {
			encrypted, err := Encrypt(plaintext, masterKey)
			if err != nil {
				results <- "encrypt-error: " + err.Error()
				return
			}
			decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
			if err != nil {
				results <- "decrypt-error: " + err.Error()
				return
			}
			results <- decrypted
		}()
	}

	for range iterations {
		result := <-results
		if result != plaintext {
			t.Errorf("Concurrent operation failed: expected %q, got %q", plaintext, result)
		}
	}
}

func TestDeriveKey_EmptyMasterKey(t *testing.T) {
	t.Parallel()
	salt := make([]byte, 32)
	key := deriveKey("", salt)
	if len(key) != keyLength {
		t.Errorf("Expected key length %d, got %d", keyLength, len(key))
	}
	// Empty master key should still produce a deterministic key
	key2 := deriveKey("", salt)
	if !bytes.Equal(key, key2) {
		t.Error("Same salt and empty master key should produce same derived key")
	}
}

func TestDeriveKey_DifferentSaltsProduceDifferentKeys(t *testing.T) {
	t.Parallel()
	salt1 := make([]byte, 32)
	salt2 := make([]byte, 32)
	// Make salts different
	salt1[0] = 0x01
	salt2[0] = 0x02

	key1 := deriveKey("master", salt1)
	key2 := deriveKey("master", salt2)

	if bytes.Equal(key1, key2) {
		t.Error("Different salts should produce different derived keys")
	}
}

func TestDeriveKey_OutputLength(t *testing.T) {
	t.Parallel()
	salt := make([]byte, 32)

	testCases := []struct {
		name      string
		masterKey string
	}{
		{"single char", "a"},
		{"16 chars", "1234567890123456"},
		{"100 chars", strings.Repeat("x", 100)},
		{"empty string", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			key := deriveKey(tc.masterKey, salt)
			if len(key) != keyLength {
				t.Errorf("Expected key length %d for %q, got %d", keyLength, tc.name, len(key))
			}
		})
	}
}

func TestDecrypt_BitflipCiphertext(t *testing.T) {
	t.Parallel()
	masterKey := "test-master-key"
	plaintext := "secret-data"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Flip a single bit in the ciphertext
	encrypted.Ciphertext[0] ^= 0x01

	_, err = Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err == nil {
		t.Error("Decrypt with bit-flipped ciphertext should fail")
	}
}

func TestDecrypt_BitflipNonce(t *testing.T) {
	t.Parallel()
	masterKey := "test-master-key"
	plaintext := "secret-data"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Flip a single bit in the nonce
	encrypted.Nonce[0] ^= 0x01

	_, err = Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err == nil {
		t.Error("Decrypt with bit-flipped nonce should fail")
	}
}

func TestDecrypt_TruncatedCiphertext(t *testing.T) {
	t.Parallel()
	masterKey := "test-master-key"
	plaintext := "secret-data"

	encrypted, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Truncate the last 16 bytes (GCM auth tag)
	if len(encrypted.Ciphertext) > 16 {
		encrypted.Ciphertext = encrypted.Ciphertext[:len(encrypted.Ciphertext)-16]
	}

	_, err = Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err == nil {
		t.Error("Decrypt with truncated ciphertext should fail")
	}
}

func TestConstantTimeCompare_EdgeCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{"empty strings", "", "", true},
		{"empty vs non-empty", "", "a", false},
		{"null bytes equal", "a\x00b", "a\x00b", true},
		{"null bytes different", "a\x00b", "a\x01b", false},
		{"long strings equal", strings.Repeat("x", 10240), strings.Repeat("x", 10240), true},
		{"long strings different", strings.Repeat("x", 10240), strings.Repeat("y", 10240), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ConstantTimeCompare(tc.a, tc.b)
			if result != tc.expected {
				t.Errorf("ConstantTimeCompare(%q, %q) = %v, expected %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

func TestEncryptDecrypt_MasterKeyBoundaryLengths(t *testing.T) {
	t.Parallel()
	plaintext := "test-data"

	testCases := []struct {
		name      string
		masterKey string
	}{
		{"1-char key", "a"},
		{"1000-char key", strings.Repeat("x", 1000)},
		{"key with null bytes", "key\x00with\x00nulls"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := Encrypt(plaintext, tc.masterKey)
			if err != nil {
				t.Fatalf("Encrypt with %s failed: %v", tc.name, err)
			}

			decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, tc.masterKey)
			if err != nil {
				t.Fatalf("Decrypt with %s failed: %v", tc.name, err)
			}

			if decrypted != plaintext {
				t.Errorf("Decrypted text doesn't match original for %s. Expected %q, got %q", tc.name, plaintext, decrypted)
			}
		})
	}
}

func TestGenerateRandomKey_Base64URLFormat(t *testing.T) {
	t.Parallel()
	key, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	// Check for base64url characters that should NOT be present
	if strings.Contains(key, "+") {
		t.Error("Generated key should not contain '+' (base64url encoding)")
	}
	if strings.Contains(key, "/") {
		t.Error("Generated key should not contain '/' (base64url encoding)")
	}
	if strings.Contains(key, "=") {
		t.Error("Generated key should not contain '=' (base64url encoding)")
	}

	// Decode and verify length
	decoded, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("Failed to decode key: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("Expected decoded key length 32, got %d", len(decoded))
	}
}

// failAfterReader succeeds for n reads then fails.
type failAfterReader struct {
	reads int
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.reads <= 0 {
		return 0, fmt.Errorf("mock rand error")
	}
	r.reads--
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestEncryptWithKey_NewCipherBlockError(t *testing.T) {
	orig := newCipherBlock
	defer func() { newCipherBlock = orig }()
	newCipherBlock = func([]byte) (cipher.Block, error) {
		return nil, fmt.Errorf("mock cipher block error")
	}
	_, err := encryptWithKey("test", make([]byte, 32))
	if err == nil {
		t.Error("expected error when newCipherBlock fails")
	}
}

func TestEncryptWithKey_NewGCMError(t *testing.T) {
	orig := newGCM
	defer func() { newGCM = orig }()
	newGCM = func(cipher.Block) (cipher.AEAD, error) {
		return nil, fmt.Errorf("mock GCM error")
	}
	_, err := encryptWithKey("test", make([]byte, 32))
	if err == nil {
		t.Error("expected error when newGCM fails")
	}
}

func TestEncryptWithKey_NonceGenerationError(t *testing.T) {
	orig := randReader
	defer func() { randReader = orig }()
	randReader = &failAfterReader{reads: 0}
	_, err := encryptWithKey("test", make([]byte, 32))
	if err == nil {
		t.Error("expected error when nonce generation fails")
	}
}

func TestDecryptWithKey_NewCipherBlockError(t *testing.T) {
	orig := newCipherBlock
	defer func() { newCipherBlock = orig }()
	newCipherBlock = func([]byte) (cipher.Block, error) {
		return nil, fmt.Errorf("mock cipher block error")
	}
	_, err := decryptWithKey([]byte("ct"), []byte("123456789012"), make([]byte, 32))
	if err == nil {
		t.Error("expected error when newCipherBlock fails")
	}
}

func TestDecryptWithKey_NewGCMError(t *testing.T) {
	orig := newGCM
	defer func() { newGCM = orig }()
	newGCM = func(cipher.Block) (cipher.AEAD, error) {
		return nil, fmt.Errorf("mock GCM error")
	}
	_, err := decryptWithKey([]byte("ct"), []byte("123456789012"), make([]byte, 32))
	if err == nil {
		t.Error("expected error when newGCM fails")
	}
}

func TestEncrypt_SaltGenerationError(t *testing.T) {
	orig := randReader
	defer func() { randReader = orig }()
	randReader = &failAfterReader{reads: 0}
	_, err := Encrypt("test", "master")
	if err == nil {
		t.Error("expected error when salt generation fails")
	}
}

func TestEncrypt_EncryptWithKeyError(t *testing.T) {
	orig := randReader
	defer func() { randReader = orig }()
	// Succeed for salt (1 read), fail for nonce (2nd read)
	randReader = &failAfterReader{reads: 1}
	_, err := Encrypt("test", "master")
	if err == nil {
		t.Error("expected error when encryptWithKey fails")
	}
}

func TestGenerateRandomKey_RandReaderError(t *testing.T) {
	orig := randReader
	defer func() { randReader = orig }()
	randReader = &failAfterReader{reads: 0}
	_, err := GenerateRandomKey()
	if err == nil {
		t.Error("expected error when rand reader fails")
	}
}
