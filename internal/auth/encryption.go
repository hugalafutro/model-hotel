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
	salt        = "llm-proxy-fixed-salt-v1"
	timeCost    = 1
	memory      = 64 * 1024
	threads     = 4
)

type KeyPair struct {
	Ciphertext []byte
	Nonce      []byte
}

func DeriveKey(masterKey string) []byte {
	return argon2.IDKey([]byte(masterKey), []byte(salt), timeCost, memory, threads, keyLength)
}

func Encrypt(plaintext, masterKey string) (*KeyPair, error) {
	key := DeriveKey(masterKey)

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

func Decrypt(ciphertext, nonce []byte, masterKey string) (string, error) {
	key := DeriveKey(masterKey)

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
