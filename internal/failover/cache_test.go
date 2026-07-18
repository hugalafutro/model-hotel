package failover

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// GetCachedFailoverByModel
// ---------------------------------------------------------------------------

func TestGetCachedFailoverByModel_EmptyCache(t *testing.T) {
	InvalidateFailoverCache()

	_, ok := GetCachedFailoverByModel("nonexistent")
	if ok {
		t.Error("GetCachedFailoverByModel should return false for empty cache")
	}
}

func TestGetCachedFailoverByModel_CacheHit(t *testing.T) {
	InvalidateFailoverCache()

	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "gpt-4",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	found, ok := GetCachedFailoverByModel("gpt-4")
	if !ok {
		t.Fatal("GetCachedFailoverByModel should find cached group")
	}
	if found.DisplayModel != "gpt-4" {
		t.Errorf("DisplayModel = %q, want %q", found.DisplayModel, "gpt-4")
	}
	if found.ID != fg.ID {
		t.Errorf("ID mismatch: got %v, want %v", found.ID, fg.ID)
	}
}

func TestGetCachedFailoverByModel_CacheMiss(t *testing.T) {
	InvalidateFailoverCache()

	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "gpt-4",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	_, ok := GetCachedFailoverByModel("claude-3")
	if ok {
		t.Error("GetCachedFailoverByModel should return false for uncached model")
	}
}

func TestGetCachedFailoverByModel_ExpiredEntry(t *testing.T) {
	InvalidateFailoverCache()

	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "gpt-4",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}

	// Manually insert an expired entry
	failoverCacheMu.Lock()
	failoverByModelCache["gpt-4"] = failoverCacheEntry{
		group:     *fg,
		expiresAt: time.Now().Add(-1 * time.Hour), // expired
	}
	failoverCacheMu.Unlock()

	_, ok := GetCachedFailoverByModel("gpt-4")
	if ok {
		t.Error("GetCachedFailoverByModel should return false for expired entry")
	}
}

func TestGetCachedFailoverByModel_ValidEntry(t *testing.T) {
	InvalidateFailoverCache()

	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "gpt-4",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	found, ok := GetCachedFailoverByModel("gpt-4")
	if !ok {
		t.Fatal("GetCachedFailoverByModel should find fresh entry")
	}
	if found.DisplayModel != "gpt-4" {
		t.Errorf("DisplayModel = %q, want %q", found.DisplayModel, "gpt-4")
	}
}

// ---------------------------------------------------------------------------
// InvalidateFailoverCache
// ---------------------------------------------------------------------------

func TestInvalidateFailoverCache_RemovesAll(t *testing.T) {
	fg1 := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "model-a",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	fg2 := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "model-b",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg1)
	cacheFailoverGroup(fg2)

	// Confirm both are cached
	_, ok := GetCachedFailoverByModel("model-a")
	if !ok {
		t.Fatal("model-a should be in cache before invalidation")
	}
	_, ok = GetCachedFailoverByModel("model-b")
	if !ok {
		t.Fatal("model-b should be in cache before invalidation")
	}

	InvalidateFailoverCache()

	_, ok = GetCachedFailoverByModel("model-a")
	if ok {
		t.Error("model-a should not be in cache after invalidation")
	}
	_, ok = GetCachedFailoverByModel("model-b")
	if ok {
		t.Error("model-b should not be in cache after invalidation")
	}
}

func TestInvalidateFailoverCache_EmptyCache(t *testing.T) {
	// Should not panic on empty cache
	InvalidateFailoverCache()
	InvalidateFailoverCache()

	// Verify cache is empty after invalidation
	_, ok := GetCachedFailoverByModel("nonexistent-model")
	if ok {
		t.Error("GetCachedFailoverByModel should return ok=false after InvalidateFailoverCache on empty cache")
	}
}

func TestInvalidateFailoverCache_AllowsReinsertion(t *testing.T) {
	InvalidateFailoverCache()

	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "reinsert-test",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	InvalidateFailoverCache()

	_, ok := GetCachedFailoverByModel("reinsert-test")
	if ok {
		t.Error("should not find entry after invalidation")
	}

	// Re-insert
	cacheFailoverGroup(fg)

	found, ok := GetCachedFailoverByModel("reinsert-test")
	if !ok {
		t.Fatal("should find entry after re-insertion")
	}
	if found.DisplayModel != "reinsert-test" {
		t.Errorf("DisplayModel = %q, want %q", found.DisplayModel, "reinsert-test")
	}
}

// ---------------------------------------------------------------------------
// InvalidateFailoverCacheKey
// ---------------------------------------------------------------------------

func TestInvalidateFailoverCacheKey_RemovesSingleEntry(t *testing.T) {
	InvalidateFailoverCache()

	// Warm cache with two groups
	group1 := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "model-a",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	group2 := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "model-b",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(group1)
	cacheFailoverGroup(group2)

	// Verify both are cached
	if _, ok := GetCachedFailoverByModel("model-a"); !ok {
		t.Fatal("expected model-a to be cached")
	}
	if _, ok := GetCachedFailoverByModel("model-b"); !ok {
		t.Fatal("expected model-b to be cached")
	}

	// Invalidate only model-a
	InvalidateFailoverCacheKey("model-a")

	// model-a should be gone
	if _, ok := GetCachedFailoverByModel("model-a"); ok {
		t.Fatal("expected model-a to be invalidated")
	}

	// model-b should still be cached
	if _, ok := GetCachedFailoverByModel("model-b"); !ok {
		t.Fatal("expected model-b to still be cached")
	}

	// Clean up
	InvalidateFailoverCache()
}

func TestInvalidateFailoverCacheKey_NonExistentKey(t *testing.T) {
	InvalidateFailoverCache()

	// Should not panic on non-existent key
	InvalidateFailoverCacheKey("nonexistent")

	// Verify cache is still empty
	_, ok := GetCachedFailoverByModel("anything")
	if ok {
		t.Error("cache should be empty after invalidating non-existent key")
	}
}

func TestInvalidateFailoverCacheKey_EmptyCache(t *testing.T) {
	InvalidateFailoverCache()

	// Should not panic on empty cache
	InvalidateFailoverCacheKey("any-key")

	// Verify cache is still empty
	_, ok := GetCachedFailoverByModel("any-key")
	if ok {
		t.Error("cache should remain empty")
	}
}

// ---------------------------------------------------------------------------
// WarmFailoverCache
// ---------------------------------------------------------------------------

func TestWarmFailoverCache_MultipleGroups(t *testing.T) {
	InvalidateFailoverCache()

	groups := []*FailoverGroup{
		{
			ID:            uuid.New(),
			DisplayModel:  "gpt-4",
			PriorityOrder: []uuid.UUID{uuid.New()},
			GroupEnabled:  true,
		},
		{
			ID:            uuid.New(),
			DisplayModel:  "claude-3",
			PriorityOrder: []uuid.UUID{uuid.New(), uuid.New()},
			GroupEnabled:  true,
		},
		{
			ID:            uuid.New(),
			DisplayModel:  "gemini-pro",
			PriorityOrder: []uuid.UUID{uuid.New()},
			GroupEnabled:  true,
		},
	}

	WarmFailoverCache(groups)

	for _, fg := range groups {
		found, ok := GetCachedFailoverByModel(fg.DisplayModel)
		if !ok {
			t.Errorf("WarmFailoverCache: %q should be in cache", fg.DisplayModel)
			continue
		}
		if found.ID != fg.ID {
			t.Errorf("WarmFailoverCache: %q ID mismatch, got %v, want %v", fg.DisplayModel, found.ID, fg.ID)
		}
		if found.DisplayModel != fg.DisplayModel {
			t.Errorf("WarmFailoverCache: %q DisplayModel mismatch, got %q, want %q", fg.DisplayModel, found.DisplayModel, fg.DisplayModel)
		}
	}
}

func TestWarmFailoverCache_EmptySlice(t *testing.T) {
	InvalidateFailoverCache()

	// Should not panic
	WarmFailoverCache([]*FailoverGroup{})

	// Verify cache is still empty after warming with empty slice
	_, ok := GetCachedFailoverByModel("nonexistent-model")
	if ok {
		t.Error("GetCachedFailoverByModel should return ok=false after WarmFailoverCache with empty slice")
	}
}

func TestWarmFailoverCache_NilSlice(t *testing.T) {
	InvalidateFailoverCache()

	// Should not panic
	WarmFailoverCache(nil)

	// Verify cache is still empty after warming with nil slice
	_, ok := GetCachedFailoverByModel("nonexistent-model")
	if ok {
		t.Error("GetCachedFailoverByModel should return ok=false after WarmFailoverCache with nil slice")
	}
}

func TestWarmFailoverCache_OverwritesExisting(t *testing.T) {
	InvalidateFailoverCache()

	id1 := uuid.New()
	fg1 := &FailoverGroup{
		ID:            id1,
		DisplayModel:  "overwrite-test",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg1)

	found, ok := GetCachedFailoverByModel("overwrite-test")
	if !ok {
		t.Fatal("should find initial entry")
	}
	if found.ID != id1 {
		t.Error("initial entry ID mismatch")
	}

	// Warm with a new group for the same model
	id2 := uuid.New()
	fg2 := &FailoverGroup{
		ID:            id2,
		DisplayModel:  "overwrite-test",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	WarmFailoverCache([]*FailoverGroup{fg2})

	found, ok = GetCachedFailoverByModel("overwrite-test")
	if !ok {
		t.Fatal("should find overwritten entry")
	}
	if found.ID != id2 {
		t.Errorf("overwritten entry should have new ID, got %v, want %v", found.ID, id2)
	}
}

func TestWarmFailoverCache_PreservesOtherEntries(t *testing.T) {
	InvalidateFailoverCache()

	// Insert an entry first
	fg1 := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "existing-model",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg1)

	// Warm with a different entry
	fg2 := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "new-model",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	WarmFailoverCache([]*FailoverGroup{fg2})

	// Both should be found
	_, ok := GetCachedFailoverByModel("existing-model")
	if !ok {
		t.Error("existing entry should still be in cache after warming different model")
	}
	_, ok = GetCachedFailoverByModel("new-model")
	if !ok {
		t.Error("new entry should be in cache after warming")
	}
}

// ---------------------------------------------------------------------------
// cacheFailoverGroup
// ---------------------------------------------------------------------------

func TestCacheFailoverGroup_NilGroup(t *testing.T) {
	InvalidateFailoverCache()

	// Should not panic
	cacheFailoverGroup(nil)

	// Cache should still be empty
	_, ok := GetCachedFailoverByModel("anything")
	if ok {
		t.Error("caching nil should not add entries")
	}
}

func TestCacheFailoverGroup_GroupWithPriorityOrder(t *testing.T) {
	InvalidateFailoverCache()

	po := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "ordered-model",
		PriorityOrder: po,
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	found, ok := GetCachedFailoverByModel("ordered-model")
	if !ok {
		t.Fatal("should find cached group")
	}
	if len(found.PriorityOrder) != 3 {
		t.Errorf("PriorityOrder length = %d, want 3", len(found.PriorityOrder))
	}
	for i, id := range po {
		if found.PriorityOrder[i] != id {
			t.Errorf("PriorityOrder[%d] = %v, want %v", i, found.PriorityOrder[i], id)
		}
	}
}

func TestCacheFailoverGroup_EntryEnabled(t *testing.T) {
	InvalidateFailoverCache()

	enabled := map[string]bool{
		uuid.New().String(): true,
		uuid.New().String(): false,
	}
	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "enabled-model",
		PriorityOrder: []uuid.UUID{uuid.New()},
		EntryEnabled:  enabled,
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	found, ok := GetCachedFailoverByModel("enabled-model")
	if !ok {
		t.Fatal("should find cached group")
	}
	for k, v := range enabled {
		if found.EntryEnabled[k] != v {
			t.Errorf("EntryEnabled[%q] = %v, want %v", k, found.EntryEnabled[k], v)
		}
	}
}

// ---------------------------------------------------------------------------
// Cache TTL
// ---------------------------------------------------------------------------

func TestFailoverCacheTTLValue(t *testing.T) {
	if failoverCacheTTL != 5*time.Minute {
		t.Errorf("failoverCacheTTL should be 5 minutes, got %v", failoverCacheTTL)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestCacheFailoverGroup_ConcurrentAccess(t *testing.T) {
	InvalidateFailoverCache()

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent writes
	for i := range 25 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			fg := &FailoverGroup{
				ID:            uuid.New(),
				DisplayModel:  "concurrent-model",
				PriorityOrder: []uuid.UUID{uuid.New()},
				GroupEnabled:  true,
			}
			cacheFailoverGroup(fg)
		}(i)
	}

	// Concurrent reads
	for range 25 {
		wg.Go(func() {
			_, _ = GetCachedFailoverByModel("concurrent-model")
		})
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent access error: %v", err)
	}
}

func TestInvalidateFailoverCache_ConcurrentWithReads(t *testing.T) {
	InvalidateFailoverCache()

	fg := &FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "concurrent-invalidate",
		PriorityOrder: []uuid.UUID{uuid.New()},
		GroupEnabled:  true,
	}
	cacheFailoverGroup(fg)

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for range 25 {
		wg.Go(func() {
			_, _ = GetCachedFailoverByModel("concurrent-invalidate")
		})
	}
	for range 25 {
		wg.Go(func() {
			InvalidateFailoverCache()
		})
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent invalidation error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IsCachedByModel
// ---------------------------------------------------------------------------

func TestIsCachedByModel_EmptyCache(t *testing.T) {
	InvalidateFailoverCache()
	if IsCachedByModel("nonexistent") {
		t.Error("IsCachedByModel should return false for empty cache")
	}
}

func TestIsCachedByModel_Cached(t *testing.T) {
	InvalidateFailoverCache()
	WarmFailoverCache([]*FailoverGroup{
		{DisplayModel: "my-model"},
	})
	if !IsCachedByModel("my-model") {
		t.Error("IsCachedByModel should return true for cached model")
	}
}

func TestIsCachedByModel_Miss(t *testing.T) {
	InvalidateFailoverCache()
	WarmFailoverCache([]*FailoverGroup{
		{DisplayModel: "my-model"},
	})
	if IsCachedByModel("other-model") {
		t.Error("IsCachedByModel should return false for different model")
	}
}
