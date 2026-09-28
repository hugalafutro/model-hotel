package provider

import (
	"strings"
	"sync"
	"sync/atomic"
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
	// providerCacheGen advances on every invalidation, whole or per id. A
	// read-through fill captures it before its SELECT and installs only if it
	// is unchanged, so a read that overlapped a write (a key rotation, a
	// disable) can never reinstall the pre-write row for the TTL.
	providerCacheGen atomic.Uint64
)

// CacheGen is the generation a read-through fill captures before querying;
// see providerCacheGen.
func CacheGen() uint64 { return providerCacheGen.Load() }

const providerCacheTTL = 5 * time.Minute

// cacheProvider installs at the current generation: for a row this process
// just wrote, or one loaded outside a read-through. cacheProviderAt takes the
// generation a read-through captured before its query and installs nothing
// when an invalidation has landed since; the caller still gets the row it
// read, the next reader refills.
func cacheProvider(p *Provider) {
	cacheProviderAt(p, providerCacheGen.Load())
}

func cacheProviderAt(p *Provider, gen uint64) {
	if p == nil {
		return
	}
	entry := providerCacheEntry{
		provider:  p,
		expiresAt: time.Now().Add(providerCacheTTL),
	}
	providerCacheMu.Lock()
	defer providerCacheMu.Unlock()
	if providerCacheGen.Load() != gen {
		return
	}
	providerByIDCache[p.ID] = entry
	providerByNameCache[p.Name] = entry
	providerByNormalNameCache[NormalizeName(p.Name)] = entry
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

// EvictProviderCacheByID removes one provider's entries from all three key
// maps, leaving the rest of the cache intact. For single-row metadata writes
// (e.g. TouchLastUsed) this keeps read-through Get/GetByIDs honest without the
// cost of a full flush on a hot path.
func EvictProviderCacheByID(id uuid.UUID) {
	providerCacheMu.Lock()
	providerCacheGen.Add(1)
	if entry, ok := providerByIDCache[id]; ok {
		delete(providerByNameCache, entry.provider.Name)
		delete(providerByNormalNameCache, NormalizeName(entry.provider.Name))
	}
	delete(providerByIDCache, id)
	providerCacheMu.Unlock()
}

// InvalidateProviderCache clears all provider cache entries.
func InvalidateProviderCache() {
	providerCacheMu.Lock()
	providerCacheGen.Add(1)
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
