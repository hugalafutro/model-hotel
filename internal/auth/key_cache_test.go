package auth

import (
	"sync"
	"testing"
	"time"
)

func TestDecryptCached_DecryptsCorrectly(t *testing.T) {
	masterKey := "cache-test-master-key"
	plaintext := "my-secret-api-key-for-cache"

	// First, encrypt the plaintext
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// DecryptCached should decrypt correctly on first call (cache miss)
	result, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached failed: %v", err)
	}
	if result != plaintext {
		t.Errorf("DecryptCached returned %q, want %q", result, plaintext)
	}
}

func TestDecryptCached_CachesResult(t *testing.T) {
	masterKey := "cache-hit-test-key"
	plaintext := "api-key-to-be-cached"

	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// First call: cache miss (decrypts)
	result1, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("First DecryptCached call failed: %v", err)
	}

	// Second call: should hit cache (same result)
	result2, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("Second DecryptCached call failed: %v", err)
	}

	if result1 != result2 {
		t.Errorf("cached result mismatch: first=%q, second=%q", result1, result2)
	}
	if result1 != plaintext {
		t.Errorf("decrypted value mismatch: got %q, want %q", result1, plaintext)
	}
}

func TestDecryptCached_WrongMasterKeyFails(t *testing.T) {
	masterKey := "correct-master-key"
	wrongKey := "wrong-master-key"
	plaintext := "secret-data"

	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, wrongKey)
	if err == nil {
		t.Error("DecryptCached should fail with wrong master key")
	}
}

func TestDecryptCached_DifferentCiphertextsCachedSeparately(t *testing.T) {
	masterKey := "multi-cache-key"

	pt1 := "first-api-key"
	pt2 := "second-api-key"

	kp1, err := Encrypt(pt1, masterKey)
	if err != nil {
		t.Fatalf("Encrypt pt1 failed: %v", err)
	}
	kp2, err := Encrypt(pt2, masterKey)
	if err != nil {
		t.Fatalf("Encrypt pt2 failed: %v", err)
	}

	result1, err := DecryptCached(kp1.Ciphertext, kp1.Nonce, kp1.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached kp1 failed: %v", err)
	}
	result2, err := DecryptCached(kp2.Ciphertext, kp2.Nonce, kp2.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached kp2 failed: %v", err)
	}

	if result1 != pt1 {
		t.Errorf("first key: got %q, want %q", result1, pt1)
	}
	if result2 != pt2 {
		t.Errorf("second key: got %q, want %q", result2, pt2)
	}
}

func TestDecryptCached_V2WithSalt(t *testing.T) {
	masterKey := "v2-cache-test-key"
	plaintext := "v2-encrypted-key"

	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Encrypt always uses v2 (with salt)
	result, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached v2 failed: %v", err)
	}
	if result != plaintext {
		t.Errorf("DecryptCached v2 returned %q, want %q", result, plaintext)
	}
}

func TestWarmKeyCache(t *testing.T) {
	masterKey := "warm-cache-test-key"
	plaintext := "key-to-be-warmed"

	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// WarmKeyCache should populate the cache
	WarmKeyCache(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)

	// Now DecryptCached should return the cached value
	result, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached after warm failed: %v", err)
	}
	if result != plaintext {
		t.Errorf("DecryptCached after warm returned %q, want %q", result, plaintext)
	}
}

func TestWarmKeyCache_WithWrongKey(t *testing.T) {
	masterKey := "correct-key"
	wrongKey := "wrong-key"
	plaintext := "key-with-wrong-master"

	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// WarmKeyCache with wrong key should log error but not panic
	WarmKeyCache(kp.Ciphertext, kp.Nonce, kp.Salt, wrongKey)

	// The cache entry with the wrong key will be stored with empty/error result
	// or not stored at all. DecryptCached with the correct key should still work
	// because it will try to decrypt fresh.
	// Actually, WarmKeyCache logs error and returns early, so no cache entry.
	// DecryptCached will do a fresh decrypt.
	result, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached with correct key after bad warm failed: %v", err)
	}
	if result != plaintext {
		t.Errorf("DecryptCached returned %q, want %q", result, plaintext)
	}
}

func TestWarmKeyCache_NilEncryptedKey(t *testing.T) {
	// WarmKeyCache calls DecryptCached internally, which will panic
	// when given nil/empty ciphertext and nonce (crypto/cipher requires
	// valid nonce length). Keyless providers in the real codebase guard
	// against this by checking len(prov.EncryptedKey) == 0 before calling
	// WarmKeyCache. We verify that WarmKeyCache with empty byte slices
	// logs an error and does not panic — but since it calls DecryptCached
	// which panics on invalid nonce length, we cannot safely test nil inputs.
	//
	// Instead, we test with a valid-length nonce (12 bytes) but corrupted
	// ciphertext, which should return a clean error rather than a panic.

	// Valid nonce length for AES-GCM is 12 bytes; invalid ciphertext should error
	validNonce := []byte("123456789012")                    // exactly 12 bytes
	validSalt := []byte("12345678901234567890123456789012") // 32 bytes
	_, err := DecryptCached([]byte("corrupted-ciphertext"), validNonce, validSalt, "master-key")
	if err == nil {
		t.Error("DecryptCached with corrupted ciphertext should return error")
	}
}

func TestDecryptCached_InvalidInputs(t *testing.T) {
	// Test with valid nonce length but empty ciphertext — should error, not panic
	validNonce := []byte("123456789012")                    // exactly 12 bytes
	validSalt := []byte("12345678901234567890123456789012") // 32 bytes
	_, err := DecryptCached([]byte{}, validNonce, validSalt, "master-key")
	if err == nil {
		t.Error("DecryptCached with empty ciphertext should return error")
	}

	// Test with valid nonce but wrong master key
	masterKey := "correct-master-key"
	wrongKey := "wrong-master-key"
	plaintext := "test-plaintext-value"
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	_, err = DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, wrongKey)
	if err == nil {
		t.Error("DecryptCached with wrong master key should return error")
	}
}

func TestEvictExpiredKeyCacheEntries(t *testing.T) {
	// Clear the cache to start fresh
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCacheMu.Unlock()

	masterKey := "eviction-test-key"

	// Add an expired entry directly
	expiredKey := "expired-entry"
	keyCacheMu.Lock()
	keyCache[expiredKey] = cacheEntry{
		plaintext: "expired-value",
		expiresAt: time.Now().Add(-1 * time.Hour), // expired 1 hour ago
	}
	keyCacheMu.Unlock()

	// Add a fresh entry via DecryptCached (should not be evicted)
	plaintext := "fresh-value"
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	_, err = DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached failed: %v", err)
	}

	evictExpiredKeyCacheEntries()

	keyCacheMu.RLock()
	_, expiredExists := keyCache[expiredKey]
	freshExists := false
	ck := decryptionCacheKey(kp.Ciphertext, kp.Nonce, kp.Salt)
	_, freshExists = keyCache[ck]
	keyCacheMu.RUnlock()

	if expiredExists {
		t.Error("expired entry should have been evicted")
	}
	if !freshExists {
		t.Error("fresh entry should still be in cache")
	}
}

func TestEvictExpiredKeyCacheEntries_AllExpired(t *testing.T) {
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCache["key1"] = cacheEntry{
		plaintext: "val1",
		expiresAt: time.Now().Add(-1 * time.Hour),
	}
	keyCache["key2"] = cacheEntry{
		plaintext: "val2",
		expiresAt: time.Now().Add(-2 * time.Hour),
	}
	keyCacheMu.Unlock()

	evictExpiredKeyCacheEntries()

	keyCacheMu.RLock()
	cacheLen := len(keyCache)
	keyCacheMu.RUnlock()

	if cacheLen != 0 {
		t.Errorf("expected all entries evicted, got %d remaining", cacheLen)
	}
}

func TestEvictExpiredKeyCacheEntries_NoneExpired(t *testing.T) {
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCache["key1"] = cacheEntry{
		plaintext: "val1",
		expiresAt: time.Now().Add(1 * time.Hour),
	}
	keyCache["key2"] = cacheEntry{
		plaintext: "val2",
		expiresAt: time.Now().Add(2 * time.Hour),
	}
	keyCacheMu.Unlock()

	evictExpiredKeyCacheEntries()

	keyCacheMu.RLock()
	cacheLen := len(keyCache)
	keyCacheMu.RUnlock()

	if cacheLen != 2 {
		t.Errorf("expected 2 entries (none expired), got %d", cacheLen)
	}
}

func TestDecryptionCacheKey(t *testing.T) {
	ct := []byte("ciphertext")
	nonce := []byte("nonce")
	salt := []byte("salt")

	// With salt
	keyWithSalt := decryptionCacheKey(ct, nonce, salt)
	if keyWithSalt == "" {
		t.Error("decryptionCacheKey with salt should not be empty")
	}

	// Without salt
	keyNoSalt := decryptionCacheKey(ct, nonce, nil)
	if keyNoSalt == "" {
		t.Error("decryptionCacheKey without salt should not be empty")
	}

	// Different salts produce different keys
	salt2 := []byte("salt2")
	keyDifferentSalt := decryptionCacheKey(ct, nonce, salt2)
	if keyWithSalt == keyDifferentSalt {
		t.Error("different salts should produce different cache keys")
	}

	// Same inputs produce same key
	keySame := decryptionCacheKey(ct, nonce, salt)
	if keyWithSalt != keySame {
		t.Error("same inputs should produce same cache key")
	}

	// nil salt and empty salt both produce empty hex encoding
	keyNilSalt := decryptionCacheKey(ct, nonce, nil)
	keyEmptySalt := decryptionCacheKey(ct, nonce, []byte{})
	if keyNilSalt != keyEmptySalt {
		t.Error("nil salt and empty salt should produce the same cache key")
	}
}

func TestDecryptCached_ConcurrentAccess(t *testing.T) {
	masterKey := "concurrent-test-key"
	plaintext := "concurrent-api-key"

	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 20)

	// Launch concurrent decrypts
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
			if err != nil {
				errors <- err
				return
			}
			if result != plaintext {
				errors <- err
				return
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent DecryptCached error: %v", err)
	}
}

func TestStopKeyCacheEviction(t *testing.T) {
	// Calling Stop should not panic even if called multiple times
	// (the init() goroutine is already running from package init)
	StopKeyCacheEviction()

	// Calling Stop again should not panic (nil channel guard)
	StopKeyCacheEviction()

	// Calling a third time should also not panic (idempotent)
	StopKeyCacheEviction()
}

func TestDecryptCached_EmptyCiphertext(t *testing.T) {
	// Empty ciphertext with a valid-length nonce and salt should fail gracefully
	// (not panic). AES-GCM will reject the empty ciphertext as a decryption error.
	validNonce := make([]byte, 12)                          // AES-GCM nonce must be exactly 12 bytes
	validSalt := []byte("12345678901234567890123456789012") // 32 bytes
	_, err := DecryptCached([]byte{}, validNonce, validSalt, "master-key")
	if err == nil {
		t.Error("DecryptCached with empty ciphertext should return error")
	}
}

func TestDecryptCached_ShortNonce_Panics(t *testing.T) {
	// DecryptCached does not guard against short nonces internally —
	// cipher.NewGCM panics if the nonce is the wrong length. This is
	// expected behavior: callers must always provide a valid 12-byte
	// nonce (which is guaranteed by the Encrypt function). We verify
	// that a short nonce causes a panic so that this edge case is documented.
	defer func() {
		if r := recover(); r == nil {
			t.Error("DecryptCached with short nonce should panic (cipher.NewGCM requires 12-byte nonce)")
		}
	}()

	masterKey := "short-nonce-test"
	plaintext := "test-key"
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Use a short (5-byte) nonce which is invalid for AES-GCM
	shortNonce := []byte("short")
	//nolint:gosec // test-only: error handling not critical
	DecryptCached(kp.Ciphertext, shortNonce, kp.Salt, masterKey)
}

func TestKeyCacheTTLValue(t *testing.T) {
	// Verify the default TTL hasn't been accidentally changed
	if DefaultKeyCacheTTL != 10*time.Minute {
		t.Errorf("DefaultKeyCacheTTL should be 10 minutes, got %v", DefaultKeyCacheTTL)
	}
	if getKeyCacheTTL() != DefaultKeyCacheTTL {
		t.Errorf("getKeyCacheTTL() should return default, got %v", getKeyCacheTTL())
	}
}

func TestSetKeyCacheTTL(t *testing.T) {
	orig := getKeyCacheTTL()
	defer SetKeyCacheTTL(orig)

	SetKeyCacheTTL(30 * time.Minute)
	if getKeyCacheTTL() != 30*time.Minute {
		t.Errorf("expected 30m TTL, got %v", getKeyCacheTTL())
	}

	// Zero or negative values should be rejected
	SetKeyCacheTTL(0)
	if getKeyCacheTTL() != 30*time.Minute {
		t.Error("TTL should remain unchanged after SetKeyCacheTTL(0)")
	}

	SetKeyCacheTTL(-1 * time.Minute)
	if getKeyCacheTTL() != 30*time.Minute {
		t.Error("TTL should remain unchanged after SetKeyCacheTTL(-1m)")
	}
}
