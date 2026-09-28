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
	// failoverFlushGen advances on every full flush and failoverEvictSeq on
	// every per-key invalidation, with failoverEvicted holding the sequence
	// at which each display model was last invalidated. A read-through
	// captures both before its SELECT (CacheGen) and installs only if no
	// flush has landed since and its own key was not invalidated since, so a
	// read that overlapped a write cannot reinstall the pre-write group for
	// the TTL. failoverEvicted is bounded by the group count and reset by a
	// flush, which supersedes every invalidation before it.
	failoverFlushGen uint64
	failoverEvictSeq uint64
	failoverEvicted  = make(map[string]uint64)
)

const failoverCacheTTL = 5 * time.Minute

// CacheMark is what a read-through captures before its query: the flush
// generation and the per-key invalidation sequence at that moment.
type CacheMark struct {
	flush uint64
	evict uint64
}

// CacheGen is the mark a read-through captures before querying; see
// failoverFlushGen.
func CacheGen() CacheMark {
	failoverCacheMu.RLock()
	defer failoverCacheMu.RUnlock()
	return CacheMark{flush: failoverFlushGen, evict: failoverEvictSeq}
}

// cacheFailoverGroup installs at the current mark; cacheFailoverGroupAt takes
// the mark a read-through captured before its query and installs nothing when
// an invalidation has landed since. The caller still gets the group it read,
// the next reader refills.
func cacheFailoverGroup(fg *FailoverGroup) {
	cacheFailoverGroupAt(fg, CacheGen())
}

func cacheFailoverGroupAt(fg *FailoverGroup, mark CacheMark) {
	if fg == nil {
		return
	}
	entry := failoverCacheEntry{group: *fg, expiresAt: time.Now().Add(failoverCacheTTL)}
	failoverCacheMu.Lock()
	defer failoverCacheMu.Unlock()
	if failoverFlushGen != mark.flush || failoverEvicted[fg.DisplayModel] > mark.evict {
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
	failoverEvictSeq++
	failoverEvicted[displayModel] = failoverEvictSeq
	delete(failoverByModelCache, displayModel)
	failoverCacheMu.Unlock()
}

// InvalidateFailoverCache clears all cached failover groups.
func InvalidateFailoverCache() {
	failoverCacheMu.Lock()
	failoverFlushGen++
	failoverEvicted = make(map[string]uint64)
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

// WarmFailoverCache populates the cache with the provided failover groups.
func WarmFailoverCache(groups []*FailoverGroup) {
	for _, fg := range groups {
		cacheFailoverGroup(fg)
	}
}
