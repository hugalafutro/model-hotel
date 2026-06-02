package api

import (
	"fmt"
	"testing"
	"time"
)

func TestLogsCacheGet_NotFound(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     2 * time.Second,
	}

	resp, ok := cache.get("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent key")
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
}

func TestLogsCacheGet_Expired(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     10 * time.Millisecond, // Short TTL for testing
	}

	// Add an entry
	resp := &LogsResponse{Entries: []LogEntry{}, Total: 1}
	cache.entries["test"] = &logsCacheEntry{
		response: resp,
		expiry:   time.Now().Add(-1 * time.Second), // Already expired
	}

	result, ok := cache.get("test")
	if ok {
		t.Error("expected ok=false for expired entry")
	}
	if result != nil {
		t.Errorf("expected nil response for expired entry, got %v", result)
	}
}

func TestLogsCacheGet_Valid(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     2 * time.Second,
	}

	// Add an entry
	expected := &LogsResponse{Entries: []LogEntry{}, Total: 1}
	cache.entries["test"] = &logsCacheEntry{
		response: expected,
		expiry:   time.Now().Add(1 * time.Hour), // Far in future
	}

	result, ok := cache.get("test")
	if !ok {
		t.Error("expected ok=true for valid entry")
	}
	if result != expected {
		t.Errorf("expected response %v, got %v", expected, result)
	}
}

func TestLogsCacheSet_NewEntry(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     2 * time.Second,
	}

	resp := &LogsResponse{Entries: []LogEntry{}, Total: 1}
	cache.set("newkey", resp)

	// Verify it was added
	retrieved, ok := cache.get("newkey")
	if !ok {
		t.Error("expected entry to be found after set")
	}
	if retrieved != resp {
		t.Errorf("expected retrieved response %v, got %v", resp, retrieved)
	}
}

func TestLogsCacheSet_UpdateExisting(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     2 * time.Second,
	}

	// Add initial entry
	oldResp := &LogsResponse{Entries: []LogEntry{}, Total: 1}
	cache.set("key", oldResp)

	// Update it
	newResp := &LogsResponse{Entries: []LogEntry{}, Total: 2}
	cache.set("key", newResp)

	// Verify it was updated
	retrieved, ok := cache.get("key")
	if !ok {
		t.Error("expected entry to be found after update")
	}
	if retrieved != newResp {
		t.Errorf("expected updated response %v, got %v", newResp, retrieved)
	}
}

func TestLogsCacheSet_ExpiresInFuture(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     1 * time.Second,
	}

	resp := &LogsResponse{Entries: []LogEntry{}, Total: 1}
	cache.set("test", resp)

	// Check that expiry is in the future
	entry := cache.entries["test"]
	if entry == nil { //nolint:staticcheck // SA5011
		t.Fatal("expected entry to exist")
	}
	if !entry.expiry.After(time.Now()) { //nolint:staticcheck // SA5011
		t.Error("expected expiry to be in the future")
	}
	if entry.expiry.After(time.Now().Add(2 * time.Second)) { //nolint:staticcheck // SA5011
		t.Error("expected expiry to be within TTL")
	}
}

func TestLogsCacheSet_LazyEviction(t *testing.T) {
	cache := &logsCache{
		entries: make(map[string]*logsCacheEntry),
		ttl:     10 * time.Millisecond, // Short TTL for testing
	}

	// Add some expired entries
	for i := 0; i < 5; i++ {
		cache.entries[fmt.Sprintf("expired%d", i)] = &logsCacheEntry{
			response: &LogsResponse{Entries: []LogEntry{}, Total: i},
			expiry:   time.Now().Add(-1 * time.Second),
		}
	}

	// Add a new entry - this should trigger lazy eviction
	newResp := &LogsResponse{Entries: []LogEntry{}, Total: 100}
	cache.set("new", newResp)

	// Check that expired entries were evicted
	if len(cache.entries) != 1 {
		t.Errorf("expected 1 entry after lazy eviction, got %d", len(cache.entries))
	}
	if _, exists := cache.entries["new"]; !exists {
		t.Error("expected new entry to exist")
	}
}

func TestGlobalLogsCache(t *testing.T) {
	// Test that globalLogsCache is properly initialized
	if globalLogsCache == nil {
		t.Error("globalLogsCache should be initialized")
	}
	if globalLogsCache.entries == nil {
		t.Error("globalLogsCache.entries should be initialized")
	}
	if globalLogsCache.ttl != 2*time.Second {
		t.Errorf("expected TTL of 2 seconds, got %v", globalLogsCache.ttl)
	}
}
