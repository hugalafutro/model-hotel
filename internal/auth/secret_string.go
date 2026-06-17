package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// secretStringPrefix tags a value produced by EncryptString. The "v1" allows a
// future format change without ambiguity.
const secretStringPrefix = "enc:v1:"

// EncryptString encrypts plaintext with masterKey and returns a single,
// self-describing token safe to store in a plain string column (e.g. the
// settings key/value table, which has no separate ciphertext/nonce/salt
// columns like the providers table does). Format:
//
//	enc:v1:<base64(ciphertext)>:<base64(nonce)>:<base64(salt)>
func EncryptString(plaintext, masterKey string) (string, error) {
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding.EncodeToString
	return secretStringPrefix +
		enc(kp.Ciphertext) + ":" + enc(kp.Nonce) + ":" + enc(kp.Salt), nil
}

// IsEncryptedString reports whether s was produced by EncryptString.
func IsEncryptedString(s string) bool {
	return strings.HasPrefix(s, secretStringPrefix)
}

// DecryptString reverses EncryptString. A value without the enc prefix is
// returned unchanged — it was never encrypted (empty string, or a value written
// before encryption was introduced) — so callers can decrypt unconditionally.
func DecryptString(stored, masterKey string) (string, error) {
	if !IsEncryptedString(stored) {
		return stored, nil
	}
	parts := strings.Split(strings.TrimPrefix(stored, secretStringPrefix), ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed encrypted string")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode salt: %w", err)
	}
	return Decrypt(ciphertext, nonce, salt, masterKey)
}
