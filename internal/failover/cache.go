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
)

const failoverCacheTTL = 5 * time.Minute

func cacheFailoverGroup(fg *FailoverGroup) {
	if fg == nil {
		return
	}
	entry := failoverCacheEntry{group: *fg, expiresAt: time.Now().Add(failoverCacheTTL)}
	failoverCacheMu.Lock()
	failoverByModelCache[fg.DisplayModel] = entry
	failoverCacheMu.Unlock()
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
	delete(failoverByModelCache, displayModel)
	failoverCacheMu.Unlock()
}

// InvalidateFailoverCache clears all cached failover groups.
func InvalidateFailoverCache() {
	failoverCacheMu.Lock()
	failoverByModelCache = make(map[string]failoverCacheEntry)
	failoverCacheMu.Unlock()
}

// IsCachedByModel reports whether a failover group for the given display model
// is present in the cache and not expired. It does not modify the cache.
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
