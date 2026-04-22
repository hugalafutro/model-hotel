package model

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type modelCacheEntry struct {
	models    []*Model
	expiresAt time.Time
}

type modelByIDCacheEntry struct {
	model     *Model
	expiresAt time.Time
}

var (
	modelByModelIDCache = make(map[string]modelCacheEntry)
	modelByUUIDCache     = make(map[uuid.UUID]modelByIDCacheEntry)
	modelByCompositeKey  = make(map[string]modelByIDCacheEntry)
	modelCacheMu         sync.RWMutex
)

const modelCacheTTL = 5 * time.Minute

func cacheModelsByModelID(modelID string, models []*Model) {
	modelCacheMu.Lock()
	modelByModelIDCache[modelID] = modelCacheEntry{models: models, expiresAt: time.Now().Add(modelCacheTTL)}
	for _, m := range models {
		modelByUUIDCache[m.ID] = modelByIDCacheEntry{model: m, expiresAt: time.Now().Add(modelCacheTTL)}
	}
	modelCacheMu.Unlock()
}

func cacheModelByUUID(m *Model) {
	if m == nil {
		return
	}
	modelCacheMu.Lock()
	modelByUUIDCache[m.ID] = modelByIDCacheEntry{model: m, expiresAt: time.Now().Add(modelCacheTTL)}
	modelCacheMu.Unlock()
}

func cacheModelByCompositeKey(providerID uuid.UUID, modelID string, m *Model) {
	if m == nil {
		return
	}
	key := providerID.String() + ":" + modelID
	modelCacheMu.Lock()
	modelByCompositeKey[key] = modelByIDCacheEntry{model: m, expiresAt: time.Now().Add(modelCacheTTL)}
	modelCacheMu.Unlock()
}

func GetCachedByModelID(modelID string) ([]*Model, bool) {
	modelCacheMu.RLock()
	entry, ok := modelByModelIDCache[modelID]
	modelCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.models, true
}

func GetCachedByUUID(id uuid.UUID) (*Model, bool) {
	modelCacheMu.RLock()
	entry, ok := modelByUUIDCache[id]
	modelCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.model, true
}

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

func InvalidateModelCache() {
	modelCacheMu.Lock()
	modelByModelIDCache = make(map[string]modelCacheEntry)
	modelByUUIDCache = make(map[uuid.UUID]modelByIDCacheEntry)
	modelByCompositeKey = make(map[string]modelByIDCacheEntry)
	modelCacheMu.Unlock()
}

func WarmModelCache(models []*Model) {
	modelCacheMu.Lock()
	for _, m := range models {
		modelByUUIDCache[m.ID] = modelByIDCacheEntry{model: m, expiresAt: time.Now().Add(modelCacheTTL)}
	}
	modelCacheMu.Unlock()
}