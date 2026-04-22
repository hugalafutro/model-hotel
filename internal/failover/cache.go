package failover

import (
	"sync"
	"time"
)

type failoverCacheEntry struct {
	group     *FailoverGroup
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
	entry := failoverCacheEntry{group: fg, expiresAt: time.Now().Add(failoverCacheTTL)}
	failoverCacheMu.Lock()
	failoverByModelCache[fg.DisplayModel] = entry
	failoverCacheMu.Unlock()
}

func GetCachedFailoverByModel(displayModel string) (*FailoverGroup, bool) {
	failoverCacheMu.RLock()
	entry, ok := failoverByModelCache[displayModel]
	failoverCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.group, true
}

func InvalidateFailoverCache() {
	failoverCacheMu.Lock()
	failoverByModelCache = make(map[string]failoverCacheEntry)
	failoverCacheMu.Unlock()
}

func WarmFailoverCache(groups []*FailoverGroup) {
	for _, fg := range groups {
		cacheFailoverGroup(fg)
	}
}