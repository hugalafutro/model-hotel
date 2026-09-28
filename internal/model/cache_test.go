package model

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// GetCachedByUUID
// ---------------------------------------------------------------------------

func TestGetCachedByUUID_EmptyCache(t *testing.T) {
	InvalidateModelCache()

	_, ok := GetCachedByUUID(uuid.New())
	if ok {
		t.Error("GetCachedByUUID should return false for empty cache")
	}
}

func TestGetCachedByUUID_CacheHit(t *testing.T) {
	InvalidateModelCache()

	id := uuid.New()
	m := &Model{
		ID:      id,
		ModelID: "gpt-4",
		Name:    "GPT-4",
		Enabled: true,
	}
	cacheModelByUUIDAt(m, CacheGen())

	found, ok := GetCachedByUUID(id)
	if !ok {
		t.Fatal("GetCachedByUUID should find cached model")
	}
	if found.ID != id {
		t.Errorf("ID mismatch: got %v, want %v", found.ID, id)
	}
	if found.ModelID != "gpt-4" {
		t.Errorf("ModelID mismatch: got %q, want %q", found.ModelID, "gpt-4")
	}
}

func TestGetCachedByUUID_CacheMiss(t *testing.T) {
	InvalidateModelCache()

	id := uuid.New()
	m := &Model{
		ID:      id,
		ModelID: "gpt-4",
	}
	cacheModelByUUIDAt(m, CacheGen())

	_, ok := GetCachedByUUID(uuid.New())
	if ok {
		t.Error("GetCachedByUUID should return false for uncached UUID")
	}
}

func TestGetCachedByUUID_ExpiredEntry(t *testing.T) {
	InvalidateModelCache()

	id := uuid.New()
	m := &Model{
		ID:      id,
		ModelID: "gpt-4",
	}

	// Manually insert an expired entry
	modelCacheMu.Lock()
	modelByUUIDCache[id] = modelByIDCacheEntry{
		model:     m,
		expiresAt: time.Now().Add(-1 * time.Hour),
	}
	modelCacheMu.Unlock()

	_, ok := GetCachedByUUID(id)
	if ok {
		t.Error("GetCachedByUUID should return false for expired entry")
	}
}

func TestGetCachedByUUID_NilModel(t *testing.T) {
	InvalidateModelCache()

	// Should not panic
	cacheModelByUUIDAt(nil, CacheGen())

	// Cache should still be empty
	_, ok := GetCachedByUUID(uuid.New())
	if ok {
		t.Error("caching nil should not add entries")
	}
}

// ---------------------------------------------------------------------------
// GetCachedByCompositeKey
// ---------------------------------------------------------------------------

func TestGetCachedByCompositeKey_EmptyCache(t *testing.T) {
	InvalidateModelCache()

	_, ok := GetCachedByCompositeKey(uuid.New(), "gpt-4")
	if ok {
		t.Error("GetCachedByCompositeKey should return false for empty cache")
	}
}

func TestGetCachedByCompositeKey_CacheHit(t *testing.T) {
	InvalidateModelCache()

	providerID := uuid.New()
	m := &Model{
		ID:         uuid.New(),
		ProviderID: providerID,
		ModelID:    "gpt-4",
		Name:       "GPT-4",
		Enabled:    true,
	}
	cacheModelByCompositeKeyAt(providerID, "gpt-4", m, CacheGen())

	found, ok := GetCachedByCompositeKey(providerID, "gpt-4")
	if !ok {
		t.Fatal("GetCachedByCompositeKey should find cached model")
	}
	if found.ModelID != "gpt-4" {
		t.Errorf("ModelID = %q, want %q", found.ModelID, "gpt-4")
	}
	if found.ProviderID != providerID {
		t.Errorf("ProviderID = %v, want %v", found.ProviderID, providerID)
	}
}

func TestGetCachedByCompositeKey_CacheMiss(t *testing.T) {
	InvalidateModelCache()

	providerID := uuid.New()
	m := &Model{
		ID:         uuid.New(),
		ProviderID: providerID,
		ModelID:    "gpt-4",
	}
	cacheModelByCompositeKeyAt(providerID, "gpt-4", m, CacheGen())

	_, ok := GetCachedByCompositeKey(uuid.New(), "gpt-4")
	if ok {
		t.Error("GetCachedByCompositeKey should return false for different provider")
	}

	_, ok = GetCachedByCompositeKey(providerID, "claude-3")
	if ok {
		t.Error("GetCachedByCompositeKey should return false for different model")
	}
}

func TestGetCachedByCompositeKey_ExpiredEntry(t *testing.T) {
	InvalidateModelCache()

	providerID := uuid.New()
	m := &Model{
		ID:         uuid.New(),
		ProviderID: providerID,
		ModelID:    "gpt-4",
	}

	// Manually insert an expired entry
	key := providerID.String() + ":" + "gpt-4"
	modelCacheMu.Lock()
	modelByCompositeKey[key] = modelByIDCacheEntry{
		model:     m,
		expiresAt: time.Now().Add(-1 * time.Hour),
	}
	modelCacheMu.Unlock()

	_, ok := GetCachedByCompositeKey(providerID, "gpt-4")
	if ok {
		t.Error("GetCachedByCompositeKey should return false for expired entry")
	}
}

func TestGetCachedByCompositeKey_NilModel(t *testing.T) {
	InvalidateModelCache()

	// Should not panic
	cacheModelByCompositeKeyAt(uuid.New(), "gpt-4", nil, CacheGen())

	// Cache should still be empty
	_, ok := GetCachedByCompositeKey(uuid.New(), "gpt-4")
	if ok {
		t.Error("caching nil should not add entries")
	}
}

// ---------------------------------------------------------------------------
// InvalidateModelCache
// ---------------------------------------------------------------------------

func TestInvalidateModelCache_RemovesAll(t *testing.T) {
	id := uuid.New()
	providerID := uuid.New()

	m := &Model{
		ID:         id,
		ProviderID: providerID,
		ModelID:    "gpt-4",
	}
	cacheModelByUUIDAt(m, CacheGen())
	cacheModelByCompositeKeyAt(providerID, "gpt-4", m, CacheGen())

	// Confirm all are cached
	_, ok := GetCachedByUUID(id)
	if !ok {
		t.Fatal("model should be in UUID cache before invalidation")
	}
	_, ok = GetCachedByCompositeKey(providerID, "gpt-4")
	if !ok {
		t.Fatal("model should be in composite key cache before invalidation")
	}

	InvalidateModelCache()

	_, ok = GetCachedByUUID(id)
	if ok {
		t.Error("UUID cache should be empty after invalidation")
	}
	_, ok = GetCachedByCompositeKey(providerID, "gpt-4")
	if ok {
		t.Error("Composite key cache should be empty after invalidation")
	}
}

func TestInvalidateModelCache_EmptyCache(t *testing.T) {
	// Should not panic on empty cache
	InvalidateModelCache()
	InvalidateModelCache()

	// Verify cache is empty after invalidation
	testUUID := uuid.New()
	_, ok := GetCachedByUUID(testUUID)
	if ok {
		t.Error("GetCachedByUUID should return ok=false after InvalidateModelCache on empty cache")
	}
}

func TestInvalidateModelCache_AllowsReinsertion(t *testing.T) {
	InvalidateModelCache()

	id := uuid.New()
	m := &Model{
		ID:      id,
		ModelID: "reinsert-test",
	}
	cacheModelByUUIDAt(m, CacheGen())

	InvalidateModelCache()

	_, ok := GetCachedByUUID(id)
	if ok {
		t.Error("should not find entry after invalidation")
	}

	// Re-insert
	cacheModelByUUIDAt(m, CacheGen())

	found, ok := GetCachedByUUID(id)
	if !ok {
		t.Fatal("should find entry after re-insertion")
	}
	if found.ID != id {
		t.Errorf("ID mismatch: got %v, want %v", found.ID, id)
	}
}

// ---------------------------------------------------------------------------
// WarmModelCache
// ---------------------------------------------------------------------------

func TestWarmModelCache_MultipleModels(t *testing.T) {
	InvalidateModelCache()

	models := []*Model{
		{ID: uuid.New(), ModelID: "gpt-4", Name: "GPT-4"},
		{ID: uuid.New(), ModelID: "claude-3", Name: "Claude 3"},
		{ID: uuid.New(), ModelID: "gemini-pro", Name: "Gemini Pro"},
	}

	WarmModelCacheAt(models, CacheGen())

	for _, m := range models {
		found, ok := GetCachedByUUID(m.ID)
		if !ok {
			t.Errorf("WarmModelCache: model %q should be in UUID cache", m.ModelID)
			continue
		}
		if found.ModelID != m.ModelID {
			t.Errorf("WarmModelCache: ModelID = %q, want %q", found.ModelID, m.ModelID)
		}
	}
}

// TestWarmModelCache_FillsAllSubCaches verifies that WarmModelCache populates
// all three model sub-caches (by UUID, by ModelID string, and by composite
// provider:modelID key), not just the UUID cache.
func TestWarmModelCache_FillsAllSubCaches(t *testing.T) {
	InvalidateModelCache()

	providerID := uuid.New()
	models := []*Model{
		{ID: uuid.New(), ProviderID: providerID, ModelID: "deepseek-r1"},
		{ID: uuid.New(), ProviderID: providerID, ModelID: "deepseek-r1"},
		{ID: uuid.New(), ProviderID: uuid.New(), ModelID: "gpt-4"},
	}

	WarmModelCacheAt(models, CacheGen())

	// 1. UUID cache: all models should be findable by their UUID.
	for _, m := range models {
		if !IsCachedByUUID(m.ID) {
			t.Errorf("IsCachedByUUID: model %s should be cached", m.ID)
		}
	}

	// 2. Composite key cache: each provider:modelID pair should be cached.
	for _, m := range models {
		if !IsCachedByCompositeKey(m.ProviderID, m.ModelID) {
			t.Errorf("IsCachedByCompositeKey: %s:%s should be cached", m.ProviderID, m.ModelID)
		}
	}
}

func TestWarmModelCache_EmptySlice(t *testing.T) {
	InvalidateModelCache()

	// Should not panic
	WarmModelCacheAt([]*Model{}, CacheGen())

	// Verify cache is still empty after warming with empty slice
	testUUID := uuid.New()
	_, ok := GetCachedByUUID(testUUID)
	if ok {
		t.Error("GetCachedByUUID should return ok=false after WarmModelCache with empty slice")
	}
}

func TestWarmModelCache_NilSlice(t *testing.T) {
	InvalidateModelCache()

	// Should not panic
	WarmModelCacheAt(nil, CacheGen())

	// Verify cache is still empty after warming with nil slice
	testUUID := uuid.New()
	_, ok := GetCachedByUUID(testUUID)
	if ok {
		t.Error("GetCachedByUUID should return ok=false after WarmModelCache with nil slice")
	}
}

func TestWarmModelCache_OverwritesExisting(t *testing.T) {
	InvalidateModelCache()

	id1 := uuid.New()
	m1 := &Model{
		ID:      id1,
		ModelID: "overwrite-test",
		Name:    "Original",
	}
	cacheModelByUUIDAt(m1, CacheGen())

	found, ok := GetCachedByUUID(id1)
	if !ok {
		t.Fatal("should find initial entry")
	}
	if found.Name != "Original" {
		t.Errorf("initial Name = %q, want %q", found.Name, "Original")
	}

	// Warm with updated model
	m2 := &Model{
		ID:      id1,
		ModelID: "overwrite-test",
		Name:    "Updated",
	}
	WarmModelCacheAt([]*Model{m2}, CacheGen())

	found, ok = GetCachedByUUID(id1)
	if !ok {
		t.Fatal("should find overwritten entry")
	}
	if found.Name != "Updated" {
		t.Errorf("overwritten Name = %q, want %q", found.Name, "Updated")
	}
}

func TestWarmModelCache_PreservesOtherEntries(t *testing.T) {
	InvalidateModelCache()

	// Insert an entry first
	id1 := uuid.New()
	m1 := &Model{
		ID:      id1,
		ModelID: "existing-model",
		Name:    "Existing",
	}
	cacheModelByUUIDAt(m1, CacheGen())

	// Warm with a different model
	id2 := uuid.New()
	m2 := &Model{
		ID:      id2,
		ModelID: "new-model",
		Name:    "New",
	}
	WarmModelCacheAt([]*Model{m2}, CacheGen())

	// Both should be found
	_, ok := GetCachedByUUID(id1)
	if !ok {
		t.Error("existing entry should still be in cache after warming different model")
	}
	_, ok = GetCachedByUUID(id2)
	if !ok {
		t.Error("new entry should be in cache after warming")
	}
}

// ---------------------------------------------------------------------------
// Cache TTL
// ---------------------------------------------------------------------------

func TestModelCacheTTLValue(t *testing.T) {
	if modelCacheTTL != 5*time.Minute {
		t.Errorf("modelCacheTTL should be 5 minutes, got %v", modelCacheTTL)
	}
}

// ---------------------------------------------------------------------------
// cacheModelsByModelID
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestCacheModelByUUID_ConcurrentAccess(t *testing.T) {
	InvalidateModelCache()

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent writes
	for range 25 {
		wg.Go(func() {
			m := &Model{
				ID:      uuid.New(),
				ModelID: "concurrent-model",
			}
			cacheModelByUUIDAt(m, CacheGen())
		})
	}

	// Concurrent reads
	for range 25 {
		wg.Go(func() {
			_, _ = GetCachedByUUID(uuid.New())
		})
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent access error: %v", err)
	}
}

func TestInvalidateModelCache_ConcurrentWithReads(t *testing.T) {
	InvalidateModelCache()

	id := uuid.New()
	m := &Model{
		ID:      id,
		ModelID: "concurrent-invalidate",
	}
	cacheModelByUUIDAt(m, CacheGen())

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for range 25 {
		wg.Go(func() {
			_, _ = GetCachedByUUID(id)
		})
	}
	for range 25 {
		wg.Go(func() {
			InvalidateModelCache()
		})
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent invalidation error: %v", err)
	}
}

func TestCacheModelByCompositeKey_NilModel(t *testing.T) {
	InvalidateModelCache()

	// Should not panic
	cacheModelByCompositeKeyAt(uuid.New(), "test", nil, CacheGen())

	// Cache should still be empty for this key
	_, ok := GetCachedByCompositeKey(uuid.New(), "test")
	if ok {
		t.Error("caching nil should not add entries")
	}
}

func TestCacheModelByCompositeKey_DifferentProviders(t *testing.T) {
	InvalidateModelCache()

	providerA := uuid.New()
	providerB := uuid.New()

	mA := &Model{
		ID:         uuid.New(),
		ProviderID: providerA,
		ModelID:    "gpt-4",
		Name:       "OpenAI GPT-4",
	}
	mB := &Model{
		ID:         uuid.New(),
		ProviderID: providerB,
		ModelID:    "gpt-4",
		Name:       "Azure GPT-4",
	}

	cacheModelByCompositeKeyAt(providerA, "gpt-4", mA, CacheGen())
	cacheModelByCompositeKeyAt(providerB, "gpt-4", mB, CacheGen())

	foundA, ok := GetCachedByCompositeKey(providerA, "gpt-4")
	if !ok {
		t.Fatal("should find OpenAI model")
	}
	if foundA.Name != "OpenAI GPT-4" {
		t.Errorf("OpenAI model Name = %q, want %q", foundA.Name, "OpenAI GPT-4")
	}

	foundB, ok := GetCachedByCompositeKey(providerB, "gpt-4")
	if !ok {
		t.Fatal("should find Azure model")
	}
	if foundB.Name != "Azure GPT-4" {
		t.Errorf("Azure model Name = %q, want %q", foundB.Name, "Azure GPT-4")
	}
}

// ---------------------------------------------------------------------------
// Generation guard: a fill that captured its generation before an
// invalidation must not reinstall the row it read.
// ---------------------------------------------------------------------------

func TestCacheFillAt_StaleGenerationDoesNotInstall(t *testing.T) {
	InvalidateModelCache()
	providerID := uuid.New()
	m := &Model{ID: uuid.New(), ProviderID: providerID, ModelID: "gpt-4", Enabled: true}

	// A read-through captured the generation, then a write invalidated
	// while its SELECT was in flight.
	gen := CacheGen()
	InvalidateModelCache()

	cacheModelByUUIDAt(m, gen)
	cacheModelByCompositeKeyAt(providerID, "gpt-4", m, gen)
	WarmModelCacheAt([]*Model{m}, gen)

	if _, ok := GetCachedByUUID(m.ID); ok {
		t.Error("stale fill installed by UUID")
	}
	if _, ok := GetCachedByCompositeKey(providerID, "gpt-4"); ok {
		t.Error("stale fill installed by composite key")
	}
}

func TestCacheFillAt_CurrentGenerationInstalls(t *testing.T) {
	InvalidateModelCache()
	providerID := uuid.New()
	m := &Model{ID: uuid.New(), ProviderID: providerID, ModelID: "gpt-4", Enabled: true}

	gen := CacheGen()
	cacheModelByUUIDAt(m, gen)
	cacheModelByCompositeKeyAt(providerID, "gpt-4", m, gen)

	if _, ok := GetCachedByUUID(m.ID); !ok {
		t.Error("current-generation fill must install by UUID")
	}
	if _, ok := GetCachedByCompositeKey(providerID, "gpt-4"); !ok {
		t.Error("current-generation fill must install by composite key")
	}
	if CacheGen() != gen {
		t.Error("a fill must not advance the generation")
	}
}
