// Package failover provides failover group management and caching.
package failover

import (
	"sync"
	"time"
)

type failoverCacheEntry struct {
	group     FailoverGroup
	expiresAt time.Time
}

var (
	failoverByModelCache = make(map[string]failoverCacheEntry)
	failoverCacheMu      sync.RWMutex
	// failoverCacheGen advances on every invalidation, per key or whole. A
	// read-through captures it before its SELECT (CacheGen) and installs only
	// if it is unchanged, so a read that overlapped a write cannot reinstall
	// the pre-write group for the TTL. One generation rather than per key:
	// every invalidation here is an admin edit, a revalidation or a sync, none
	// on the proxy's per-request path, so a dropped overlapping fill is rare
	// and costs one query, while a per-key map would hold every display model
	// ever renamed until the next flush.
	failoverCacheGen uint64
)

const failoverCacheTTL = 5 * time.Minute

// CacheGen is the generation a read-through captures before querying; see
// failoverCacheGen.
func CacheGen() uint64 {
	failoverCacheMu.RLock()
	defer failoverCacheMu.RUnlock()
	return failoverCacheGen
}

// cacheFailoverGroupAt takes the generation a read-through captured before
// its query and installs nothing when an invalidation has landed since. The
// caller still gets the group it read, the next reader refills. Nothing on a
// write path installs.
func cacheFailoverGroupAt(fg *FailoverGroup, gen uint64) {
	if fg == nil {
		return
	}
	entry := failoverCacheEntry{group: *fg, expiresAt: time.Now().Add(failoverCacheTTL)}
	failoverCacheMu.Lock()
	defer failoverCacheMu.Unlock()
	if failoverCacheGen != gen {
		return
	}
	failoverByModelCache[fg.DisplayModel] = entry
}

// GetCachedFailoverByModel returns a cached failover group by display model name.
func GetCachedFailoverByModel(displayModel string) (*FailoverGroup, bool) {
	failoverCacheMu.RLock()
	entry, ok := failoverByModelCache[displayModel]
	failoverCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	cachedGroup := entry.group
	return &cachedGroup, true
}

// InvalidateFailoverCacheKey removes a single display model key from the cache.
func InvalidateFailoverCacheKey(displayModel string) {
	failoverCacheMu.Lock()
	failoverCacheGen++
	delete(failoverByModelCache, displayModel)
	failoverCacheMu.Unlock()
}

// InvalidateFailoverCache clears all cached failover groups.
func InvalidateFailoverCache() {
	failoverCacheMu.Lock()
	failoverCacheGen++
	failoverByModelCache = make(map[string]failoverCacheEntry)
	failoverCacheMu.Unlock()
}

// IsCachedByModel reports whether a failover group for the given display model
// is present in the cache and not expired. It does not modify the cache. The
// probe is inline rather than a GetCachedFailoverByModel call so the per-request
// proxy resolve path does not copy a group it immediately discards.
func IsCachedByModel(displayModel string) bool {
	failoverCacheMu.RLock()
	entry, ok := failoverByModelCache[displayModel]
	failoverCacheMu.RUnlock()
	return ok && !time.Now().After(entry.expiresAt)
}

// WarmFailoverCacheAt installs rows read at a captured generation: nothing
// installs if an invalidation has landed since.
func WarmFailoverCacheAt(groups []*FailoverGroup, gen uint64) {
	for _, fg := range groups {
		cacheFailoverGroupAt(fg, gen)
	}
}
