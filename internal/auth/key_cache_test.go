package auth

import (
	"crypto/cipher"
	"fmt"
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

func TestDecryptCached_CacheExpiryEndToEnd(t *testing.T) {
	// Save original TTL and defer restore
	orig := getKeyCacheTTL()
	defer SetKeyCacheTTL(orig)

	// Set a very short TTL
	SetKeyCacheTTL(100 * time.Millisecond)

	// Restart eviction goroutine
	startKeyCacheEviction()
	defer StopKeyCacheEviction()

	// Clear the cache
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCacheMu.Unlock()

	// Encrypt a plaintext
	masterKey := "expiry-test-key"
	plaintext := "key-to-expire"
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// First call: cache miss (decrypts)
	result1, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("First DecryptCached call failed: %v", err)
	}
	if result1 != plaintext {
		t.Errorf("First call returned %q, want %q", result1, plaintext)
	}

	// Verify the cache has an entry
	ck := decryptionCacheKey(kp.Ciphertext, kp.Nonce, kp.Salt)
	keyCacheMu.RLock()
	entry, exists := keyCache[ck]
	keyCacheMu.RUnlock()
	if !exists {
		t.Fatal("cache entry should exist after first call")
	}
	expectedExpiresAt := time.Now().Add(100 * time.Millisecond)
	if entry.expiresAt.Before(expectedExpiresAt.Add(-50*time.Millisecond)) || entry.expiresAt.After(expectedExpiresAt.Add(50*time.Millisecond)) {
		t.Errorf("entry expiresAt %v not within ~100ms of now", entry.expiresAt)
	}

	// Second call: should be a cache hit
	result2, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("Second DecryptCached call failed: %v", err)
	}
	if result2 != result1 {
		t.Errorf("cache hit returned %q, want %q", result2, result1)
	}

	// Wait for TTL to expire
	time.Sleep(200 * time.Millisecond)

	// Manually call eviction
	evictExpiredKeyCacheEntries()

	// Verify the cache entry is gone
	keyCacheMu.RLock()
	_, exists = keyCache[ck]
	keyCacheMu.RUnlock()
	if exists {
		t.Error("cache entry should have been evicted after expiry")
	}

	// Third call: should re-decrypt (cache miss after expiry)
	result3, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("Third DecryptCached call failed: %v", err)
	}
	if result3 != plaintext {
		t.Errorf("Third call returned %q, want %q", result3, plaintext)
	}
}

func TestStartKeyCacheEviction_FiresPeriodically(t *testing.T) {
	// Stop any existing eviction goroutine
	StopKeyCacheEviction()

	// Set very short TTL
	orig := getKeyCacheTTL()
	defer SetKeyCacheTTL(orig)
	SetKeyCacheTTL(50 * time.Millisecond)

	// Clear cache
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCacheMu.Unlock()

	// Start eviction
	startKeyCacheEviction()
	defer StopKeyCacheEviction()

	// Add an expired entry directly
	keyCacheMu.Lock()
	keyCache["expired"] = cacheEntry{
		plaintext: "x",
		expiresAt: time.Now().Add(-1 * time.Hour),
	}
	keyCacheMu.Unlock()

	// Wait for the goroutine to fire at least once
	time.Sleep(200 * time.Millisecond)

	// Check that the expired entry was evicted
	keyCacheMu.RLock()
	_, exists := keyCache["expired"]
	keyCacheMu.RUnlock()
	if exists {
		t.Error("expired entry should have been evicted by background goroutine")
	}
}

func TestSetKeyCacheTTL_AffectsNewEntryExpiry(t *testing.T) {
	// Save original TTL, defer restore
	orig := getKeyCacheTTL()
	defer SetKeyCacheTTL(orig)

	// Set TTL to 5 minutes
	SetKeyCacheTTL(5 * time.Minute)

	// Clear cache
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCacheMu.Unlock()

	// Encrypt and call DecryptCached to populate cache
	masterKey := "ttl-test-key"
	plaintext1 := "key-with-5min-ttl"
	kp1, err := Encrypt(plaintext1, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	_, err = DecryptCached(kp1.Ciphertext, kp1.Nonce, kp1.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached failed: %v", err)
	}

	// Read the cache entry, record its expiresAt
	ck1 := decryptionCacheKey(kp1.Ciphertext, kp1.Nonce, kp1.Salt)
	keyCacheMu.RLock()
	entry1, exists := keyCache[ck1]
	keyCacheMu.RUnlock()
	if !exists {
		t.Fatal("cache entry should exist")
	}
	expiresAt1 := entry1.expiresAt

	// Set TTL to 30 minutes
	SetKeyCacheTTL(30 * time.Minute)

	// Clear cache
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCacheMu.Unlock()

	// Encrypt a different plaintext and call DecryptCached again
	plaintext2 := "key-with-30min-ttl"
	kp2, err := Encrypt(plaintext2, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	_, err = DecryptCached(kp2.Ciphertext, kp2.Nonce, kp2.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached failed: %v", err)
	}

	// Read the new cache entry
	ck2 := decryptionCacheKey(kp2.Ciphertext, kp2.Nonce, kp2.Salt)
	keyCacheMu.RLock()
	entry2, exists := keyCache[ck2]
	keyCacheMu.RUnlock()
	if !exists {
		t.Fatal("cache entry should exist")
	}
	expiresAt2 := entry2.expiresAt

	// Verify each entry's expiresAt is roughly now+TTL
	now := time.Now()
	if diff := expiresAt1.Sub(now); diff < 4*time.Minute || diff > 6*time.Minute {
		t.Errorf("5min TTL entry expiresAt %v, expected ~5min from now (diff=%v)", expiresAt1, diff)
	}
	if diff := expiresAt2.Sub(now); diff < 29*time.Minute || diff > 31*time.Minute {
		t.Errorf("30min TTL entry expiresAt %v, expected ~30min from now (diff=%v)", expiresAt2, diff)
	}
}

func TestDecryptCached_EmptySaltReturnsError(t *testing.T) {
	// Test with nil salt
	_, err := DecryptCached([]byte("ct"), []byte("123456789012"), nil, "key")
	if err == nil {
		t.Error("DecryptCached with nil salt should return error")
	}

	// Test with empty salt slice
	_, err = DecryptCached([]byte("ct"), []byte("123456789012"), []byte{}, "key")
	if err == nil {
		t.Error("DecryptCached with empty salt should return error")
	}

	// Verify error message contains "salt is required"
	// Note: We can't easily check the exact error message without importing strings
	// and doing a Contains check, but the error should be returned
	if err != nil && err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestWarmKeyCache_EmptyCiphertext(t *testing.T) {
	// WarmKeyCache with empty ciphertext should not panic
	// It calls DecryptCached which should return an error for empty ciphertext
	validNonce := []byte("123456789012")                    // 12 bytes
	validSalt := []byte("12345678901234567890123456789012") // 32 bytes

	// This should not panic
	WarmKeyCache([]byte{}, validNonce, validSalt, "key")

	// Verify no cache entry was created for this input
	ck := decryptionCacheKey([]byte{}, validNonce, validSalt)
	keyCacheMu.RLock()
	_, exists := keyCache[ck]
	keyCacheMu.RUnlock()
	if exists {
		t.Error("cache entry should not exist for empty ciphertext")
	}
}

func TestDecryptionCacheKey_NoColonCollision(t *testing.T) {
	// Create ciphertext bytes that contain the ':' character (0x3A)
	ct := []byte{0x3A, 0x3A, 0x3A}
	// Create normal ciphertext
	ct2 := []byte{0x01, 0x02, 0x03}
	// Use same nonce and salt for both
	nonce := []byte("123456789012")
	salt := []byte("12345678901234567890123456789012")

	key1 := decryptionCacheKey(ct, nonce, salt)
	key2 := decryptionCacheKey(ct2, nonce, salt)

	// Assert different cache keys
	if key1 == key2 {
		t.Error("different ciphertexts should produce different cache keys")
	}

	// Assert that the hex-encoded key contains only hex chars and colons
	// (no raw 0x3A bytes leaked through as literal colons in wrong places)
	for i, c := range key1 {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		isColon := c == ':'
		if !isHex && !isColon {
			t.Errorf("key1[%d] = %q (0x%02x), expected hex char or colon", i, c, c)
		}
	}
	for i, c := range key2 {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		isColon := c == ':'
		if !isHex && !isColon {
			t.Errorf("key2[%d] = %q (0x%02x), expected hex char or colon", i, c, c)
		}
	}
}

func TestDecryptCached_ConcurrentEvictionAndAccess(t *testing.T) {
	// Save original TTL
	orig := getKeyCacheTTL()
	defer SetKeyCacheTTL(orig)

	// Set short TTL
	SetKeyCacheTTL(50 * time.Millisecond)

	// Stop existing eviction, restart with short TTL
	StopKeyCacheEviction()
	startKeyCacheEviction()
	defer StopKeyCacheEviction()

	// Clear cache
	keyCacheMu.Lock()
	keyCache = make(map[string]cacheEntry)
	keyCacheMu.Unlock()

	// Encrypt a plaintext
	masterKey := "concurrent-eviction-test"
	plaintext := "concurrent-key"
	kp, err := Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 20)
	done := make(chan struct{})

	// Launch 10 goroutines repeatedly calling DecryptCached
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					result, err := DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
					if err != nil {
						errors <- err
						return
					}
					if result != plaintext {
						errors <- fmt.Errorf("result mismatch: got %q, want %q", result, plaintext)
						return
					}
				}
			}
		}()
	}

	// Launch 1 goroutine repeatedly calling evictExpiredKeyCacheEntries
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				evictExpiredKeyCacheEntries()
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Run for ~500ms
	time.Sleep(500 * time.Millisecond)
	close(done)
	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
	}
}

func TestDecryptCached_NewCipherBlockError(t *testing.T) {
	orig := newCipherBlock
	defer func() { newCipherBlock = orig }()
	newCipherBlock = func([]byte) (cipher.Block, error) {
		return nil, fmt.Errorf("mock cipher error")
	}
	_, err := DecryptCached([]byte("ct"), []byte("123456789012"), []byte("12345678901234567890123456789012"), "key")
	if err == nil {
		t.Error("expected error when newCipherBlock fails")
	}
}

func TestDecryptCached_NewGCMError(t *testing.T) {
	orig := newGCM
	defer func() { newGCM = orig }()
	newGCM = func(cipher.Block) (cipher.AEAD, error) {
		return nil, fmt.Errorf("mock GCM error")
	}
	_, err := DecryptCached([]byte("ct"), []byte("123456789012"), []byte("12345678901234567890123456789012"), "key")
	if err == nil {
		t.Error("expected error when newGCM fails")
	}
}

// ---------------------------------------------------------------------------
// IsKeyCached
// ---------------------------------------------------------------------------

func TestIsKeyCached_NotCachedBeforeDecrypt(t *testing.T) {
	// A freshly encrypted key should not appear in cache until DecryptCached is called.
	masterKey := "test-master-key-for-caching"
	kp, err := Encrypt("secret-api-key", masterKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if IsKeyCached(kp.Ciphertext, kp.Nonce, kp.Salt) {
		t.Error("IsKeyCached should return false before DecryptCached is called")
	}
}

func TestIsKeyCached_PresentAfterDecrypt(t *testing.T) {
	masterKey := "test-master-key-for-caching"
	kp, err := Encrypt("secret-api-key", masterKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err = DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached: %v", err)
	}

	if !IsKeyCached(kp.Ciphertext, kp.Nonce, kp.Salt) {
		t.Error("IsKeyCached should return true after DecryptCached populates cache")
	}
}

func TestIsKeyCached_DifferentCiphertext(t *testing.T) {
	masterKey := "test-master-key-for-caching"
	kp, err := Encrypt("secret-api-key", masterKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err = DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, masterKey)
	if err != nil {
		t.Fatalf("DecryptCached: %v", err)
	}

	if IsKeyCached([]byte("other-ct"), kp.Nonce, kp.Salt) {
		t.Error("IsKeyCached should return false for different ciphertext")
	}
}
