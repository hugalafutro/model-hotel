package failover

import (
	"context"
	"slices"
	"time"
)

// maxModelCircuitsPerProvider bounds how many model circuits one provider may
// track. Breaker state is in-memory and every distinct upstream model id a
// request resolves to earns a circuit, so an unbounded map would grow with the
// provider's catalog (and with anything that looks like a model id to the
// upstream). The cap is far above any real catalog, so it only ever bites on
// churn that carries no routing signal.
const maxModelCircuitsPerProvider = 256

// defaultSpanModels is how many distinct model circuits must be open before the
// provider itself counts as down. Two is the smallest number that requires
// corroboration: one model refusing says something about that model, two models
// refusing says something about the provider.
const defaultSpanModels = 2

// circuit is the health state of one (provider, resolved upstream model) pair.
// It is the only thing the breaker charges directly; the provider-wide verdict
// is derived from the set of them (see providerOpen).
type circuit struct {
	state            State
	consecutiveFails int
	openedAt         time.Time // when the circuit last transitioned to Open
	halfOpenProbes   int       // successful probes in half-open state
	// cooldownOverride replaces the global cooldown for this circuit only. Set
	// when the circuit opens against a provider whose quota window is spent;
	// zero means "use the configured cooldown".
	cooldownOverride time.Duration
	// lastCharged is when a failure or success last landed on this circuit. It
	// orders eviction, so the circuits that are dropped when a provider exceeds
	// the cap are the ones nothing has routed to in the longest time.
	lastCharged time.Time
	// opens counts the transitions into Open inside the window that began at
	// openWindowStart. A model whose circuit keeps reopening is failing in a way
	// no single cooldown fixes, which is worth telling an operator once.
	opens           int
	openWindowStart time.Time
}

const (
	// reopenWindow is how long the open transitions that escalate together must
	// fall within. A day is long enough to catch a model that breaks a few times
	// across a working day and short enough that failures months apart never add
	// up to a report about the present.
	reopenWindow = 24 * time.Hour
	// opensBeforeEscalation is how many times a circuit opens inside one window
	// before the breaker says so. Three, because two is an ordinary bad
	// afternoon for a provider and the second open is already the first repeat.
	opensBeforeEscalation = 3
)

// noteOpen records a transition into Open and reports whether this circuit has
// now opened often enough inside one window to be worth escalating.
//
// A transition arriving after the window has run out starts a new window rather
// than extending the old one, so an escalation is always drawn from one run of
// recent failures rather than from unrelated outages that share a model. This
// mirrors goneStreak.strike, for the same reason.
//
// Crossing the threshold clears the window: one window escalates once. Without
// that, the fourth, fifth and sixth open each report the same unhealthy model
// again, and the operator learns nothing from any of them.
func (c *circuit) noteOpen(now time.Time) bool {
	if c.openWindowStart.IsZero() || now.Sub(c.openWindowStart) > reopenWindow {
		c.openWindowStart = now
		c.opens = 1
		return false
	}
	c.opens++
	if c.opens < opensBeforeEscalation {
		return false
	}
	c.openWindowStart = time.Time{}
	c.opens = 0
	return true
}

// modelCircuits holds one provider's circuits, keyed by the resolved upstream
// model id: the id actually sent upstream, never the `hotel/` group alias.
type modelCircuits map[string]*circuit

// getOrCreate returns the circuit for one (provider, model) pair, creating the
// provider's map and the circuit as needed. Must be called with cb.mu held.
func (cb *CircuitBreaker) getOrCreate(providerID, model string) *circuit {
	models, ok := cb.circuits[providerID]
	if !ok {
		models = make(modelCircuits)
		cb.circuits[providerID] = models
	}
	c, ok := models[model]
	if !ok {
		cb.evictIfFull(models)
		c = &circuit{state: StateClosed}
		models[model] = c
	}
	return c
}

// evictIfFull drops the least recently charged closed circuit when a provider
// is at the cap, making room for the one about to be created.
//
// Only closed circuits are candidates. Evicting an open or half-open circuit
// would silently restore the provider for a model the breaker has decided is
// broken, which is the one outcome the cap must never cause: a provider that
// somehow holds more open circuits than the cap keeps every one of them and the
// map is allowed to grow instead. Evicting a closed circuit costs at most a
// partial failure streak on a model nothing has routed to in a long time.
//
// Must be called with cb.mu held.
func (cb *CircuitBreaker) evictIfFull(models modelCircuits) {
	if len(models) < maxModelCircuitsPerProvider {
		return
	}
	base := cb.effectiveCooldown()
	victim := ""
	var oldest time.Time
	// found, rather than an empty victim, marks "nothing chosen yet": the empty
	// string is a legitimate model key and must be as evictable as any other.
	found := false
	for model, c := range models {
		if cb.logicalStateWith(c, base) != StateClosed {
			continue
		}
		// The model id breaks ties so eviction is deterministic even when two
		// circuits were charged in the same instant.
		if !found || c.lastCharged.Before(oldest) || (c.lastCharged.Equal(oldest) && model < victim) {
			victim, oldest, found = model, c.lastCharged, true
		}
	}
	if found {
		delete(models, victim)
	}
}

// providerOpen is the derived provider-wide verdict: the breaker never charges
// a provider directly, it reads one off the circuits it does charge.
//
// A provider is down when a quota pin says its window is spent, or when the
// failures span at least `span` distinct models. One model refusing is evidence
// about that model (a plan that excludes it, a retirement, a bad routing id) and
// must not darken its healthy siblings; corroboration across models is what
// makes "the provider is broken" the better explanation.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) providerOpen(models modelCircuits) bool {
	// Cheap pre-pass: a provider with nothing stored open has no verdict to
	// derive. That is the state nearly every request finds, and this way it
	// reads no settings at all to establish it — a cold settings cache turns
	// each of the settings reads below into a DB round trip taken under the
	// lock, on the request path. It also keeps the healthy path allocation-free,
	// because providerReport builds a list.
	anyOpen := false
	for _, c := range models {
		if c.state == StateOpen {
			anyOpen = true
			break
		}
	}
	if !anyOpen {
		return false
	}

	// The cooldown is read only now, after the pre-pass has established there is
	// a verdict to derive, and once for the whole walk.
	open, _, _ := cb.providerReport(models, cb.effectiveCooldown())
	return open
}

// providerReport derives the provider-wide verdict together with the model ids
// it rests on. It is the single implementation of the rule: the request path
// takes the verdict alone (providerOpen) and the status surfaces take both, so
// the flag an operator reads and the models listed beside it can never disagree
// about a cooldown that elapsed between two separate walks.
//
// blocked is sorted, because it is printed in a provider detail that refetches
// every few seconds and Go's map iteration order would otherwise reshuffle it.
//
// pinned is the third thing the same walk already knows: whether any of the
// blocking circuits is held dark by a quota pin. It is returned rather than
// re-derived from the row's dominant circuit, because those two readings
// disagree at exactly the shape this arm exists for — a provider indicted by a
// pinned sibling while its most degraded circuit carries no pin would otherwise
// be reported as skipped outright rather than as waiting for a quota window.
//
// base is the configured cooldown, hoisted by the caller so a walk over one
// provider's circuits reads the setting once instead of once per circuit.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) providerReport(models modelCircuits, base time.Duration) (open bool, blocked []string, pinned bool) {
	for model, c := range models {
		if !cb.blocking(c, base) {
			continue
		}
		blocked = append(blocked, model)
		if cb.quotaPinnedFor(c) {
			pinned = true
		}
	}
	slices.Sort(blocked)
	return pinned || len(blocked) >= cb.effectiveSpan(), blocked, pinned
}

// blocking reports whether a circuit is turning requests away right now: open
// and still inside the cooldown that governs it. A circuit owed a probe is not
// blocking, which is why it counts for neither the verdict nor the list beside
// it. base is the configured cooldown, hoisted by the caller.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) blocking(c *circuit, base time.Duration) bool {
	return c.state == StateOpen && cb.stillDark(c, base)
}

// stillDark reports whether an open circuit is still inside the cooldown that
// governs it, i.e. whether it is blocking traffic rather than owed a probe.
// base is the configured cooldown, hoisted by the caller so a walk over one
// provider's circuits reads the setting once instead of once per circuit.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) stillDark(c *circuit, base time.Duration) bool {
	if c.openedAt.IsZero() {
		return true
	}
	cooldown := base
	if cb.quotaPinnedFor(c) {
		cooldown = c.cooldownOverride
	}
	return time.Since(c.openedAt) < cooldown
}

// logicalState maps a circuit's stored state to the state every observer
// reports: an open circuit whose cooldown has elapsed is "ready to probe" and
// reads as half-open, even though the stored state only flips to StateHalfOpen
// for the brief duration of an in-flight probe request. Without this the
// half-open bucket is effectively unobservable (and the sidebar badge's middle
// count never moves). Purely derived — it never mutates the circuit — so every
// surface (Status, GetState, Reset) agrees on what a circuit is doing.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) logicalState(c *circuit) State {
	return cb.logicalStateWith(c, cb.effectiveCooldown())
}

// logicalStateWith is logicalState with the configured cooldown supplied by the
// caller, for walks that would otherwise re-read the setting per circuit.
func (cb *CircuitBreaker) logicalStateWith(c *circuit, base time.Duration) State {
	if c.state == StateOpen && !cb.stillDark(c, base) {
		return StateHalfOpen
	}
	return c.state
}

// circuitRank orders one provider's circuits so the per-provider surfaces
// (Status, Reset) can report a single row for a set of circuits. The most
// degraded circuit wins: it is the one an operator is looking for, and with a
// single model in play it is the only one, which keeps those surfaces reading
// exactly as they did when the breaker was keyed on the provider alone.
type circuitRank struct {
	model string
	state int
	retry time.Time
	fails int
}

// beats orders by how much the circuit is blocking: state first, then how far
// out its retry instant is, then the failure streak, with the model id as a
// final tie-break so the choice is deterministic.
func (r circuitRank) beats(o circuitRank) bool {
	switch {
	case r.state != o.state:
		return r.state > o.state
	case !r.retry.Equal(o.retry):
		return r.retry.After(o.retry)
	case r.fails != o.fails:
		return r.fails > o.fails
	default:
		return r.model < o.model
	}
}

// stateRank scores a logical state by how much it is blocking traffic.
func stateRank(s State) int {
	switch s {
	case StateOpen:
		return 2
	case StateHalfOpen:
		return 1
	default:
		return 0
	}
}

// dominant returns the circuit that represents a provider on the per-provider
// surfaces, or nil when the provider tracks none.
//
// base is the configured cooldown, hoisted by the caller: every read in this
// walk goes through the *With helpers, so ranking a provider's circuits reads
// settings zero times however many circuits it holds. That matters because
// Status runs this for every provider on every Prometheus scrape, under the
// lock the request path takes, and an uncached settings read is a DB round trip.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) dominant(models modelCircuits, base time.Duration) *circuit {
	var best *circuit
	var bestRank circuitRank
	for model, c := range models {
		r := circuitRank{
			model: model,
			state: stateRank(cb.logicalStateWith(c, base)),
			retry: c.openedAt.Add(cb.effectiveCooldownForWith(c, base)),
			fails: c.consecutiveFails,
		}
		if best == nil || r.beats(bestRank) {
			best, bestRank = c, r
		}
	}
	return best
}

// effectiveThreshold returns the failure count threshold, reading from
// settings if available, otherwise falling back to the struct default.
func (cb *CircuitBreaker) effectiveThreshold() int {
	if cb.settings != nil {
		if v := cb.settings.GetInt(context.Background(), "circuit_breaker_threshold", 0); v > 0 {
			return v
		}
	}
	return cb.Threshold
}

// effectiveSpan returns how many distinct open model circuits it takes to call
// the provider down, reading from settings if available. The floor of 1 is the
// escape hatch: it reproduces the provider-keyed behaviour the breaker had
// before circuits were split per model.
func (cb *CircuitBreaker) effectiveSpan() int {
	span := cb.SpanModels
	if cb.settings != nil {
		if v := cb.settings.GetInt(context.Background(), "circuit_breaker_span_models", 0); v > 0 {
			span = v
		}
	}
	if span < 1 {
		return 1
	}
	return span
}

// effectiveCooldown returns the open-state cooldown duration, reading from
// settings if available, otherwise falling back to the struct default.
func (cb *CircuitBreaker) effectiveCooldown() time.Duration {
	if cb.settings != nil {
		if v := cb.settings.GetDuration(context.Background(), "circuit_breaker_cooldown", 0); v > 0 {
			return v
		}
	}
	return cb.Cooldown
}

// effectiveCooldownFor returns the cooldown governing a specific circuit: its
// quota pin when one is in force, otherwise the configured global value. The
// settings read behind quotaPinnedFor is only reached for circuits that carry
// an override, so the common path costs nothing extra.
func (cb *CircuitBreaker) effectiveCooldownFor(c *circuit) time.Duration {
	if cb.quotaPinnedFor(c) {
		return c.cooldownOverride
	}
	return cb.effectiveCooldown()
}

// effectiveCooldownForWith is effectiveCooldownFor with the configured cooldown
// supplied by the caller, for walks that would otherwise re-read the setting per
// circuit. It pairs with logicalStateWith: the same hoisted base serves both, so
// a per-provider walk costs one settings read rather than one per circuit.
func (cb *CircuitBreaker) effectiveCooldownForWith(c *circuit, base time.Duration) time.Duration {
	if cb.quotaPinnedFor(c) {
		return c.cooldownOverride
	}
	return base
}

// quotaPinnedFor reports whether a quota pin is actually governing this circuit
// right now. The kill switch is deliberately re-read here rather than only at
// the moment a circuit opens: an operator who disables quota pinning to recover
// a provider sidelined for hours expects every pin already in force to be
// released at once, not only the circuits that open afterwards. It is the
// fleet-wide lever; Reset is the per-provider one.
//
// Every surface derives from this one predicate — the cooldown the breaker
// enforces, the CooldownMs/NextRetryAt the status API publishes, the
// quota_pinned flag beside them, and the pin arm of the provider-wide verdict —
// so the number and the explanation can never disagree.
func (cb *CircuitBreaker) quotaPinnedFor(c *circuit) bool {
	return c != nil && c.cooldownOverride > 0 && cb.quotaPinEnabled()
}

func (cb *CircuitBreaker) quotaPinEnabled() bool {
	if cb.settings == nil {
		return true
	}
	return cb.settings.GetBool(context.Background(), "circuit_breaker_quota_pin_enabled", true)
}

func (cb *CircuitBreaker) quotaPinMax() time.Duration {
	if cb.settings != nil {
		if v := cb.settings.GetDuration(context.Background(), "circuit_breaker_quota_pin_max", 0); v > 0 {
			return v
		}
	}
	return 24 * time.Hour
}
