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

type circuit struct {
	state            State
	consecutiveFails int
	openedAt         time.Time // when the circuit last transitioned to Open
	halfOpenProbes   int       // successful probes in half-open state
	// cooldownOverride replaces the global cooldown for this circuit only. Set
	// when the circuit opens against a provider whose quota window is spent;
	// zero means "use the configured cooldown".
	cooldownOverride time.Duration
}

// ProviderStatus represents the health status of a single provider for
// API responses and SSE events.
type ProviderStatus struct {
	ProviderID       string `json:"provider_id"`
	ProviderName     string `json:"provider_name,omitempty"`
	State            string `json:"state"`
	ConsecutiveFails int    `json:"consecutive_fails"`
	OpenedAt         string `json:"opened_at,omitempty"`
	CooldownMs       int64  `json:"cooldown_ms,omitempty"`
	NextRetryAt      string `json:"next_retry_at,omitempty"`
	QuotaPinned      bool   `json:"quota_pinned,omitempty"`
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

// CircuitBreaker tracks per-provider health and prevents requests to
// consistently failing providers.
type CircuitBreaker struct {
	mu       sync.RWMutex
	circuits map[string]*circuit // keyed by provider UUID string

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

	// Threshold is the number of consecutive failures before opening.
	Threshold int

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
//
// If settings is non-nil, threshold and cooldown are read from it at
// runtime (via "circuit_breaker_threshold" and "circuit_breaker_cooldown").
// Hardcoded defaults are used when settings is nil or a key is missing.
func NewCircuitBreaker(settings SettingsReader) *CircuitBreaker {
	return &CircuitBreaker{
		circuits:          make(map[string]*circuit),
		settings:          settings,
		Threshold:         5,
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

func (cb *CircuitBreaker) getOrCreate(providerID string) *circuit {
	c, ok := cb.circuits[providerID]
	if !ok {
		c = &circuit{state: StateClosed}
		cb.circuits[providerID] = c
	}
	return c
}

// IsOpen returns true if the circuit breaker is preventing requests to
// this provider. It also handles the Open → Half-Open transition when
// the cooldown has elapsed.
//
// Fast path: most calls hit the Closed state, which only needs a read lock.
// Only the Open→HalfOpen transition requires a write lock.
func (cb *CircuitBreaker) IsOpen(providerID uuid.UUID, providerName string) bool {
	// Fast path: read lock for the common case (StateClosed or unknown).
	cb.mu.RLock()
	c, ok := cb.circuits[providerID.String()]
	if !ok || c.state == StateClosed {
		cb.mu.RUnlock()
		return false
	}
	// Need to inspect state more closely — if HalfOpen, also fast path.
	if c.state == StateHalfOpen {
		cb.mu.RUnlock()
		return false
	}
	cb.mu.RUnlock()

	// Slow path: write lock for potential Open→HalfOpen transition.
	// We re-read the circuit via getOrCreate after acquiring the write lock,
	// which ensures we operate on the current state — not the snapshot from
	// the RLock phase. If another goroutine transitioned the state between
	// our RUnlock and Lock (e.g. RecordSuccess: HalfOpen→Closed), we see
	// the up-to-date state and return the correct result.
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c = cb.getOrCreate(providerID.String())

	switch c.state {
	case StateClosed:
		return false
	case StateOpen:
		if time.Since(c.openedAt) >= cb.effectiveCooldownFor(c) {
			c.state = StateHalfOpen
			c.halfOpenProbes = 0
			debuglog.Info("circuit-breaker: provider state=open→half-open (cooldown elapsed)", "provider", providerName, "provider_id", providerID)
			return false // allow probe through
		}
		return true
	case StateHalfOpen:
		return false // allow probe through
	default:
		return false
	}
}

// RecordFailure records a failed request to a provider.
//   - Closed: increments the failure counter. Opens the circuit if the
//     threshold is reached.
//   - Half-open: immediately re-opens the circuit with a fresh cooldown.
//   - Open: no-op.
func (cb *CircuitBreaker) RecordFailure(providerID uuid.UUID, providerName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c := cb.getOrCreate(providerID.String())

	switch c.state {
	case StateClosed:
		c.consecutiveFails++
		if c.consecutiveFails >= cb.effectiveThreshold() {
			c.state = StateOpen
			c.openedAt = time.Now()
			cb.applyQuotaPin(providerID, c)
			// cooldown_ms and quota_pinned are the operator's only log trail for
			// how long this provider will be dark and why: a quota pin can hold a
			// circuit open for a day, and the failure count alone says nothing
			// about that. Routing metadata only — never payload or credentials.
			debuglog.Warn("circuit-breaker: provider state=closed→open", "provider", providerName, "provider_id", providerID, "consecutive_failures", c.consecutiveFails, "cooldown_ms", cb.effectiveCooldownFor(c).Milliseconds(), "quota_pinned", cb.quotaPinnedFor(c))
			cb.publishEvent(providerID, providerName, "open", c)
			cb.notifyOpen(providerID)
		}
	case StateHalfOpen:
		c.state = StateOpen
		c.openedAt = time.Now()
		c.consecutiveFails = cb.effectiveThreshold()
		cb.applyQuotaPin(providerID, c)
		debuglog.Warn("circuit-breaker: provider state=half-open→open (probe failed)", "provider", providerName, "provider_id", providerID, "cooldown_ms", cb.effectiveCooldownFor(c).Milliseconds(), "quota_pinned", cb.quotaPinnedFor(c))
		cb.publishEvent(providerID, providerName, "open", c)
		cb.notifyOpen(providerID)
	case StateOpen:
		// Already open — no-op.
	}
}

// RecordSuccess records a successful request to a provider.
//   - Closed: resets the failure counter.
//   - Half-open: increments the probe counter. Closes the circuit if
//     enough probes succeed.
func (cb *CircuitBreaker) RecordSuccess(providerID uuid.UUID, providerName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c := cb.getOrCreate(providerID.String())

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
			debuglog.Info("circuit-breaker: provider state=half-open→closed (probe succeeded)", "provider", providerName, "provider_id", providerID)
			cb.publishEvent(providerID, providerName, "closed", c)
		}
	}
}

// publishEvent fires an SSE event for circuit breaker state transitions.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) publishEvent(providerID uuid.UUID, providerName, state string, c *circuit) {
	// quota_pinned reports the override currently governing this circuit, not a
	// claim about whether the circuit is blocking traffic right now — the same
	// predicate ProviderStatus.QuotaPinned uses. With the default
	// HalfOpenMaxProbes of 1 the distinction never surfaces, but a half-open
	// circuit that has banked a probe still carries its override until
	// RecordSuccess closes it.
	pinned := cb.quotaPinnedFor(c)
	meta := map[string]any{
		"provider_id":       providerID.String(),
		"provider":          providerName,
		"state":             state,
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
		Message:  fmt.Sprintf("Provider %s circuit breaker: %s", providerName, state),
		Metadata: meta,
	})
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

// quotaPinnedFor reports whether a quota pin is actually governing this circuit
// right now. The kill switch is deliberately re-read here rather than only at
// the moment a circuit opens: an operator who disables quota pinning to recover
// a provider sidelined for hours expects every pin already in force to be
// released at once, not only the circuits that open afterwards. It is the
// fleet-wide lever; Reset is the per-provider one.
//
// Every surface derives from this one predicate — the cooldown the breaker
// enforces, the CooldownMs/NextRetryAt the status API publishes, and the
// quota_pinned flag beside them — so the number and the explanation can never
// disagree.
func (cb *CircuitBreaker) quotaPinnedFor(c *circuit) bool {
	return c != nil && c.cooldownOverride > 0 && cb.quotaPinEnabled()
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
		c, ok := cb.circuits[id]
		if !ok || c.cooldownOverride == 0 {
			continue
		}
		cb.releasePin("circuit-breaker: quota pin released (provider no longer exhausted)", id, c, base)
		released++
	}
	return released
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
	for id, c := range cb.circuits {
		if c.cooldownOverride == 0 {
			continue
		}
		cb.releasePin("circuit-breaker: quota pin released (quota polling disabled)", id, c, base)
		released++
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
func (cb *CircuitBreaker) releasePin(msg, providerID string, c *circuit, base time.Duration) {
	c.cooldownOverride = 0
	debuglog.Info(msg, "provider_id", providerID, "state", cb.logicalState(c).String(), "cooldown_ms", base.Milliseconds())
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
	if c.state == StateOpen && !c.openedAt.IsZero() && time.Since(c.openedAt) >= cb.effectiveCooldownFor(c) {
		return StateHalfOpen
	}
	return c.state
}

// Status returns the current status of all tracked providers.
func (cb *CircuitBreaker) Status() []ProviderStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	statuses := make([]ProviderStatus, 0, len(cb.circuits))
	for id, c := range cb.circuits {
		cooldown := cb.effectiveCooldownFor(c)
		state := cb.logicalState(c)
		s := ProviderStatus{
			ProviderID:       id,
			State:            state.String(),
			ConsecutiveFails: c.consecutiveFails,
			QuotaPinned:      cb.quotaPinnedFor(c),
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

// GetState returns the current state for a specific provider.
// Returns StateClosed for unknown providers.
func (cb *CircuitBreaker) GetState(providerID uuid.UUID) State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	c, ok := cb.circuits[providerID.String()]
	if !ok {
		return StateClosed
	}
	return cb.logicalState(c)
}

// Reset clears the circuit breaker state for a specific provider and returns
// the logical state the circuit was in immediately before being cleared, so an
// operator-facing caller can report whether the reset actually recovered a
// sidelined provider or was a no-op.
//
// An untracked provider reports StateClosed: a provider only enters the map
// once it has been routed, and until then it is implicitly healthy. Resetting
// one is therefore harmless and idempotent, not an error.
func (cb *CircuitBreaker) Reset(providerID uuid.UUID) State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, ok := cb.circuits[providerID.String()]
	if !ok {
		return StateClosed
	}
	prev := cb.logicalState(c)
	delete(cb.circuits, providerID.String())
	return prev
}

// ResetAll clears all circuit breaker state. It returns how many circuits were
// discarded in total and how many of those were actually sidelining their
// provider (logically open or half-open), so a bulk reset can report what it
// recovered instead of implying every tracked provider was broken.
func (cb *CircuitBreaker) ResetAll() (cleared, recovered int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cleared = len(cb.circuits)
	for _, c := range cb.circuits {
		if cb.logicalState(c) != StateClosed {
			recovered++
		}
	}
	cb.circuits = make(map[string]*circuit)
	return cleared, recovered
}
