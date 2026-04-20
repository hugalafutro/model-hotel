package virtualkey

import (
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func Generate() (string, error) {
	key := make([]byte, 16)
	if _, err := io.ReadFull(cryptoRand.Reader, key); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(key), nil
}

func Hash(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
