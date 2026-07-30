package proxy

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
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

// TestVerdictForStream pins the three-way rule that decides what a finished
// stream proves about a model. Review caught this twice: first that a
// gone-report mid-stream was never recorded at all, then that treating every
// non-gone outcome as a success let a retired model reset its own strike streak
// with its own failures and stay routable indefinitely.
func TestVerdictForStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     ErrorKind
		produced bool
		want     streamVerdict
	}{
		{"clean finish that delivered content proves the model answered", "", true, verdictServed},
		{"explicit gone report strikes", KindProviderModelGone, true, verdictGone},
		{"gone report strikes even with no content", KindProviderModelGone, false, verdictGone},
		// A stream that opened, emitted nothing and ended without recording an
		// error is not proof of anything. Crediting it would clear a retirement
		// streak on the strength of an empty response.
		{"truncated stream with no content is inconclusive", "", false, verdictInconclusive},
		// Everything below is a failure that says nothing about whether the
		// model exists, so it must not clear the streak.
		{"transient provider error", KindProviderError, true, verdictInconclusive},
		{"client hung up", KindClientDisconnect, true, verdictInconclusive},
		{"provider stalled", KindProviderTimeout, false, verdictInconclusive},
		{"failover deadline", KindFailoverTimeout, false, verdictInconclusive},
		{"retry deadline", KindRetryTimeout, false, verdictInconclusive},
		{"payload rejected", KindProviderBadRequest, false, verdictInconclusive},
		{"not entitled", KindProviderNotEntitled, false, verdictInconclusive},
		{"gateway fault", KindInternal, false, verdictInconclusive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := verdictForStream(tc.kind, tc.produced); got != tc.want {
				t.Errorf("verdictForStream(%q, produced=%v) = %v, want %v", tc.kind, tc.produced, got, tc.want)
			}
		})
	}
}

// TestStreamProducedOutput covers the two independent signals that a stream
// actually delivered content. Neither is reliable alone: completion tokens are
// absent when a provider omits the usage chunk, TTFT is zero when the probe is
// disabled.
func TestStreamProducedOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		log  *requestLogData
		want bool
	}{
		{"tokens only", &requestLogData{tokensCompletion: 12}, true},
		{"ttft only", &requestLogData{ttftMs: 42.5}, true},
		{"both", &requestLogData{tokensCompletion: 12, ttftMs: 42.5}, true},
		{"neither: nothing ever flowed", &requestLogData{}, false},
		{"nil is not evidence", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := streamProducedOutput(tc.log); got != tc.want {
				t.Errorf("streamProducedOutput() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNoteStreamOutcome_EmptyStreamDoesNotClearStreak is the composed form of
// the truncation case: a model accumulating strikes must not have them wiped by
// a stream that delivered nothing.
func TestNoteStreamOutcome_EmptyStreamDoesNotClearStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	candidate := modelCandidate{model: m, provider: &provider.Provider{Name: "Google AI Studio (Gemini)"}}

	for range goneStrikeThreshold {
		h.noteModelGone(m, "Google AI Studio (Gemini)")
		// An empty, error-free stream lands between strikes.
		h.noteStreamOutcome(&requestLogData{}, candidate)
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the model to still be disabled, got %d disable calls", len(calls))
	}
}

// TestNoteStreamOutcome_RealSuccessClears is the other direction: a stream that
// genuinely delivered content still resets the streak.
func TestNoteStreamOutcome_RealSuccessClears(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	m := &model.Model{ID: uuid.New(), ModelID: "glm-5.2"}
	candidate := modelCandidate{model: m, provider: &provider.Provider{Name: "OpenCode Zen"}}

	for range goneStrikeThreshold * 3 {
		for i := 1; i < goneStrikeThreshold; i++ {
			h.noteModelGone(m, "OpenCode Zen")
		}
		h.noteStreamOutcome(&requestLogData{tokensCompletion: 5, ttftMs: 30}, candidate)
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a model that keeps streaming content must never be disabled, got %d", len(calls))
	}
}

// TestNoteModelGone_FailedStreamsDoNotResetStreak is the composed consequence:
// a model that is genuinely gone still reaches the threshold even when its
// attempts are interleaved with unrelated stream failures, because those are
// inconclusive and touch neither counter.
func TestNoteModelGone_FailedStreamsDoNotResetStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}

	for range goneStrikeThreshold {
		h.noteModelGone(m, "Google AI Studio (Gemini)")
		// A transient stream failure lands between strikes. Under the old
		// "anything not gone is a success" rule this cleared the streak and the
		// model could never be retired.
		if v := verdictForStream(KindProviderError, true); v == verdictServed {
			h.noteModelServed(m)
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the model to still be disabled, got %d disable calls", len(calls))
	}
}
