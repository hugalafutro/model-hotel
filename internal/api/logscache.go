package api

import (
	"sync"
	"time"
)

type logsCacheEntry struct {
	response *LogsResponse
	expiry   time.Time
}

type logsCache struct {
	mu      sync.RWMutex
	entries map[string]*logsCacheEntry
	ttl     time.Duration
}

var globalLogsCache = &logsCache{
	entries: make(map[string]*logsCacheEntry),
	ttl:     2 * time.Second,
}

func (c *logsCache) get(key string) (*LogsResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiry) {
		return nil, false
	}
	return entry.response, true
}

func (c *logsCache) set(key string, response *LogsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Lazy eviction: remove expired entries when setting new ones.
	now := time.Now()
	for k, v := range c.entries {
		if now.After(v.expiry) {
			delete(c.entries, k)
		}
	}
	c.entries[key] = &logsCacheEntry{
		response: response,
		expiry:   now.Add(c.ttl),
	}
}
