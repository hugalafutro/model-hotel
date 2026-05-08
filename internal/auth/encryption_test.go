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

	decrypted, err := Decrypt(encrypted.Ciphertext, encrypted.Nonce, encrypted.Salt, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match original. Expected %q, got %q", plaintext, decrypted)
	}
}

func TestV1BackwardCompatibility(t *testing.T) {
	masterKey := "test-master-key-123"
	plaintext := "my-v1-api-key"

	v1Key := deriveKeyV1(masterKey)
	kp, err := encryptWithKey(plaintext, v1Key)
	if err != nil {
		t.Fatalf("encryptWithKey failed: %v", err)
	}

	decrypted, err := Decrypt(kp.Ciphertext, kp.Nonce, nil, masterKey)
	if err != nil {
		t.Fatalf("Decrypt with nil salt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("v1 decrypt mismatch: expected %q, got %q", plaintext, decrypted)
	}

	decrypted2, err := Decrypt(kp.Ciphertext, kp.Nonce, []byte{}, masterKey)
	if err != nil {
		t.Fatalf("Decrypt with empty salt failed: %v", err)
	}
	if decrypted2 != plaintext {
		t.Errorf("v1 decrypt (empty salt) mismatch: expected %q, got %q", plaintext, decrypted2)
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

func TestDeriveKey(t *testing.T) {
	key := DeriveKey("test-master-key")
	if len(key) == 0 {
		t.Error("DeriveKey should return non-empty key")
	}
	// Same input should produce same output
	key2 := DeriveKey("test-master-key")
	if string(key) != string(key2) {
		t.Error("DeriveKey should be deterministic")
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

func TestDecryptWithKey_WrongCiphertext(t *testing.T) {
	// Valid key size but wrong ciphertext should fail GCM authentication
	_, err := decryptWithKey([]byte("wrong-ciphertext"), make([]byte, 12), make([]byte, 32))
	if err == nil {
		t.Error("expected error with wrong ciphertext")
	}
}
