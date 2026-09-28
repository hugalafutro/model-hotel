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
	// providerFlushGen advances on every full flush and providerEvictSeq on
	// every per-id eviction, with providerEvicted holding the sequence at
	// which each id was last evicted. A read-through fill captures both
	// counters before its SELECT (CacheGen) and installs a row only if no
	// flush has landed since and the row's own id was not evicted since, so
	// a read that overlapped a write (a key rotation, a disable) can never
	// reinstall the pre-write row for the TTL. The eviction side is per id
	// because TouchLastUsed evicts on every proxied attempt: one global
	// counter would discard every unrelated fill in flight under load.
	// providerEvicted is bounded by the provider count and is reset by a
	// flush, which supersedes every eviction before it.
	providerFlushGen uint64
	providerEvictSeq uint64
	providerEvicted  = make(map[uuid.UUID]uint64)
)

// CacheMark is what a read-through captures before its query: the flush
// generation and the eviction sequence at that moment.
type CacheMark struct {
	flush uint64
	evict uint64
}

// CacheGen is the generation a read-through fill captures before querying;
// see providerFlushGen.
func CacheGen() CacheMark {
	providerCacheMu.RLock()
	defer providerCacheMu.RUnlock()
	return CacheMark{flush: providerFlushGen, evict: providerEvictSeq}
}

const providerCacheTTL = 5 * time.Minute

// cacheProviderAt takes the mark a read-through captured before its query and
// installs nothing when an invalidation has landed since; the caller still
// gets the row it read, the next reader refills. Write paths install nothing:
// two concurrent writes can finish in reverse order and an install would hold
// the older row for the TTL.
func cacheProviderAt(p *Provider, gen CacheMark) {
	if p == nil {
		return
	}
	entry := providerCacheEntry{
		provider:  p,
		expiresAt: time.Now().Add(providerCacheTTL),
	}
	providerCacheMu.Lock()
	defer providerCacheMu.Unlock()
	if providerFlushGen != gen.flush || providerEvicted[p.ID] > gen.evict {
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
	providerEvictSeq++
	providerEvicted[id] = providerEvictSeq
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
	providerFlushGen++
	providerEvicted = make(map[uuid.UUID]uint64)
	providerByIDCache = make(map[uuid.UUID]providerCacheEntry)
	providerByNameCache = make(map[string]providerCacheEntry)
	providerByNormalNameCache = make(map[string]providerCacheEntry)
	providerCacheMu.Unlock()
}

// WarmProviderCacheAt installs rows read at a captured mark: nothing installs
// for a row invalidated since the capture.
func WarmProviderCacheAt(providers []*Provider, mark CacheMark) {
	for _, p := range providers {
		cacheProviderAt(p, mark)
	}
	debuglog.Info("provider: warmed cache", "providers", len(providers))
}
