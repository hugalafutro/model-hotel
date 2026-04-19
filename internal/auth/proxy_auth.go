package auth

import (
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func GenerateProxyKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(cryptoRand.Reader, key); err != nil {
		return "", err
	}
	return "llmp_" + hex.EncodeToString(key), nil
}

func HashProxyKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func ValidateProxyKey(providedKey, storedHash string) bool {
	providedHash := HashProxyKey(providedKey)
	return ConstantTimeCompare(providedHash, storedHash)
}
