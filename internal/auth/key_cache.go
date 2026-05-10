package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

var (
	keyCacheEvictionStop chan struct{}
	keyCacheEvictionDone chan struct{}
)

type cacheEntry struct {
	plaintext string
	expiresAt time.Time
}

var (
	keyCache   = make(map[string]cacheEntry)
	keyCacheMu sync.RWMutex
)

// keyCacheTTLNanos stores the current key cache TTL as nanoseconds.
// It can be updated at runtime via SetKeyCacheTTL.
var keyCacheTTLNanos atomic.Int64

// DefaultKeyCacheTTL is the default cache TTL used when no setting is provided.
const DefaultKeyCacheTTL = 10 * time.Minute

func init() {
	keyCacheTTLNanos.Store(int64(DefaultKeyCacheTTL))
	startKeyCacheEviction()
}

// getKeyCacheTTL returns the current key cache TTL.
func getKeyCacheTTL() time.Duration {
	return time.Duration(keyCacheTTLNanos.Load())
}

// SetKeyCacheTTL updates the key cache TTL. Existing cache entries retain
// their original expiry; only newly cached entries use the updated TTL.
func SetKeyCacheTTL(d time.Duration) {
	if d <= 0 {
		debuglog.Warn("keycache: refusing to set TTL <= 0, keeping current value")
		return
	}
	keyCacheTTLNanos.Store(int64(d))
}

func decryptionCacheKey(ciphertext, nonce, salt []byte) string {
	if len(salt) == 0 {
		return hex.EncodeToString(ciphertext) + ":" + hex.EncodeToString(nonce)
	}
	return hex.EncodeToString(ciphertext) + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(salt)
}

// DecryptCached attempts to decrypt a provider key using the cached Argon2id key.
func DecryptCached(ciphertext, nonce, salt []byte, masterKey string) (string, error) {
	ck := decryptionCacheKey(ciphertext, nonce, salt)

	keyCacheMu.RLock()
	if entry, ok := keyCache[ck]; ok && time.Now().Before(entry.expiresAt) {
		keyCacheMu.RUnlock()
		return entry.plaintext, nil
	}
	keyCacheMu.RUnlock()

	var key []byte
	if len(salt) == 0 {
		key = deriveKeyV1(masterKey)
	} else {
		key = deriveKeyV2(masterKey, salt)
	}

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
		debuglog.Warn("keycache: decryption failed, possible wrong master key", "error", err)
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	ttl := getKeyCacheTTL()
	keyCacheMu.Lock()
	keyCache[ck] = cacheEntry{
		plaintext: string(plaintext),
		expiresAt: time.Now().Add(ttl),
	}
	keyCacheMu.Unlock()

	return string(plaintext), nil
}

// WarmKeyCache pre-computes Argon2id keys for active providers.
func WarmKeyCache(encryptedKey, keyNonce, keySalt []byte, masterKey string) {
	_, err := DecryptCached(encryptedKey, keyNonce, keySalt, masterKey)
	if err != nil {
		debuglog.Error("keycache: failed to warm key cache", "error", err)
	}
}

func startKeyCacheEviction() {
	keyCacheEvictionStop = make(chan struct{})
	keyCacheEvictionDone = make(chan struct{})
	go func() {
		defer close(keyCacheEvictionDone)
		ticker := time.NewTicker(getKeyCacheTTL())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				evictExpiredKeyCacheEntries()
				// Reset ticker to pick up any TTL changes.
				ticker.Reset(getKeyCacheTTL())
			case <-keyCacheEvictionStop:
				return
			}
		}
	}()
}

func evictExpiredKeyCacheEntries() {
	keyCacheMu.Lock()
	defer keyCacheMu.Unlock()
	now := time.Now()
	for k, v := range keyCache {
		if now.After(v.expiresAt) {
			delete(keyCache, k)
		}
	}
}

// StopKeyCacheEviction stops the periodic key cache eviction goroutine.
func StopKeyCacheEviction() {
	if keyCacheEvictionStop != nil {
		close(keyCacheEvictionStop)
		<-keyCacheEvictionDone
		keyCacheEvictionStop = nil
		keyCacheEvictionDone = nil
	}
}
