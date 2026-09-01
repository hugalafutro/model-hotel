package failover

import (
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The manual resets: one circuit, one provider, or everything. Split out of
// circuitbreaker.go when that file reached the size ceiling. Every reset
// collects what it cleared under the lock and logs it after; see afterUnlock.

// Reset clears every model circuit of a specific provider and returns the
// logical state the provider's most degraded circuit was in immediately before
// being cleared, so an operator-facing caller can report whether the reset
// actually recovered a sidelined provider or was a no-op.
//
// An untracked provider reports StateClosed: a provider only enters the map
// once it has been routed, and until then it is implicitly healthy. Resetting
// one is therefore harmless and idempotent, not an error.
func (cb *CircuitBreaker) Reset(providerID uuid.UUID) State {
	prev, resets := cb.resetProvider(providerID)
	logManualResets(resets)
	return prev
}

func (cb *CircuitBreaker) resetProvider(providerID uuid.UUID) (State, []manualReset) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	r := cb.cooldowns()
	models := cb.circuits[providerID.String()]
	c := cb.dominant(models, r)
	if c == nil {
		return StateClosed, nil
	}
	prev := cb.logicalStateWith(c, r)
	resets := make([]manualReset, 0, len(models))
	for model, mc := range models {
		resets = append(resets, manualReset{providerID.String(), model, cb.logicalStateWith(mc, r)})
	}
	delete(cb.circuits, providerID.String())
	return prev, resets
}

// ResetModel clears ONE circuit, the (provider, resolved upstream model) pair,
// and leaves the provider's other circuits as they are. It returns the logical
// state the circuit was in and whether a circuit existed at all, so a caller
// can report "recovered", "was already closed" and "nothing tracked" apart. An
// untracked pair is a harmless no-op, like Reset on an untracked provider.
//
// This is the lever the 2026-08-31 reset loop lacked: Reset clears every model
// of the provider, which also forgets the charges its healthy siblings have
// legitimately accrued.
func (cb *CircuitBreaker) ResetModel(providerID uuid.UUID, model string) (prev State, existed bool) {
	prev, existed = cb.resetModel(providerID, model)
	if existed {
		logManualResets([]manualReset{{providerID.String(), model, prev}})
	}
	return prev, existed
}

func (cb *CircuitBreaker) resetModel(providerID uuid.UUID, model string) (State, bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	models := cb.circuits[providerID.String()]
	c, ok := models[model]
	if !ok {
		return StateClosed, false
	}
	prev := cb.logicalStateWith(c, cb.cooldowns())
	delete(models, model)
	if len(models) == 0 {
		delete(cb.circuits, providerID.String())
	}
	return prev, true
}

// manualReset is one circuit a manual reset cleared, remembered under the lock
// and logged after it: the app-log handler can block on a backed-up log store
// for seconds per line, and cb.mu is the lock every request's IsOpen takes.
type manualReset struct {
	providerID, model string
	prev              State
}

// logManualResets writes the one line every manual reset produces per circuit,
// with the same cause vocabulary the open line and the status API use, so a
// fleet-wide reset reads in the app log as N circuits with N causes rather than
// one summary a search for a model cannot find. Called with the lock released.
func logManualResets(resets []manualReset) {
	for _, r := range resets {
		debuglog.Info("circuit-breaker: manual reset", "provider_id", r.providerID, "cause", "manual reset", "previous_state", r.prev.String(), "model", r.model)
	}
}

// ResetAll clears all circuit breaker state. It returns how many model circuits
// were discarded in total and how many of those were actually being sidelined
// (logically open or half-open), so a bulk reset can report what it recovered
// instead of implying every tracked circuit was broken.
//
// Both counts are circuits, not providers: the map holds one entry per
// (provider, resolved upstream model), and a provider serving five models that
// have all been charged is five things the lever just threw away. The API hands
// these numbers to the operator verbatim.
func (cb *CircuitBreaker) ResetAll() (cleared, recovered int) {
	cleared, recovered, resets := cb.resetAll()
	logManualResets(resets)
	return cleared, recovered
}

func (cb *CircuitBreaker) resetAll() (cleared, recovered int, resets []manualReset) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Hoisted for the same reason Status hoists it: this walks every circuit in
	// the fleet under the write lock, and reading the cooldown per circuit would
	// take a DB round trip per circuit on a deployment that never overrode it.
	r := cb.cooldowns()

	for id, models := range cb.circuits {
		cleared += len(models)
		for model, c := range models {
			prev := cb.logicalStateWith(c, r)
			if prev != StateClosed {
				recovered++
			}
			resets = append(resets, manualReset{id, model, prev})
		}
	}
	cb.circuits = make(map[string]modelCircuits)
	return cleared, recovered, resets
}
