package failover

import (
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Quota pinning: the one way anything other than an HTTP response chooses how
// long an open circuit stays dark. It never opens a circuit, never closes one,
// never blocks a request; it only lengthens (and, on affirmative recovery,
// shortens back) the cooldown of a circuit that is already open. The open
// transition stamps a pin (applyQuotaPin), the quota poller retargets and
// releases them (ApplyQuotaPins, ReleaseQuotaPins, ReleaseAllQuotaPins), and
// every read goes through quotaPinnedForWith in model_circuits.go.

// applyQuotaPin sets c.cooldownOverride when the provider's quota window is
// spent and resets further out than the cooldown already in force. Must be
// called with cb.mu held, immediately after c transitions to Open and after
// applyBackoff, because the floor is the backoff when one is stamped: pinning
// must never make the breaker more aggressive, and a backed-off circuit is one
// the breaker has already decided to leave alone for longer.
//
// Clamp order is ceiling, then floor, then jitter, the same order ApplyQuotaPins
// uses and for the same reason: a pin has to be compared against the floor
// AFTER the ceiling has been applied, or a ceiling below the cooldown in force
// (a quota_pin_max of ten minutes against a backed-off half hour) would stamp a
// pin that shortens the wait the floor exists to protect. Jitter is positive
// only: a negative offset would probe before the window actually resets, which
// is a guaranteed 429 and precisely the waste this exists to avoid. The ceiling
// is applied before jitter, so quotaPinMax() is a pre-jitter cap, not a hard
// one — e.g. the default 24h ceiling can yield up to ~25.2h once jitter is
// added. Jittering before capping would let two providers pinned at the
// ceiling collide on the same retry instant, which is exactly the fleet
// stampede jitter exists to prevent.
func (cb *CircuitBreaker) applyQuotaPin(providerID uuid.UUID, c *circuit) {
	c.cooldownOverride = 0
	if cb.quota == nil || !cb.quotaPinEnabled() {
		return
	}
	resetsAt, ok := cb.quota.ResetsAt(providerID)
	if !ok {
		return
	}
	d := time.Until(resetsAt)
	if maxPin := cb.quotaPinMax(); d > maxPin {
		d = maxPin
	}
	if d <= cb.unpinnedCooldownWith(c, cb.cooldowns()) {
		return // floor: pinning must never make the breaker more aggressive
	}
	if spread := int64(d / 20); spread > 0 {
		d += time.Duration(rand.Int64N(spread + 1))
	}
	c.cooldownOverride = d
}

// ReleaseQuotaPins lifts the quota cooldown override from every circuit whose
// provider appears in recovered, and reports how many pins it lifted. It is how
// a provider that has recovered (a topped-up plan, a reset window observed early
// by the quota poller) stops serving out a pin that was stamped on when its
// circuit opened and could otherwise run to the 24h ceiling.
//
// It only ever shortens a wait. The circuit keeps its state and its failure
// count and simply reverts to the cooldown it would otherwise serve (its probe
// backoff if one is in force, else the configured cooldown), so HTTP still
// decides recovery through the ordinary half-open probe. That is the whole
// quota contract: quota never opens a circuit, never closes one, never blocks a
// request, and only chooses the cooldown of an already-open circuit.
//
// recovered must carry *affirmative* evidence: providers a successful refresh
// assessed from a fresh snapshot and found not exhausted. Absence is not
// evidence. A provider is equally absent when its snapshot went stale, when its
// payload could not be assessed, and when it has no snapshot at all — and those
// are precisely the cases where quota fetching is broken and the window is most
// likely still spent. Releasing on absence would therefore unpin exactly the
// provider the pin exists to protect, so anything not affirmatively recovered
// is left untouched.
func (cb *CircuitBreaker) ReleaseQuotaPins(recovered map[uuid.UUID]struct{}) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// One walk's reads for the loop: this runs on the quota poll goroutine every
	// few minutes and the values are identical for every circuit.
	r := cb.cooldowns()

	// Walk the recovered set rather than every circuit: it is the smaller side
	// (a fleet has few providers recovering per pass), and the circuits map is
	// keyed by the provider's UUID string, so one conversion per candidate
	// replaces parsing every key.
	released := 0
	for providerID := range recovered {
		id := providerID.String()
		for model, c := range cb.circuits[id] {
			if c.cooldownOverride == 0 {
				continue
			}
			cb.releasePin("circuit-breaker: quota pin released (provider no longer exhausted)", id, model, c, r)
			released++
		}
	}
	return released
}

// ApplyQuotaPins retargets the cooldown of every already-open circuit whose
// provider is now known to be exhausted, and reports how many it retargeted. It
// is the counterpart to applyQuotaPin, which stamps a pin at the instant a
// circuit opens and therefore only ever sees the advice that existed by then. A
// reading that lands moments later (the poll a breaker open triggers) has to
// reach the circuit that prompted it, or that circuit serves out an ordinary
// cooldown and probes into a certain 429 before the pin finally applies on the
// re-open.
//
// It only ever lengthens a wait, and only for circuits open right now:
//
//   - A closed circuit is serving traffic and has no cooldown to retarget.
//   - A half-open circuit has a probe out or due, so HTTP is mid-verdict.
//     logicalState decides that, which means an open circuit whose cooldown has
//     already elapsed counts as half-open here too: it is owed a probe, and
//     pushing it back into the dark would overturn a decision the breaker has
//     already handed to the request path.
//   - A pin already reaching further than the advice stands. Releasing needs
//     affirmative proof the provider recovered, which is ReleaseQuotaPins' job;
//     a nearer deadline arriving here is not that proof.
//   - A probe backoff already reaching further than the advice stands too, for
//     the reason applyQuotaPin gives: the floor is the cooldown in force.
//
// advice is read, never retained: the caller may hand the same map to the
// advisor afterwards.
func (cb *CircuitBreaker) ApplyQuotaPins(advice map[uuid.UUID]time.Time) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if len(advice) == 0 || !cb.quotaPinEnabled() {
		return 0
	}
	r := cb.cooldowns()
	// Hoisted with the walk's reads: it is a settings read too, and a cold
	// settings cache turns that into a DB round trip under cb.mu held for write.
	maxPin := cb.quotaPinMax()

	// Walk the advice rather than every circuit, for the same reason
	// ReleaseQuotaPins walks the recovered set: it is the smaller side, and the
	// circuits map is keyed by the provider's UUID string.
	retargeted := 0
	for providerID, resetsAt := range advice {
		for model, c := range cb.circuits[providerID.String()] {
			if cb.logicalStateWith(c, r) != StateOpen {
				continue
			}
			// Measured from openedAt, because that is what the enforced cooldown is
			// measured from. applyQuotaPin computes this from time.Until(resetsAt)
			// instead, which is the same number at the one instant it runs; here
			// openedAt is already in the past, and a pin derived from "time until
			// reset" would expire that much too early and probe before the window
			// rolls over.
			d := resetsAt.Sub(c.openedAt)
			// Ceiling first, so a clamped value is compared against the floor
			// rather than smuggled past it: capping after the check could shorten
			// a wait that is already longer.
			if d > maxPin {
				d = maxPin
			}
			// One comparison covers every floor: the cooldown in force is never
			// less than the configured one, is the backoff when one governs, and is
			// the pin already stamped when that reaches further still.
			if d <= cb.effectiveCooldownForWith(c, r) {
				continue
			}
			if spread := int64(d / 20); spread > 0 {
				d += time.Duration(rand.Int64N(spread + 1))
			}
			c.cooldownOverride = d
			retargeted++
			// The open transition already logged a cooldown_ms that is now wrong,
			// and the corrected one can mean hours of darkness, so an operator gets
			// the same Info-level line a release gets. Routing metadata only, never
			// payload or credentials.
			debuglog.Info("circuit-breaker: quota pin retargeted (fresh exhaustion reading)", "provider_id", providerID, "cooldown_ms", d.Milliseconds(), "model", model)
		}
	}
	return retargeted
}

// ReleaseAllQuotaPins lifts the quota cooldown override from every circuit that
// carries one, and reports how many it lifted.
//
// This is the other half of the release rule, and the reason it can be this
// blunt where ReleaseQuotaPins must not be: it is called when quota polling has
// been switched off. No refresh will ever report a recovery again, so every pin
// still in force would be served out to its ceiling — up to 24 hours — on
// evidence the operator deliberately stopped collecting. Absence of evidence
// keeps a pin only while the gateway is still looking; once it stops looking it
// stops holding, because benching a healthy provider is the expensive mistake
// and an unnecessary probe is the cheap one.
//
// Like ReleaseQuotaPins it only shortens a wait: circuit state and failure
// counts are untouched, and HTTP still decides recovery through the ordinary
// half-open probe. It is idempotent, so a caller can run it once per disabled
// span without bookkeeping.
func (cb *CircuitBreaker) ReleaseAllQuotaPins() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	r := cb.cooldowns()

	released := 0
	for id, models := range cb.circuits {
		for model, c := range models {
			if c.cooldownOverride == 0 {
				continue
			}
			cb.releasePin("circuit-breaker: quota pin released (quota polling disabled)", id, model, c, r)
			released++
		}
	}
	return released
}

// releasePin drops one circuit's quota override and logs it. The message names
// the reason rather than being assembled from parts: an operator reading "pin
// released" needs to know whether the provider recovered or whether the poller
// was switched off, because only one of those means the window is actually back.
//
// The open transition logged a cooldown_ms that may have promised hours of
// darkness, so the line that says it ended early is logged at the same Info
// level the half-open→closed recovery uses. cooldown_ms is the wait the circuit
// falls back to, which is its backoff when one is in force: a recovered quota
// does not undo the probes that failed. Routing metadata only — never payload
// or credentials. Must be called with cb.mu held.
func (cb *CircuitBreaker) releasePin(msg, providerID, model string, c *circuit, r *cooldownReads) {
	c.cooldownOverride = 0
	debuglog.Info(msg, "provider_id", providerID, "state", cb.logicalStateWith(c, r).String(), "cooldown_ms", cb.unpinnedCooldownWith(c, r).Milliseconds(), "model", model)
}
