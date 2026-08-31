package failover

import (
	"context"
	"fmt"
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

// defaultBackoffMax is the ceiling a probe backoff may reach. Fifteen minutes:
// at the default cooldown the waits run 1, 2, 4, 8 minutes and then hold, so a
// model broken all day costs a hundred or so wasted requests rather than 1440,
// while a model fixed upstream is back within a quarter of an hour with nobody
// resetting anything. It is deliberately not longer: a probe is the only way a
// model with no healthy sibling ever gets tried again, and the verdict that
// skips a whole provider holds for as long as enough of its circuits keep
// blocking, which a backoff stretches.
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
	// request, so a model that is broken all day fails one real request per
	// cooldown unless each failure buys a longer wait before the next.
	failedProbes    int
	cooldownBackoff time.Duration
	// lastCharged is when a failure or success last landed on this circuit. It
	// orders eviction, so the circuits that are dropped when a provider exceeds
	// the cap are the ones nothing has routed to in the longest time.
	lastCharged time.Time
	// lastSuccess is when a success last landed on this circuit. It backs the
	// 429 behavioural fallback (LastSuccessWithin): a rate limit from a model
	// that served moments ago is load, not a spent window.
	lastSuccess time.Time
	// pinSource says where the quota pin in force came from: "advisor" (a
	// measured quota reading) or "response" (inferred from the exhausted 429
	// itself). Empty when no pin is stamped. Observability only; the pin's
	// mechanics do not read it.
	pinSource string
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
	// fall within. A day is long enough to catch a model that breaks a few times
	// across a working day and short enough that failures months apart never add
	// up to a report about the present.
	reopenWindow = 24 * time.Hour
	// opensBeforeEscalation is how many times a circuit opens inside one window
	// before the breaker says so. Three, because two is an ordinary bad
	// afternoon for a provider and the second open is already the first repeat.
	opensBeforeEscalation = 3
)

// reopenWindowLabel writes the window the way an operator reads it and the way
// the event publishes it. Duration.String renders a day as "24h0m0s", which is
// noise in a sentence and a surprise to a consumer comparing the metadata
// against the documented value. Derived rather than written twice so the
// sentence cannot drift from the constant it describes.
var reopenWindowLabel = fmt.Sprintf("%dh", int(reopenWindow.Hours()))

// noteOpen records a transition into Open and reports whether this circuit has
// now opened often enough inside one window to be worth escalating.
//
// A transition arriving after the window has run out starts a new window rather
// than extending the old one, so an escalation is always drawn from one run of
// recent failures rather than from unrelated outages that share a model. This
// mirrors goneStreak.strike, for the same reason.
//
// Crossing the threshold marks the window reported rather than clearing it, so
// the window keeps running and a report costs a full reopenWindow before another
// can follow. Clearing it instead would start a fresh window on the very next
// open: a model failing every cooldown reaches three opens again within minutes,
// and the operator would be told every few minutes that it "broke 3 times in
// 24h" — a sentence the cadence itself contradicts.
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
// Same window semantics as noteOpen, tracked separately because the two answer
// different questions: noteOpen reports chronic instability whatever the
// cause, this one escalates only when EVERY open in the run was a rate limit —
// a 5xx-caused open in between is evidence of a different failure and resets
// the streak, exactly as the design requires.
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

// exhaustedEscalated reports whether the streak has reached the point where
// the circuit is treated as exhausted without any provider phrase saying so.
// Three, matching opensBeforeEscalation: the provider's own text can shorten
// this to one open; its absence only makes it slower, never wrong.
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
	r := cb.cooldowns()
	victim := ""
	var oldest time.Time
	// found, rather than an empty victim, marks "nothing chosen yet": the empty
	// string is a legitimate model key and must be as evictable as any other.
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
// most once per walk. A deployment that never overrode a key has no settings row
// to serve from cache, so each read is a DB round trip, and every walk here runs
// under cb.mu: Status on every Prometheus scrape, the provider verdict on the
// request path. The configured cooldown is read up front because every circuit
// needs it. The two kill switches are read lazily, on the first circuit that
// carries an override or a backoff, so the healthy case (nothing overridden,
// nothing backed off) costs exactly the one read it always did, and a provider
// with a hundred backed-off circuits costs one more, not a hundred.
//
// The switches are read per walk rather than only when a circuit opens because
// an operator who flips one to get a provider back expects every override
// already in force to be released at once, not only the circuits that open
// afterwards. Per walk is also what keeps the surfaces consistent: one Status
// row cannot report a pin its neighbour's read said was disabled.
type cooldownReads struct {
	cb        *CircuitBreaker
	base      time.Duration
	backoffOn *bool
	pinOn     *bool
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

// providerOpen is the derived provider-wide verdict: the breaker never charges
// a provider directly, it reads one off the circuits it does charge.
//
// A provider is down when the quota ADVISOR's pin says its window is spent, or
// when the failures span at least `span` distinct models. One model refusing
// is evidence about that model (a plan that excludes it, a retirement, a bad
// routing id) and must not darken its healthy siblings; corroboration across
// models is what makes "the provider is broken" the better explanation. A pin
// inferred from a single response (pinSource "response") is one model's
// evidence and deliberately does not count here — see providerReport.
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

	// Settings are read only now, after the pre-pass has established there is
	// a verdict to derive, and once for the whole walk.
	open, _, _ := cb.providerReport(models, cb.cooldowns())
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
// Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) providerReport(models modelCircuits, r *cooldownReads) (open bool, blocked []string, pinned bool) {
	advisorPinned := false
	for model, c := range models {
		if !cb.blocking(c, r) {
			continue
		}
		blocked = append(blocked, model)
		if cb.quotaPinnedForWith(c, r) {
			pinned = true
			if c.pinSource == pinSourceAdvisor {
				advisorPinned = true
			}
		}
	}
	slices.Sort(blocked)
	// Only an ADVISOR pin indicts the provider on its own: the advisor
	// measured the provider's account, so its verdict is provider-scoped by
	// construction. A response-derived pin is inferred from one model's 429,
	// and the refusal that made per-model keying necessary — a plan excluding
	// ONE model, answered with a balance error — is exactly that shape: pinning
	// it must darken that model for as long as the pin says, and nothing else.
	// Corroboration across models (the span) still reaches the provider.
	return advisorPinned || len(blocked) >= cb.effectiveSpan(), blocked, pinned
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
// half-open bucket is effectively unobservable (and the sidebar badge's middle
// count never moves). Purely derived — it never mutates the circuit — so every
// surface (Status, GetState, Reset) agrees on what a circuit is doing.
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
// r carries the walk's settings, hoisted by the caller: every read in this walk
// goes through the *With helpers, so ranking a provider's circuits reads
// settings zero times however many circuits it holds. That matters because
// Status runs this for every provider on every Prometheus scrape, under the
// lock the request path takes, and an uncached settings read is a DB round trip.
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

// effectiveCooldownForWith is the cooldown governing a circuit: the longest of
// the configured cooldown, the circuit's probe backoff when backoff is switched
// on, and its quota pin when pinning is switched on.
//
// The longest, not a precedence order, because each of the three is a reason
// the circuit must not be probed yet, and none of them is a reason it may be.
// Taking the longest is also what makes the stored values safe against
// everything that can change under them: a base raised above a stamped backoff
// (the setting governs, and the row stops claiming a backoff), a switch flipped
// between the moment a pin was floored and now (the backoff the pin was meant
// to clear still holds), and a ceiling lowered after a pin was stamped. In
// the ordinary course a pin is at least the backoff, because applyQuotaPin
// floors it there, so the longest is simply the pin whenever one is in force.
func (cb *CircuitBreaker) effectiveCooldownForWith(c *circuit, r *cooldownReads) time.Duration {
	cooldown := cb.unpinnedCooldownWith(c, r)
	if cb.quotaPinnedForWith(c, r) && c.cooldownOverride > cooldown {
		cooldown = c.cooldownOverride
	}
	return cooldown
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
// The backoff is computed once, at the open transition, and stored, rather than
// derived on every read from the count and the ceiling: every walk over circuits
// reads the configured cooldown once and then takes the rest from the circuit,
// and a ceiling read per circuit would put a DB round trip per circuit back
// under the lock. What the stored value cannot know is a base raised after it
// was stamped; effectiveCooldownForWith covers that by never serving less than
// the base. Always stamped, gated only at read time by backedOffForWith, so the
// kill switch acts at once in both directions.
//
// A ceiling at or below the base is not a shorter cooldown: the ceiling bounds
// what the backoff may add, and a backoff that could add nothing is left off.
//
// A circuit escalated to exhausted-without-a-phrase (see exhaustedEscalated)
// gets the quota-pin ceiling instead when that reaches further: its probes are
// live requests spent against a window that resets in hours, so the ordinary
// 15-minute cap is exactly the re-probe churn the escalation exists to stop.
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
	// Doubled step by step and stopped at the ceiling, never shifted by the
	// count: a model that has failed its probe for a week has a count that would
	// overflow a shift long before it reached anything a ceiling could clamp.
	// The halfway test keeps the doubling itself inside int64 whatever the
	// ceiling is.
	for range c.failedProbes {
		if d > ceiling/2 {
			d = ceiling
			break
		}
		d *= 2
	}
	c.cooldownBackoff = d
}

// backedOffForWith reports whether a probe backoff is actually governing this
// circuit right now: one is stamped, it reaches beyond the configured cooldown,
// and backoff is switched on. Like quotaPinnedForWith, the kill switch is
// re-read per walk rather than only when a circuit opens, so an operator who
// disables backoff to get a provider back releases every backoff already in
// force, not only the circuits that open afterwards. The "beyond the base" test
// is what keeps the flag honest when the base is raised after the stamp: a row
// must not claim a backoff for a cooldown identical to the setting. Every
// surface that reports or enforces the backoff derives from this one predicate.
// The flag says the backoff is in force, not that it is the longest; a pin can
// reach further, and effectiveCooldownForWith decides which one the number is.
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

// quotaPinnedForWith reports whether a quota pin is actually governing this
// circuit right now. The kill switch is deliberately re-read per walk rather
// than only at the moment a circuit opens: an operator who disables quota
// pinning to recover a provider sidelined for hours expects every pin already
// in force to be released at once, not only the circuits that open afterwards.
// It is the fleet-wide lever; Reset is the per-provider one.
//
// Every surface derives from this one predicate — the cooldown the breaker
// enforces, the CooldownMs/NextRetryAt the status API publishes, the
// quota_pinned flag beside them, and the pin arm of the provider-wide verdict.
// The flag says the pin is in force, not that it is the longest; a backoff can
// reach further, and effectiveCooldownForWith decides which one the number is.
func (cb *CircuitBreaker) quotaPinnedForWith(c *circuit, r *cooldownReads) bool {
	return c != nil && c.cooldownOverride > 0 && r.pinEnabled()
}

// pinSourceForWith is the pin's provenance ("advisor" or "response"), reported
// only while the pin is actually in force — the same predicate the
// quota_pinned flag derives from, so a row can never name a source for a pin
// it does not claim.
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
