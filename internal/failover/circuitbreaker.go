package failover

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// State represents the health state of a single provider endpoint.
type State int

// Circuit breaker states.
const (
	StateClosed   State = iota // Normal operation — requests pass through
	StateOpen                  // Provider is failing — requests are skipped
	StateHalfOpen              // Testing recovery — limited probe requests allowed
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// MarshalText implements encoding.TextMarshaler for JSON serialization.
func (s State) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

// ProviderStatus represents the health status of a single provider for
// API responses and SSE events.
//
// State and the fields beside it describe the provider's most degraded model
// circuit. ProviderOpen and OpenModels describe the provider as a whole: the
// derived verdict and the evidence it is derived from. The two answer different
// questions and can disagree — one open model at the default span of 2 gives
// State "open" and ProviderOpen false, which is exactly the case an operator
// needs to see, because the provider is still serving every other model.
type ProviderStatus struct {
	ProviderID       string `json:"provider_id"`
	ProviderName     string `json:"provider_name,omitempty"`
	State            string `json:"state"`
	ConsecutiveFails int    `json:"consecutive_fails"`
	OpenedAt         string `json:"opened_at,omitempty"`
	CooldownMs       int64  `json:"cooldown_ms,omitempty"`
	NextRetryAt      string `json:"next_retry_at,omitempty"`
	QuotaPinned      bool   `json:"quota_pinned,omitempty"`
	// ProviderOpen is the derived provider-wide verdict: whether the breaker is
	// skipping this provider for every model. Always emitted, including its
	// false, so a consumer can tell "the provider is fine" from "the field is
	// missing" without re-deriving it from OpenModels and the span setting.
	ProviderOpen bool `json:"provider_open"`
	// OpenModels lists the resolved upstream model ids the breaker is currently
	// blocking, sorted so a polling UI does not reshuffle the list. It is exactly
	// the set ProviderOpen is counted from, so a provider detail can name the
	// models a verdict rests on. Circuits owed a probe are not in it: they are no
	// longer blocking anything.
	OpenModels []string `json:"open_models,omitempty"`
}

// SettingsReader provides dynamic configuration for the circuit breaker.
// This decouples the breaker from the settings package — callers inject
// a thin shim that reads from their settings repository.
type SettingsReader interface {
	GetInt(ctx context.Context, key string, defaultValue int) int
	GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration
	GetBool(ctx context.Context, key string, defaultValue bool) bool
}

// QuotaAdvisor supplies the reset deadline for a provider whose quota window is
// spent. Implementations must be non-blocking: this is consulted while the
// breaker holds its write lock on the request path. Implementations must not
// call back into the CircuitBreaker (e.g. GetState, Status) — cb.mu is held
// exclusively across the call and any such re-entry self-deadlocks.
type QuotaAdvisor interface {
	ResetsAt(providerID uuid.UUID) (time.Time, bool)
}

// CircuitBreaker tracks health per (provider, resolved upstream model) and
// prevents requests to consistently failing models, and to providers whose
// failures span enough distinct models to indict the provider itself.
type CircuitBreaker struct {
	mu sync.RWMutex
	// circuits holds every provider's model circuits, keyed by provider UUID
	// string and then by resolved upstream model id.
	circuits map[string]modelCircuits

	// settings provides runtime-configurable threshold and cooldown.
	settings SettingsReader

	// quota supplies per-provider quota reset deadlines, used to pin the
	// cooldown of an already-open circuit. Nil disables quota pinning.
	quota QuotaAdvisor

	// onOpen is notified whenever a circuit transitions to Open. It exists so a
	// consumer can refresh what it knows about the provider at the one moment
	// that knowledge decides how long the circuit stays dark. A plain callback
	// rather than a package dependency: the breaker must not know what the
	// consumer does with the notification. Nil when nothing is wired.
	onOpen func(providerID uuid.UUID)

	// Threshold is the number of consecutive failures before opening a model
	// circuit.
	Threshold int

	// SpanModels is how many of a provider's model circuits must be open before
	// the provider itself counts as down. Overridden at runtime by the
	// "circuit_breaker_span_models" setting.
	SpanModels int

	// Cooldown is how long a circuit stays open before transitioning
	// to half-open.
	Cooldown time.Duration

	// HalfOpenMaxProbes is the number of consecutive successes needed
	// in half-open state to close the circuit.
	HalfOpenMaxProbes int
}

// NewCircuitBreaker creates a circuit breaker with sensible defaults:
//   - Threshold: 5 consecutive failures
//   - Cooldown: 60 seconds
//   - HalfOpenMaxProbes: 1 success to close
//   - SpanModels: 2 open model circuits to call the provider down
//
// If settings is non-nil, threshold, cooldown and span are read from it at
// runtime (via "circuit_breaker_threshold", "circuit_breaker_cooldown" and
// "circuit_breaker_span_models"). Hardcoded defaults are used when settings is
// nil or a key is missing.
func NewCircuitBreaker(settings SettingsReader) *CircuitBreaker {
	return &CircuitBreaker{
		circuits:          make(map[string]modelCircuits),
		settings:          settings,
		Threshold:         5,
		SpanModels:        defaultSpanModels,
		Cooldown:          60 * time.Second,
		HalfOpenMaxProbes: 1,
	}
}

// SetQuotaAdvisor installs the quota advisor. Call during startup wiring,
// before the breaker serves traffic. A nil advisor disables quota pinning.
func (cb *CircuitBreaker) SetQuotaAdvisor(a QuotaAdvisor) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.quota = a
}

// SetOnOpen installs the open-transition callback. Call during startup wiring,
// before the breaker serves traffic. A nil callback disables the notification.
func (cb *CircuitBreaker) SetOnOpen(fn func(providerID uuid.UUID)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onOpen = fn
}

// notifyOpen fires the open-transition callback for a circuit that just went
// open. The callback runs on its own goroutine, so it never executes under
// cb.mu and never adds latency to the request that opened the circuit: this is
// reached from RecordFailure on the proxy path, and a consumer is free to reach
// the network. Must be called with cb.mu held.
func (cb *CircuitBreaker) notifyOpen(providerID uuid.UUID) {
	if cb.onOpen == nil {
		return
	}
	fn := cb.onOpen
	go fn(providerID)
}

// IsOpen returns true if the circuit breaker is preventing requests to this
// provider for this resolved upstream model: either the model's own circuit is
// open, or the provider-wide verdict derived from all of them is open. It also
// handles the Open → Half-Open transition when the model circuit's cooldown has
// elapsed.
//
// Fast path: most calls hit the Closed state, which only needs a read lock.
// Only the Open→HalfOpen transition requires a write lock.
func (cb *CircuitBreaker) IsOpen(providerID uuid.UUID, providerName, model string) bool {
	// Fast path: read lock for the common case (nothing tracked, or a closed
	// model circuit against a provider that is not indicted).
	cb.mu.RLock()
	models, ok := cb.circuits[providerID.String()]
	if !ok {
		cb.mu.RUnlock()
		return false
	}
	c := models[model]
	if c == nil || c.state != StateOpen {
		open := cb.providerOpen(models)
		cb.mu.RUnlock()
		return open
	}
	if cb.stillDark(c, cb.effectiveCooldown()) {
		cb.mu.RUnlock()
		return true
	}
	cb.mu.RUnlock()

	// Slow path: write lock for the Open→HalfOpen transition. The circuit is
	// re-read after acquiring the write lock, so we operate on the current
	// state and not the snapshot from the RLock phase. If another goroutine
	// transitioned it in between (e.g. RecordSuccess: HalfOpen→Closed), we see
	// the up-to-date state and return the correct result.
	cb.mu.Lock()
	defer cb.mu.Unlock()

	models, ok = cb.circuits[providerID.String()]
	if !ok {
		return false
	}
	if c = models[model]; c != nil && c.state == StateOpen && !cb.stillDark(c, cb.effectiveCooldown()) {
		c.state = StateHalfOpen
		c.halfOpenProbes = 0
		debuglog.Info("circuit-breaker: model state=open→half-open (cooldown elapsed)", "provider", providerName, "provider_id", providerID, "model", model)
	}
	// This circuit is owed a probe and so counts for nothing in the provider
	// verdict: only circuits still inside their cooldown do. That is what lets a
	// provider recover, because a circuit that kept counting after its cooldown
	// elapsed would keep the provider dark and block the very probes that close
	// it. The provider can still be open on the others, which have not changed.
	return cb.providerOpen(models)
}

// RecordFailure records a failed request to one of a provider's models. It
// charges that model's circuit and nothing else: a failure is evidence about
// the model it was routed to, and only the derived provider verdict decides
// whether enough models agree to call the provider itself down.
//   - Closed: increments the failure counter. Opens the circuit if the
//     threshold is reached.
//   - Half-open: immediately re-opens the circuit with a fresh cooldown.
//   - Open: no-op.
func (cb *CircuitBreaker) RecordFailure(providerID uuid.UUID, providerName, model string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c := cb.getOrCreate(providerID.String(), model)
	c.lastCharged = time.Now()

	switch c.state {
	case StateClosed:
		c.consecutiveFails++
		if c.consecutiveFails >= cb.effectiveThreshold() {
			cb.openCircuit("circuit-breaker: model state=closed→open", providerID, providerName, model, c)
		}
	case StateHalfOpen:
		c.consecutiveFails = cb.effectiveThreshold()
		cb.openCircuit("circuit-breaker: model state=half-open→open (probe failed)", providerID, providerName, model, c)
	case StateOpen:
		// Already open — no-op.
	}
}

// openCircuit moves one model circuit to Open, stamps the quota pin that
// governs its cooldown, and tells everything that watches for it.
//
// cooldown_ms and quota_pinned are the operator's only log trail for how long
// this model will be dark and why: a quota pin can hold a circuit open for a
// day, and the failure count alone says nothing about that. Routing metadata
// only — never payload or credentials, and the model id goes last because it
// is the one attribute a request can influence.
//
// Must be called with cb.mu held.
func (cb *CircuitBreaker) openCircuit(msg string, providerID uuid.UUID, providerName, model string, c *circuit) {
	c.state = StateOpen
	c.openedAt = time.Now()
	cb.applyQuotaPin(providerID, c)
	debuglog.Warn(msg, "provider", providerName, "provider_id", providerID, "consecutive_failures", c.consecutiveFails, "cooldown_ms", cb.effectiveCooldownFor(c).Milliseconds(), "quota_pinned", cb.quotaPinnedFor(c), "model", model)
	cb.publishEvent(providerID, providerName, "open", model, c)
	cb.notifyOpen(providerID)
}

// RecordSuccess records a successful request to one of a provider's models. It
// resets that model's circuit only: a model that works says nothing about a
// sibling that does not, and erasing the sibling's streak is exactly how a
// failing model stays in rotation forever.
//   - Closed: resets the failure counter.
//   - Half-open: increments the probe counter. Closes the circuit if
//     enough probes succeed.
func (cb *CircuitBreaker) RecordSuccess(providerID uuid.UUID, providerName, model string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c := cb.getOrCreate(providerID.String(), model)
	c.lastCharged = time.Now()

	switch c.state {
	case StateClosed:
		c.consecutiveFails = 0
	case StateHalfOpen:
		c.halfOpenProbes++
		if c.halfOpenProbes >= cb.HalfOpenMaxProbes {
			c.state = StateClosed
			c.consecutiveFails = 0
			c.halfOpenProbes = 0
			c.cooldownOverride = 0
			debuglog.Info("circuit-breaker: model state=half-open→closed (probe succeeded)", "provider", providerName, "provider_id", providerID, "model", model)
			cb.publishEvent(providerID, providerName, "closed", model, c)
		}
	}
}

// publishEvent fires an SSE event for circuit breaker state transitions.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) publishEvent(providerID uuid.UUID, providerName, state, model string, c *circuit) {
	// quota_pinned reports the override currently governing this circuit, not a
	// claim about whether the circuit is blocking traffic right now — the same
	// predicate ProviderStatus.QuotaPinned uses. With the default
	// HalfOpenMaxProbes of 1 the distinction never surfaces, but a half-open
	// circuit that has banked a probe still carries its override until
	// RecordSuccess closes it.
	pinned := cb.quotaPinnedFor(c)
	providerOpen := cb.providerOpen(cb.circuits[providerID.String()])
	meta := map[string]any{
		"provider_id": providerID.String(),
		"provider":    providerName,
		"model":       model,
		"state":       state,
		// provider_open is the derived verdict as it stands after this
		// transition. The event names one model, and at the default span of 2 the
		// first model to open leaves the provider serving everything else, so
		// without this flag a consumer would have to re-derive the verdict from a
		// span setting it cannot see and circuits it is not shown.
		"provider_open":     providerOpen,
		"consecutive_fails": c.consecutiveFails,
		"quota_pinned":      pinned,
	}
	if pinned {
		// next_retry_at, not "resets_at": this is openedAt plus the ceiling-clamped
		// and jittered pin, i.e. exactly the instant the status API publishes under
		// that name — not the provider's quota reset, which on a weekly plan lies
		// days beyond a 24h-capped pin.
		meta["next_retry_at"] = c.openedAt.Add(c.cooldownOverride).Format(time.RFC3339)
	}
	events.Publish(events.Event{
		Type:     "circuit_breaker." + state,
		Severity: cb.severityForState(state),
		Source:   "failover",
		Message:  breakerEventMessage(providerName, state, model, providerOpen),
		Metadata: meta,
	})
}

// breakerEventMessage is the sentence an operator reads in a dashboard toast and
// in an Apprise alert. It names the model because the breaker charges one model
// circuit at a time: at the default span of 2 the first model to open leaves the
// provider serving everything else, and "Provider X circuit breaker: open" alone
// reports an outage that is not happening. The provider-wide verdict is spelled
// out when it flips, because that is the transition that takes the remaining
// models out of rotation, and it is the part worth acting on.
//
// The model id goes last in the sentence, and the closed form deliberately says
// nothing about the verdict: a recovery says only what recovered.
func breakerEventMessage(providerName, state, model string, providerOpen bool) string {
	msg := fmt.Sprintf("Provider %s circuit breaker: %s", providerName, state)
	if model == "" {
		return msg
	}
	msg += " for model " + model
	if state == "open" && providerOpen {
		msg += " (provider skipped)"
	}
	return msg
}

func (cb *CircuitBreaker) severityForState(state string) string {
	switch state {
	case "open":
		return "warning"
	case "closed":
		return "success"
	default:
		return "info"
	}
}

// applyQuotaPin sets c.cooldownOverride when the provider's quota window is
// spent and resets further out than the normal cooldown. Must be called with
// cb.mu held, immediately after c transitions to Open.
//
// Clamp order is floor, then ceiling, then jitter. Jitter is positive only:
// a negative offset would probe before the window actually resets, which is a
// guaranteed 429 and precisely the waste this exists to avoid. The ceiling is
// applied before jitter, so quotaPinMax() is a pre-jitter cap, not a hard one —
// e.g. the default 24h ceiling can yield up to ~25.2h once jitter is added.
// This order must not be reversed: jittering before capping would let two
// providers pinned at the ceiling collide on the same retry instant, which is
// exactly the fleet stampede jitter exists to prevent.
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
	base := cb.effectiveCooldown()
	if d <= base {
		return // floor: pinning must never make the breaker more aggressive
	}
	if maxPin := cb.quotaPinMax(); d > maxPin {
		d = maxPin
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
// count and simply reverts to the configured cooldown, so HTTP still decides
// recovery through the ordinary half-open probe. That is the whole quota
// contract: quota never opens a circuit, never closes one, never blocks a
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

	// Read once outside the loop: this runs on the quota poll goroutine every
	// few minutes and the value is identical for every circuit.
	base := cb.effectiveCooldown()

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
			cb.releasePin("circuit-breaker: quota pin released (provider no longer exhausted)", id, model, c, base)
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
//
// advice is read, never retained: the caller may hand the same map to the
// advisor afterwards.
func (cb *CircuitBreaker) ApplyQuotaPins(advice map[uuid.UUID]time.Time) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if len(advice) == 0 || !cb.quotaPinEnabled() {
		return 0
	}
	base := cb.effectiveCooldown()
	// Hoisted with base: both read settings, and a cold settings cache turns that
	// into a DB round trip under cb.mu held for write.
	maxPin := cb.quotaPinMax()

	// Walk the advice rather than every circuit, for the same reason
	// ReleaseQuotaPins walks the recovered set: it is the smaller side, and the
	// circuits map is keyed by the provider's UUID string.
	retargeted := 0
	for providerID, resetsAt := range advice {
		for model, c := range cb.circuits[providerID.String()] {
			if cb.logicalStateWith(c, base) != StateOpen {
				continue
			}
			// Measured from openedAt, because that is what the enforced cooldown is
			// measured from. applyQuotaPin computes this from time.Until(resetsAt)
			// instead, which is the same number at the one instant it runs; here
			// openedAt is already in the past, and a pin derived from "time until
			// reset" would expire that much too early and probe before the window
			// rolls over.
			d := resetsAt.Sub(c.openedAt)
			// Ceiling first, so a clamped value is compared against the floors
			// rather than smuggled past them: capping after those checks could
			// shorten a pin that is already longer.
			if d > maxPin {
				d = maxPin
			}
			if d <= base || d <= c.cooldownOverride {
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

	base := cb.effectiveCooldown()

	released := 0
	for id, models := range cb.circuits {
		for model, c := range models {
			if c.cooldownOverride == 0 {
				continue
			}
			cb.releasePin("circuit-breaker: quota pin released (quota polling disabled)", id, model, c, base)
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
// level the half-open→closed recovery uses. Routing metadata only — never
// payload or credentials. Must be called with cb.mu held.
func (cb *CircuitBreaker) releasePin(msg, providerID, model string, c *circuit, base time.Duration) {
	c.cooldownOverride = 0
	debuglog.Info(msg, "provider_id", providerID, "state", cb.logicalState(c).String(), "cooldown_ms", base.Milliseconds(), "model", model)
}

// Status returns the current status of all tracked providers, one row per
// provider built from its most degraded model circuit.
func (cb *CircuitBreaker) Status() []ProviderStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	statuses := make([]ProviderStatus, 0, len(cb.circuits))
	for id, models := range cb.circuits {
		c := cb.dominant(models)
		if c == nil {
			continue
		}
		cooldown := cb.effectiveCooldownFor(c)
		state := cb.logicalState(c)
		providerOpen, openModels := cb.providerReport(models)
		s := ProviderStatus{
			ProviderID:       id,
			State:            state.String(),
			ConsecutiveFails: c.consecutiveFails,
			QuotaPinned:      cb.quotaPinnedFor(c),
			ProviderOpen:     providerOpen,
			OpenModels:       openModels,
		}
		if state == StateOpen && !c.openedAt.IsZero() {
			s.OpenedAt = c.openedAt.Format(time.RFC3339)
			s.CooldownMs = cooldown.Milliseconds()
			nextRetry := c.openedAt.Add(cooldown)
			s.NextRetryAt = nextRetry.Format(time.RFC3339)
		}
		if state == StateHalfOpen && !c.openedAt.IsZero() {
			s.OpenedAt = c.openedAt.Format(time.RFC3339)
		}
		statuses = append(statuses, s)
	}
	return statuses
}

// GetState returns the current state of one provider's circuit for a specific
// resolved upstream model. Returns StateClosed for untracked pairs.
func (cb *CircuitBreaker) GetState(providerID uuid.UUID, model string) State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	c, ok := cb.circuits[providerID.String()][model]
	if !ok {
		return StateClosed
	}
	return cb.logicalState(c)
}

// Reset clears every model circuit of a specific provider and returns the
// logical state the provider's most degraded circuit was in immediately before
// being cleared, so an operator-facing caller can report whether the reset
// actually recovered a sidelined provider or was a no-op.
//
// An untracked provider reports StateClosed: a provider only enters the map
// once it has been routed, and until then it is implicitly healthy. Resetting
// one is therefore harmless and idempotent, not an error.
func (cb *CircuitBreaker) Reset(providerID uuid.UUID) State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c := cb.dominant(cb.circuits[providerID.String()])
	if c == nil {
		return StateClosed
	}
	prev := cb.logicalState(c)
	delete(cb.circuits, providerID.String())
	return prev
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
	cb.mu.Lock()
	defer cb.mu.Unlock()

	for _, models := range cb.circuits {
		cleared += len(models)
		for _, c := range models {
			if cb.logicalState(c) != StateClosed {
				recovered++
			}
		}
	}
	cb.circuits = make(map[string]modelCircuits)
	return cleared, recovered
}
