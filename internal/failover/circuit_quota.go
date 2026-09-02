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
// applyBackoff, because the floor is the backoff when one is stamped.
//
// Clamp order is ceiling, then floor, then jitter, the same order ApplyQuotaPins
// uses: the pin is compared against the floor AFTER the ceiling, or a ceiling
// below the cooldown in force (a quota_pin_max of ten minutes against a
// backed-off half hour) would stamp a pin that shortens the wait. Jitter is
// positive only, since a negative offset probes before the window resets and
// draws a certain 429. Applying the ceiling before jitter makes quotaPinMax() a
// pre-jitter cap (the default 24h can yield ~25.2h) and keeps two providers
// pinned at the ceiling from colliding on one retry instant.
//
// exhaustHint is a second pin source: the exhausted 429's own claim (a dated
// Retry-After, or the matched phrase's per-marker default), used only when the
// advisor has no reading; the advisor measured the actual window, so it wins. The hint
// goes through the same ceiling, floor and jitter, and the release paths lift it
// as they lift an advisor pin. pinSource records which source stamped the pin in
// force.
func (cb *CircuitBreaker) applyQuotaPin(providerID uuid.UUID, c *circuit, exhaustHint time.Duration) {
	c.cooldownOverride = 0
	c.pinSource = ""
	if !cb.quotaPinEnabled() {
		return
	}
	d := exhaustHint
	source := pinSourceResponse
	if cb.quota != nil {
		if resetsAt, ok := cb.quota.ResetsAt(providerID); ok {
			d = time.Until(resetsAt)
			source = pinSourceAdvisor
		}
	}
	if d <= 0 {
		return
	}
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
	c.pinSource = source
}

// The two pin sources ProviderStatus and the breaker events publish.
const (
	pinSourceAdvisor  = "advisor"
	pinSourceResponse = "response"
)

// ReleaseQuotaPins lifts the quota cooldown override from every circuit whose
// provider appears in recovered, and reports how many pins it lifted. It is how
// a provider that has recovered (a topped-up plan, a reset window observed early
// by the quota poller) stops serving out a pin that could otherwise run to the
// 24h ceiling.
//
// It only ever shortens a wait. The circuit keeps its state and its failure
// count and reverts to the cooldown it would otherwise serve (its probe backoff
// if one is in force, else the configured cooldown), so HTTP still decides
// recovery through the ordinary half-open probe.
//
// recovered must carry *affirmative* evidence: providers a successful refresh
// assessed from a fresh snapshot and found not exhausted. Absence is not
// evidence: a provider is equally absent when its snapshot went stale, when its
// payload could not be assessed, and when it has none at all, which are the
// cases where the window is most likely still spent. Anything not affirmatively
// recovered is left untouched.
func (cb *CircuitBreaker) ReleaseQuotaPins(recovered map[uuid.UUID]struct{}) int {
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	// One walk's settings reads for the loop: the values are identical for every
	// circuit.
	r := cb.cooldowns()

	// Walk the recovered set rather than every circuit: it is the smaller side,
	// and the circuits map is keyed by the provider's UUID string, so one
	// conversion per candidate replaces parsing every key.
	released := 0
	for providerID := range recovered {
		id := providerID.String()
		for model, c := range cb.circuits[id] {
			// An affirmative "not exhausted" reading also clears the 429-open
			// escalation: it inferred exhaustion from behaviour, and the advisor
			// measured the opposite. Cleared on every circuit, pinned or not, because
			// escalated ones usually carry a backoff rather than a pin.
			c.clear429Escalation()
			if c.cooldownOverride == 0 {
				continue
			}
			cb.releasePin(&after, causePinReleasedQuota, id, model, c, r)
			released++
		}
	}
	return released
}

// ApplyQuotaPins retargets the cooldown of every already-open circuit whose
// provider is known to be exhausted, and reports how many it retargeted. It is
// the counterpart to applyQuotaPin, which stamps a pin at the instant a circuit
// opens and so only sees the advice existing by then. A reading that lands
// moments later (the poll a breaker open triggers) has to reach the circuit that
// prompted it, or that circuit probes into a certain 429 first.
//
// It only ever lengthens a wait, and only for circuits open right now:
//
//   - A closed circuit is serving traffic and has no cooldown to retarget.
//   - A half-open circuit has a probe out or due, so HTTP is mid-verdict.
//     logicalState decides that, so an open circuit whose cooldown has elapsed
//     counts as half-open here too: it is owed a probe, and pushing it back into
//     the dark would overturn a decision already handed to the request path.
//   - A pin already reaching further than the advice stands. Releasing needs
//     affirmative proof the provider recovered, which is ReleaseQuotaPins' job.
//   - A probe backoff already reaching further than the advice stands too: the
//     floor is the cooldown in force.
//
// advice is read, never retained: the caller may hand the same map to the
// advisor afterwards.
func (cb *CircuitBreaker) ApplyQuotaPins(advice map[uuid.UUID]time.Time) int {
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	if len(advice) == 0 || !cb.quotaPinEnabled() {
		return 0
	}
	r := cb.cooldowns()
	// Hoisted with the walk's reads: it is a settings read too, and a cold cache
	// turns that into a DB round trip under cb.mu held for write.
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
			// measured from. openedAt is in the past here, so a pin derived from
			// time until reset would expire that much too early and probe before
			// the window rolls over.
			d := resetsAt.Sub(c.openedAt)
			// Ceiling first, so a clamped value is compared against the floor:
			// capping after the check could shorten a longer wait.
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
			c.pinSource = pinSourceAdvisor
			// A transition with no request behind it: the wait changed, and the
			// status the open recorded stays, so the row says both what opened the
			// circuit and what holds it.
			c.note(time.Now(), Cause{Status: c.lastStatus, Reason: causePinRetargeted})
			retargeted++
			// The open transition logged a cooldown_ms this supersedes, and the
			// corrected one can mean hours of darkness, so it gets the same
			// Info-level line a release gets. Routing metadata only, never payload
			// or credentials.
			ms := d.Milliseconds()
			after.add(func() {
				debuglog.Info("circuit-breaker: quota pin retargeted (fresh exhaustion reading)", "provider_id", providerID, "cooldown_ms", ms, "model", model)
			})
		}
	}
	return retargeted
}

// ReleaseAllQuotaPins lifts the quota cooldown override from every circuit that
// carries one, and reports how many it lifted.
//
// It can be this blunt where ReleaseQuotaPins must not be because it is called
// when quota polling is switched off: no refresh will report a recovery again,
// so every pin in force would be served out to its ceiling (up to 24 hours) on
// evidence nobody is collecting.
//
// Like ReleaseQuotaPins it only shortens a wait: circuit state and failure
// counts are untouched, and HTTP still decides recovery through the ordinary
// half-open probe. It is idempotent.
func (cb *CircuitBreaker) ReleaseAllQuotaPins() int {
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	r := cb.cooldowns()

	released := 0
	for id, models := range cb.circuits {
		for model, c := range models {
			if c.cooldownOverride == 0 {
				continue
			}
			cb.releasePin(&after, causePinReleasedOff, id, model, c, r)
			released++
		}
	}
	return released
}

// releasePin drops one circuit's quota override, logs it and records it as the
// circuit's last verdict. The reason is a named phrase, so an operator can tell
// a recovered provider from a switched-off poller.
//
// The line is logged at the same Info level the half-open to closed recovery
// uses. cooldown_ms is the wait the circuit falls back to, which is its backoff
// when one is in force: a recovered quota does not undo the probes that failed.
// Routing metadata only, never payload or credentials. Must be called with cb.mu
// held; the line goes to after, for the caller to write once the lock is
// released.
func (cb *CircuitBreaker) releasePin(after *afterUnlock, reason, providerID, model string, c *circuit, r *cooldownReads) {
	c.cooldownOverride = 0
	c.pinSource = ""
	c.note(time.Now(), Cause{Status: c.lastStatus, Reason: reason})
	state := cb.logicalStateWith(c, r).String()
	ms := cb.unpinnedCooldownWith(c, r).Milliseconds()
	after.add(func() {
		debuglog.Info("circuit-breaker: "+reason, "provider_id", providerID, "state", state, "cooldown_ms", ms, "model", model)
	})
}
