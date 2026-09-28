// Package model provides model discovery and caching functionality.
package model

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

type modelByIDCacheEntry struct {
	model     *Model
	expiresAt time.Time
}

var (
	modelByUUIDCache    = make(map[uuid.UUID]modelByIDCacheEntry)
	modelByCompositeKey = make(map[string]modelByIDCacheEntry)
	modelCacheMu        sync.RWMutex
	// modelCacheGen advances on every invalidation. A read-through fill
	// captures it before its SELECT and installs only if it is unchanged, so a
	// read that overlapped a write (a disable, a provider key rotation) can
	// never reinstall the pre-write row for the TTL. Global rather than per
	// key because the invalidations here are whole-cache flushes anyway: a
	// discovery scan's per-row Upsert flushes drop the read-throughs that
	// overlap it, rows the flush had already made a miss.
	modelCacheGen atomic.Uint64
)

// CacheGen is the generation a read-through fill captures before querying;
// see modelCacheGen.
func CacheGen() uint64 { return modelCacheGen.Load() }

const modelCacheTTL = 5 * time.Minute

func cacheModelByUUIDAt(m *Model, gen uint64) {
	if m == nil {
		return
	}
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	if modelCacheGen.Load() != gen {
		return
	}
	modelByUUIDCache[m.ID] = modelByIDCacheEntry{model: m, expiresAt: time.Now().Add(modelCacheTTL)}
}

func cacheModelByCompositeKeyAt(providerID uuid.UUID, modelID string, m *Model, gen uint64) {
	if m == nil {
		return
	}
	key := providerID.String() + ":" + modelID
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	if modelCacheGen.Load() != gen {
		return
	}
	modelByCompositeKey[key] = modelByIDCacheEntry{model: m, expiresAt: time.Now().Add(modelCacheTTL)}
}

// GetCachedByUUID returns a cached model by UUID if not expired.
func GetCachedByUUID(id uuid.UUID) (*Model, bool) {
	modelCacheMu.RLock()
	entry, ok := modelByUUIDCache[id]
	modelCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.model, true
}

// GetCachedByCompositeKey returns a cached model by provider ID and model ID composite key if not expired.
func GetCachedByCompositeKey(providerID uuid.UUID, modelID string) (*Model, bool) {
	key := providerID.String() + ":" + modelID
	modelCacheMu.RLock()
	entry, ok := modelByCompositeKey[key]
	modelCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.model, true
}

// IsCachedByUUID reports whether a model for the given UUID is present in the
// cache and not expired. It does not modify the cache.
func IsCachedByUUID(id uuid.UUID) bool {
	_, ok := GetCachedByUUID(id)
	return ok
}

// IsCachedByCompositeKey reports whether a model for the given provider+model
// composite key is present in the cache and not expired. It does not modify
// the cache.
func IsCachedByCompositeKey(providerID uuid.UUID, modelID string) bool {
	_, ok := GetCachedByCompositeKey(providerID, modelID)
	return ok
}

// InvalidateModelCache clears all model cache entries.
func InvalidateModelCache() {
	modelCacheMu.Lock()
	modelCacheGen.Add(1)
	modelByUUIDCache = make(map[uuid.UUID]modelByIDCacheEntry)
	modelByCompositeKey = make(map[string]modelByIDCacheEntry)
	modelCacheMu.Unlock()
}

// WarmModelCacheAt installs rows read at a captured generation into both
// sub-caches (by UUID and by composite provider:modelID key); nothing installs
// if an invalidation has landed since the capture.
func WarmModelCacheAt(models []*Model, gen uint64) {
	exp := time.Now().Add(modelCacheTTL)
	modelCacheMu.Lock()
	if modelCacheGen.Load() != gen {
		modelCacheMu.Unlock()
		debuglog.Info("model: warm skipped, the cache was invalidated during the read", "count", len(models))
		return
	}
	for _, m := range models {
		modelByUUIDCache[m.ID] = modelByIDCacheEntry{model: m, expiresAt: exp}
		modelByCompositeKey[m.ProviderID.String()+":"+m.ModelID] = modelByIDCacheEntry{model: m, expiresAt: exp}
	}
	modelCacheMu.Unlock()
	debuglog.Info("model: warmed cache", "count", len(models))
}
