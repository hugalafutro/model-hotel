package failover

import (
	"context"
	"fmt"
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
}

// SettingsReader provides dynamic configuration for the circuit breaker.
// This decouples the breaker from the settings package — callers inject
// a thin shim that reads from their settings repository.
type SettingsReader interface {
	GetInt(ctx context.Context, key string, defaultValue int) int
	GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration
}

// CircuitBreaker tracks per-provider health and prevents requests to
// consistently failing providers.
type CircuitBreaker struct {
	mu       sync.RWMutex
	circuits map[string]*circuit // keyed by provider UUID string

	// settings provides runtime-configurable threshold and cooldown.
	settings SettingsReader

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
		if time.Since(c.openedAt) >= cb.effectiveCooldown() {
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
			debuglog.Warn("circuit-breaker: provider state=closed→open", "provider", providerName, "provider_id", providerID, "consecutive_failures", c.consecutiveFails)
			cb.publishEvent(providerID, providerName, "open", c)
		}
	case StateHalfOpen:
		c.state = StateOpen
		c.openedAt = time.Now()
		c.consecutiveFails = cb.effectiveThreshold()
		debuglog.Warn("circuit-breaker: provider state=half-open→open (probe failed)", "provider", providerName, "provider_id", providerID)
		cb.publishEvent(providerID, providerName, "open", c)
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
			debuglog.Info("circuit-breaker: provider state=half-open→closed (probe succeeded)", "provider", providerName, "provider_id", providerID)
			cb.publishEvent(providerID, providerName, "closed", c)
		}
	}
}

// publishEvent fires an SSE event for circuit breaker state transitions.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) publishEvent(providerID uuid.UUID, providerName, state string, c *circuit) {
	events.Publish(events.Event{
		Type:     "circuit_breaker." + state,
		Severity: cb.severityForState(state),
		Source:   "failover",
		Message:  fmt.Sprintf("Provider %s circuit breaker: %s", providerName, state),
		Metadata: map[string]interface{}{
			"provider_id":       providerID.String(),
			"provider":          providerName,
			"state":             state,
			"consecutive_fails": c.consecutiveFails,
		},
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

// Status returns the current status of all tracked providers.
func (cb *CircuitBreaker) Status() []ProviderStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	cooldown := cb.effectiveCooldown()
	statuses := make([]ProviderStatus, 0, len(cb.circuits))
	for id, c := range cb.circuits {
		// Apply the same logical cooldown transition as GetState: an open
		// circuit whose cooldown has elapsed is "ready to probe" and is
		// reported as half-open, even though the internal state only flips to
		// StateHalfOpen for the brief duration of an in-flight probe request.
		// Without this the half-open bucket is effectively unobservable from
		// the status API (and the sidebar badge's middle count never moves).
		state := c.state
		if state == StateOpen && !c.openedAt.IsZero() && time.Since(c.openedAt) >= cooldown {
			state = StateHalfOpen
		}
		s := ProviderStatus{
			ProviderID:       id,
			State:            state.String(),
			ConsecutiveFails: c.consecutiveFails,
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

	// Check if an open circuit should transition to half-open
	if c.state == StateOpen && time.Since(c.openedAt) >= cb.effectiveCooldown() {
		return StateHalfOpen // logical state, don't mutate
	}
	return c.state
}

// Reset clears the circuit breaker state for a specific provider.
func (cb *CircuitBreaker) Reset(providerID uuid.UUID) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.circuits, providerID.String())
}

// ResetAll clears all circuit breaker state.
func (cb *CircuitBreaker) ResetAll() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.circuits = make(map[string]*circuit)
}
