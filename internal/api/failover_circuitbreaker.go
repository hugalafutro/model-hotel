package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
)

// CircuitBreakerReader provides read-only access to circuit breaker status.
type CircuitBreakerReader interface {
	Status() []failover.ProviderStatus
	// StatusDetail is Status with every row's circuits[] filled in; only the
	// detail response pays for it.
	StatusDetail() []failover.ProviderStatus
}

// CircuitBreakerResetter clears breaker state so an operator can force a
// sidelined provider back into rotation early. Kept separate from
// CircuitBreakerReader so a status-only consumer never acquires the ability to
// mutate breaker state just by depending on the read contract.
type CircuitBreakerResetter interface {
	// Reset clears one provider's circuit and returns the state it was in
	// beforehand (closed for an untracked provider, which is a no-op).
	Reset(providerID uuid.UUID) failover.State
	// ResetAll clears every circuit, returning how many were discarded and how
	// many of those were actually sidelining a provider.
	ResetAll() (cleared, recovered int)
}

// CircuitBreakerQuotaPinner lets a successful quota refresh lift the cooldown
// pins of providers that are no longer exhausted. Separate from the reset
// contract because it is a different power: it shortens a wait, it never clears
// a circuit. Keeping it its own interface is also what keeps internal/failover
// free of any dependency on internal/quota — the set of recovered providers
// crosses the boundary as plain UUIDs.
type CircuitBreakerQuotaPinner interface {
	// ReleaseQuotaPins clears the quota cooldown override on every tracked
	// circuit whose provider appears in recovered, returning how many pins it
	// lifted. It must not change any circuit's state. recovered carries only
	// providers a fresh snapshot was assessed for and found not exhausted;
	// anything absent (stale, unassessable, or never snapshotted) keeps its pin.
	ReleaseQuotaPins(recovered map[uuid.UUID]struct{}) int
	// ReleaseAllQuotaPins clears the override on every pinned circuit, for the
	// one case where absence of evidence is decisive: quota polling has been
	// switched off, so no refresh will ever report a recovery again. It must
	// not change any circuit's state either.
	ReleaseAllQuotaPins() int
	// ApplyQuotaPins retargets the cooldown of every already-open circuit whose
	// provider appears in advice, returning how many it retargeted. It only
	// lengthens a wait: it must not change any circuit's state, must leave
	// closed and half-open circuits alone, and must never shorten a pin already
	// reaching further than the advice. Implementations may read advice only for
	// the duration of the call.
	ApplyQuotaPins(advice map[uuid.UUID]time.Time) int
}

// CircuitBreakerControl is the whole breaker surface the failover API needs.
// Composed from the narrow interfaces above so internal/api still depends
// on behaviour it names rather than on *failover.CircuitBreaker.
type CircuitBreakerControl interface {
	CircuitBreakerReader
	CircuitBreakerResetter
	CircuitBreakerQuotaPinner
}

// CircuitBreakerStatusResponse contains counts of providers in each circuit breaker state.
type CircuitBreakerStatusResponse struct {
	Closed    int                       `json:"closed"`
	HalfOpen  int                       `json:"half_open"`
	Open      int                       `json:"open"`
	Providers []failover.ProviderStatus `json:"providers,omitempty"`
}

// cbStatusCacheTTL is how long the aggregate circuit-breaker status cache
// is valid before re-computing. This avoids scanning all failover groups
// on every 15s poll from each connected client.
const cbStatusCacheTTL = 5 * time.Second

// CircuitBreakerStatus returns the current circuit breaker state for all tracked providers.
func (h *FailoverHandler) CircuitBreakerStatus(w http.ResponseWriter, r *http.Request) {
	wantDetail := r.URL.Query().Get("detail") == "1"

	h.cbStatusMu.Lock()
	if wantDetail {
		if time.Since(h.cbDetailCacheTime) < cbStatusCacheTTL {
			cached := h.cbDetailCache
			h.cbStatusMu.Unlock()
			writeJSON(w, cached)
			return
		}
	} else {
		if time.Since(h.cbStatusCacheTime) < cbStatusCacheTTL {
			cached := h.cbStatusCache
			h.cbStatusMu.Unlock()
			writeJSON(w, cached)
			return
		}
	}
	h.cbStatusMu.Unlock()

	resp := CircuitBreakerStatusResponse{}
	trackedProviders := make([]failover.ProviderStatus, 0)
	if h.cb != nil {
		if wantDetail {
			trackedProviders = h.cb.StatusDetail()
		} else {
			trackedProviders = h.cb.Status()
		}
		for _, s := range trackedProviders {
			switch s.State {
			case failover.StateClosed.String():
				resp.Closed++
			case failover.StateHalfOpen.String():
				resp.HalfOpen++
			case failover.StateOpen.String():
				resp.Open++
			}
		}
	}

	// Count failover group members not yet tracked by the circuit breaker as closed.
	// Providers only appear in the CB map after being routed; until then they're
	// implicitly healthy (closed).
	//
	// Note: there is an inherent race between reading cbReader.Status() above and
	// failoverRepo.List() below. A provider that transitions from untracked to
	// tracked (e.g., after a route that triggers its first CB circuit creation)
	// could be counted in both passes, slightly inflating totals. This is acceptable
	// for an aggregate dashboard endpoint with a 5s cache TTL — the next poll
	// will correct any transient overcount.
	var providerNameMap map[string]string // provider UUID -> name (for detail responses)
	if h.failoverRepo != nil {
		groups, err := h.failoverRepo.List(r.Context())
		if err == nil {
			tracked := make(map[string]struct{}, len(trackedProviders))
			for _, p := range trackedProviders {
				tracked[p.ProviderID] = struct{}{}
			}

			// Collect all model UUIDs across all groups, then resolve to
			// provider UUIDs so we can compare against the tracked map
			// (which is keyed by provider UUID, not model UUID).
			var allModelIDs []uuid.UUID
			seenModel := make(map[string]struct{})
			for _, g := range groups {
				for _, mid := range g.PriorityOrder {
					key := mid.String()
					if _, ok := seenModel[key]; ok {
						continue
					}
					seenModel[key] = struct{}{}
					allModelIDs = append(allModelIDs, mid)
				}
			}

			if len(allModelIDs) > 0 {
				models, err := h.modelRepo.GetByIDs(r.Context(), allModelIDs)
				if err == nil {
					seenProvider := make(map[string]struct{})
					for _, mid := range allModelIDs {
						m, ok := models[mid]
						if !ok {
							continue
						}
						providerID := m.ProviderID.String()
						if _, ok := seenProvider[providerID]; ok {
							continue
						}
						seenProvider[providerID] = struct{}{}
						if _, ok := tracked[providerID]; !ok {
							resp.Closed++
						}
					}

					// Build provider name map for detail responses.
					if wantDetail {
						providerNameMap = make(map[string]string, len(models))
						for _, m := range models {
							pid := m.ProviderID.String()
							if _, exists := providerNameMap[pid]; !exists {
								providerNameMap[pid] = m.ProviderName
							}
						}
					}
				}
			}
		}
	}

	// Include per-provider detail when requested (for the Failover page UI).
	if wantDetail {
		resp.Providers = trackedProviders

		// Populate provider names from the name map built during untracked counting.
		if providerNameMap != nil {
			for i := range resp.Providers {
				if name, ok := providerNameMap[resp.Providers[i].ProviderID]; ok {
					resp.Providers[i].ProviderName = name
				}
			}
		}
	}

	// Cache the response (after providers are appended for detail requests).
	h.cbStatusMu.Lock()
	if wantDetail {
		h.cbDetailCache = resp
		h.cbDetailCacheTime = time.Now()
	} else {
		h.cbStatusCache = resp
		h.cbStatusCacheTime = time.Now()
	}
	h.cbStatusMu.Unlock()

	writeJSON(w, resp)
}

// CircuitBreakerResetResponse reports the outcome of resetting one provider's
// circuit. PreviousState is what the breaker reported for that provider a
// moment before it was cleared; Reset is false when there was nothing to clear
// (an already-closed or never-tracked provider), so the UI can say "no change"
// instead of claiming a recovery that did not happen.
type CircuitBreakerResetResponse struct {
	ProviderID    string `json:"provider_id"`
	PreviousState string `json:"previous_state"`
	Reset         bool   `json:"reset"`
}

// CircuitBreakerResetAllResponse reports the outcome of a bulk reset: Cleared
// counts every model circuit discarded, Recovered only those that were
// sidelining the model they belong to. Both are circuits, not providers, so a
// provider with five charged models contributes five.
type CircuitBreakerResetAllResponse struct {
	Cleared   int `json:"cleared"`
	Recovered int `json:"recovered"`
}

// invalidateCBStatusCache drops both cached circuit-breaker status slots. A
// reset must be visible on the very next poll: without this the dashboard
// refetches immediately after the mutation and is served the pre-reset snapshot
// for up to cbStatusCacheTTL, which reads as "the reset did nothing".
func (h *FailoverHandler) invalidateCBStatusCache() {
	h.cbStatusMu.Lock()
	defer h.cbStatusMu.Unlock()
	h.cbStatusCacheTime = time.Time{}
	h.cbDetailCacheTime = time.Time{}
}

// ResetCircuitBreaker clears one provider's circuit, returning it to rotation
// immediately instead of waiting out the cooldown. Resetting an untracked or
// already-closed provider is a successful no-op (reset=false), not an error:
// the breaker only tracks providers it has routed, so "no circuit" and "closed
// circuit" are the same healthy state.
func (h *FailoverHandler) ResetCircuitBreaker(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "provider_id", "provider ID")
	if !ok {
		return
	}
	if h.cb == nil {
		respondError(w, "circuit breaker is not available", nil, http.StatusServiceUnavailable)
		return
	}

	previous := h.cb.Reset(providerID)
	h.invalidateCBStatusCache()

	debuglog.Info("circuit-breaker: manual reset", "provider_id", providerID, "previous_state", previous.String())

	writeJSON(w, CircuitBreakerResetResponse{
		ProviderID:    providerID.String(),
		PreviousState: previous.String(),
		Reset:         previous != failover.StateClosed,
	})
}

// ResetAllCircuitBreakers clears every tracked circuit at once, for recovering
// a whole fleet-wide upstream incident without resetting providers one by one.
func (h *FailoverHandler) ResetAllCircuitBreakers(w http.ResponseWriter, _ *http.Request) {
	if h.cb == nil {
		respondError(w, "circuit breaker is not available", nil, http.StatusServiceUnavailable)
		return
	}

	cleared, recovered := h.cb.ResetAll()
	h.invalidateCBStatusCache()

	debuglog.Info("circuit-breaker: manual reset of all circuits", "cleared", cleared, "recovered", recovered)

	writeJSON(w, CircuitBreakerResetAllResponse{Cleared: cleared, Recovered: recovered})
}
