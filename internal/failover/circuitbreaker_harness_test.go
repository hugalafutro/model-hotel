package failover

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// Fixtures the circuit-breaker tests share.

func newTestCB(threshold int, cooldown time.Duration) *CircuitBreaker {
	cb := NewCircuitBreaker(nil)
	cb.Threshold = threshold
	cb.Cooldown = cooldown
	cb.HalfOpenMaxProbes = 1
	return cb
}

// stubSettings implements SettingsReader for tests.
type stubSettings struct {
	threshold int
	cooldown  time.Duration
	// span overrides circuit_breaker_span_models when non-zero. Negative values
	// are passed through so the fallback to the default can be asserted.
	span int
	// pinEnabled overrides circuit_breaker_quota_pin_enabled when non-nil.
	pinEnabled *bool
	// pinMax overrides circuit_breaker_quota_pin_max when positive.
	pinMax time.Duration
	// backoffEnabled overrides circuit_breaker_backoff_enabled when non-nil.
	// A pointer, so a test can flip it after the breaker has stamped a backoff.
	backoffEnabled *bool
	// backoffMax overrides circuit_breaker_backoff_max when positive.
	backoffMax time.Duration
}

func (s *stubSettings) GetInt(_ context.Context, key string, def int) int {
	if key == "circuit_breaker_threshold" && s.threshold > 0 {
		return s.threshold
	}
	if key == "circuit_breaker_span_models" && s.span != 0 {
		return s.span
	}
	return def
}

func (s *stubSettings) GetDuration(_ context.Context, key string, def time.Duration) time.Duration {
	if key == "circuit_breaker_cooldown" && s.cooldown > 0 {
		return s.cooldown
	}
	if key == "circuit_breaker_quota_pin_max" && s.pinMax > 0 {
		return s.pinMax
	}
	if key == "circuit_breaker_backoff_max" && s.backoffMax > 0 {
		return s.backoffMax
	}
	return def
}

func (s *stubSettings) GetBool(_ context.Context, key string, def bool) bool {
	if key == "circuit_breaker_quota_pin_enabled" && s.pinEnabled != nil {
		return *s.pinEnabled
	}
	if key == "circuit_breaker_backoff_enabled" && s.backoffEnabled != nil {
		return *s.backoffEnabled
	}
	return def
}

// countingSettings answers every key with the caller's default, like a
// deployment that has never overridden a breaker setting, and counts the reads.
// That is the shape the read cost matters in: with no row to serve, each read is
// an uncached DB round trip taken under the breaker's lock.
type countingSettings struct {
	mu        sync.Mutex
	durations map[string]int
	booleans  map[string]int
}

func newCountingSettings() *countingSettings {
	return &countingSettings{durations: make(map[string]int), booleans: make(map[string]int)}
}

func (s *countingSettings) GetInt(_ context.Context, _ string, def int) int { return def }

func (s *countingSettings) GetDuration(_ context.Context, key string, def time.Duration) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durations[key]++
	return def
}

func (s *countingSettings) GetBool(_ context.Context, key string, def bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.booleans[key]++
	return def
}

// reset drops the reads taken during setup, so a count describes one call.
func (s *countingSettings) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durations = make(map[string]int)
	s.booleans = make(map[string]int)
}

func (s *countingSettings) reads(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.durations[key]
}

func (s *countingSettings) bools(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.booleans[key]
}

type stubAdvisor struct {
	at time.Time
	ok bool
}

func (s stubAdvisor) ResetsAt(uuid.UUID) (time.Time, bool) { return s.at, s.ok }

// openBreaker drives a fresh breaker to the Open state using real failures.
func openBreaker(t *testing.T, cb *CircuitBreaker, id uuid.UUID) {
	t.Helper()
	for i := 0; i < cb.effectiveThreshold(); i++ {
		cb.RecordFailure(id, "test-provider", "", Cause{})
	}
	if got := cb.GetState(id, ""); got != StateOpen {
		t.Fatalf("setup: got state %v, want open", got)
	}
}

// waitForOpenEvent reads from sub until it sees a "circuit_breaker.open" event
// whose provider_id metadata matches id, or the deadline elapses. Filtering by
// type and provider makes the assertion deterministic even though the shared
// DefaultBus channel could otherwise carry events from a different provider
// or a different transition first.
func waitForOpenEvent(t *testing.T, sub chan events.Event, id uuid.UUID) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type != "circuit_breaker.open" {
				continue
			}
			if pid, _ := ev.Metadata["provider_id"].(string); pid != id.String() {
				continue
			}
			return ev
		case <-deadline:
			t.Fatalf("no circuit_breaker.open event for provider %s published within timeout", id)
			return events.Event{}
		}
	}
}

// ---------------------------------------------------------------------------
// Open-transition log trail
// ---------------------------------------------------------------------------

// capturedLog is one slog record reduced to what these assertions care about.
type capturedLog struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

// logCaptureHandler records every slog record emitted while it is installed.
type logCaptureHandler struct {
	mu      sync.Mutex
	records []capturedLog
}

func (h *logCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *logCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := capturedLog{level: r.Level, msg: r.Message, attrs: make(map[string]any, r.NumAttrs())}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

func (h *logCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logCaptureHandler) WithGroup(string) slog.Handler      { return h }

// forProvider returns the records whose provider_id attribute matches id, so an
// assertion can never be satisfied by a line another test or another provider
// emitted onto the process-wide logger.
func (h *logCaptureHandler) forProvider(id uuid.UUID) []capturedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []capturedLog
	for _, r := range h.records {
		if pid, ok := r.attrs["provider_id"].(uuid.UUID); ok && pid == id {
			out = append(out, r)
		}
	}
	return out
}

// last returns the most recent record with the given message. Used where the
// line's own provider_id is a plain string (the circuits map is keyed by the
// provider's UUID string), which forProvider's uuid.UUID assertion cannot match.
func (h *logCaptureHandler) last(msg string) (slog.Level, map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.records) - 1; i >= 0; i-- {
		if h.records[i].msg == msg {
			return h.records[i].level, h.records[i].attrs, true
		}
	}
	return 0, nil, false
}

// captureLogs installs a capturing slog handler for the duration of the test.
// SetHandler swaps the process-wide default, so the previous one is restored.
func captureLogs(t *testing.T) *logCaptureHandler {
	t.Helper()
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	capt := &logCaptureHandler{}
	debuglog.SetHandler(capt)
	return capt
}

// waitForOnOpen returns the provider the open callback reported, or fails the
// test if nothing arrives. The callback runs on its own goroutine, so a channel
// with a deadline is the only sound way to observe it.
func waitForOnOpen(t *testing.T, got <-chan uuid.UUID) uuid.UUID {
	t.Helper()
	select {
	case id := <-got:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("open transition did not invoke the open callback")
		return uuid.Nil
	}
}

// backdateOpen moves a circuit's open instant into the past, so a cooldown
// computed from openedAt is distinguishable from one computed from now without
// the test waiting out real time.
func backdateOpen(t *testing.T, cb *CircuitBreaker, id uuid.UUID, by time.Duration) {
	t.Helper()
	cb.mu.Lock()
	defer cb.mu.Unlock()
	c, ok := cb.circuits[id.String()][""]
	if !ok {
		t.Fatalf("setup: no circuit tracked for %s", id)
	}
	c.openedAt = c.openedAt.Add(-by)
}

// pinSibling backdates one model circuit's open instant and stamps a quota
// override on it. It builds the shape a fleet reaches whenever a provider's
// models go dark at different moments: a blocking, quota-pinned circuit that is
// NOT the provider's most degraded one, because the sibling that opened later
// has an ordinary cooldown running further out.
func pinSibling(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string, openedAgo, override time.Duration) {
	t.Helper()
	cb.mu.Lock()
	defer cb.mu.Unlock()
	c, ok := cb.circuits[id.String()][model]
	if !ok {
		t.Fatalf("setup: no circuit tracked for %s/%s", id, model)
	}
	c.openedAt = c.openedAt.Add(-openedAgo)
	c.cooldownOverride = override
	// An advisor pin: the provider-wide verdict's pin arm only counts pins the
	// quota advisor measured, which is what this helper simulates.
	c.pinSource = pinSourceAdvisor
}

// overrideFor reads a circuit's stored quota override. Status reports the
// cooldown actually governing the circuit, which hides the override whenever
// pinning is switched off, so assertions about the override itself read it here.
func overrideFor(t *testing.T, cb *CircuitBreaker, id uuid.UUID) time.Duration {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	c, ok := cb.circuits[id.String()][""]
	if !ok {
		t.Fatalf("no circuit tracked for %s", id)
	}
	return c.cooldownOverride
}

// countCircuits reports how many model circuits a provider is tracking. The
// eviction cap has no observable effect on routing (that is the point of it),
// so the only honest way to assert it is to read the map the cap bounds.
func countCircuits(t *testing.T, cb *CircuitBreaker, id uuid.UUID) int {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.circuits[id.String()])
}

// hasCircuit reports whether one model circuit survived eviction.
func hasCircuit(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string) bool {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	_, ok := cb.circuits[id.String()][model]
	return ok
}

func onlyStatus(t *testing.T, cb *CircuitBreaker) ProviderStatus {
	t.Helper()
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	return statuses[0]
}

// has reports whether a record with exactly this message was captured.
func (h *logCaptureHandler) has(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.msg == msg {
			return true
		}
	}
	return false
}
