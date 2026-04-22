package auth

import (
	"crypto/aes"
	"crypto/cipher"
	cryptoRand "crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	keyLength   = 32
	nonceLength = 12

	// v1: fixed salt, 64MB memory (backward compatible)
	v1Salt  = "llm-proxy-fixed-salt-v1"
	v1Time  = 1
	v1Mem   = 64 * 1024
	v1Thr   = 4

	// v2: per-provider salt, 8MB memory (fast, secure for high-entropy keys)
	v2Time = 1
	v2Mem  = 8 * 1024
	v2Thr  = 4
)

type KeyPair struct {
	Ciphertext []byte
	Nonce      []byte
	Salt       []byte
}

func deriveKeyV1(masterKey string) []byte {
	return argon2.IDKey([]byte(masterKey), []byte(v1Salt), v1Time, v1Mem, v1Thr, keyLength)
}

func deriveKeyV2(masterKey string, salt []byte) []byte {
	return argon2.IDKey([]byte(masterKey), salt, v2Time, v2Mem, v2Thr, keyLength)
}

func encryptWithKey(plaintext string, key []byte) (*KeyPair, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, nonceLength)
	if _, err := io.ReadFull(cryptoRand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	return &KeyPair{
		Ciphertext: ciphertext,
		Nonce:      nonce,
	}, nil
}

func decryptWithKey(ciphertext, nonce []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// DeriveKey derives a key using v1 parameters (fixed salt, 64MB).
// Kept for backward compatibility.
func DeriveKey(masterKey string) []byte {
	return deriveKeyV1(masterKey)
}

// Encrypt encrypts plaintext using v2 parameters (per-provider random salt, 8MB).
// Generates a random 32-byte salt stored alongside the key.
func Encrypt(plaintext, masterKey string) (*KeyPair, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(cryptoRand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	key := deriveKeyV2(masterKey, salt)
	kp, err := encryptWithKey(plaintext, key)
	if err != nil {
		return nil, err
	}
	kp.Salt = salt
	return kp, nil
}

// Decrypt decrypts ciphertext. If salt is nil/empty, uses v1 (backward compatible).
// If salt is provided, uses v2.
func Decrypt(ciphertext, nonce, salt []byte, masterKey string) (string, error) {
	var key []byte
	if len(salt) == 0 {
		key = deriveKeyV1(masterKey)
	} else {
		key = deriveKeyV2(masterKey, salt)
	}
	return decryptWithKey(ciphertext, nonce, key)
}

func GenerateRandomKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(cryptoRand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(key), nil
}

func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}


