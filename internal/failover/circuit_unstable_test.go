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

func TestNoteOpen_EscalatesOncePerWindow(t *testing.T) {
	c := &circuit{}
	start := time.Now()

	c.noteOpen(start)
	c.noteOpen(start.Add(time.Minute))
	if !c.noteOpen(start.Add(2 * time.Minute)) {
		t.Fatal("setup: the third open did not escalate")
	}

	// A model that stays broken keeps opening. Reporting each one tells the
	// operator nothing they were not told at the third.
	if c.noteOpen(start.Add(3 * time.Minute)) {
		t.Error("the fourth open escalated again inside the same window")
	}
	if c.noteOpen(start.Add(4 * time.Minute)) {
		t.Error("the fifth open escalated again inside the same window")
	}
	if !c.noteOpen(start.Add(5 * time.Minute)) {
		t.Error("the count never restarted, so a still-broken model goes quiet forever")
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

	for range opensBeforeEscalation {
		cb.RecordFailure(id, "test-provider", modelA)
		cb.IsOpen(id, "test-provider", modelA)
	}

	ev := waitForUnstableEvent(t, sub, id)
	if got, _ := ev.Metadata["model"].(string); got != modelA {
		t.Errorf("unstable event model metadata %q, want %q", got, modelA)
	}
	if ev.Severity != "warning" {
		t.Errorf("unstable event severity %q, want warning: a chronically broken model is not routine", ev.Severity)
	}
	if _, _, ok := capt.last("circuit-breaker: model keeps reopening its circuit"); !ok {
		t.Error("no escalation log line captured")
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
