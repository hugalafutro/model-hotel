package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"sync"
	"time"
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

func deriveKeyCached(masterKey string, salt []byte) []byte {
	ck := cacheKey(masterKey, salt)

	keyCacheMu.RLock()
	if entry, ok := keyCache[ck]; ok && time.Now().Before(entry.expiresAt) {
		key := make([]byte, len(entry.key))
		copy(key, entry.key)
		keyCacheMu.RUnlock()
		return key
	}
	keyCacheMu.RUnlock()

	var key []byte
	if len(salt) == 0 {
		key = deriveKeyV1(masterKey)
	} else {
		key = deriveKeyV2(masterKey, salt)
	}

	keyCacheMu.Lock()
	keyCache[ck] = cacheEntry{
		key:       key,
		expiresAt: time.Now().Add(keyCacheTTL),
	}
	keyCacheMu.Unlock()

	result := make([]byte, len(key))
	copy(result, key)
	return result
}

func DecryptCached(ciphertext, nonce, salt []byte, masterKey string) (string, error) {
	key := deriveKeyCached(masterKey, salt)

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

func WarmKeyCache(encryptedKey, keyNonce, keySalt []byte, masterKey string) {
	deriveKeyCached(masterKey, keySalt)
	DecryptCached(encryptedKey, keyNonce, keySalt, masterKey)
}