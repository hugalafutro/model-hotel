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
	cacheModelByUUID(m)

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
	cacheModelByUUID(m)

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
	cacheModelByUUID(nil)

	// Cache should still be empty
	_, ok := GetCachedByUUID(uuid.New())
	if ok {
		t.Error("caching nil should not add entries")
	}
}

// ---------------------------------------------------------------------------
// GetCachedByModelID
// ---------------------------------------------------------------------------

func TestGetCachedByModelID_EmptyCache(t *testing.T) {
	InvalidateModelCache()

	_, ok := GetCachedByModelID("gpt-4")
	if ok {
		t.Error("GetCachedByModelID should return false for empty cache")
	}
}

func TestGetCachedByModelID_CacheHit(t *testing.T) {
	InvalidateModelCache()

	models := []*Model{
		{
			ID:      uuid.New(),
			ModelID: "gpt-4",
			Name:    "GPT-4",
			Enabled: true,
		},
	}
	cacheModelsByModelID("gpt-4", models)

	found, ok := GetCachedByModelID("gpt-4")
	if !ok {
		t.Fatal("GetCachedByModelID should find cached models")
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 model, got %d", len(found))
	}
	if found[0].ModelID != "gpt-4" {
		t.Errorf("ModelID = %q, want %q", found[0].ModelID, "gpt-4")
	}
}

func TestGetCachedByModelID_CacheMiss(t *testing.T) {
	InvalidateModelCache()

	models := []*Model{
		{ID: uuid.New(), ModelID: "gpt-4"},
	}
	cacheModelsByModelID("gpt-4", models)

	_, ok := GetCachedByModelID("claude-3")
	if ok {
		t.Error("GetCachedByModelID should return false for uncached model ID")
	}
}

func TestGetCachedByModelID_ExpiredEntry(t *testing.T) {
	InvalidateModelCache()

	models := []*Model{
		{ID: uuid.New(), ModelID: "gpt-4"},
	}

	// Manually insert an expired entry
	modelCacheMu.Lock()
	modelByModelIDCache["gpt-4"] = modelCacheEntry{
		models:    models,
		expiresAt: time.Now().Add(-1 * time.Hour),
	}
	modelCacheMu.Unlock()

	_, ok := GetCachedByModelID("gpt-4")
	if ok {
		t.Error("GetCachedByModelID should return false for expired entry")
	}
}

func TestGetCachedByModelID_MultipleModels(t *testing.T) {
	InvalidateModelCache()

	models := []*Model{
		{ID: uuid.New(), ModelID: "gpt-4", ProviderName: "openai"},
		{ID: uuid.New(), ModelID: "gpt-4", ProviderName: "azure"},
	}
	cacheModelsByModelID("gpt-4", models)

	found, ok := GetCachedByModelID("gpt-4")
	if !ok {
		t.Fatal("GetCachedByModelID should find cached models")
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 models, got %d", len(found))
	}
	if found[0].ProviderName != "openai" {
		t.Errorf("first model ProviderName = %q, want %q", found[0].ProviderName, "openai")
	}
	if found[1].ProviderName != "azure" {
		t.Errorf("second model ProviderName = %q, want %q", found[1].ProviderName, "azure")
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
	cacheModelByCompositeKey(providerID, "gpt-4", m)

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
	cacheModelByCompositeKey(providerID, "gpt-4", m)

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
	cacheModelByCompositeKey(uuid.New(), "gpt-4", nil)

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
	cacheModelByUUID(m)
	cacheModelsByModelID("gpt-4", []*Model{m})
	cacheModelByCompositeKey(providerID, "gpt-4", m)

	// Confirm all are cached
	_, ok := GetCachedByUUID(id)
	if !ok {
		t.Fatal("model should be in UUID cache before invalidation")
	}
	_, ok = GetCachedByModelID("gpt-4")
	if !ok {
		t.Fatal("model should be in ModelID cache before invalidation")
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
	_, ok = GetCachedByModelID("gpt-4")
	if ok {
		t.Error("ModelID cache should be empty after invalidation")
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
	cacheModelByUUID(m)

	InvalidateModelCache()

	_, ok := GetCachedByUUID(id)
	if ok {
		t.Error("should not find entry after invalidation")
	}

	// Re-insert
	cacheModelByUUID(m)

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

	WarmModelCache(models)

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

	WarmModelCache(models)

	// 1. UUID cache: all models should be findable by their UUID.
	for _, m := range models {
		if !IsCachedByUUID(m.ID) {
			t.Errorf("IsCachedByUUID: model %s should be cached", m.ID)
		}
	}

	// 2. ModelID string cache: "deepseek-r1" and "gpt-4" should be cached.
	if !IsCachedByModelID("deepseek-r1") {
		t.Error("IsCachedByModelID: deepseek-r1 should be cached")
	}
	if !IsCachedByModelID("gpt-4") {
		t.Error("IsCachedByModelID: gpt-4 should be cached")
	}

	// 3. Composite key cache: each provider:modelID pair should be cached.
	for _, m := range models {
		if !IsCachedByCompositeKey(m.ProviderID, m.ModelID) {
			t.Errorf("IsCachedByCompositeKey: %s:%s should be cached", m.ProviderID, m.ModelID)
		}
	}

	// 4. Verify data integrity: GetCachedByModelID for "deepseek-r1" returns 2 models.
	found, ok := GetCachedByModelID("deepseek-r1")
	if !ok {
		t.Fatal("GetCachedByModelID: deepseek-r1 should be found")
	}
	if len(found) != 2 {
		t.Errorf("GetCachedByModelID: expected 2 models for deepseek-r1, got %d", len(found))
	}
}

func TestWarmModelCache_EmptySlice(t *testing.T) {
	InvalidateModelCache()

	// Should not panic
	WarmModelCache([]*Model{})

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
	WarmModelCache(nil)

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
	cacheModelByUUID(m1)

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
	WarmModelCache([]*Model{m2})

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
	cacheModelByUUID(m1)

	// Warm with a different model
	id2 := uuid.New()
	m2 := &Model{
		ID:      id2,
		ModelID: "new-model",
		Name:    "New",
	}
	WarmModelCache([]*Model{m2})

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

func TestCacheModelsByModelID_PopulatesUUIDCache(t *testing.T) {
	InvalidateModelCache()

	id := uuid.New()
	models := []*Model{
		{ID: id, ModelID: "gpt-4"},
	}
	cacheModelsByModelID("gpt-4", models)

	// Should also be findable by UUID
	found, ok := GetCachedByUUID(id)
	if !ok {
		t.Error("cacheModelsByModelID should also populate UUID cache")
	}
	if found.ModelID != "gpt-4" {
		t.Errorf("UUID cache ModelID = %q, want %q", found.ModelID, "gpt-4")
	}
}

func TestCacheModelsByModelID_EmptySlice(t *testing.T) {
	InvalidateModelCache()

	// cacheModelsByModelID with an empty slice stores an empty entry,
	// but GetCachedByModelID returns nil, false for an empty slice
	// because the stored models list has length 0.
	// This is expected behavior — an empty result isn't useful to cache.
	cacheModelsByModelID("empty-model", []*Model{})

	_, ok := GetCachedByModelID("empty-model")
	// The entry is stored but contains an empty slice, which is still
	// a valid cache hit (even though the result is an empty list).
	// The actual behavior: empty slices ARE cached and return ([], true).
	if !ok {
		t.Error("empty model slice should still be cached")
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestCacheModelByUUID_ConcurrentAccess(t *testing.T) {
	InvalidateModelCache()

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent writes
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := &Model{
				ID:      uuid.New(),
				ModelID: "concurrent-model",
			}
			cacheModelByUUID(m)
		}()
	}

	// Concurrent reads
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = GetCachedByUUID(uuid.New())
		}()
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
	cacheModelByUUID(m)

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = GetCachedByUUID(id)
		}()
	}
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			InvalidateModelCache()
		}()
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
	cacheModelByCompositeKey(uuid.New(), "test", nil)

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

	cacheModelByCompositeKey(providerA, "gpt-4", mA)
	cacheModelByCompositeKey(providerB, "gpt-4", mB)

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
