package provider

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type providerCacheEntry struct {
	provider  *Provider
	expiresAt time.Time
}

var (
	providerByIDCache   = make(map[uuid.UUID]providerCacheEntry)
	providerByNameCache = make(map[string]providerCacheEntry)
	providerCacheMu     sync.RWMutex
)

const providerCacheTTL = 5 * time.Minute

func cacheProvider(p *Provider) {
	if p == nil {
		return
	}
	entry := providerCacheEntry{
		provider:  p,
		expiresAt: time.Now().Add(providerCacheTTL),
	}
	providerCacheMu.Lock()
	providerByIDCache[p.ID] = entry
	providerByNameCache[p.Name] = entry
	providerCacheMu.Unlock()
}

func GetCachedByID(id uuid.UUID) (*Provider, bool) {
	providerCacheMu.RLock()
	entry, ok := providerByIDCache[id]
	providerCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.provider, true
}

func GetCachedByName(name string) (*Provider, bool) {
	providerCacheMu.RLock()
	entry, ok := providerByNameCache[name]
	providerCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.provider, true
}

func InvalidateProviderCache() {
	providerCacheMu.Lock()
	providerByIDCache = make(map[uuid.UUID]providerCacheEntry)
	providerByNameCache = make(map[string]providerCacheEntry)
	providerCacheMu.Unlock()
}

func WarmProviderCache(providers []*Provider) {
	for _, p := range providers {
		cacheProvider(p)
	}
}