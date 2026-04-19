package auth

import (
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

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, masterKey)
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

	if string(encrypted1.Ciphertext) == string(encrypted2.Ciphertext) {
		t.Error("Different master keys should produce different ciphertexts")
	}

	_, err = Decrypt(encrypted1.Ciphertext, encrypted1.Nonce, masterKey2)
	if err == nil {
		t.Error("Decrypting with wrong master key should fail")
	}
}

func TestGenerateRandomKey(t *testing.T) {
	key1, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	if len(key1) == 0 {
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
