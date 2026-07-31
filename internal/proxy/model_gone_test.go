package proxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// goneAPIKey is the decrypted credential the fixture candidates carry. The
// probe authenticates exactly like real traffic, so a candidate without one is
// not the candidate production hands to noteModelGone.
const goneAPIKey = "sk-gone-fixture-key"

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

// waitForStreakCleared blocks until the disable goroutine has finished dropping
// the model's streak. Observing the SetEnabled call is not enough: the call is
// recorded inside the mock, while the streak is cleared only after it returns,
// so a test that starts a second round on the call alone can strike the OLD
// streak — pushing it past the threshold, where every strike is a no-op — and
// then watch the first goroutine delete the evidence.
func waitForStreakCleared(t *testing.T, h *Handler, id uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := h.goneStrikes.Load(id); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the streak was never cleared")
}

// goneRefusalServer starts a fake provider that refuses modelID as retired on
// every request: a 404 naming the model beside a phrase from modelGoneVerbs,
// which is precisely what classifyUpstreamError reads as KindProviderModelGone.
//
// It exists because the probe now adjudicates every retirement. The tests below
// are about what the disable machinery does once the evidence is in — the
// threshold, the two cancellation windows, the rollback, the streak-identity
// rules — and every one of them was written expecting the disable to be
// written. Giving the probe nothing to reach would turn each of them into an
// assertion about an unreachable provider instead, so the fixture reproduces
// the evidence rather than standing in for it.
func goneRefusalServer(t *testing.T, modelID string) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf("{\"error\":{\"message\":\"The model `%s` does not exist\"}}", modelID)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// goneCandidateFor builds the candidate the retirement path actually carries,
// pointed at a provider that refuses this model. It is the whole candidate and
// not just the model because the probe needs a base URL, a provider name and a
// decrypted key to ask anything at all.
func goneCandidateFor(t *testing.T, m *model.Model, providerName string) modelCandidate {
	t.Helper()
	return modelCandidate{
		model:    m,
		provider: &provider.Provider{ID: uuid.New(), Name: providerName, BaseURL: goneRefusalServer(t, m.ModelID).URL},
		apiKey:   goneAPIKey,
	}
}

// newGoneHandler builds a Handler carrying a real shared upstream transport,
// which is what production has and what the pre-retirement probe now goes out
// over.
func newGoneHandler(t *testing.T, repo *mockModelRepo) *Handler {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return &Handler{modelRepo: repo, upstreamTransport: tr}
}

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

// TestNoteModelServed_ResetsStreak pins that the streak must be consecutive. A
// provider blip that happens to match a gone-pattern must not accumulate
// towards a disable across an otherwise healthy period.
func TestNoteModelServed_ResetsStreak(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "glm-5.2"}
	cand := goneCandidateFor(t, m, "OpenCode Zen")

	for range goneStrikeThreshold * 3 {
		// One short of the threshold, then a success, forever.
		for i := 1; i < goneStrikeThreshold; i++ {
			h.noteModelGone(cand, endpointTypeChat)
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
	h := newGoneHandler(t, repo)
	dead := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	alive := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-5"}
	deadCand := goneCandidateFor(t, dead, "OpenCode Zen")

	for range goneStrikeThreshold {
		h.noteModelGone(deadCand, endpointTypeChat)
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
// The nil provider is the case the widened signature adds. It used to be
// unreachable — the old parameter was a provider NAME, and a missing one was
// just an empty string in a log line — so a candidate with no provider counted
// strikes and disabled the model like any other. It cannot now: the disable is
// adjudicated by a real request, there is nobody to send it to, and the
// retirement stops rather than being written on evidence that can never be
// checked.
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
		upstream ErrorKind
		produced bool
		want     streamVerdict
	}{
		{"clean finish that delivered content proves the model answered", "", "", true, verdictServed},
		{"explicit gone report strikes", KindProviderModelGone, KindProviderModelGone, true, verdictGone},
		{"gone report strikes even with no content", KindProviderModelGone, KindProviderModelGone, false, verdictGone},
		// The provider's verdict must survive a later cause overwriting the
		// recorded kind. A client hanging up on the error chunk is the ordinary
		// case, and judging the model by that would let the client suppress the
		// evidence by reacting to it.
		{"client hangup cannot erase the provider's gone report", KindClientDisconnect, KindProviderModelGone, false, verdictGone},
		{"nor can a stall reported after it", KindProviderTimeout, KindProviderModelGone, false, verdictGone},
		// A stream that opened, emitted nothing and ended without recording an
		// error is not proof of anything. Crediting it would clear a retirement
		// streak on the strength of an empty response.
		{"truncated stream with no content is inconclusive", "", "", false, verdictInconclusive},
		// Everything below is a failure that says nothing about whether the
		// model exists, so it must not clear the streak.
		{"transient provider error", KindProviderError, KindProviderError, true, verdictInconclusive},
		{"client hung up", KindClientDisconnect, "", true, verdictInconclusive},
		{"provider stalled", KindProviderTimeout, "", false, verdictInconclusive},
		{"failover deadline", KindFailoverTimeout, "", false, verdictInconclusive},
		{"retry deadline", KindRetryTimeout, "", false, verdictInconclusive},
		{"payload rejected", KindProviderBadRequest, KindProviderBadRequest, false, verdictInconclusive},
		{"not entitled", KindProviderNotEntitled, KindProviderNotEntitled, false, verdictInconclusive},
		{"gateway fault", KindInternal, "", false, verdictInconclusive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := verdictForStream(tc.kind, tc.upstream, tc.produced); got != tc.want {
				t.Errorf("verdictForStream(%q, upstream=%q, produced=%v) = %v, want %v", tc.kind, tc.upstream, tc.produced, got, tc.want)
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
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
		// An empty, error-free stream lands between strikes. The log entry
		// carries its endpoint family exactly as ingest stamps it, since that is
		// what noteStreamOutcome forwards on a gone verdict.
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat}, cand)
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
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "glm-5.2"}
	cand := goneCandidateFor(t, m, "OpenCode Zen")

	for range goneStrikeThreshold * 3 {
		for i := 1; i < goneStrikeThreshold; i++ {
			h.noteModelGone(cand, endpointTypeChat)
		}
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat, tokensCompletion: 5, ttftMs: 30}, cand)
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
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
		// A transient stream failure lands between strikes. Under the old
		// "anything not gone is a success" rule this cleared the streak and the
		// model could never be retired.
		if v := verdictForStream(KindProviderError, KindProviderError, true); v == verdictServed {
			h.noteModelServed(m)
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the model to still be disabled, got %d disable calls", len(calls))
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

// TestNoteModelGone_RealRetirementStillDisables is the paired positive: the
// veto must not have blunted the feature itself.
func TestNoteModelGone_RealRetirementStillDisables(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")
	body := `{"error":{"code":404,"message":"This model models/gemini-2.0-flash is no longer available. Please update your code to use a newer model."}}`

	for range goneStrikeThreshold {
		if kind, _ := classifyUpstreamError(404, body, "gemini-2.0-flash"); kind == KindProviderModelGone {
			h.noteModelGone(cand, endpointTypeChat)
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("a genuinely retired model must still be disabled, got %d disable calls", len(calls))
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

// TestNoteModelGone_FailedDisableIsRetried pins the failure path of the
// no-clear-on-success rule. Leaving the count above the threshold is what stops
// a burst re-disabling and re-alerting, but a disable that never landed must not
// be parked there forever, or a transient database error would leave a dead
// model enabled permanently.
func TestNoteModelGone_FailedDisableIsRetried(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{setEnabledErr: errors.New("database unavailable")}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("expected the first (failing) attempt, got %d", len(calls))
	}
	waitForStreakCleared(t, h, m.ID)

	// The streak was cleared by the failure, so a fresh run of refusals must
	// reach the threshold and try again rather than being swallowed.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

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
	h.noteModelServed(m)
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
	h.noteModelServed(m)
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
	repo.afterConfirm = func() { h.noteModelServed(m) }

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

// TestNoteModelGone_StaleFailedDisableKeepsANewerStreak pins that a disable
// goroutine may only retire the streak it was actually started for.
//
// The sequence is reachable because the write is detached: the goroutine can
// still be inside SetEnabled when the model answers (clearing its streak) and
// later refusals build a fresh one. If its cleanup then deleted by model id, it
// would erase that newer count on its way out — and a model refusing every
// request would restart from zero each time a disable failed, staying enabled
// exactly when it is most clearly dead.
func TestNoteModelGone_StaleFailedDisableKeepsANewerStreak(t *testing.T) {
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

	// While it is held open: the model answers, dropping that streak, and then
	// refuses again, building a new one one strike short of the threshold.
	h.noteModelServed(m)
	for range goneStrikeThreshold - 1 {
		h.noteModelGone(cand, endpointTypeChat)
	}
	newer, ok := h.goneStrikes.Load(m.ID)
	if !ok {
		t.Fatal("the later refusals did not start a new streak")
	}

	// Release the stale write, then hold the newer streak under observation.
	// Asserting on identity rather than on the disable count is what makes this
	// deterministic: an unconditional delete erases the entry within
	// microseconds of the write returning, whereas counting disables would race
	// the cleanup against the strike below and could pass either way.
	close(repo.setEnabledGate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(repo.disableCalls()) < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	watch := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(watch) {
		switch current, ok := h.goneStrikes.Load(m.ID); {
		case !ok:
			t.Fatal("the stale failed disable deleted the newer streak")
		case current != newer:
			t.Fatal("the newer streak was replaced while the stale disable unwound")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// One more refusal now completes the newer streak, so a second disable must
	// be attempted. If the stale cleanup had erased it, this would be strike one
	// of three and nothing would happen.
	h.noteModelGone(cand, endpointTypeChat)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.disableCalls()) >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("a stale failed disable erased the newer streak: only %d disable attempts", len(repo.disableCalls()))
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
	repo.afterConfirm = func() { h.noteModelServed(m) }

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
	raw, ok := h.goneStrikes.Load(m.ID)
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
			h.noteModelServed(m)
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
	repo.afterConfirm = func() { h.noteModelServed(m) }

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

// TestNoteStreamOutcome_InconclusiveTouchesNeitherCounter pins the middle
// verdict end to end, through the shared entry point both dispatch paths use.
//
// The verdict table is tested directly elsewhere, but that does not prove
// noteStreamOutcome acts on it: a stream that failed for an unrelated reason
// must leave the streak exactly where it was. Clearing it there lets a retired
// model stay routable forever on the strength of its own unrelated failures,
// and striking there retires a healthy model during an outage.
func TestNoteStreamOutcome_InconclusiveTouchesNeitherCounter(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// One short of the threshold, so a stray strike would disable and a stray
	// clear would be visible as a restart.
	for range goneStrikeThreshold - 1 {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// A timeout, a client disconnect and a transient provider failure: none of
	// them is evidence about whether the model still exists.
	for _, kind := range []ErrorKind{KindProviderTimeout, KindClientDisconnect, KindProviderError} {
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat, errorKind: kind}, cand)
	}

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("an inconclusive stream must not disable anything, got %+v", calls)
	}

	// The streak survived intact, so the next real refusal completes it.
	h.noteModelGone(cand, endpointTypeChat)
	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("an inconclusive stream cleared the streak: expected the disable, got %+v", calls)
	}
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
	repo.afterConfirm = func() { h.noteModelServed(m) }

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
