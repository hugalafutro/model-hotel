package api

import (
	"sync"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// QuotaAdvisor holds the latest per-provider quota advice (reset deadline and
// whether the operator's reserve drew the line) in memory so the circuit
// breaker can consult it under its write lock without touching the database.
// Refreshed by the quota poller; satisfies failover.QuotaAdvisor.
type QuotaAdvisor struct {
	mu     sync.RWMutex
	advice map[uuid.UUID]failover.QuotaAdvice
}

// NewQuotaAdvisor returns an empty advisor. Until the first refresh it declines
// every lookup, so the breaker uses its configured cooldown.
func NewQuotaAdvisor() *QuotaAdvisor {
	return &QuotaAdvisor{advice: make(map[uuid.UUID]failover.QuotaAdvice)}
}

// Advice reports the verdict for a provider whose quota is spent or past its
// reserve.
func (a *QuotaAdvisor) Advice(providerID uuid.UUID) (failover.QuotaAdvice, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	adv, ok := a.advice[providerID]
	return adv, ok
}

// Replace swaps the whole map so providers that recovered stop being advised.
// Replace takes ownership of m: the caller must not read or mutate it after
// the call, since Advice may be reading it concurrently under RLock with no
// synchronization against a caller-side write.
func (a *QuotaAdvisor) Replace(m map[uuid.UUID]failover.QuotaAdvice) {
	if m == nil {
		m = make(map[uuid.UUID]failover.QuotaAdvice)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.advice = m
}
