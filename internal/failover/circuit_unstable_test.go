package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// The window arithmetic is unit-tested directly because the escalation it
// governs is a once-a-day event: driving 24 hours of transitions through the
// breaker would need a clock seam in production code to observe at all.

func TestNoteOpen_EscalatesOnTheThirdOpenInOneWindow(t *testing.T) {
	c := &circuit{}
	start := time.Now()

	if c.noteOpen(start) {
		t.Error("the first open escalated: one outage is not a pattern")
	}
	if c.noteOpen(start.Add(time.Hour)) {
		t.Error("the second open escalated: two is an ordinary bad afternoon")
	}
	if !c.noteOpen(start.Add(2 * time.Hour)) {
		t.Error("the third open in one window did not escalate")
	}
}

func TestNoteOpen_OpensSpanningTheWindowDoNotAddUp(t *testing.T) {
	c := &circuit{}
	start := time.Now()

	c.noteOpen(start)
	c.noteOpen(start.Add(time.Hour))
	// Past the window, so this starts a fresh count rather than being the third.
	if c.noteOpen(start.Add(reopenWindow + time.Minute)) {
		t.Fatal("an open past the window escalated: failures a day apart are not one run")
	}
	// It really did start over: two more are needed, and the second of them fires.
	if c.noteOpen(start.Add(reopenWindow + 2*time.Minute)) {
		t.Error("escalated on the second open of the new window")
	}
	if !c.noteOpen(start.Add(reopenWindow + 3*time.Minute)) {
		t.Error("the new window never reached its own third open")
	}
}

// TestNoteOpen_ReportsOncePerWindowNotPerThreeOpens is the regression that
// matters most here. A model failing every cooldown reaches three opens again
// within minutes, so a design that restarts the window on report would tell the
// operator every few minutes that the model "opened its circuit 3 times in 24h"
// - a sentence its own cadence disproves. The window has to keep running.
func TestNoteOpen_ReportsOncePerWindowNotPerThreeOpens(t *testing.T) {
	c := &circuit{}
	start := time.Now()

	c.noteOpen(start)
	c.noteOpen(start.Add(time.Minute))
	if !c.noteOpen(start.Add(2 * time.Minute)) {
		t.Fatal("setup: the third open did not report")
	}

	// A model broken all day keeps opening every cooldown. None of these may
	// report: the window that was reported is still running.
	for i := 3; i < 40; i++ {
		if c.noteOpen(start.Add(time.Duration(i) * time.Minute)) {
			t.Fatalf("open %d reported again inside the window already reported", i+1)
		}
	}
	// Still inside the window a day later, right up to its edge.
	if c.noteOpen(start.Add(reopenWindow)) {
		t.Error("an open at the window's edge reported again")
	}
	// Past it, the count starts over and needs three fresh opens of its own.
	past := start.Add(reopenWindow + time.Minute)
	if c.noteOpen(past) {
		t.Error("the first open of the new window reported")
	}
	if c.noteOpen(past.Add(time.Minute)) {
		t.Error("the second open of the new window reported")
	}
	if !c.noteOpen(past.Add(2 * time.Minute)) {
		t.Error("a still-broken model never reports again, so it goes quiet forever")
	}
}

// TestCircuitBreaker_ReportsAModelThatKeepsReopening drives the real breaker
// through three open transitions and asserts the operator is told once, by an
// event carrying the model, and that the report names the model rather than only
// its provider: the breaker charges one model at a time, so "provider X is
// unstable" would accuse every sibling the provider still serves.
func TestCircuitBreaker_ReportsAModelThatKeepsReopening(t *testing.T) {
	capt := captureLogs(t)
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	// Threshold 1 and no cooldown: one failure opens, the next IsOpen hands out
	// a half-open probe, and the next failure re-opens.
	cb := newTestCB(1, 0)
	id := uuid.New()

	// Opens one and two must stay silent. Asserting that is what stops an
	// unconditional report passing this test: without it, publishing on every
	// open looks identical to publishing on the third.
	for range opensBeforeEscalation - 1 {
		cb.RecordFailure(id, "test-provider", modelA)
		cb.IsOpen(id, "test-provider", modelA)
		if ev, ok := drainUnstable(sub, id); ok {
			t.Fatalf("reported after fewer than %d opens: %s", opensBeforeEscalation, ev.Message)
		}
	}

	cb.RecordFailure(id, "test-provider", modelA)

	ev := waitForUnstableEvent(t, sub, id)
	if got, _ := ev.Metadata["model"].(string); got != modelA {
		t.Errorf("unstable event model metadata %q, want %q", got, modelA)
	}
	// The alert dispatcher debounces on the narrowest identity present, so
	// without model_id two models failing on one provider collapse to one alert.
	if got, _ := ev.Metadata["model_id"].(string); got != modelA {
		t.Errorf("unstable event model_id metadata %q, want %q", got, modelA)
	}
	if got, _ := ev.Metadata["opens"].(int); got != opensBeforeEscalation {
		t.Errorf("unstable event opens metadata %d, want %d", got, opensBeforeEscalation)
	}
	// The wiki documents this exact string as the contract; Duration.String
	// would publish "24h0m0s" and quietly break a consumer comparing it.
	if got, _ := ev.Metadata["window"].(string); got != "24h" {
		t.Errorf("unstable event window metadata %q, want %q", got, "24h")
	}
	if ev.Severity != "warning" {
		t.Errorf("unstable event severity %q, want warning: a chronically broken model is not routine", ev.Severity)
	}
	if _, _, ok := capt.last("circuit-breaker: model keeps reopening its circuit"); !ok {
		t.Error("no escalation log line captured")
	}
}

// drainUnstable reports whether an unstable event for this provider is already
// waiting, without blocking on one that should not exist.
func drainUnstable(sub chan events.Event, id uuid.UUID) (events.Event, bool) {
	for {
		select {
		case ev := <-sub:
			if ev.Type != "circuit_breaker.unstable" {
				continue
			}
			if pid, _ := ev.Metadata["provider_id"].(string); pid == id.String() {
				return ev, true
			}
		default:
			return events.Event{}, false
		}
	}
}

func waitForUnstableEvent(t *testing.T, sub chan events.Event, id uuid.UUID) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type != "circuit_breaker.unstable" {
				continue
			}
			if pid, _ := ev.Metadata["provider_id"].(string); pid != id.String() {
				continue
			}
			return ev
		case <-deadline:
			t.Fatalf("no circuit_breaker.unstable event for provider %s published within timeout", id)
			return events.Event{}
		}
	}
}
