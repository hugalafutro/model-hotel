package failover

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// maxModelCircuitsPerProvider bounds how many model circuits one provider may
// track. Breaker state is in-memory and every distinct upstream model id a
// request resolves to earns a circuit, so an unbounded map grows with the
// provider's catalog and with anything that looks like a model id upstream. The
// cap sits far above any real catalog.
const maxModelCircuitsPerProvider = 256

// defaultSpanModels is how many distinct model circuits must be open before the
// provider itself counts as down. Two is the smallest number that requires
// corroboration: one model refusing says something about that model, two models
// refusing says something about the provider.
const defaultSpanModels = 2

// defaultBackoffMax is the ceiling a probe backoff may reach. Fifteen minutes:
// at the default cooldown the waits run 1, 2, 4, 8 minutes and then hold, so a
// model broken all day costs a hundred or so wasted requests rather than 1440,
// while a model fixed upstream is back within a quarter of an hour. Not longer,
// because a probe is the only way a model with no healthy sibling gets tried
// again, and a backoff also stretches the provider-wide verdict.
const defaultBackoffMax = 15 * time.Minute

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
	// failedProbes counts the half-open probes that failed since the circuit
	// last closed, and cooldownBackoff is the cooldown those failures have
	// earned: the configured cooldown doubled once per failed probe, capped by
	// circuit_breaker_backoff_max, stamped at the open transition the way the
	// quota pin is. Zero means "no backoff in force". A probe is a live user
	// request, so each failure buys a longer wait before the next one is spent.
	failedProbes    int
	cooldownBackoff time.Duration
	// lastCharged is when a failure or success last landed on this circuit. It
	// orders eviction, so the circuits that are dropped when a provider exceeds
	// the cap are the ones nothing has routed to in the longest time. A
	// saturated 429 is neither and leaves it alone (see RecordSaturated).
	lastCharged time.Time
	// lastSuccess is when a success last landed on this circuit. It backs the
	// 429 behavioural fallback (LastSuccessWithin): a rate limit from a model
	// that served moments ago is load, not a spent window.
	lastSuccess time.Time
	// pinSource says where the quota pin in force came from: "advisor" (a
	// measured quota reading), "response" (inferred from the exhausted 429
	// itself) or "account" (inferred from a 429 that refused the whole
	// account). Empty when no pin is stamped. The cooldown reads it: a response
	// pin is capped at the probe interval, an advisor pin is not, so a wrong
	// source changes how long the circuit stays dark.
	pinSource string
	// probeSeed spreads a response pin's probes: a fraction in [0, 1) drawn once
	// with the pin and scaled by the interval at read time, so every read agrees
	// on the instant, a changed interval rescales it, and one provider's models
	// that went dark together do not all probe in the same second.
	probeSeed float64
	// lastCause, lastStatus and lastAt are the circuit's most recent verdict:
	// why it was last charged, credited, pinned or released, the upstream status
	// behind that (0 when none was seen) and when it landed. Like pinSource they
	// are observability only; nothing in the breaker's mechanics reads them. They
	// let a status row, an event and an alert say WHY a circuit is open (see
	// Cause).
	lastCause  string
	lastStatus int
	lastAt     time.Time
	// opens429Streak counts consecutive rate-limit-caused opens inside the
	// window that began at open429WindowStart. Any open with another cause, or
	// the window running out, resets it. From the third such open the circuit
	// is treated as exhausted-without-a-phrase: its probe backoff may climb
	// past circuit_breaker_backoff_max up to circuit_breaker_quota_pin_max,
	// because a circuit that only ever reopens on 429 probes is burning a
	// request per cooldown against a window that resets in hours. A successful
	// probe, a manual reset, or an affirmative quota recovery clears it.
	opens429Streak     int
	open429WindowStart time.Time
	// opens counts the transitions into Open inside the window that began at
	// openWindowStart, and unstableReported records whether that window has
	// already been reported. A model whose circuit keeps reopening is failing in
	// a way no single cooldown fixes, which is worth telling an operator once.
	opens            int
	openWindowStart  time.Time
	unstableReported bool
}

const (
	// reopenWindow is how long the open transitions that escalate together must
	// fall within. A day catches a model that breaks a few times across a working
	// day without letting failures months apart add up.
	reopenWindow = 24 * time.Hour
	// opensBeforeEscalation is how many times a circuit opens inside one window
	// before the breaker says so. Three, because two is an ordinary bad afternoon
	// for a provider.
	opensBeforeEscalation = 3
)

// reopenWindowLabel writes the window the way an operator reads it and the way
// the event publishes it, since Duration.String renders a day as "24h0m0s".
// Derived from the constant so the two cannot drift apart.
var reopenWindowLabel = fmt.Sprintf("%dh", int(reopenWindow.Hours()))

// noteOpen records a transition into Open and reports whether this circuit has
// now opened often enough inside one window to be worth escalating.
//
// A transition arriving after the window has run out starts a new window rather
// than extending the old one, so an escalation is drawn from one run of recent
// failures rather than from unrelated outages that share a model. This mirrors
// goneStreak.strike.
//
// Crossing the threshold marks the window reported rather than clearing it, so
// the window keeps running and a report costs a full reopenWindow before another
// can follow. Clearing it would start a fresh window on the next open, and a
// model failing every cooldown would report every few minutes.
func (c *circuit) noteOpen(now time.Time) bool {
	if c.openWindowStart.IsZero() || now.Sub(c.openWindowStart) > reopenWindow {
		c.openWindowStart = now
		c.opens = 1
		c.unstableReported = false
		return false
	}
	c.opens++
	if c.opens < opensBeforeEscalation || c.unstableReported {
		return false
	}
	c.unstableReported = true
	return true
}

// note429Open updates the rate-limit-open streak for one transition into Open.
// Same window semantics as noteOpen, tracked separately: noteOpen reports
// chronic instability whatever the cause, this one escalates only when EVERY
// open in the run was a rate limit, so a 5xx-caused open in between resets the
// streak.
func (c *circuit) note429Open(now time.Time, by429 bool) {
	if !by429 {
		c.opens429Streak = 0
		c.open429WindowStart = time.Time{}
		return
	}
	if c.open429WindowStart.IsZero() || now.Sub(c.open429WindowStart) > reopenWindow {
		c.open429WindowStart = now
		c.opens429Streak = 1
		return
	}
	c.opens429Streak++
}

// exhaustedEscalated reports whether the streak has reached the point where the
// circuit is treated as exhausted without any provider phrase saying so. Three,
// matching opensBeforeEscalation; a provider phrase shortens this to one open.
func (c *circuit) exhaustedEscalated() bool {
	return c.opens429Streak >= opensBeforeEscalation
}

// clear429Escalation drops the streak: called when HTTP proves recovery (a
// successful probe closes the circuit) and when a quota refresh affirms the
// provider is not exhausted.
func (c *circuit) clear429Escalation() {
	c.opens429Streak = 0
	c.open429WindowStart = time.Time{}
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
// Only closed circuits are candidates: evicting an open or half-open one would
// silently restore the provider for a model the breaker has decided is broken.
// A provider holding more open circuits than the cap keeps every one of them and
// the map grows instead. Evicting a closed circuit costs at most a partial
// failure streak on a model nothing has routed to in a long time.
//
// Must be called with cb.mu held.
func (cb *CircuitBreaker) evictIfFull(models modelCircuits) {
	if len(models) < maxModelCircuitsPerProvider {
		return
	}
	r := cb.cooldowns()
	victim := ""
	var oldest time.Time
	// found, rather than an empty victim, marks "nothing chosen yet": the empty
	// string is a legitimate model key and is as evictable as any other.
	found := false
	for model, c := range models {
		if cb.logicalStateWith(c, r) != StateClosed {
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

// cooldownReads is everything a walk over circuits needs from settings, read at
// most once per walk. An unoverridden key has no settings row to serve from
// cache, so each read is a DB round trip, and every walk here runs under cb.mu:
// Status on every Prometheus scrape, the provider verdict on the request path.
// The configured cooldown is read up front because every circuit needs it. The
// two kill switches are read lazily, on the first circuit carrying an override
// or a backoff, so the healthy case costs one read and a provider with a hundred
// backed-off circuits costs one more, not a hundred.
//
// The switches are read per walk rather than only when a circuit opens, so an
// operator who flips one releases every override already in force, and one
// Status row cannot report a pin its neighbour's read said was disabled.
type cooldownReads struct {
	cb        *CircuitBreaker
	base      time.Duration
	backoffOn *bool
	pinOn     *bool
	pinProbe  *time.Duration
}

// cooldowns starts a walk: one read of the configured cooldown, the switches
// deferred. Must be called with cb.mu held (read lock suffices), like every
// helper that takes the result.
func (cb *CircuitBreaker) cooldowns() *cooldownReads {
	return &cooldownReads{cb: cb, base: cb.effectiveCooldown()}
}

func (r *cooldownReads) backoffEnabled() bool {
	if r.backoffOn == nil {
		v := r.cb.backoffEnabled()
		r.backoffOn = &v
	}
	return *r.backoffOn
}

func (r *cooldownReads) pinEnabled() bool {
	if r.pinOn == nil {
		v := r.cb.quotaPinEnabled()
		r.pinOn = &v
	}
	return *r.pinOn
}

func (r *cooldownReads) pinProbeInterval() time.Duration {
	if r.pinProbe == nil {
		v := r.cb.pinProbeInterval()
		r.pinProbe = &v
	}
	return *r.pinProbe
}

// providerOpen is the derived provider-wide verdict: the breaker never charges
// a provider directly, it reads one off the circuits it does charge.
//
// A provider is down when a pin that speaks for the account says so (the quota
// ADVISOR's measured window, or a response that refused the account itself),
// or when the failures span at least `span` distinct models. One model
// refusing is evidence about that model (a plan that excludes it, a
// retirement, a bad routing id) and must not darken its healthy siblings. A
// pin inferred from one model's response (pinSource "response") is one model's
// evidence and does not count here; see providerReport.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) providerOpen(models modelCircuits) bool {
	// Pre-pass: a provider with nothing stored open has no verdict to derive,
	// which is the state nearly every request finds, and establishing it reads no
	// settings (a cold cache would make each read below a DB round trip under the
	// lock, on the request path). It also keeps the healthy path allocation-free,
	// since providerReport builds a list.
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

	// Settings are read only now, after the pre-pass establishes there is a
	// verdict to derive, and once for the whole walk.
	open, _, _ := cb.providerReport(models, cb.cooldowns())
	return open
}

// providerReport derives the provider-wide verdict together with the model ids
// it rests on. It is the single implementation of the rule: the request path
// takes the verdict alone (providerOpen) and the status surfaces take both, so
// the flag an operator reads and the models listed beside it cannot disagree
// about a cooldown that elapsed between two separate walks.
//
// blocked is sorted, because it is printed in a provider detail that refetches
// every few seconds and Go's map iteration order would reshuffle it.
//
// pinned says whether any blocking circuit is held dark by a quota pin. It comes
// from this walk rather than from the row's dominant circuit, which would report
// a provider indicted by a pinned sibling as skipped outright rather than as
// waiting for a quota window.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) providerReport(models modelCircuits, r *cooldownReads) (open bool, blocked []string, pinned bool) {
	providerPinned := false
	for model, c := range models {
		if !cb.blocking(c, r) {
			continue
		}
		blocked = append(blocked, model)
		if cb.quotaPinnedForWith(c, r) {
			pinned = true
			if pinSpeaksForAccount(c.pinSource) {
				providerPinned = true
			}
		}
	}
	slices.Sort(blocked)
	// A pin that speaks for the ACCOUNT indicts the provider on its own
	// (pinSpeaksForAccount). A plain response pin is inferred from one model's
	// 429 (a plan excluding ONE model, answered with a balance error), so it
	// darkens that model alone. Corroboration across models (the span) still
	// reaches the provider.
	return providerPinned || len(blocked) >= cb.effectiveSpan(), blocked, pinned
}

// blocking reports whether a circuit is turning requests away right now: open
// and still inside the cooldown that governs it. A circuit owed a probe is not
// blocking, which is why it counts for neither the verdict nor the list beside
// it.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) blocking(c *circuit, r *cooldownReads) bool {
	return c.state == StateOpen && cb.stillDark(c, r)
}

// stillDark reports whether an open circuit is still inside the cooldown that
// governs it, i.e. whether it is blocking traffic rather than owed a probe.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) stillDark(c *circuit, r *cooldownReads) bool {
	if c.openedAt.IsZero() {
		return true
	}
	return time.Since(c.openedAt) < cb.effectiveCooldownForWith(c, r)
}

// logicalState maps a circuit's stored state to the state every observer
// reports: an open circuit whose cooldown has elapsed is "ready to probe" and
// reads as half-open, even though the stored state only flips to StateHalfOpen
// for the brief duration of an in-flight probe request. Without this the
// half-open bucket is effectively unobservable. Purely derived, never mutating
// the circuit, so every surface (Status, GetState, Reset) agrees.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) logicalState(c *circuit) State {
	return cb.logicalStateWith(c, cb.cooldowns())
}

// logicalStateWith is logicalState with the walk's settings supplied by the
// caller, for walks that would otherwise re-read them per circuit.
func (cb *CircuitBreaker) logicalStateWith(c *circuit, r *cooldownReads) State {
	if c.state == StateOpen && !cb.stillDark(c, r) {
		return StateHalfOpen
	}
	return c.state
}

// circuitRank orders one provider's circuits so the per-provider surfaces
// (Status, Reset) can report a single row for a set of circuits. The most
// degraded circuit wins: it is the one an operator is looking for, and with a
// single model in play it is the only one.
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
// r carries the walk's settings, hoisted by the caller: every read here goes
// through the *With helpers, so ranking a provider's circuits reads settings
// zero times. Status runs this for every provider on every Prometheus scrape,
// under the lock the request path takes, and an uncached settings read is a DB
// round trip.
//
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) dominant(models modelCircuits, r *cooldownReads) *circuit {
	var best *circuit
	var bestRank circuitRank
	for model, c := range models {
		rank := circuitRank{
			model: model,
			state: stateRank(cb.logicalStateWith(c, r)),
			retry: c.openedAt.Add(cb.effectiveCooldownForWith(c, r)),
			fails: c.consecutiveFails,
		}
		if best == nil || rank.beats(bestRank) {
			best, bestRank = c, rank
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
// the provider down, reading from settings if available. A span of 1 is the
// escape hatch: any single open circuit indicts the provider.
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

// effectiveCooldownForWith is the cooldown governing a circuit: the longest of
// the configured cooldown, the circuit's probe backoff when backoff is switched
// on, and its quota pin when pinning is switched on, except that a pin inferred
// from a response is served as the probe interval instead of its full length.
//
// The longest, not a precedence order, because each of the three is a reason the
// circuit must not be probed yet and none is a reason it may be. It also keeps
// the stored values safe against changes underneath them: a base raised above a
// stamped backoff, a switch flipped after a pin was floored, a ceiling lowered
// after a pin was stamped. applyQuotaPin floors a pin at the backoff, so the
// longest is the pin whenever one is in force.
func (cb *CircuitBreaker) effectiveCooldownForWith(c *circuit, r *cooldownReads) time.Duration {
	cooldown := cb.unpinnedCooldownWith(c, r)
	if !cb.quotaPinnedForWith(c, r) || c.cooldownOverride <= cooldown {
		return cooldown
	}
	pinned := c.cooldownOverride
	// A pin inferred from a response is a guess at a window nothing measures:
	// the providers that answer with a bare "no credits" have no quota
	// endpoint, so no advisor reading will ever release the pin early, and a
	// top-up would otherwise wait out the whole ceiling. Such a circuit probes
	// once per interval instead: the probe re-pins on another refusal and
	// closes the circuit on a success. The interval, not the backoff, is the
	// rate limit on those probes (a refused probe does not grow the backoff,
	// see RecordExhausted), and it never undercuts the cooldown the circuit
	// would serve unpinned, so the pin can never make the breaker more
	// aggressive. An advisor pin measured the window and keeps its full length.
	if pinProbes(c.pinSource) {
		if probe := r.pinProbeInterval(); probe > 0 {
			return max(probe, cooldown) + time.Duration(c.probeSeed*float64(probe)/20)
		}
	}
	return pinned
}

// defaultPinProbeInterval is how often a circuit pinned on a response's own
// claim is probed when the operator has not set circuit_breaker_pin_probe_interval.
const defaultPinProbeInterval = time.Hour

// pinProbeInterval reads circuit_breaker_pin_probe_interval. Unlike its sibling
// ceilings, where a non-positive value means "unset", a zero here is a real
// setting: it disables the periodic probe and lets a response pin run to the
// ceiling.
func (cb *CircuitBreaker) pinProbeInterval() time.Duration {
	if cb.settings != nil {
		return cb.settings.GetDuration(context.Background(), "circuit_breaker_pin_probe_interval", defaultPinProbeInterval)
	}
	return defaultPinProbeInterval
}

// unpinnedCooldownWith is the cooldown a circuit serves when no quota pin is
// counted: its backoff when one is in force, otherwise the configured cooldown.
// It is the floor a pin must clear when it is stamped, and the wait a released
// pin falls back to.
func (cb *CircuitBreaker) unpinnedCooldownWith(c *circuit, r *cooldownReads) time.Duration {
	if cb.backedOffForWith(c, r) {
		return c.cooldownBackoff
	}
	return r.base
}

// applyBackoff stamps the cooldown a circuit's failed probes have earned. Must
// be called with cb.mu held, immediately after c transitions to Open and before
// applyQuotaPin, which floors the pin at the value stamped here.
//
// The backoff is computed once, at the open transition, and stored rather than
// derived on every read: a ceiling read per circuit would put a DB round trip
// per circuit back under the lock. The stored value cannot know a base raised
// after it was stamped, which effectiveCooldownForWith covers by never serving
// less than the base. Always stamped, gated only at read time by
// backedOffForWith, so the kill switch acts at once in both directions.
//
// A ceiling at or below the base is not a shorter cooldown: the ceiling bounds
// what the backoff may add, and a backoff that could add nothing is left off.
//
// A circuit escalated to exhausted-without-a-phrase (see exhaustedEscalated)
// takes the quota-pin ceiling when that reaches further: its probes are live
// requests spent against a window that resets in hours, which the ordinary
// 15-minute cap would re-probe through.
func (cb *CircuitBreaker) applyBackoff(c *circuit) {
	c.cooldownBackoff = 0
	if c.failedProbes == 0 {
		return
	}
	base := cb.effectiveCooldown()
	ceiling := cb.backoffMax()
	if c.exhaustedEscalated() {
		ceiling = max(ceiling, cb.quotaPinMax())
	}
	if base <= 0 || ceiling <= base {
		return
	}
	d := base
	// Doubled step by step and stopped at the ceiling, never shifted by the count:
	// a week of failed probes gives a count that overflows a shift. The halfway
	// test keeps the doubling inside int64 whatever the ceiling is.
	for range c.failedProbes {
		if d > ceiling/2 {
			d = ceiling
			break
		}
		d *= 2
	}
	c.cooldownBackoff = d
}

// backedOffForWith reports whether a probe backoff is governing this circuit:
// one is stamped, it reaches beyond the configured cooldown, and backoff is
// switched on. Like quotaPinnedForWith, the kill switch is re-read per walk, so
// disabling backoff releases every backoff already in force. The "beyond the
// base" test keeps the flag honest when the base is raised after the stamp: a
// row must not claim a backoff for a cooldown identical to the setting. Every
// surface that reports or enforces the backoff derives from this predicate. The
// flag says the backoff is in force, not that it is the longest; a pin can reach
// further, and effectiveCooldownForWith decides which one the number is.
func (cb *CircuitBreaker) backedOffForWith(c *circuit, r *cooldownReads) bool {
	return c != nil && c.cooldownBackoff > r.base && r.backoffEnabled()
}

func (cb *CircuitBreaker) backoffEnabled() bool {
	if cb.settings == nil {
		return true
	}
	return cb.settings.GetBool(context.Background(), "circuit_breaker_backoff_enabled", true)
}

// backoffMax is the ceiling a probe backoff may reach; see defaultBackoffMax.
func (cb *CircuitBreaker) backoffMax() time.Duration {
	if cb.settings != nil {
		if v := cb.settings.GetDuration(context.Background(), "circuit_breaker_backoff_max", 0); v > 0 {
			return v
		}
	}
	return defaultBackoffMax
}

// quotaPinnedForWith reports whether a quota pin is governing this circuit. The
// kill switch is re-read per walk, so disabling quota pinning releases every pin
// already in force. It is the fleet-wide lever; Reset is the per-provider one.
//
// Every surface derives from this predicate: the cooldown the breaker enforces,
// the CooldownMs/NextRetryAt the status API publishes, the quota_pinned flag
// beside them, and the pin arm of the provider-wide verdict. The flag says the
// pin is in force, not that it is the longest; a backoff can reach further, and
// effectiveCooldownForWith decides which one the number is.
func (cb *CircuitBreaker) quotaPinnedForWith(c *circuit, r *cooldownReads) bool {
	return c != nil && c.cooldownOverride > 0 && r.pinEnabled()
}

// pinSourceForWith is the pin's provenance ("advisor", "response" or "account"), reported
// only while the pin is in force. It uses the predicate the quota_pinned flag
// derives from, so a row cannot name a source for a pin it does not claim.
func (cb *CircuitBreaker) pinSourceForWith(c *circuit, r *cooldownReads) string {
	if !cb.quotaPinnedForWith(c, r) {
		return ""
	}
	return c.pinSource
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
