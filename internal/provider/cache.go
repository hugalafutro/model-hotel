package provider

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// NormalizeName normalizes a provider name by replacing spaces with hyphens.
func NormalizeName(name string) string {
	s := strings.ReplaceAll(name, " ", "-")
	return s
}

type providerCacheEntry struct {
	provider  *Provider
	expiresAt time.Time
}

var (
	providerByIDCache         = make(map[uuid.UUID]providerCacheEntry)
	providerByNameCache       = make(map[string]providerCacheEntry)
	providerByNormalNameCache = make(map[string]providerCacheEntry)
	providerCacheMu           sync.RWMutex
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
	providerByNormalNameCache[NormalizeName(p.Name)] = entry
	providerCacheMu.Unlock()
}

// GetCachedByID returns a cached provider by ID if not expired.
func GetCachedByID(id uuid.UUID) (*Provider, bool) {
	providerCacheMu.RLock()
	entry, ok := providerByIDCache[id]
	providerCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.provider, true
}

// GetCachedByName returns a cached provider by name (exact or normalized) if not expired.
func GetCachedByName(name string) (*Provider, bool) {
	providerCacheMu.RLock()
	entry, ok := providerByNameCache[name]
	if !ok {
		entry, ok = providerByNormalNameCache[name]
	}
	providerCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.provider, true
}

// IsCachedByID reports whether a provider for the given ID is present in the
// cache and not expired. It does not modify the cache.
func IsCachedByID(id uuid.UUID) bool {
	providerCacheMu.RLock()
	entry, ok := providerByIDCache[id]
	providerCacheMu.RUnlock()
	return ok && !time.Now().After(entry.expiresAt)
}

// IsCachedByName reports whether a provider for the given name (exact or
// normalized) is present in the cache and not expired. It does not modify the
// cache.
func IsCachedByName(name string) bool {
	providerCacheMu.RLock()
	entry, ok := providerByNameCache[name]
	if !ok {
		entry, ok = providerByNormalNameCache[name]
	}
	providerCacheMu.RUnlock()
	return ok && !time.Now().After(entry.expiresAt)
}

// InvalidateProviderCache clears all provider cache entries.
func InvalidateProviderCache() {
	providerCacheMu.Lock()
	providerByIDCache = make(map[uuid.UUID]providerCacheEntry)
	providerByNameCache = make(map[string]providerCacheEntry)
	providerByNormalNameCache = make(map[string]providerCacheEntry)
	providerCacheMu.Unlock()
}

// WarmProviderCache populates the provider cache with the given providers.
func WarmProviderCache(providers []*Provider) {
	for _, p := range providers {
		cacheProvider(p)
	}
	debuglog.Info("provider: warmed cache", "providers", len(providers))
}
