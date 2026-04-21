package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type cacheEntry struct {
	key       []byte
	expiresAt time.Time
}

var (
	keyCache   = make(map[string]cacheEntry)
	keyCacheMu sync.RWMutex
)

const keyCacheTTL = 5 * time.Minute

func DeriveKeyCached(masterKey string) []byte {
	keyCacheMu.RLock()
	if entry, ok := keyCache[masterKey]; ok && time.Now().Before(entry.expiresAt) {
		key := make([]byte, len(entry.key))
		copy(key, entry.key)
		keyCacheMu.RUnlock()
		return key
	}
	keyCacheMu.RUnlock()

	key := DeriveKey(masterKey)

	keyCacheMu.Lock()
	keyCache[masterKey] = cacheEntry{
		key:       key,
		expiresAt: time.Now().Add(keyCacheTTL),
	}
	keyCacheMu.Unlock()

	result := make([]byte, len(key))
	copy(result, key)
	return result
}

func DecryptCached(ciphertext, nonce []byte, masterKey string) (string, error) {
	key := DeriveKeyCached(masterKey)

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

func WarmKeyCache(providerID uuid.UUID, encryptedKey, keyNonce []byte, masterKey string) {
	DeriveKeyCached(masterKey)
	DecryptCached(encryptedKey, keyNonce, masterKey)
}