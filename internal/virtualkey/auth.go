// Package virtualkey provides virtual API key authentication and management.
package virtualkey

import (
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// randReader is the source of cryptographic randomness. Overridable for testing.
var randReader = cryptoRand.Reader

// Generate creates a new virtual API key and returns the plain text key and its SHA-256 hash.
func Generate() (string, error) {
	key := make([]byte, 16)
	if _, err := io.ReadFull(randReader, key); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(key), nil
}

// Hash computes the SHA-256 hash of a virtual API key.
func Hash(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
