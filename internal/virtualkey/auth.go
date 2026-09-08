// Package virtualkey provides virtual API key authentication and management.
package virtualkey

import (
	cryptoRand "crypto/rand"
	"encoding/hex"
	"io"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// Generate creates a new virtual API key and returns the plain text key and its SHA-256 hash.
func Generate() (string, error) {
	key := make([]byte, 16)
	if _, err := io.ReadFull(cryptoRand.Reader, key); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(key), nil
}

// Hash computes the SHA-256 hash of a virtual API key.
func Hash(key string) string {
	return util.SHA256Hex(key)
}
