package proxy

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// waitForDisable polls the mock for a recorded SetEnabled call, since
// noteModelGone disables on a detached goroutine so the request path is not
// blocked by the write.
func waitForDisable(t *testing.T, repo *mockModelRepo) []setEnabledCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := repo.disableCalls(); len(calls) > 0 {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	return repo.disableCalls()
}

func newGoneHandler(repo *mockModelRepo) *Handler {
	return &Handler{modelRepo: repo}
}

// TestNoteModelGone_DisablesAfterThreshold covers the whole point of the
// feature: a provider that keeps a retired model in its listing (Google did
// this with gemini-2.0-flash for two months) can only be caught from real
// traffic, because discovery's RecordMissingModels never sees it leave.
func TestNoteModelGone_DisablesAfterThreshold(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}

	// Below the threshold nothing is touched.
	for i := 1; i < goneStrikeThreshold; i++ {
		h.noteModelGone(m, "Google AI Studio (Gemini)")
		if calls := repo.disableCalls(); len(calls) != 0 {
			t.Fatalf("disabled after %d strike(s), threshold is %d", i, goneStrikeThreshold)
		}
	}

	h.noteModelGone(m, "Google AI Studio (Gemini)")

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("expected exactly one SetEnabled call, got %d", len(calls))
	}
	if calls[0].id != m.ID {
		t.Errorf("disabled %s, want %s", calls[0].id, m.ID)
	}
	if calls[0].enabled {
		t.Error("model must be disabled, not enabled")
	}
}

// TestNoteModelServed_ResetsStreak pins that the streak must be consecutive. A
// provider blip that happens to match a gone-pattern must not accumulate
// towards a disable across an otherwise healthy period.
func TestNoteModelServed_ResetsStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	m := &model.Model{ID: uuid.New(), ModelID: "glm-5.2"}

	for range goneStrikeThreshold * 3 {
		// One short of the threshold, then a success, forever.
		for i := 1; i < goneStrikeThreshold; i++ {
			h.noteModelGone(m, "OpenCode Zen")
		}
		h.noteModelServed(m)
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a model that keeps answering must never be disabled, got %d calls", len(calls))
	}
}

// TestNoteModelGone_StrikesArePerModel guards against one dead model dragging
// its healthy neighbours down: the counter is keyed by model UUID.
func TestNoteModelGone_StrikesArePerModel(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	dead := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	alive := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-5"}

	for range goneStrikeThreshold {
		h.noteModelGone(dead, "OpenCode Zen")
		h.noteModelServed(alive)
	}

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("expected one disable, got %d", len(calls))
	}
	if calls[0].id != dead.ID {
		t.Errorf("disabled the wrong model: %s", calls[0].id)
	}
}

// TestNoteModelGone_ResetsAfterDisable stops a model that keeps being requested
// after it was disabled from issuing a disable per request.
func TestNoteModelGone_ResetsAfterDisable(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	m := &model.Model{ID: uuid.New(), ModelID: "hy3-preview"}

	for range goneStrikeThreshold {
		h.noteModelGone(m, "OpenCode Go")
	}
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected one disable, got %d", len(calls))
	}

	// Two more refusals must not reach the threshold again on their own.
	h.noteModelGone(m, "OpenCode Go")
	h.noteModelGone(m, "OpenCode Go")
	if calls := repo.disableCalls(); len(calls) != 1 {
		t.Errorf("expected still one disable, got %d", len(calls))
	}
}

// TestNoteModelGone_NilSafe: the failover drain path passes candidate.model
// straight through, so a malformed candidate must not panic the proxy.
func TestNoteModelGone_NilSafe(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)

	h.noteModelGone(nil, "Somewhere")
	h.noteModelGone(&model.Model{ModelID: "no-uuid"}, "Somewhere")
	h.noteModelServed(nil)
	h.noteModelServed(&model.Model{ModelID: "no-uuid"})

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Errorf("expected no disable calls, got %d", len(calls))
	}
}
