package auth

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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
	return hex.EncodeToString(ciphertext) + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(salt)
}

// IsKeyCached reports whether a decrypted key for the given ciphertext/nonce/salt
// combination is present in the cache and not expired. It does not modify the
// cache or perform any decryption.
func IsKeyCached(ciphertext, nonce, salt []byte) bool {
	ck := decryptionCacheKey(ciphertext, nonce, salt)
	keyCacheMu.RLock()
	entry, ok := keyCache[ck]
	keyCacheMu.RUnlock()
	return ok && time.Now().Before(entry.expiresAt)
}

// DecryptCached attempts to decrypt a provider key using the cached Argon2id key.
func DecryptCached(ciphertext, nonce, salt []byte, masterKey string) (string, error) {
	if len(salt) == 0 {
		return "", fmt.Errorf("cannot decrypt: salt is required")
	}

	ck := decryptionCacheKey(ciphertext, nonce, salt)

	keyCacheMu.RLock()
	if entry, ok := keyCache[ck]; ok && time.Now().Before(entry.expiresAt) {
		keyCacheMu.RUnlock()
		return entry.plaintext, nil
	}
	keyCacheMu.RUnlock()

	plaintext, err := Decrypt(ciphertext, nonce, salt, masterKey)
	if err != nil {
		debuglog.Warn("keycache: decryption failed, possible wrong master key", "error", err)
		return "", err
	}

	ttl := getKeyCacheTTL()
	keyCacheMu.Lock()
	keyCache[ck] = cacheEntry{
		plaintext: plaintext,
		expiresAt: time.Now().Add(ttl),
	}
	keyCacheMu.Unlock()

	return plaintext, nil
}

// WarmKeyCache pre-computes Argon2id keys for active providers.
func WarmKeyCache(encryptedKey, keyNonce, keySalt []byte, masterKey string) {
	_, err := DecryptCached(encryptedKey, keyNonce, keySalt, masterKey)
	if err != nil {
		debuglog.Error("keycache: failed to warm key cache", "error", err)
	}
}

// KeyCacheEvictionLoop sweeps expired entries on a ticker that adopts the
// current TTL at each tick and returns when ctx is done. Each binary that
// decrypts keys starts it on its own background group, so the sweep is joined
// at shutdown instead of running unjoinably for the life of every process
// that links this package. Nothing starts it implicitly: a binary that links
// this package, decrypts keys and never runs the loop keeps expired plaintext
// entries in memory for its whole life. A tick that lands together with the
// cancellation starts no sweep, so the join budget is never spent on work
// begun after the cancel.
func KeyCacheEvictionLoop(ctx context.Context) {
	ticker := time.NewTicker(getKeyCacheTTL())
	defer ticker.Stop()
	runKeyCacheEviction(ctx, ticker.C, func() { ticker.Reset(getKeyCacheTTL()) })
}

// runKeyCacheEviction is the loop body behind KeyCacheEvictionLoop with the
// tick source and the TTL re-arm injected, so a test can hand it a cancelled
// context together with a pending tick and prove that no sweep starts.
func runKeyCacheEviction(ctx context.Context, ticks <-chan time.Time, rearm func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if ctx.Err() != nil {
				return
			}
			evictExpiredKeyCacheEntries()
			rearm()
		}
	}
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
