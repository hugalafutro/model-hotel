package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// SHA256Hex renders the SHA-256 digest of s as lowercase hex. Every token the
// project stores is held as this digest rather than the token itself, so the
// spelling lives in one place.
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// RandomHex returns nBytes of cryptographically random data as lowercase hex,
// so the string is twice nBytes long.
func RandomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// MintHexToken generates a random token of nBytes entropy and returns it
// alongside the SHA-256 hex digest that gets stored in its place.
func MintHexToken(nBytes int) (token, hash string, err error) {
	token, err = RandomHex(nBytes)
	if err != nil {
		return "", "", err
	}
	return token, SHA256Hex(token), nil
}
