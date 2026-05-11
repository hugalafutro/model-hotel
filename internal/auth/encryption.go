// Package auth provides encryption and decryption for API keys.
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

	// Argon2id parameters for key derivation.
	//
	// The parameters (t=1, m=8MB, p=4) are intentionally below the RFC 9106
	// minimum (t=3, m=64MB). This is deliberate: MASTER_KEY is a high-entropy
	// random value (32+ bytes), not a user-chosen password. Argon2id's primary
	// defense is against low-entropy brute-force, which does not apply here.
	// Increasing parameters would add latency to every provider key decrypt
	// (including per-request) for no meaningful security gain.
	argonTime = 1
	argonMem  = 8 * 1024
	argonThr  = 4
)

// KeyPair holds an encrypted key and its Argon2id parameters.
type KeyPair struct {
	Ciphertext []byte
	Nonce      []byte
	Salt       []byte
}

func deriveKey(masterKey string, salt []byte) []byte {
	return argon2.IDKey([]byte(masterKey), salt, argonTime, argonMem, argonThr, keyLength)
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

func decryptWithKey(ciphertext, nonce, key []byte) (string, error) {
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

func Encrypt(plaintext, masterKey string) (*KeyPair, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(cryptoRand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	key := deriveKey(masterKey, salt)
	kp, err := encryptWithKey(plaintext, key)
	if err != nil {
		return nil, err
	}
	kp.Salt = salt
	return kp, nil
}

// Decrypt decrypts ciphertext using the per-provider salt.
// The salt parameter is required - nil/empty salt will return an error.
func Decrypt(ciphertext, nonce, salt []byte, masterKey string) (string, error) {
	if len(salt) == 0 {
		return "", fmt.Errorf("cannot decrypt: salt is required")
	}
	key := deriveKey(masterKey, salt)
	return decryptWithKey(ciphertext, nonce, key)
}

// GenerateRandomKey creates a cryptographically secure random key of the specified length.
func GenerateRandomKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(cryptoRand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(key), nil
}

// ConstantTimeCompare compares two byte slices in constant time to prevent timing attacks.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
