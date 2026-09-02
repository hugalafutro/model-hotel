package failover

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/metrics"
)

// State represents the health state of a single provider endpoint.
type State int

// Circuit breaker states.
const (
	StateClosed   State = iota // requests pass through
	StateOpen                  // requests are skipped
	StateHalfOpen              // limited probe requests allowed
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

// ProviderStatus is the health status of a single provider for API responses
// and SSE events.
//
// State and the fields beside it describe the provider's most degraded model
// circuit. ProviderOpen and OpenModels describe the provider as a whole. The
// two can disagree: one open model at the default span of 2 gives State "open"
// and ProviderOpen false.
type ProviderStatus struct {
	ProviderID       string `json:"provider_id"`
	ProviderName     string `json:"provider_name,omitempty"`
	State            string `json:"state"`
	ConsecutiveFails int    `json:"consecutive_fails"`
	OpenedAt         string `json:"opened_at,omitempty"`
	CooldownMs       int64  `json:"cooldown_ms,omitempty"`
	NextRetryAt      string `json:"next_retry_at,omitempty"`
	QuotaPinned      bool   `json:"quota_pinned,omitempty"`
	// PinSource says where the dominant circuit's quota pin came from:
	// "advisor" (a measured quota reading) or "response" (inferred from the
	// exhausted 429 itself). Empty when that circuit carries no pin.
	PinSource string `json:"pin_source,omitempty"`
	// BackedOff is true when the cooldown governing the circuit is the probe
	// backoff rather than circuit_breaker_cooldown: CooldownMs and NextRetryAt
	// are then the doubled value. FailedProbes is the count behind it, the
	// half-open probes that failed since the circuit last closed, and is
	// reported even when backoff is switched off.
	BackedOff    bool `json:"backed_off,omitempty"`
	FailedProbes int  `json:"failed_probes,omitempty"`
	// ProviderOpen is the derived provider-wide verdict: whether the breaker is
	// skipping this provider for every model. Always emitted, including when
	// false, so a consumer never re-derives it from OpenModels and the span
	// setting.
	ProviderOpen bool `json:"provider_open"`
	// OpenModels lists the resolved upstream model ids the breaker is currently
	// blocking, sorted so a polling UI does not reshuffle the list. It is the
	// set ProviderOpen is counted from. Circuits owed a probe are excluded:
	// they block nothing.
	OpenModels []string `json:"open_models,omitempty"`
	// Circuits lists every circuit the row above is built from, sorted by
	// model, each with its own state, cooldown and last verdict. Filled only by
	// StatusDetail.
	Circuits []CircuitStatus `json:"circuits,omitempty"`
}

// SettingsReader provides dynamic configuration for the circuit breaker.
// Callers inject a thin shim over their settings repository.
type SettingsReader interface {
	GetInt(ctx context.Context, key string, defaultValue int) int
	GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration
	GetBool(ctx context.Context, key string, defaultValue bool) bool
}

// QuotaAdvisor supplies the reset deadline for a provider whose quota window is
// spent. Implementations must be non-blocking: this is consulted while the
// breaker holds its write lock on the request path. They must not call back
// into the CircuitBreaker (e.g. GetState, Status): cb.mu is held exclusively
// across the call and any such re-entry self-deadlocks.
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

	// onOpen is notified whenever a circuit transitions to Open, letting a
	// consumer refresh what it knows about the provider. Nil when nothing is
	// wired.
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
//   - Cooldown: 60 seconds, doubled per failed probe up to 1 hour
//   - HalfOpenMaxProbes: 1 success to close
//   - SpanModels: 2 open model circuits to call the provider down
//
// If settings is non-nil, threshold, cooldown, span and the probe backoff are
// read from it at runtime (via "circuit_breaker_threshold",
// "circuit_breaker_cooldown", "circuit_breaker_span_models",
// "circuit_breaker_backoff_enabled" and "circuit_breaker_backoff_max").
// Hardcoded defaults are used when settings is nil or a key is missing.
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
// cb.mu and never adds latency to the request that opened the circuit. Must be
// called with cb.mu held.
func (cb *CircuitBreaker) notifyOpen(providerID uuid.UUID) {
	if cb.onOpen == nil {
		return
	}
	fn := cb.onOpen
	go fn(providerID)
}

// afterUnlock collects what a locked section wants emitted once cb.mu is
// released: log lines and bus events. The app-log sink can block for seconds
// per line on a backed-up store, and cb.mu is the lock every request's IsOpen
// takes, so nothing may write a line while holding it. Every value a line or
// event carries is computed under the lock; only the write waits. Used as
// `defer func() { cb.mu.Unlock(); after.run() }()`. Emission order across
// goroutines is not transition order, so events carry the timestamp taken
// under the lock.
type afterUnlock []func()

func (a *afterUnlock) add(f func()) { *a = append(*a, f) }

func (a afterUnlock) run() {
	for _, f := range a {
		f()
	}
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
	// Read lock for the common case: nothing tracked, or a closed model circuit
	// against a provider that is not indicted.
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
	if cb.stillDark(c, cb.cooldowns()) {
		cb.mu.RUnlock()
		return true
	}
	cb.mu.RUnlock()

	// Write lock for the Open→HalfOpen transition. The circuit is re-read under
	// the write lock, so a transition by another goroutine in between (e.g.
	// RecordSuccess: HalfOpen→Closed) is seen rather than the RLock snapshot.
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	models, ok = cb.circuits[providerID.String()]
	if !ok {
		return false
	}
	if c = models[model]; c != nil && c.state == StateOpen && !cb.stillDark(c, cb.cooldowns()) {
		c.state = StateHalfOpen
		c.halfOpenProbes = 0
		after.add(func() {
			debuglog.Info("circuit-breaker: model state=open→half-open (cooldown elapsed)", "provider", providerName, "provider_id", providerID, "model", model)
		})
	}
	// A circuit owed a probe counts for nothing in the provider verdict: only
	// circuits still inside their cooldown do, which is what lets a provider
	// recover. The provider can still be open on the others.
	return cb.providerOpen(models)
}

// RecordFailure records a failed request to one of a provider's models. It
// charges that model's circuit and nothing else; only the derived provider
// verdict decides whether enough models agree to call the provider down.
//   - Closed: increments the failure counter. Opens the circuit if the
//     threshold is reached.
//   - Half-open: immediately re-opens the circuit with a fresh cooldown, doubled
//     for every probe that has failed since the circuit last closed.
//   - Open: no-op.
//
// cause is remembered as the circuit's last verdict and published with the open
// it may produce.
func (cb *CircuitBreaker) RecordFailure(providerID uuid.UUID, providerName, model string, cause Cause) {
	cb.recordFailure(providerID, providerName, model, false, cause)
}

// RecordRateLimited is RecordFailure for an UNCLASSIFIED 429: it charges
// identically and additionally feeds the rate-limit-open streak, so a circuit
// that only ever opens on 429s escalates to exhausted handling without any
// provider phrase saying so (see circuit.note429Open). Classified cases go
// elsewhere: a saturated 429 to RecordSaturated, an exhausted one to
// RecordExhausted. cause says which reading of the 429 the caller reached.
func (cb *CircuitBreaker) RecordRateLimited(providerID uuid.UUID, providerName, model string, cause Cause) {
	cb.recordFailure(providerID, providerName, model, true, cause)
}

func (cb *CircuitBreaker) recordFailure(providerID uuid.UUID, providerName, model string, by429 bool, cause Cause) {
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	c := cb.getOrCreate(providerID.String(), model)
	c.lastCharged = time.Now()
	// Stamped before the switch so an open transition logs and publishes the
	// verdict that produced it.
	c.note(c.lastCharged, cause)

	switch c.state {
	case StateClosed:
		c.consecutiveFails++
		if c.consecutiveFails >= cb.effectiveThreshold() {
			cb.openCircuit(&after, "circuit-breaker: model state=closed→open", providerID, providerName, model, c, by429, 0)
		}
	case StateHalfOpen:
		c.consecutiveFails = cb.effectiveThreshold()
		// Counted before the open so the cooldown stamped there reflects this
		// failed probe.
		c.failedProbes++
		cb.openCircuit(&after, "circuit-breaker: model state=half-open→open (probe failed)", providerID, providerName, model, c, by429, 0)
	case StateOpen:
		// Already open, no-op.
	}
}

// RecordExhausted records a 429 whose body says the window or balance behind
// this model is SPENT. One such response opens the circuit outright: the charge
// jumps to the threshold the way a failed half-open probe does.
//
// pinHint is the cooldown the response itself suggests (a dated Retry-After, or
// the matched phrase's per-marker default); zero means none. It reaches the
// cooldown through the same clamps an advisor pin gets, and a real advisor
// reading beats it (see applyQuotaPin).
//
// status is the status the upstream answered. It is a parameter rather than a
// constant 429 because a 402 payment_required makes the same exhaustion claim
// and takes the same pin, and the status is stamped into the circuit's cause,
// the breaker-open log line and the opens_total metric.
func (cb *CircuitBreaker) RecordExhausted(providerID uuid.UUID, providerName, model string, status int, pinHint time.Duration) {
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	c := cb.getOrCreate(providerID.String(), model)
	c.lastCharged = time.Now()
	c.note(c.lastCharged, UpstreamStatus(status, causeExhausted))

	// by429 feeds the rate-limit-only open streak, so it follows the status the
	// provider sent rather than the fact that this is the exhaustion path. A 402
	// is an exhaustion but not a rate limit, and counting it would let repeated
	// payment refusals escalate a streak defined as 429-only into a probe
	// backoff that no quota lever clears.
	by429 := status == 429

	switch c.state {
	case StateClosed:
		c.consecutiveFails = cb.effectiveThreshold()
		cb.openCircuit(&after, "circuit-breaker: model state=closed→open (exhausted)", providerID, providerName, model, c, by429, pinHint)
	case StateHalfOpen:
		c.consecutiveFails = cb.effectiveThreshold()
		c.failedProbes++
		cb.openCircuit(&after, "circuit-breaker: model state=half-open→open (probe drew exhaustion)", providerID, providerName, model, c, by429, pinHint)
	case StateOpen:
		// Already open, no-op. ApplyQuotaPins retargets an open circuit when a
		// fresh quota reading lands; a second exhausted body is not fresher
		// evidence than the one that opened it.
	}
}

// RecordSaturated remembers a 429 the classifier read as load rather than a
// spent window. It is deliberately neither a charge nor a credit: a provider at
// its concurrency ceiling is alive. It only leaves the verdict on the circuit,
// so a status row can say "busy" about a provider whose circuit is closed but
// whose last answers were 429s.
//
// It does not touch lastCharged, so a circuit that exists only because of this
// stamp ranks first for eviction, behind every circuit a charge or credit
// reached.
func (cb *CircuitBreaker) RecordSaturated(providerID uuid.UUID, model string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.getOrCreate(providerID.String(), model).note(time.Now(), UpstreamStatus(429, causeSaturated))
}

// BlockedUntil reports when the breaker next lets a request at this
// (provider, model) pair through, and whether every circuit blocking it is
// quota-pinned. It answers for the same rule IsOpen enforces: the model's own
// blocking circuit when it has one, else the provider-wide verdict's blocking
// set (whose earliest expiry is when the verdict can lapse). ok is false when
// nothing is blocking the pair: the caller should not have skipped it.
//
// It backs the all-skipped response: "no available provider" with a Retry-After
// naming the earliest of these instants, and an exhausted kind only when every
// blocking circuit is pinned (spent windows), else saturated.
func (cb *CircuitBreaker) BlockedUntil(providerID uuid.UUID, model string) (retryAt time.Time, pinned, ok bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	models := cb.circuits[providerID.String()]
	if len(models) == 0 {
		return time.Time{}, false, false
	}
	r := cb.cooldowns()
	if c := models[model]; c != nil && cb.blocking(c, r) {
		return c.openedAt.Add(cb.effectiveCooldownForWith(c, r)), cb.quotaPinnedForWith(c, r), true
	}
	open, blocked, _ := cb.providerReport(models, r)
	if !open || len(blocked) == 0 {
		return time.Time{}, false, false
	}
	pinned = true
	for _, m := range blocked {
		c := models[m]
		at := c.openedAt.Add(cb.effectiveCooldownForWith(c, r))
		if retryAt.IsZero() || at.Before(retryAt) {
			retryAt = at
		}
		if !cb.quotaPinnedForWith(c, r) {
			pinned = false
		}
	}
	return retryAt, pinned, true
}

// LastSuccessWithin reports whether this (provider, model) circuit served a
// success inside the window. It backs the 429 behavioural fallback: a rate
// limit from a model that answered moments ago is load, not a spent window. An
// untracked pair reports false.
func (cb *CircuitBreaker) LastSuccessWithin(providerID uuid.UUID, model string, window time.Duration) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	c, ok := cb.circuits[providerID.String()][model]
	if !ok || c.lastSuccess.IsZero() {
		return false
	}
	return time.Since(c.lastSuccess) <= window
}

// openCircuit moves one model circuit to Open, stamps the probe backoff and
// then the quota pin that govern its cooldown, and tells everything that
// watches for it.
//
// cooldown_ms, quota_pinned, backed_off and failed_probes are the operator's
// log trail for how long this model stays dark and why. Routing metadata only,
// never payload or credentials, and the model id goes last because it is the
// one attribute a request can influence.
//
// by429 feeds the rate-limit-open streak (note429Open) BEFORE the backoff is
// stamped, so the escalation's lifted ceiling applies to the open that earned
// it. exhaustHint is the response-derived pin suggestion for an exhausted 429,
// zero everywhere else.
//
// Must be called with cb.mu held; the line and the event it produces are
// handed to after, for the caller to emit once the lock is released.
func (cb *CircuitBreaker) openCircuit(after *afterUnlock, msg string, providerID uuid.UUID, providerName, model string, c *circuit, by429 bool, exhaustHint time.Duration) {
	now := time.Now()
	c.state = StateOpen
	c.openedAt = now
	c.note429Open(now, by429)
	// Backoff first: the pin is floored at the cooldown in force, and that is
	// the backoff once one is stamped.
	cb.applyBackoff(c)
	cb.applyQuotaPin(providerID, c, exhaustHint)
	// One walk for the log line and the event, so the two cannot disagree. The
	// attributes are read under the lock, because the circuit keeps changing
	// once it is released.
	r := cb.cooldowns()
	attrs := []any{"provider", providerName, "provider_id", providerID, "cause", c.lastCause, "status", c.lastStatus, "consecutive_failures", c.consecutiveFails, "cooldown_ms", cb.effectiveCooldownForWith(c, r).Milliseconds(), "quota_pinned", cb.quotaPinnedForWith(c, r), "backed_off", cb.backedOffForWith(c, r), "failed_probes", c.failedProbes, "model", model}
	cause := c.lastCause
	after.add(func() {
		debuglog.Warn(msg, attrs...)
		metrics.RecordBreakerOpen(providerName, model, cause)
	})
	cb.publishEvent(after, providerID, providerName, "open", model, c, r)
	if c.noteOpen(now) {
		opens := c.opens
		after.add(func() { cb.reportUnstable(providerID, providerName, model, opens) })
	}
	// After the line and the event: the callback is the quota refresh, whose
	// retarget line must not precede the open it retargets.
	after.add(func() { cb.notifyOpen(providerID) })
}

// reportUnstable says once that a model has opened its circuit repeatedly inside
// one window, so a chronically broken model stops being invisible.
//
// It reports rather than retires: the failures counted here (timeouts, 5xx) say
// only "upstream is unwell", not that the provider stopped serving the model.
// It cannot nominate the model to the retirement probe either, because that
// probe skips models whose circuit is open (see model_gone.go), which is this
// model's state whenever this fires.
//
// Routing metadata only, and the model id goes last because it is the one
// attribute a request can influence. Reads nothing from the breaker, so it
// runs after the lock is released.
func (cb *CircuitBreaker) reportUnstable(providerID uuid.UUID, providerName, model string, opens int) {
	debuglog.Warn("circuit-breaker: model keeps reopening its circuit",
		"provider", providerName, "provider_id", providerID,
		"opens", opens, "window", reopenWindowLabel, "model", model)
	events.Publish(events.Event{
		Type:     "circuit_breaker.unstable",
		Severity: "warning",
		Source:   "failover",
		// The counter says how many times the circuit opened, not what the model
		// did in between: a success does not reset it, so recovered blips land
		// here too.
		Message: fmt.Sprintf("%s on %s opened its circuit %d times in %s",
			model, providerName, opens, reopenWindowLabel),
		Metadata: map[string]any{
			"provider_id": providerID.String(),
			"provider":    providerName,
			"model":       model,
			// model_id repeats model as the identity the alert dispatcher
			// debounces on. Without it the most specific key is provider_id, and
			// two models crossing the threshold on one provider inside the
			// dispatcher's cooldown collapse to a single alert.
			"model_id": model,
			"opens":    opens,
			"window":   reopenWindowLabel,
		},
	})
}

// RecordSuccess records a successful request to one of a provider's models. It
// resets that model's circuit only: a model that works says nothing about a
// sibling that does not.
//   - Closed: resets the failure counter.
//   - Half-open: increments the probe counter. Closes the circuit if
//     enough probes succeed.
func (cb *CircuitBreaker) RecordSuccess(providerID uuid.UUID, providerName, model string) {
	cb.recordSuccess(providerID, providerName, model, true, Cause{Reason: causeSuccess})
}

// RecordAlive is RecordSuccess for a response that proves the provider ALIVE
// without having served anything: a non-failover-eligible non-2xx (a plain 400,
// a 422). It credits the circuit identically but does not stamp lastSuccess,
// which the 429 behavioural fallback reads as "this model SERVED moments ago";
// a client's malformed payloads must not classify a spent window as busy.
// status is that response's status, remembered as the verdict.
func (cb *CircuitBreaker) RecordAlive(providerID uuid.UUID, providerName, model string, status int) {
	cb.recordSuccess(providerID, providerName, model, false, UpstreamStatus(status, causeAlive))
}

func (cb *CircuitBreaker) recordSuccess(providerID uuid.UUID, providerName, model string, served bool, cause Cause) {
	cb.mu.Lock()
	var after afterUnlock
	defer func() { cb.mu.Unlock(); after.run() }()

	c := cb.getOrCreate(providerID.String(), model)
	c.lastCharged = time.Now()
	c.note(c.lastCharged, cause)
	if served {
		c.lastSuccess = c.lastCharged
	}

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
			c.pinSource = ""
			// A succeeded probe clears the backoff: the next open is a fresh
			// incident and starts from the base cooldown. The 429-open streak
			// clears with it.
			c.failedProbes = 0
			c.cooldownBackoff = 0
			c.clear429Escalation()
			after.add(func() {
				debuglog.Info("circuit-breaker: model state=half-open→closed (probe succeeded)", "provider", providerName, "provider_id", providerID, "model", model)
			})
			cb.publishEvent(&after, providerID, providerName, "closed", model, c, cb.cooldowns())
		}
	}
}

// Status returns the current status of all tracked providers, one row per
// provider built from its most degraded model circuit, without the per-circuit
// list. The Prometheus scrape and the aggregate status poll read it.
func (cb *CircuitBreaker) Status() []ProviderStatus {
	return cb.status(false)
}

// StatusDetail is Status with Circuits filled in on every row: one entry per
// circuit, sorted by model, with its own state, wait and last verdict. Only the
// detail endpoint asks for it, because building the list allocates per circuit
// under the lock the request path takes.
func (cb *CircuitBreaker) StatusDetail() []ProviderStatus {
	return cb.status(true)
}

func (cb *CircuitBreaker) status(detail bool) []ProviderStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// One walk's settings reads for the whole scan, not per circuit: this holds
	// the lock the request path takes, and an unoverridden key has no settings
	// row to cache, so every read is a DB round trip.
	r := cb.cooldowns()

	statuses := make([]ProviderStatus, 0, len(cb.circuits))
	for id, models := range cb.circuits {
		c := cb.dominant(models, r)
		if c == nil {
			continue
		}
		cooldown := cb.effectiveCooldownForWith(c, r)
		state := cb.logicalStateWith(c, r)
		// quotaPinned comes from this walk rather than from the dominant circuit:
		// the verdict's pin arm is "any blocking circuit is pinned", and the
		// dominant circuit alone would misreport a provider waiting out a quota
		// window on a sibling model.
		providerOpen, openModels, quotaPinned := cb.providerReport(models, r)
		s := ProviderStatus{
			ProviderID:       id,
			State:            state.String(),
			ConsecutiveFails: c.consecutiveFails,
			QuotaPinned:      quotaPinned,
			PinSource:        cb.pinSourceForWith(c, r),
			// From the dominant circuit, like CooldownMs and NextRetryAt: these
			// explain those two numbers, so they come from the same circuit.
			BackedOff:    cb.backedOffForWith(c, r),
			FailedProbes: c.failedProbes,
			ProviderOpen: providerOpen,
			OpenModels:   openModels,
		}
		if detail {
			s.Circuits = cb.circuitStatuses(models, r)
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
