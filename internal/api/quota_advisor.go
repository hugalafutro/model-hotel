package api

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// QuotaAdvisor holds the latest per-provider quota reset deadlines in memory so
// the circuit breaker can consult them under its write lock without touching
// the database. Refreshed by the quota poller; satisfies failover.QuotaAdvisor.
type QuotaAdvisor struct {
	mu     sync.RWMutex
	resets map[uuid.UUID]time.Time
}

// NewQuotaAdvisor returns an empty advisor. Until the first refresh it declines
// every lookup, so the breaker uses its configured cooldown.
func NewQuotaAdvisor() *QuotaAdvisor {
	return &QuotaAdvisor{resets: make(map[uuid.UUID]time.Time)}
}

// ResetsAt reports the reset deadline for a provider whose quota is spent.
func (a *QuotaAdvisor) ResetsAt(providerID uuid.UUID) (time.Time, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t, ok := a.resets[providerID]
	return t, ok
}

// Replace swaps the whole map so providers that recovered stop being advised.
// Replace takes ownership of m: the caller must not read or mutate it after
// the call, since ResetsAt may be reading it concurrently under RLock with no
// synchronization against a caller-side write.
func (a *QuotaAdvisor) Replace(m map[uuid.UUID]time.Time) {
	if m == nil {
		m = make(map[uuid.UUID]time.Time)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resets = m
}
