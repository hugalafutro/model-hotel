package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestNoteModelGone_DisablesAfterThreshold covers the whole point of the
// feature: a provider that keeps a retired model in its listing (Google did
// this with gemini-2.0-flash for two months) can only be caught from real
// traffic, because discovery's RecordMissingModels never sees it leave.
func TestNoteModelGone_DisablesAfterThreshold(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// Below the threshold nothing is touched.
	for i := 1; i < goneStrikeThreshold; i++ {
		h.noteModelGone(cand, endpointTypeChat)
		if calls := repo.disableCalls(); len(calls) != 0 {
			t.Fatalf("disabled after %d strike(s), threshold is %d", i, goneStrikeThreshold)
		}
	}

	h.noteModelGone(cand, endpointTypeChat)

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

// TestNoteModelGone_StrikesArePerModel guards against one dead model dragging
// its healthy neighbours down: the counter is keyed by model UUID.
func TestNoteModelGone_StrikesArePerModel(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	dead := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	alive := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-5"}
	deadCand := goneCandidateFor(t, dead, "OpenCode Zen")

	for range goneStrikeThreshold {
		h.noteModelGone(deadCand, endpointTypeChat)
		h.noteModelServed(alive, endpointTypeChat)
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
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "hy3-preview"}
	cand := goneCandidateFor(t, m, "OpenCode Go")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected one disable, got %d", len(calls))
	}

	// Two more refusals must not reach the threshold again on their own.
	h.noteModelGone(cand, endpointTypeChat)
	h.noteModelGone(cand, endpointTypeChat)
	if calls := repo.disableCalls(); len(calls) != 1 {
		t.Errorf("expected still one disable, got %d", len(calls))
	}
}

// TestNoteModelGone_NilSafe: the failover drain path passes its candidate
// straight through, so a malformed one must not panic the proxy.
//
// A candidate with no provider must stop the retirement outright rather than
// counting strikes: the disable is adjudicated by a real request, and there is
// nobody to send it to.
func TestNoteModelGone_NilSafe(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)

	somewhere := &provider.Provider{ID: uuid.New(), Name: "Somewhere"}
	noModel := modelCandidate{}
	noID := modelCandidate{model: &model.Model{ModelID: "no-uuid"}, provider: somewhere}
	// A real, stable model id with nothing to send a probe to. Stable on
	// purpose: a fresh uuid per iteration could never accumulate, so the loop
	// would prove nothing about the guard it is here to exercise.
	noProvider := modelCandidate{model: &model.Model{ID: uuid.New(), ModelID: "no-provider"}}
	for range goneStrikeThreshold {
		h.noteModelGone(noModel, endpointTypeChat)
		h.noteModelGone(noID, endpointTypeChat)
		h.noteModelGone(noProvider, endpointTypeChat)
	}
	h.noteModelServed(nil, endpointTypeChat)
	h.noteModelServed(&model.Model{ModelID: "no-uuid"}, endpointTypeChat)

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Errorf("expected no disable calls, got %d", len(calls))
	}
}

// TestProducedOutput covers the two independent signals that a stream
// actually delivered content. Neither is reliable alone: completion tokens are
// absent when a provider omits the usage chunk, TTFT is zero when the probe is
// disabled.
func TestProducedOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		log  *requestLogData
		want bool
	}{
		{"tokens only", &requestLogData{tokensCompletion: 12}, true},
		{"ttft only", &requestLogData{ttftMs: 42.5}, true},
		{"both", &requestLogData{tokensCompletion: 12, ttftMs: 42.5}, true},
		// The case the other two miss between them, and it is an ordinary
		// configuration rather than an exotic one: a provider that omits the
		// usage chunk, on a gateway with the TTFT probe switched off. Without
		// this the success does not clear the streak, and later refusals retire
		// a model whose failures were never consecutive.
		{"content only: no usage reported, probe disabled", &requestLogData{deliveredContent: true}, true},
		{"neither: nothing ever flowed", &requestLogData{}, false},
		{"nil is not evidence", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := producedOutput(tc.log); got != tc.want {
				t.Errorf("producedOutput() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNoteModelGone_CapabilityRefusalsNeverDisable is the composed consequence
// review asked for: the classifier and the strike counter together must not
// retire a live model that keeps refusing one capability.
//
// It drives the same two steps the proxy does — classify the upstream body,
// then strike only on provider_model_gone — so a future loosening of the
// patterns fails here rather than silently disabling a working model in
// production.
func TestNoteModelGone_CapabilityRefusalsNeverDisable(t *testing.T) {
	t.Parallel()

	refusals := []string{
		`{"error":{"message":"Model gpt-5.6-sol is not supported for this operation"}}`,
		`{"error":{"message":"Model gpt-5.6-sol is not supported for this endpoint"}}`,
		`{"error":{"message":"Parameter 'temperature' is not supported with this model"}}`,
	}

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	// Deliberately NOT goneCandidateFor: the classifier never returns
	// KindProviderModelGone for these bodies, so noteModelGone is never reached
	// and no probe is ever sent. A listener here would be dead weight, and worse,
	// it would read as though the provider had a say in the outcome. What this
	// test pins is that the classifier alone declines to nominate the model.
	cand := modelCandidate{model: m, provider: &provider.Provider{ID: uuid.New(), Name: "OpenAI"}}

	// Well past the threshold, cycling through every refusal shape.
	for i := range goneStrikeThreshold * 3 {
		body := refusals[i%len(refusals)]
		if kind, _ := classifyUpstreamError(400, body, "gpt-5.6-sol"); kind == KindProviderModelGone {
			h.noteModelGone(cand, endpointTypeChat)
		}
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a live model refusing one capability must never be auto-disabled, got %d disable calls", len(calls))
	}
}

// TestNoteModelGone_ConcurrentStrikesAreNotLost pins the atomicity of the strike
// counter. The counter was originally a plain int behind a sync.Map, read then
// written, so two refusals racing both read the same value and stored the same
// increment. A retired model is exactly the one that draws concurrent refusals —
// clients retry it and a failover group can hit it from several requests at
// once — so the lost update stalled the streak below the threshold and left the
// model routable indefinitely.
//
// Run under -race this also catches the data race itself, not just the
// arithmetic.
func TestNoteModelGone_ConcurrentStrikesAreNotLost(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goneStrikeThreshold {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // maximise overlap
			h.noteModelGone(cand, endpointTypeChat)
		}()
	}
	close(start)
	wg.Wait()

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("exactly %d concurrent refusals must disable the model once, got %d disable calls", goneStrikeThreshold, len(calls))
	}
	if calls[0].enabled {
		t.Error("model must be disabled, not enabled")
	}
}

// TestNoteModelGone_ConcurrentBurstDisablesOnce guards the other side of the
// atomic counter: a flood of refusals well past the threshold must still issue a
// single disable, not one per goroutine that observed the count at or above it.
func TestNoteModelGone_ConcurrentBurstDisablesOnce(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	cand := goneCandidateFor(t, m, "OpenCode Zen")

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			h.noteModelGone(cand, endpointTypeChat)
		}()
	}
	close(start)
	wg.Wait()

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a burst must still disable exactly once, got %d disable calls", len(calls))
	}
}

// TestNoteModelGone_FailedDisableIsRetried pins both halves of what a write
// that never landed must do.
//
// It must be retried: a transient database error may not leave a dead model
// enabled forever. And the retry must respect the same bound as everything else
// on this path — the failure says nothing about the provider, so it is no reason
// to spend another upstream request now. The old shape deleted the streak to
// make the retry reachable, which cost the cooldown with it and turned a
// database outage into three fresh refusals buying another probe, on repeat, for
// every model refusing traffic at the time.
func TestNoteModelGone_FailedDisableIsRetried(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{setEnabledErr: errors.New("database unavailable")}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gemini-2.0-flash"))
	cand := goneCandidateAt(m, "Google AI Studio (Gemini)", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the first (failing) attempt, got %d", len(calls))
	}

	// Refusals inside the cooldown cost nothing: no second probe, no second
	// write attempt.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("a failed write is not a reason to re-probe the provider, got %v", paths)
	}
	if calls := repo.disableCalls(); len(calls) != 1 {
		t.Fatalf("expected no retry inside the cooldown, got %+v", calls)
	}

	// Once it lapses, the parked evidence is still there and one refusal is
	// enough to try the write again.
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.disableCalls()) >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("a failed disable must be retried, got %d attempts", len(repo.disableCalls()))
}

// TestNoteModelGone_SuccessCancelsAQueuedDisable pins the window between
// deciding to disable and the write landing.
//
// The disable is detached so it never adds latency to the request path, which
// means it can still be in flight — for as long as the database takes — when the
// model answers a request and proves it is alive. Without a cancellation the
// already-queued write lands anyway and retires a model that is demonstrably
// working, on evidence that has since been superseded.
//
// The assertion is the end state rather than the branch taken, because which
// branch runs is genuinely up to the scheduler: a success racing a queued
// disable can arrive before the goroutine's pre-write check (skipped outright)
// or after it (written, then reverted). Both are correct and neither can be
// forced from outside, so pinning either one specifically would be pinning the
// Go scheduler. What must hold every time is that the model is not left
// disabled. The revert branch has its own deterministic test below.
func TestNoteModelGone_SuccessCancelsAQueuedDisable(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// Reach the threshold: the disable goroutine is now queued.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// The model answers before the write is released.
	h.noteModelServed(m, endpointTypeChat)
	close(repo.setEnabledGate)

	// Give the goroutine room to run to completion either way.
	time.Sleep(200 * time.Millisecond)

	calls := repo.committedCalls()
	if len(calls) == 0 {
		return // Skipped or abandoned: nothing was ever committed.
	}
	if !calls[len(calls)-1].enabled {
		t.Fatalf("a model that answered must not be left disabled, got %+v", calls)
	}
}

// TestNoteModelGone_SuccessDuringTheWriteIsAbandoned covers the window the
// pre-write check cannot reach: the model answers once the write has already
// started.
//
// The contract is that nothing COMMITS, not merely that the model ends up
// enabled. A committed-then-undone disable is briefly visible to every other
// session, and a custom-group revalidation that samples it will disable the
// group for having too few routable members — which re-enabling the model does
// not undo. Staging the write inside a transaction is what keeps that
// intermediate state private.
func TestNoteModelGone_SuccessDuringTheWriteIsAbandoned(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// Wait for the write to be genuinely in flight — past the pre-check and
	// inside the staged write — so the interleaving is deterministic rather
	// than a bet on goroutine scheduling.
	select {
	case <-repo.setEnabledEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the disable write never started")
	}

	// The model answers while the disable is staged.
	h.noteModelServed(m, endpointTypeChat)
	close(repo.setEnabledGate)

	// Let the goroutine run to completion, then assert on what was durable.
	time.Sleep(200 * time.Millisecond)
	if committed := repo.committedCalls(); len(committed) != 0 {
		t.Fatalf("a staged disable that a success overtook must not commit, got %+v", committed)
	}
	if attempts := repo.disableCalls(); len(attempts) != 1 {
		t.Fatalf("expected exactly one abandoned attempt, got %+v", attempts)
	}
}

// TestNoteModelGone_SuccessAfterConfirmIsRolledBack covers the one interleaving
// staging cannot close: the success lands after confirm has returned, while the
// commit is already on its way. That write cannot be recalled, so the contract
// there is that it is undone rather than prevented.
func TestNoteModelGone_SuccessAfterConfirmIsRolledBack(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")
	repo.afterConfirm = func() { h.noteModelServed(m, endpointTypeChat) }

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := repo.committedCalls(); len(calls) == 2 {
			if calls[0].enabled {
				t.Errorf("first committed write should be the disable, got enabled=%v", calls[0].enabled)
			}
			if !calls[1].enabled {
				t.Error("a model that answered mid-commit must be re-enabled, not left disabled")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected the committed disable to be rolled back, got %+v", repo.committedCalls())
}

// TestNoteModelGone_StaleFailedDisableKeepsTheEvidence pins what a disable
// goroutine may do to a streak that has moved on underneath it, and what the
// next claim owes the disable it spawns.
//
// The sequence is reachable because the write is detached: the goroutine can
// still be inside SetEnabled when the model answers — standing its write down
// and clearing the count — and fresh refusals then start rebuilding. Two things
// must survive that. The unwinding goroutine must not erase the evidence
// accumulated after it (a model refusing every request would otherwise restart
// from zero each time a disable failed, staying enabled exactly when it is most
// clearly dead), and the tombstone that success left behind must not outlive the
// decision it belonged to, or the next disable stands down at its own pre-write
// check and the model is never retired again.
func TestNoteModelGone_StaleFailedDisableKeepsTheEvidence(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{
		setEnabledErr:     errors.New("database unavailable"),
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// Reach the threshold and let the (failing) write start.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	select {
	case <-repo.setEnabledEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the disable write never started")
	}

	// While it is held open: the model answers, which cancels that write and
	// clears the count, and then refuses again — evidence that belongs to the
	// next decision, not to the one now unwinding.
	h.noteModelServed(m, endpointTypeChat)
	for range goneStrikeThreshold - 1 {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// Release the stale write and watch what it does on its way out. Asserting
	// on the count rather than on a later disable is what makes this
	// deterministic: a cleanup that erases evidence does so within microseconds
	// of the write returning, whereas counting disables would race it.
	close(repo.setEnabledGate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(repo.disableCalls()) < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	watch := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(watch) {
		if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold-1 {
			t.Fatalf("the stale failed disable took the newer evidence with it: streak = %d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// One more refusal completes the rebuilt streak. The cooldown from the first
	// claim is still running, so this is also the point at which the retry
	// becomes reachable rather than immediate.
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.disableCalls()) >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the rebuilt streak never reached a second disable: only %d attempts", len(repo.disableCalls()))
}

// TestNoteModelGone_FailedRollbackStopsThere pins the worst case on the
// rollback path: the disable committed, the model then answered, and the undo
// could not be written.
//
// The model is left disabled and the gateway knows it should not be, which is
// the one state this path cannot repair. What it must NOT do is carry on as
// though the disable stood — announcing a retirement and resizing failover
// groups around a model it has just been told is alive. It stops instead, and
// the operator has the error log.
func TestNoteModelGone_FailedRollbackStopsThere(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{reEnableErr: errors.New("database unavailable")}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")
	repo.afterConfirm = func() { h.noteModelServed(m, endpointTypeChat) }

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		calls := repo.disableCalls()
		if len(calls) < 2 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if len(calls) != 2 {
			t.Fatalf("expected the disable and one failed rollback, got %+v", calls)
		}
		if calls[0].enabled || !calls[0].committed {
			t.Errorf("first write should be a committed disable, got %+v", calls[0])
		}
		if !calls[1].enabled || calls[1].committed {
			t.Errorf("second write should be a failed re-enable, got %+v", calls[1])
		}
		return
	}
	t.Fatalf("the rollback was never attempted, got %+v", repo.disableCalls())
}

// TestNoteModelGone_StaleStrikesDoNotAccumulate pins that a streak is
// consecutive in time, not merely in sequence.
//
// The count had no time bound, so two refusals from a provider incident this
// morning and one this afternoon combined into a retirement, on evidence whose
// halves had nothing to do with each other. The stale half is also the dangerous
// half: those strikes may predate an operator looking at the model and enabling
// it, and a count they cannot see then finishes and turns it off again.
//
// The age is driven through the streak's own timestamp rather than a knob on the
// production code, so nothing here exists only for the test.
func TestNoteModelGone_StaleStrikesDoNotAccumulate(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// Two refusals, one short of the threshold.
	for range goneStrikeThreshold - 1 {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// Age them past the window, as if the incident were hours ago.
	raw, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: probeChatEndpoint})
	if !ok {
		t.Fatal("the strikes did not start a streak")
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		t.Fatal("unexpected streak type")
	}
	streak.mu.Lock()
	streak.lastStrike = time.Now().Add(-2 * goneStrikeWindow)
	streak.mu.Unlock()

	// The next refusal begins a new streak instead of completing the old one.
	h.noteModelGone(cand, endpointTypeChat)
	if n := streak.count(); n != 1 {
		t.Errorf("a strike after the window must start over, got a streak of %d", n)
	}

	time.Sleep(100 * time.Millisecond)
	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("stale evidence must not retire a model, got %+v", calls)
	}

	// Fresh traffic still retires it: the window bounds the evidence, it does
	// not disarm the feature.
	for range goneStrikeThreshold - 1 {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("three recent refusals must still retire, got %+v", calls)
	}
}

// TestGoneStreak_ConcurrentResetKeepsEveryStrike pins the window reset against
// the increments racing it.
//
// Deciding whether the window has lapsed and applying that decision is one
// operation. Split across two atomics it is not: a reset storing 1 after two
// increments have already added erases them, so a model refused three times ends
// the burst on a count of one and never reaches the threshold — a dead model
// left routable by the very traffic proving it is dead.
//
// Every strike here is deliberately at the boundary, so the reset branch and the
// increment branch race on every iteration rather than only by luck.
func TestGoneStreak_ConcurrentResetKeepsEveryStrike(t *testing.T) {
	t.Parallel()

	for range 200 {
		s := &goneStreak{}
		// Seed a last strike old enough that the first caller to look must
		// reset, while the others arrive at the same instant.
		s.mu.Lock()
		s.lastStrike = time.Now().Add(-2 * goneStrikeWindow)
		s.mu.Unlock()

		var wg sync.WaitGroup
		start := make(chan struct{})
		now := time.Now()
		for range goneStrikeThreshold {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				s.strike(now)
			}()
		}
		close(start)
		wg.Wait()

		// One strike resets to 1 and the other two add, so the streak must be
		// exactly the number of refusals.
		if n := s.count(); n != goneStrikeThreshold {
			t.Fatalf("concurrent strikes at the window boundary lost some: streak = %d, want %d", n, goneStrikeThreshold)
		}
	}
}

// TestNoteModelGone_FreshEvidenceBeatsAStaleRevert covers a retirement being
// undone by a success that current traffic has already contradicted.
//
// The undo is scheduled by a success and runs after the disable commits. In
// between, new refusals can build a replacement streak — and that replacement
// finds the model already retired and stands down, correctly, because the
// disable it wanted is already there. If the older undo then runs unconditionally
// it re-enables the model, and the replacement streak, parked above the
// threshold, never disables it again. The model stays routable with three fresh
// refusals against it.
func TestNoteModelGone_FreshEvidenceBeatsAStaleRevert(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// The success lands after confirm, so the retirement commits and an undo is
	// scheduled. Before it runs, the model refuses again and reaches a fresh
	// threshold.
	// Once: the hook runs on every staged write, and the replacement retirement
	// below is itself a staged write, so an unguarded hook would recurse.
	var once sync.Once
	repo.afterConfirm = func() {
		once.Do(func() {
			h.noteModelServed(m, endpointTypeChat)
			for range goneStrikeThreshold {
				h.noteModelGone(cand, endpointTypeChat)
			}
		})
	}

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	time.Sleep(200 * time.Millisecond)

	for _, c := range repo.committedCalls() {
		if c.enabled {
			t.Fatalf("a model with three fresh refusals against it was re-enabled: %+v", repo.committedCalls())
		}
	}
}

// TestNoteModelGone_SupersededRevertStandsDown covers the undo finding that the
// row has moved on — in practice, an operator disabling the model by hand while
// the retirement was committing.
//
// The repository refuses the write in that case; this pins that the proxy treats
// the refusal as an outcome rather than an error, and stops there. Carrying on
// would announce a retirement for a model whose state it no longer owns.
func TestNoteModelGone_SupersededRevertStandsDown(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{revertSuperseded: true}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")
	repo.afterConfirm = func() { h.noteModelServed(m, endpointTypeChat) }

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		calls := repo.disableCalls()
		if len(calls) < 2 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if len(calls) != 2 {
			t.Fatalf("expected the retirement and one refused undo, got %+v", calls)
		}
		if calls[1].committed {
			t.Error("a superseded undo must not be recorded as having restored the model")
		}
		return
	}
	t.Fatalf("the undo was never attempted, got %+v", repo.disableCalls())
}

// TestNoteModelGone_EachWriteGetsAFreshDeadline pins that the out-of-band
// writes do not share one budget.
//
// They run in sequence, so a shared deadline lets a slow write starve every
// write after it — and slowness is correlated, not random: the database load
// that made the disable take its whole budget is the same load the follow-up
// work has to get through. The follow-up would then fail instantly with a
// deadline that was already spent, in precisely the conditions where it matters.
//
// Asserted on the compensating re-enable because that is the sequence this mock
// can observe. The custom-group revalidation after a disable has the same shape
// and the same fix, but failoverRepo is a concrete *failover.Repository and
// needs PostgreSQL, so it is not covered here — stating that rather than letting
// this test imply it.
func TestNoteModelGone_EachWriteGetsAFreshDeadline(t *testing.T) {
	t.Parallel()

	// Long enough that inheriting it would be unmistakable against the
	// tolerance below, short enough to keep the test quick.
	const slowWrite = 500 * time.Millisecond

	repo := &mockModelRepo{
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")
	// The success has to land AFTER confirm for a rollback to happen at all;
	// landing earlier abandons the write and there is no second one to measure.
	repo.afterConfirm = func() { h.noteModelServed(m, endpointTypeChat) }

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	select {
	case <-repo.setEnabledEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the disable write never started")
	}

	// Hold the disable open so it burns a visible slice of its budget before it
	// commits and the rollback follows.
	time.Sleep(slowWrite)
	close(repo.setEnabledGate)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		calls := repo.committedCalls()
		if len(calls) < 2 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		// Tolerance well under slowWrite: an inherited deadline would show up
		// as roughly goneWriteTimeout-slowWrite, a fresh one as the full budget
		// minus only the scheduling gap.
		if floor := goneWriteTimeout - slowWrite/2; calls[1].budget < floor {
			t.Fatalf("the re-enable inherited a spent deadline: %v left, want more than %v", calls[1].budget, floor)
		}
		return
	}
	t.Fatalf("expected the disable to be rolled back, got %+v", repo.committedCalls())
}

// TestNoteModelGone_QueuedDisableStillLandsWithoutASuccess is the control: the
// cancellation must only fire on an actual success, not swallow every disable
// that happens to be slow.
func TestNoteModelGone_QueuedDisableStillLandsWithoutASuccess(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// No success this time; release the slow write.
	close(repo.setEnabledGate)

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("expected the queued disable to land, got %d calls", len(calls))
	}
	if calls[0].enabled {
		t.Error("model must be disabled, not enabled")
	}
}

// TestNoteModelGone_APanicWhileDisablingDoesNotKillTheGateway pins the
// recover() on the detached goroutine.
//
// It makes an upstream request and runs bytes a provider chose through json and
// the dialect translators, and an unrecovered panic on any goroutine takes the
// whole process down — so one model's retirement could kill a gateway serving
// everything else fine. Every other detached goroutine in this package recovers.
//
// The failure mode is why this test looks the way it does: without the recover,
// the panic is not a failed assertion, it is the test binary dying, and the
// second half below never runs at all. What is asserted is that the gateway
// carried on and retired the next model normally.
func TestNoteModelGone_APanicWhileDisablingDoesNotKillTheGateway(t *testing.T) {
	t.Parallel()

	// Panic once, on the first retirement only, so the second one exercises a
	// gateway that is still working rather than a mock that stopped panicking.
	var panicked atomic.Bool
	repo := &mockModelRepo{afterConfirm: func() {
		if panicked.CompareAndSwap(false, true) {
			panic("a provider answer blew up mid-retirement")
		}
	}}
	h := newGoneHandler(t, repo)

	doomed := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	doomedCand := goneCandidateFor(t, doomed, "OpenCode Zen")
	for range goneStrikeThreshold {
		h.noteModelGone(doomedCand, endpointTypeChat)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !panicked.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !panicked.Load() {
		t.Fatal("the retirement never reached the write, so nothing was proved about surviving one")
	}

	// The gateway is still here, and still retiring models.
	next := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	nextCand := goneCandidateFor(t, next, "Google AI Studio (Gemini)")
	for range goneStrikeThreshold {
		h.noteModelGone(nextCand, endpointTypeChat)
	}

	calls := waitForDisable(t, repo)
	if len(calls) != 1 || calls[0].id != next.ID || !calls[0].committed {
		t.Fatalf("the retirement after the panic must land normally, got %+v", calls)
	}
}

// TestAttemptCandidate_ATranslationFailureKeepsTheStreak pins where the chat
// path is allowed to call a 200 a success.
//
// A vertex-express candidate answers in Gemini's wire format, and a 200 whose
// body is not a Gemini answer is a provider fault: the attempt FAILS OVER to the
// next candidate. Crediting the model on the 200 headers cleared the streak of a
// model the gateway was in the act of giving up on, so a dead model behind a
// provider that 200s garbage could never accumulate three consecutive strikes.
func TestAttemptCandidate_ATranslationFailureKeepsTheStreak(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// A 200 that is not a Gemini answer, so translateEgressResponseBody fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `<html>502 Bad Gateway</html>`)
	}))
	defer srv.Close()
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	// The hostname decides the dialect, which is why the transport above dials
	// the test server regardless of it.
	cand := goneCandidateAt(m, "Vertex Express", "http://us-central1-aiplatform.googleapis.com/v1")

	// One real refusal, so there is a streak for the 200 to wrongly clear.
	h.noteModelGone(cand, endpointTypeChat)
	streak := goneStreakFor(t, h, m.ID, probeChatEndpoint)
	if n := streak.count(); n != 1 {
		t.Fatalf("streak = %d, want 1 before the attempt", n)
	}

	st := &requestState{
		startTime:       time.Now(),
		reqModel:        "gemini-2.0-flash",
		bodyBytes:       []byte(`{"model":"gemini-2.0-flash","messages":[{"role":"user","content":"hi"}]}`),
		failoverTimeout: 30 * time.Second,
		logData:         &requestLogData{modelID: "gemini-2.0-flash", endpointType: endpointTypeChat},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

	// Two candidates, so a failover is available and the attempt is free to
	// reject this one.
	if got := h.attemptCandidate(w, r, st, cand, 0, 2); got != outcomeFailover {
		t.Fatalf("outcome = %v, want a failover on an untranslatable 200", got)
	}
	if n := streak.count(); n != 1 {
		t.Fatalf("streak = %d, want the strike kept: an attempt that failed over did not serve the model", n)
	}
}

// TestGoneStreak_SupersedeReportsWhetherItChangedAnything pins the report the
// log line depends on.
//
// Nothing removes a streak any more, which is what keeps the probe cooldown. The
// consequence is that a model which drew a single refusal at 09:00 carries a
// parked streak for the life of the process, and every successful request to it
// from then on reaches supersede with nothing left to do. An unconditional line
// would claim to have "cleared gone-strikes" on every one of them.
func TestGoneStreak_SupersedeReportsWhetherItChangedAnything(t *testing.T) {
	t.Parallel()

	s := &goneStreak{}
	s.strike(time.Now())
	if !s.supersede() {
		t.Fatal("the first success had a strike to clear and said it did nothing")
	}
	if s.supersede() {
		t.Fatal("a second success on an already-superseded streak did no work but reported that it had")
	}

	// A fresh refusal makes it real work again.
	s.strike(time.Now())
	if !s.supersede() {
		t.Fatal("a success after a new strike had evidence to clear")
	}

	// And the early return is deliberately narrow: an empty count with no
	// tombstone can still belong to a disable this success has to stand down,
	// which is the state a claim leaves behind.
	claimed := &goneStreak{}
	for range goneStrikeThreshold {
		claimed.strike(time.Now())
	}
	if !claimed.claimProbe(time.Now()) {
		t.Fatal("a streak at the threshold must admit the first claim")
	}
	claimed.park()
	if !claimed.supersede() {
		t.Fatal("a success against a claimed streak must still set the tombstone")
	}
}

// TestGoneStreak_SupersedeIsAtomicToAReader pins the one ordering the revert
// path depends on and cannot check for itself.
//
// The disable goroutine reads the tombstone without the lock and then asks for
// the count, and it treats "cancelled, but the strikes are still standing" as
// "the model is refusing again" and skips the revert. If a success sets the flag
// before it clears the count, that reader can land in the gap and see the very
// strikes that caused the retirement it is holding — so a model that has just
// answered stays disabled, the count is parked at zero a moment later, and
// nothing ever triggers a revert again. An operator has to re-enable it by hand,
// which is the outcome the whole cancellation exists to prevent.
//
// Driven directly against the streak, and by holding its lock rather than by
// racing it. The real window is one mutex acquire, so a test that merely raced
// would pass against the broken ordering almost every time — a test that cannot
// fail pins nothing. Holding mu is what any concurrent strike or claim does
// anyway, and it makes the question exact: while the count CANNOT be cleared,
// the tombstone must not be visible either.
func TestGoneStreak_SupersedeIsAtomicToAReader(t *testing.T) {
	t.Parallel()

	s := &goneStreak{}
	now := time.Now()
	for range goneStrikeThreshold {
		s.strike(now)
	}

	s.mu.Lock()
	started := make(chan struct{})
	go func() {
		close(started)
		s.supersede()
	}()
	<-started
	// Long enough that a supersede setting the tombstone outside the lock would
	// have done it by now. The fixed one cannot do it at any wait, so the
	// assertion below has no flaky direction.
	time.Sleep(50 * time.Millisecond)
	visible := s.cancelled.Load()
	s.mu.Unlock()
	if visible {
		t.Fatal("the tombstone became visible while the strikes it stands down were still on the streak: a reader landing here skips the revert and leaves a model that just answered disabled")
	}

	// And once it can run, both halves land together.
	deadline := time.Now().Add(2 * time.Second)
	for !s.cancelled.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !s.cancelled.Load() {
		t.Fatal("the success never stood the disable down")
	}
	if n := s.count(); n != 0 {
		t.Fatalf("streak = %d, want the evidence cleared alongside the tombstone", n)
	}
}

// TestNoteModelGone_ProbeCooldownBoundsTheProbeRate pins the bound itself.
//
// The postponement above must not become an invitation to keep asking. A dead
// model under a client's retry loop draws gone-classified refusals as fast as
// the client sends them, and every one of them arrives at a streak already
// sitting on the threshold. Without the cooldown each one is a fresh chance to
// spend an upstream request — and the probe deliberately bypasses both the rate
// limiter and the circuit breaker, so nothing else would slow it down. Deleting
// this test loses the only thing standing between a provider incident and the
// gateway hammering the provider that is having it.
// A model whose probe can never answer keeps its strikes and keeps spending one
// upstream request per cooldown, indefinitely — a provider rate limiting the
// gateway, or a model that cannot be reached on the surface the probe asks. The
// run counter is what escalates that from Info to Warn, so it has to survive
// repeated postponements and end when the question is finally settled.
func TestNoteModelGone_UnadjudicableModelIsEscalated(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	waitForProbes(t, script, 1)
	waitForInconclusiveRun(t, h, m.ID, probeChatEndpoint, 1)

	// Each further refusal past the cooldown buys another probe that answers
	// nothing, and the run counts them rather than resetting with each verdict.
	for probes := 2; probes <= goneProbeInconclusiveWarnAfter; probes++ {
		expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
		h.noteModelGone(cand, endpointTypeChat)
		waitForProbes(t, script, probes)
		waitForInconclusiveRun(t, h, m.ID, probeChatEndpoint, probes)
	}

	// The model answers, which is the outcome the run was waiting for. Nothing
	// is disabled and the run ends with the evidence it belonged to.
	script.answer(http.StatusOK, goneServedAnswer)
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)
	waitForInconclusiveRun(t, h, m.ID, probeChatEndpoint, 0)
	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a model that answered its probe must not be retired, got %+v", calls)
	}
}

// TestNoteModelGone_ASurfaceTheModelIsNotForNeverStrikes pins the second gate.
//
// Nothing filters a request by modality on the way in: `POST /v1/embeddings`
// naming a chat model is forwarded to the provider's embeddings endpoint, and a
// provider that answers "gpt-4o is not supported for embeddings" has named the
// model beside a gone-phrase, which the classifier reads as a retirement. The
// probe cannot save the model from that, because it asks on the family the
// strikes arrived on: it reproduces the misuse, draws the same refusal, and
// confirms a retirement of a model that serves chat perfectly. And the disable
// is model-wide, so one misconfigured client would take it out of routing
// everywhere.
//
// The control case is the same model, same refusal, on the surface it IS for.
func TestNoteModelGone_ASurfaceTheModelIsNotForNeverStrikes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		inputModalities  string
		outputModalities string
		endpointType     string
		endpoint         string
		wantStrike       bool
	}{
		// A chat model sent to the embeddings surface: the refusal is about the
		// misuse, not about the model.
		{"text model on embeddings", `["text"]`, `["text"]`, endpointTypeEmbeddings, probeEmbeddingsEndpoint, false},
		// And the mirror image, which is just as wrong.
		{"embedding model on chat", `["text"]`, `["embedding"]`, endpointTypeChat, probeChatEndpoint, false},
		// Every other non-chat class is the same mistake with a different noun,
		// and all of them are reachable: nothing filters a request by modality on
		// the way in, and a provider that answers "Model 'flux-1.1-pro' is not
		// supported for chat completions" has named the model beside a
		// gone-phrase.
		{"image model on chat", `["text"]`, `["image"]`, endpointTypeChat, probeChatEndpoint, false},
		{"video model on chat", `["text"]`, `["video"]`, endpointTypeChat, probeChatEndpoint, false},
		{"tts model on chat", `["text"]`, `["audio"]`, endpointTypeChat, probeChatEndpoint, false},
		{"rerank model on chat", `["text"]`, `["rerank"]`, endpointTypeChat, probeChatEndpoint, false},
		// Speech-to-text produces text like any chat model and gives itself away
		// on the input side, which is why both arrays are read.
		{"stt model on chat", `["audio"]`, `["text"]`, endpointTypeChat, probeChatEndpoint, false},
		// The same refusals on the surfaces the models are for.
		{"text model on chat", `["text"]`, `["text"]`, endpointTypeChat, probeChatEndpoint, true},
		{"embedding model on embeddings", `["text"]`, `["embedding"]`, endpointTypeEmbeddings, probeEmbeddingsEndpoint, true},
		// A chat model that also emits images is a chat model; the text admits
		// it, and so does a vision model's image INPUT.
		{"image-emitting chat model", `["text"]`, `["text","image"]`, endpointTypeChat, probeChatEndpoint, true},
		{"vision chat model", `["text","image"]`, `["text"]`, endpointTypeChat, probeChatEndpoint, true},
		{"code model on chat", `["text"]`, `["code"]`, endpointTypeChat, probeChatEndpoint, true},
		// A model that serves BOTH probeable surfaces strikes on neither, and
		// that is the third gate rather than this one: the refusal is about one
		// surface and the disable it could lead to is about the row, which for
		// this model is two surfaces. See modalityAdmitsBothProbeSurfaces.
		{"serves both surfaces, refused on chat", `["text"]`, `["text","embedding"]`, endpointTypeChat, probeChatEndpoint, false},
		{"serves both surfaces, refused on embeddings", `["text"]`, `["text","embedding"]`, endpointTypeEmbeddings, probeEmbeddingsEndpoint, false},
		// The two surfaces answer to different burdens of proof when the catalog
		// says nothing, and this is where that shows. liveModelStub writes "[]"
		// for every model no catalog covers, so failing open on embeddings would
		// leave the whole misuse case reachable for exactly those rows; failing
		// closed on chat would switch traffic-driven retirement off for them
		// altogether.
		{"unknown modality on embeddings", "", "", endpointTypeEmbeddings, probeEmbeddingsEndpoint, false},
		{"empty modality on embeddings", "[]", "[]", endpointTypeEmbeddings, probeEmbeddingsEndpoint, false},
		{"unparseable modality on embeddings", `{"not":"a list"}`, `{"not":"a list"}`, endpointTypeEmbeddings, probeEmbeddingsEndpoint, false},
		{"unknown modality on chat", "", "", endpointTypeChat, probeChatEndpoint, true},
		{"empty modality on chat", "[]", "[]", endpointTypeChat, probeChatEndpoint, true},
		{"unparseable modality on chat", `{"not":"a list"}`, `{"not":"a list"}`, endpointTypeChat, probeChatEndpoint, true},
		// A declared output with an undeclared input is still admitted: an
		// unreadable array is not evidence, and only evidence rules a surface out.
		{"text out, unknown in, on chat", "", `["text"]`, endpointTypeChat, probeChatEndpoint, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockModelRepo{}
			h := newGoneHandler(t, repo)
			m := &model.Model{ID: uuid.New(), ModelID: "gpt-4o", InputModalities: tc.inputModalities, OutputModalities: tc.outputModalities}
			cand := goneCandidateFor(t, m, "OpenAI")

			h.noteModelGone(cand, tc.endpointType)

			raw, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: tc.endpoint})
			if ok != tc.wantStrike {
				t.Fatalf("streak recorded = %v, want %v", ok, tc.wantStrike)
			}
			if !tc.wantStrike {
				return
			}
			if streak, isStreak := raw.(*goneStreak); !isStreak || streak.count() != 1 {
				t.Fatalf("expected exactly one strike, got %+v", raw)
			}
		})
	}
}
