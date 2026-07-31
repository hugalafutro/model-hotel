package proxy

import (
	"errors"
	"fmt"
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

// goneStreakFor returns the model's live streak, failing the test if there is
// none. The streak is what the postponement paths now leave behind, so several
// tests below assert on it directly rather than on the absence of a write.
func goneStreakFor(t *testing.T, h *Handler, id uuid.UUID) *goneStreak {
	t.Helper()
	raw, ok := h.goneStrikes.Load(id)
	if !ok {
		t.Fatal("the model has no streak")
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		t.Fatal("unexpected streak type")
	}
	return streak
}

// waitForStreakCount blocks until the model's streak holds want strikes, for the
// paths that reset the count in place instead of dropping the entry. The reset
// happens on the detached goroutine, so reading the count straight after the
// refusals that triggered it races the reset rather than observing it.
func waitForStreakCount(t *testing.T, h *Handler, id uuid.UUID, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, ok := h.goneStrikes.Load(id); ok {
			if s, ok := raw.(*goneStreak); ok && s.count() == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the streak never settled at %d strikes", want)
}

// expireProbeCooldown ages the model's probe claim so the next refusal may spend
// a probe, standing in for goneProbeCooldown having elapsed.
//
// Driven through the streak's own field, exactly as the staleness test drives
// lastStrike. The alternative — a knob, an injected clock, a var instead of a
// const — would put a seam in production code that exists only for tests, and
// the wait it replaces is five minutes.
func expireProbeCooldown(t *testing.T, h *Handler, id uuid.UUID) {
	t.Helper()
	streak := goneStreakFor(t, h, id)
	streak.mu.Lock()
	streak.nextProbeAt = time.Now().Add(-time.Second)
	streak.mu.Unlock()
}

// waitForProbes blocks until the fake provider has been asked n times, so a
// test can assert on what a probe did rather than on how long it took. Without
// it, "no second probe was sent" and "the second probe has not been sent yet"
// are the same observation.
func waitForProbes(t *testing.T, script *goneScriptedServer, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if paths := script.requestedPaths(); len(paths) >= n {
			return paths
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d probe(s), got %v", n, script.requestedPaths())
	return nil
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
	body := goneRefusalBody(modelID)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// goneRefusalBody is the retirement refusal itself, split out from the server so
// a test that needs its own server can still produce the one body
// classifyUpstreamError reads as KindProviderModelGone. The model id has to look
// like one (looksLikeAModelID wants a digit or a hyphen) or the classifier will
// not attribute the phrase to it.
func goneRefusalBody(modelID string) string {
	return fmt.Sprintf("{\"error\":{\"message\":\"The model `%s` does not exist\"}}", modelID)
}

// goneServedAnswer is a real completion: a 200 carrying visible content, which
// is what probeDeliveredContent requires before it will call a probe served.
// Deliberately not an empty 200 — that is inconclusive, not a success.
const goneServedAnswer = `{"id":"chatcmpl-probe","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"completion_tokens":1}}`

// goneRateLimitedAnswer is the provider incident the postponement exists for: a
// 429 says nothing whatever about whether the model still exists.
const goneRateLimitedAnswer = `{"error":{"message":"Rate limit reached for requests","type":"requests"}}`

// goneScriptedServer is a fake provider whose answer a test can change while the
// test is running, and which records every path it was asked for.
//
// Both halves are needed and neither is served by the fixtures already here.
// goneRefusalServer only ever refuses, and the postponement test has to switch a
// live candidate from a 429 to a refusal without moving it to a second base URL
// — the candidate is built once and carries the URL. probeServer (model_probe_test.go)
// keeps only the LAST path, whereas the family tests assert over every path:
// "never asked on /chat/completions" and "never asked at all" are claims about
// all of them.
type goneScriptedServer struct {
	mu     sync.Mutex
	status int
	body   string
	paths  []string
}

// answer replaces what the server returns from the next request onwards.
func (s *goneScriptedServer) answer(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

// requestedPaths returns a copy of every path the server was asked for, in
// order. Copied under the lock: the probe runs on a detached goroutine, so the
// server's handler may be appending while a test reads.
func (s *goneScriptedServer) requestedPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// newGoneScriptedServer starts the fake provider with its opening answer.
func newGoneScriptedServer(t *testing.T, status int, body string) (*httptest.Server, *goneScriptedServer) {
	t.Helper()
	script := &goneScriptedServer{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		script.mu.Lock()
		status, body := script.status, script.body
		script.paths = append(script.paths, r.URL.Path)
		script.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, script
}

// goneCandidateFor builds the candidate the retirement path actually carries,
// pointed at a provider that refuses this model. It is the whole candidate and
// not just the model because the probe needs a base URL, a provider name and a
// decrypted key to ask anything at all.
func goneCandidateFor(t *testing.T, m *model.Model, providerName string) modelCandidate {
	t.Helper()
	return goneCandidateAt(m, providerName, goneRefusalServer(t, m.ModelID).URL)
}

// goneCandidateAt is the same candidate pointed at a base URL the test chose,
// for the cases whose whole subject is what the provider answers.
func goneCandidateAt(m *model.Model, providerName, baseURL string) modelCandidate {
	return modelCandidate{
		model:    m,
		provider: &provider.Provider{ID: uuid.New(), Name: providerName, BaseURL: baseURL},
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
	expireProbeCooldown(t, h, m.ID)
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
	h.noteModelServed(m)
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
		if n := goneStreakFor(t, h, m.ID).count(); n != goneStrikeThreshold-1 {
			t.Fatalf("the stale failed disable took the newer evidence with it: streak = %d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// One more refusal completes the rebuilt streak. The cooldown from the first
	// claim is still running, so this is also the point at which the retry
	// becomes reachable rather than immediate.
	expireProbeCooldown(t, h, m.ID)
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

// TestNoteModelGone_AnsweredProbePreventsTheDisable is the whole point of
// verifying before retiring: three refusals read out of provider prose are a
// nomination, not a verdict, and a model that answers a request the gateway
// makes itself is not retired however the classifier read the traffic.
//
// Delete this and a drifted gone-pattern — a phrasing some provider starts using
// for a transient fault — takes a working model out of routing after three
// requests, which is exactly what the old code did.
func TestNoteModelGone_AnsweredProbePreventsTheDisable(t *testing.T) {
	t.Parallel()

	// Guard on the fixture rather than trusting it. A served verdict and an
	// inconclusive one are indistinguishable from outside — neither writes
	// anything and both drop the streak — so a body that failed to parse would
	// let every assertion below pass without the served branch ever running.
	if !probeDeliveredContent(endpointTypeChat, []byte(goneServedAnswer)) {
		t.Fatal("the fixture answer does not read as served, so this test would pass on an inconclusive probe")
	}

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusOK, goneServedAnswer)
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// disableCalls records every attempt, committed or not, and RevertAutoRetire
	// records into the same list — so an empty list is the full claim: nothing
	// was written, and nothing had to be undone.
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a model that answered a direct probe must not be retired, got %+v", calls)
	}
	// The count is reset by the served branch, so the model needs three FRESH
	// refusals before it is reconsidered. The entry itself stays — it is what
	// carries the probe cooldown, which the test below is about.
	waitForStreakCount(t, h, m.ID, 0)
	// And the outcome came from asking rather than from nothing happening.
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeChatEndpoint, paths)
	}
}

// TestNoteModelGone_APanicWhileDisablingDoesNotKillTheGateway pins the
// recover() on the detached goroutine.
//
// That goroutine used to call repository methods and nothing else. It now makes
// an upstream request and runs bytes a provider chose through json and the
// dialect translators, and an unrecovered panic on any goroutine takes the whole
// process down — so one model's retirement would be able to kill a gateway that
// is serving everything else fine. Every other detached goroutine in this
// package already recovers.
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

// TestNoteModelGone_AnsweredProbeKeepsTheCooldown pins the bound on the one
// verdict that is expected to REPEAT.
//
// A model whose real traffic keeps drawing retirement prose while a minimal
// probe keeps answering is not an edge case: it is the disagreement the whole
// feature exists to catch, and it does not resolve itself. Three refusals, a
// probe, a served verdict, and the traffic goes straight back to refusing. If
// the served branch drops the whole streak the way a real success does, the
// cooldown goes with it, the rebuilt streak starts from a zero-valued one that
// admits immediately, and the gateway probes once per three refusals for as long
// as the disagreement lasts — roughly three upstream requests a second under a
// client retry loop, at a provider that is already answering everything with an
// error.
//
// Delete this test and that regression is invisible: every other served-probe
// assertion still passes, because the model is still not retired.
func TestNoteModelGone_AnsweredProbeKeepsTheCooldown(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusOK, goneServedAnswer)
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	// Wait for the probe to have happened, then for the served branch to have
	// finished with the streak. Without both, "no second probe" and "the first
	// probe has not landed yet" are the same observation.
	waitForProbes(t, script, 1)
	waitForStreakCount(t, h, m.ID, 0)

	// The traffic goes back to refusing, which is what a real disagreement looks
	// like. The count is allowed to climb again — that is the backoff working —
	// but not one of these may buy an upstream request inside the cooldown.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a model that answered a direct probe must not be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("refusals inside the cooldown must cost no further upstream requests, got %v", paths)
	}

	// And it is a delay rather than a lockout: once the cooldown lapses the model
	// is reconsidered on the strikes it has rebuilt since.
	expireProbeCooldown(t, h, m.ID)
	h.noteModelGone(cand, endpointTypeChat)
	waitForProbes(t, script, 2)
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

// TestNoteModelGone_ASuccessDoesNotResetTheProbeCooldown closes the other door
// into the same unbounded loop.
//
// Clearing the count on a served probe was only half the bound, because a real
// request succeeding clears it too — and that is the MIXED case, which is at
// least as likely as the pure one: a provider that refuses one request shape
// with retirement prose while serving another produces both signals from the
// same client traffic. If the success drops the whole streak, the probe cooldown
// goes with it, and three more refusals buy another upstream call immediately.
// A success clears what the model is accused of; it does not buy the gateway a
// free probe.
func TestNoteModelGone_ASuccessDoesNotResetTheProbeCooldown(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusOK, goneServedAnswer)
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	waitForProbes(t, script, 1)
	waitForStreakCount(t, h, m.ID, 0)

	// An ordinary request to the same model succeeds, exactly as the request
	// path reports it.
	h.noteModelServed(m)

	// And the traffic that does not succeed carries on refusing. None of it may
	// buy an upstream request while the cooldown is running.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a model that answered must not be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("a success must not reset the probe cooldown, got %v", paths)
	}

	// Still a delay rather than a lockout.
	expireProbeCooldown(t, h, m.ID)
	h.noteModelGone(cand, endpointTypeChat)
	waitForProbes(t, script, 2)
}

// TestNoteModelGone_RefusedProbeStillDisables is the paired control for the test
// above: identical strikes, identical code path, the provider's answer the only
// difference, and the opposite outcome.
//
// Together they pin that the retirement is written on what the probe found and
// not on the strike count. This is the half that keeps the verification from
// quietly disarming the feature — the probe may only ever PREVENT a disable,
// never weaken one that three refusals and a refused probe have earned.
func TestNoteModelGone_RefusedProbeStillDisables(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gpt-5.6-sol"))
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a probe that refused the model must still retire it, got %+v", calls)
	}
	if calls[0].id != m.ID || calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable of %s, got %+v", m.ID, calls[0])
	}
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeChatEndpoint, paths)
	}
}

// TestNoteModelGone_RateLimitedProbePostponesThenRetires pins the postponement
// and its exit in one sequence.
//
// A 429 is the provider incident case: it establishes nothing about whether the
// model exists, so retiring on it would turn one bad afternoon at a provider
// into a mass retirement of its whole catalog. But postponing must not become
// permanent either — a model that is genuinely dead has to be retirable once the
// incident passes. Deleting this test loses one or the other: either the
// gateway retires models it never verified, or the inconclusive branch parks the
// streak above the threshold where every later refusal is a no-op and the dead
// model stays enabled forever.
func TestNoteModelGone_RateLimitedProbePostponesThenRetires(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	// One server that changes its answer, not two servers: the candidate is built
	// once and carries the base URL, so the provider has to be the same host
	// before and after the incident.
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a rate-limited probe proved nothing, so nothing may be retired on it, got %+v", calls)
	}

	// The postponement KEEPS the evidence. Dropping the streak was how the
	// retirement used to stay reachable, and it was also what left the probe
	// rate unbounded: three fresh refusals bought another probe, forever, at a
	// provider that was already rate limiting us. The claim is what gates the
	// retry now, so the strikes stay where they are.
	if n := goneStreakFor(t, h, m.ID).count(); n != goneStrikeThreshold {
		t.Fatalf("a postponed retirement must keep the strikes it was built on, got a streak of %d", n)
	}

	// The incident passes and the model turns out to be genuinely gone. One
	// refusal is now enough, precisely because the earlier strikes survived.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID)
	h.noteModelGone(cand, endpointTypeChat)

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a postponed retirement must still be reachable once the provider answers again, got %+v", calls)
	}
	if calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable, got %+v", calls[0])
	}
	if paths := script.requestedPaths(); len(paths) != 2 {
		t.Errorf("expected one probe per claim, got %v", paths)
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
func TestNoteModelGone_ProbeCooldownBoundsTheProbeRate(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	// Wait for the first probe rather than assuming it: "no second probe" and
	// "no probe yet" would otherwise be indistinguishable, and the test would
	// pass against code that never probed at all.
	waitForProbes(t, script, 1)

	// A client retrying the dead model. Every one of these lands on a streak
	// that is already at the threshold, and not one of them may buy a probe.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("nothing established anything, so nothing may be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("30 refusals inside the cooldown must cost exactly one upstream request, got %v", paths)
	}

	// The bound is a delay, not a lockout: once the cooldown lapses the next
	// refusal probes again.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID)
	h.noteModelGone(cand, endpointTypeChat)

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("the cooldown must expire, got %+v", calls)
	}
	if paths := waitForProbes(t, script, 2); len(paths) != 2 {
		t.Errorf("expected exactly one probe per claim, got %v", paths)
	}
}

// TestNoteModelGone_ExhaustedProbeSlotsPostponeWithoutAsking pins the other
// bound: the one across models rather than across time.
//
// A provider event nominates its whole catalog at once, and each nomination that
// reaches the threshold runs on its own detached goroutine. Without a cap they
// all open a connection to the same host simultaneously, each holding it for up
// to goneProbeTimeout, so the gateway's diagnosis of an incident becomes part of
// the incident. What the cap must NOT do is convert a missing slot into a
// retirement: no request was sent, so nothing was established, and the honest
// outcome is the same postponement every other unproven case gets.
func TestNoteModelGone_ExhaustedProbeSlotsPostponeWithoutAsking(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("claude-sonnet-4"))
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	// Every slot for this provider taken, as a burst of concurrent retirements
	// against the same host leaves them. Seeding the map is how the test gets
	// there without spawning goneProbeMaxConcurrent real probes and hoping they
	// overlap; the slots are per provider, so this constrains nothing outside
	// this test.
	full := make(chan struct{}, goneProbeMaxConcurrent)
	for range goneProbeMaxConcurrent {
		full <- struct{}{}
	}
	h.goneProbeSlots.Store(cand.provider.ID, full)

	// The provider WOULD refuse this model, so a probe that went out would
	// retire it. Nothing may be retired on a probe that never went out.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a probe that was never sent established nothing, so nothing may be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 0 {
		t.Fatalf("no slot was free, so no upstream request may be spent, got %v", paths)
	}
	if n := goneStreakFor(t, h, m.ID).count(); n != goneStrikeThreshold {
		t.Fatalf("the strikes must survive so the retry is reachable, got a streak of %d", n)
	}

	// A slot frees up and the cooldown lapses: the retirement lands on the
	// evidence that was already there.
	<-full
	expireProbeCooldown(t, h, m.ID)
	h.noteModelGone(cand, endpointTypeChat)

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a retirement postponed for want of a slot must still be reachable, got %+v", calls)
	}
	if calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable, got %+v", calls[0])
	}
	if paths := waitForProbes(t, script, 1); len(paths) != 1 {
		t.Errorf("expected exactly one probe, got %v", paths)
	}
}

// TestNoteModelGone_EmbeddingsAreProbedOnTheirOwnEndpoint pins that the probe
// asks the question on the surface the model actually serves.
//
// An embeddings model has no /chat/completions, so a chat probe against one
// fails for a reason that has nothing to do with retirement — and that failure
// reads as confirmation of the retirement it was supposed to adjudicate. The
// verification would then manufacture exactly the wrong answer, and every
// embeddings model that drew three classifier strikes would be retired
// unverified.
func TestNoteModelGone_EmbeddingsAreProbedOnTheirOwnEndpoint(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("text-embedding-3-small"))
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeEmbeddings)
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("an embeddings model refused by its own endpoint must still be retired, got %+v", calls)
	}

	paths := script.requestedPaths()
	if len(paths) != 1 || paths[0] != probeEmbeddingsEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeEmbeddingsEndpoint, paths)
	}
	for _, p := range paths {
		if p == probeChatEndpoint {
			t.Errorf("an embeddings model was probed on the chat endpoint (%s), which fails for reasons unrelated to retirement", p)
		}
	}
}

// TestNoteModelGone_UnprobeableFamiliesAreNeverRetired pins the family gate, and
// pins it before the strike rather than after.
//
// Image, speech and transcription models cannot be probed cheaply or safely: the
// call costs real money and real seconds, and a chat probe against one fails for
// reasons unrelated to retirement. Where the answer cannot be substantiated the
// correct outcome is to never auto-retire the family at all — falling back to
// the classifier alone would leave the guessing running unsupervised precisely
// where it is least observed.
//
// The streak assertion is the other half. A counter that can never fire is not
// evidence, and recording one would both manufacture the appearance of some and
// let image refusals top up a chat streak that CAN retire the model.
func TestNoteModelGone_UnprobeableFamiliesAreNeverRetired(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)

	families := []struct {
		name         string
		endpointType string
		modelID      string
	}{
		{"image", endpointTypeImage, "dall-e-3"},
		{"speech", endpointTypeTTS, "tts-1-hd"},
		// An entry whose family was never stamped is unprobeable for the same
		// reason as the two above: nothing can be established about it.
		{"unstamped", "", "gpt-5.6-sol"},
	}

	scripts := make([]*goneScriptedServer, len(families))
	for i, f := range families {
		m := &model.Model{ID: uuid.New(), ModelID: f.modelID}
		srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody(f.modelID))
		scripts[i] = script
		cand := goneCandidateAt(m, "OpenAI", srv.URL)

		// Well past the threshold: the gate is unconditional, not a delay.
		for range goneStrikeThreshold + 3 {
			h.noteModelGone(cand, f.endpointType)
		}
		if _, ok := h.goneStrikes.Load(m.ID); ok {
			t.Errorf("%s: a family that can never be adjudicated must not record a streak", f.name)
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("an unprobeable family must never be auto-retired, got %+v", calls)
	}
	for i, script := range scripts {
		if paths := script.requestedPaths(); len(paths) != 0 {
			t.Errorf("%s: an unprobeable family must not spend an upstream request, got %v", families[i].name, paths)
		}
	}
}

// TestNoteStreamOutcome_GoneVerdictReachesTheProbe covers the third call site.
//
// A model can be reported retired mid-stream, and that report arrives through
// noteStreamOutcome rather than through the non-streaming error path. If the
// gone verdict did not forward to the retirement machinery, a model that only
// ever fails on streaming requests would stay routable forever, which is most
// of the gateway's traffic.
//
// It does not claim to catch a forwarded family that differs from the one on
// the log entry, and deliberately does not try. noteStreamOutcome is reachable
// only from dispatchStreaming and serveHedgeWinner, both chat/messages
// surfaces, so a row driving any other family would pin an input the production
// code cannot produce.
//
// The probe assertion is what makes this about the verdict reaching the PROBE:
// a disable that landed without an upstream request would be the unverified
// retirement this whole change removes.
func TestNoteStreamOutcome_GoneVerdictReachesTheProbe(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gemini-2.0-flash"))
	cand := goneCandidateAt(m, "Google AI Studio (Gemini)", srv.URL)

	for range goneStrikeThreshold {
		// upstreamKind is what the provider said, which is the only thing that can
		// establish a retirement; the endpoint family is stamped at ingest exactly
		// as newPendingRequestLog leaves it.
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat, upstreamKind: KindProviderModelGone}, cand)
	}

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a model reported retired mid-stream must be retired like any other, got %+v", calls)
	}
	if calls[0].id != m.ID || calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable of %s, got %+v", m.ID, calls[0])
	}
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("the stream verdict must be adjudicated by a probe on %s, got %v", probeChatEndpoint, paths)
	}
}
