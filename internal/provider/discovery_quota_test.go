package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Discovery-adjacent account reads: the provider cache, quota, balance and
// usage endpoints.

func TestCacheProvider_NilProvider(t *testing.T) {
	// Should not panic
	cacheProvider(nil)

	// Verify nil provider was not cached
	testUUID := uuid.New()
	_, ok := GetCachedByID(testUUID)
	if ok {
		t.Error("GetCachedByID should return ok=false after cacheProvider(nil)")
	}
}

func TestCacheProvider_RoundTrip(t *testing.T) {
	// Clear cache
	InvalidateProviderCache()

	id := uuid.New()
	p := &Provider{
		ID:   id,
		Name: "cache-test-provider",
	}

	cacheProvider(p)

	// Should be retrievable by ID
	found, ok := GetCachedByID(id)
	if !ok {
		t.Fatal("GetCachedByID should find cached provider")
	}
	if found.ID != id {
		t.Errorf("GetCachedByID: expected ID %v, got %v", id, found.ID)
	}

	// Should be retrievable by Name
	found, ok = GetCachedByName("cache-test-provider")
	if !ok {
		t.Fatal("GetCachedByName should find cached provider")
	}
	if found.Name != "cache-test-provider" {
		t.Errorf("GetCachedByName: expected Name %q, got %q", "cache-test-provider", found.Name)
	}

	// Should be retrievable by normalized name
	found, ok = GetCachedByName("cache-test-provider")
	if !ok {
		t.Fatal("GetCachedByName should find cached provider via normalized name")
	}
	if found.ID != id {
		t.Errorf("GetCachedByName normalized: expected ID %v, got %v", id, found.ID)
	}
}

func TestCacheProvider_ExpiredEntry(t *testing.T) {
	InvalidateProviderCache()

	id := uuid.New()
	p := &Provider{
		ID:   id,
		Name: "expired-provider",
	}

	// Manually insert an expired entry
	providerCacheMu.Lock()
	providerByIDCache[id] = providerCacheEntry{
		provider:  p,
		expiresAt: mustParseTime("2020-01-01T00:00:00Z"), // expired
	}
	providerByNameCache["expired-provider"] = providerCacheEntry{
		provider:  p,
		expiresAt: mustParseTime("2020-01-01T00:00:00Z"),
	}
	providerCacheMu.Unlock()

	// Expired entries should not be found
	_, ok := GetCachedByID(id)
	if ok {
		t.Error("GetCachedByID should not return expired entry")
	}
	_, ok = GetCachedByName("expired-provider")
	if ok {
		t.Error("GetCachedByName should not return expired entry")
	}
}

func TestInvalidateProviderCache(t *testing.T) {
	id := uuid.New()
	p := &Provider{
		ID:   id,
		Name: "to-be-invalidated",
	}

	cacheProvider(p)

	// Should exist before invalidation
	_, ok := GetCachedByID(id)
	if !ok {
		t.Fatal("provider should be in cache before invalidation")
	}

	InvalidateProviderCache()

	// Should not exist after invalidation
	_, ok = GetCachedByID(id)
	if ok {
		t.Error("provider should not be in cache after invalidation")
	}
}

func TestWarmProviderCache(t *testing.T) {
	InvalidateProviderCache()

	providers := []*Provider{
		{ID: uuid.New(), Name: "warm-a"},
		{ID: uuid.New(), Name: "warm-b"},
		{ID: uuid.New(), Name: "warm-c"},
	}

	WarmProviderCache(providers)

	for _, p := range providers {
		found, ok := GetCachedByID(p.ID)
		if !ok {
			t.Errorf("provider %s should be in cache after WarmProviderCache", p.Name)
		}
		if found.Name != p.Name {
			t.Errorf("cached provider name mismatch: got %q, want %q", found.Name, p.Name)
		}
	}
}

func TestNormalizeName_RoundTripWithCache(t *testing.T) {
	InvalidateProviderCache()

	// Provider with spaces in name
	p := &Provider{
		ID:   uuid.New(),
		Name: "My Provider",
	}
	cacheProvider(p)

	// Should be findable by normalized name (spaces → hyphens)
	normalized := NormalizeName("My Provider")
	found, ok := GetCachedByName(normalized)
	if !ok {
		t.Errorf("GetCachedByName(%q) should find provider cached under name %q", normalized, p.Name)
	}
	if found.ID != p.ID {
		t.Errorf("wrong provider found via normalized name")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels
// ---------------------------------------------------------------------------

func TestQuotaCircuitState_ClosedByDefault(t *testing.T) {
	s := &quotaCircuitState{}
	if s.isCircuitOpen() {
		t.Error("new circuit should be closed")
	}
}

func TestQuotaCircuitState_OpensAfterThreshold(t *testing.T) {
	s := &quotaCircuitState{}
	for i := range quotaBreakerThreshold - 1 {
		if s.recordFailure() {
			t.Errorf("circuit should not open at failure %d (threshold=%d)", i+1, quotaBreakerThreshold)
		}
	}
	// The threshold-th failure should open the circuit.
	if !s.recordFailure() {
		t.Error("circuit should open on threshold-th failure")
	}
	if !s.isCircuitOpen() {
		t.Error("circuit should be open after reaching threshold")
	}
}

func TestQuotaCircuitState_SuccessResets(t *testing.T) {
	s := &quotaCircuitState{}
	// Fail a few times (not enough to open).
	for range quotaBreakerThreshold - 1 {
		s.recordFailure()
	}
	s.recordSuccess()
	// consecFailures reset to 0, so threshold more failures needed.
	for range quotaBreakerThreshold - 1 {
		if s.isCircuitOpen() {
			t.Error("circuit should not be open yet")
		}
		s.recordFailure()
	}
	// One more should open it.
	if !s.recordFailure() {
		t.Error("circuit should open after threshold failures post-reset")
	}
}

func TestQuotaCircuitState_HalfOpenAfterReset(t *testing.T) {
	s := &quotaCircuitState{}
	// Open the circuit.
	for range quotaBreakerThreshold {
		s.recordFailure()
	}
	if !s.isCircuitOpen() {
		t.Fatal("circuit should be open")
	}
	// Manually set openUntil to the past to simulate expiry.
	s.mu.Lock()
	s.openUntil = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()
	// isCircuitOpen should transition to half-open (returns false).
	if s.isCircuitOpen() {
		t.Error("expired circuit should transition to half-open (return false)")
	}
	// A success should fully close it.
	s.recordSuccess()
	if s.isCircuitOpen() {
		t.Error("circuit should be closed after success")
	}
}

func TestQuotaCircuitState_HalfOpenFailureReopens(t *testing.T) {
	s := &quotaCircuitState{}
	// Open the circuit.
	for range quotaBreakerThreshold {
		s.recordFailure()
	}
	// Expire the open window.
	s.mu.Lock()
	s.openUntil = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()
	// Transition to half-open.
	s.isCircuitOpen()
	// A failure should re-open the circuit immediately.
	s.recordFailure()
	if !s.isCircuitOpen() {
		t.Error("circuit should re-open after failure in half-open state")
	}
}

// ---------------------------------------------------------------------------
// doQuotaRequestWithRetry (integration-ish)
// ---------------------------------------------------------------------------

func TestDoQuotaRequestWithRetry_CircuitBreakerShortCircuits(t *testing.T) {
	svc := NewDiscoveryService(nil, nil)
	svc.SetRetryBaseDelay(time.Millisecond)
	providerID := "test-provider-123"

	// Open the circuit by recording enough failures.
	circuit := svc.getOrCreateCircuit(providerID)
	for range quotaBreakerThreshold {
		circuit.recordFailure()
	}

	req, _ := http.NewRequest("GET", "http://example.com/quota", http.NoBody)
	ctx := context.Background()
	_, err := svc.doQuotaRequestWithRetry(ctx, req, providerID, "test-provider", "zai-coding")
	if err == nil {
		t.Fatal("expected error when circuit breaker is open")
		return
	}
	if !strings.Contains(err.Error(), "circuit breaker open") {
		t.Errorf("expected circuit breaker error, got: %v", err)
	}
}

func TestDoQuotaRequestWithRetry_Retries429(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"quota": 100}`))
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	svc.SetRetryBaseDelay(time.Millisecond)
	svc.httpClient = server.Client()
	req, _ := http.NewRequest("GET", server.URL+"/quota", http.NoBody)
	ctx := context.Background()
	_, err := svc.doQuotaRequestWithRetry(ctx, req, "test-provider-429", "test", "zai-coding")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2x429 + 1x200), got %d", callCount)
	}
}

func TestDoQuotaRequestWithRetry_Retries5xx(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("maintenance"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"quota": 100}`))
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	svc.SetRetryBaseDelay(time.Millisecond)
	svc.httpClient = server.Client()
	req, _ := http.NewRequest("GET", server.URL+"/quota", http.NoBody)
	ctx := context.Background()
	_, err := svc.doQuotaRequestWithRetry(ctx, req, "test-provider-5xx", "test", "zai-coding")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestDoQuotaRequestWithRetry_NonRetryableStatusNoRetry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	svc.SetRetryBaseDelay(time.Millisecond)
	svc.httpClient = server.Client()
	req, _ := http.NewRequest("GET", server.URL+"/quota", http.NoBody)
	ctx := context.Background()
	resp, err := svc.doQuotaRequestWithRetry(ctx, req, "test-provider-403", "test", "zai-coding")
	if err != nil {
		t.Fatalf("expected no error for non-retryable status, got: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	if callCount != 1 {
		t.Errorf("expected 1 call (no retry for 403), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels - Additional Tests
// ---------------------------------------------------------------------------
