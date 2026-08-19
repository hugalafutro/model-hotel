package proxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
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
// none. The postponement paths leave a streak behind rather than a write, so
// several tests below assert on it directly.
func goneStreakFor(t *testing.T, h *Handler, id uuid.UUID, endpoint string) *goneStreak {
	t.Helper()
	raw, ok := h.goneStrikes.Load(goneStreakKey{model: id, endpoint: endpoint})
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
func waitForStreakCount(t *testing.T, h *Handler, id uuid.UUID, endpoint string, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, ok := h.goneStrikes.Load(goneStreakKey{model: id, endpoint: endpoint}); ok {
			if s, ok := raw.(*goneStreak); ok && s.count() == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the streak never settled at %d strikes", want)
}

// probeClaimAt reports when the model's streak will next admit a probe. The zero
// time means no claim has been spent on this surface.
func probeClaimAt(t *testing.T, h *Handler, id uuid.UUID, endpoint string) time.Time {
	t.Helper()
	streak := goneStreakFor(t, h, id, endpoint)
	streak.mu.Lock()
	defer streak.mu.Unlock()
	return streak.nextProbeAt
}

// expireProbeCooldown ages the model's probe claim so the next refusal may spend
// a probe, standing in for goneProbeCooldown having elapsed. Driven through the
// streak's own field, as the staleness test drives lastStrike: a knob or an
// injected clock would put a seam in production code that exists only for tests.
func expireProbeCooldown(t *testing.T, h *Handler, id uuid.UUID, endpoint string) {
	t.Helper()
	streak := goneStreakFor(t, h, id, endpoint)
	streak.mu.Lock()
	streak.nextProbeAt = time.Now().Add(-time.Second)
	streak.mu.Unlock()
}

// waitForInconclusiveRun blocks until the model's streak reports want probes in
// a row that established nothing. The verdict is applied on the detached probe
// goroutine, so reading the run straight after the refusal that triggered it
// races the update.
func waitForInconclusiveRun(t *testing.T, h *Handler, id uuid.UUID, endpoint string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		streak := goneStreakFor(t, h, id, endpoint)
		streak.mu.Lock()
		got = streak.inconclusive
		streak.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("inconclusive run settled at %d, want %d", got, want)
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
// which classifyUpstreamError reads as KindProviderModelGone.
//
// The tests below are about what the disable machinery does once the evidence is
// in — the threshold, the two cancellation windows, the rollback, the
// streak-identity rules — and the probe adjudicates every retirement, so giving
// it nothing to reach would turn each of them into an assertion about an
// unreachable provider instead.
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
// Neither half is served by the fixtures already here: goneRefusalServer only
// ever refuses, and the candidate is built once and carries its base URL, so a
// postponement test cannot switch to a second server. probeServer
// (model_probe_test.go) keeps only the LAST path, whereas the family tests
// assert over all of them.
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
		h.noteModelServed(m, endpointTypeChat)
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
		// A transient stream failure lands between strikes. Under an "anything
		// not gone is a success" rule this would clear the streak and the model
		// could never be retired.
		if v := verdictForStream(KindProviderError, KindProviderError, true); v == verdictServed {
			h.noteModelServed(m, endpointTypeChat)
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
	waitForStreakCount(t, h, m.ID, probeChatEndpoint, 0)
	// And the outcome came from asking rather than from nothing happening.
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeChatEndpoint, paths)
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
	waitForStreakCount(t, h, m.ID, probeChatEndpoint, 0)

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
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)
	waitForProbes(t, script, 2)
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

// TestAttemptCandidate_AnEmptyCompletionKeepsTheStreak pins that the
// non-streaming chat path judges the answer rather than the status.
//
// `200 {"choices":[]}` decodes, is forwarded to the client as a normal
// completion, and is exactly what an aggregator in front of a retired model can
// return between its gone-shaped 404s. Crediting it resets the count, so the
// three refusals never land consecutively and the model is never nominated,
// probed or retired. Every other path on this branch already draws this line.
func TestAttemptCandidate_AnEmptyCompletionKeepsTheStreak(t *testing.T) {
	cases := []struct {
		name       string
		answer     string
		wantStreak int64
	}{
		{"empty completion", `{"id":"x","object":"chat.completion","choices":[]}`, 1},
		// The control, without which this test would also pass against a path
		// that had simply stopped clearing streaks altogether.
		{"real completion", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],"usage":{"completion_tokens":1}}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandler(h)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.answer)
			}))
			defer srv.Close()

			m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol", InputModalities: `["text"]`, OutputModalities: `["text"]`}
			cand := goneCandidateAt(m, "OpenAI", srv.URL)

			// One real refusal, so there is a streak for the 200 to clear.
			h.noteModelGone(cand, endpointTypeChat)
			streak := goneStreakFor(t, h, m.ID, probeChatEndpoint)

			st := &requestState{
				startTime:       time.Now(),
				reqModel:        "gpt-5.6-sol",
				bodyBytes:       []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}`),
				failoverTimeout: 30 * time.Second,
				logData:         &requestLogData{modelID: "gpt-5.6-sol", endpointType: endpointTypeChat},
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

			if got := h.attemptCandidate(w, r, st, cand, 0, 1); got != outcomeServed {
				t.Fatalf("outcome = %v, want served", got)
			}
			if n := streak.count(); n != tc.wantStreak {
				t.Fatalf("streak = %d, want %d", n, tc.wantStreak)
			}
		})
	}
}

// TestNoteModelGone_ARetirementThatStandsIsStillAnnounced pins that a disable
// which stuck gets its follow-through.
//
// Once AutoRetireIfConfirmed commits, the row is off. Three of the four ways out
// of the post-commit cancelled branch leave it off — no revert attempted, a
// revert that errored, a revert the row had moved past — and all three used to
// return before the custom-group revalidation and the alert. The revalidation is
// the load-bearing half: a disabled member does not resize the groups it belongs
// to, so skipping it leaves a group enabled with fewer than two routable members
// until an unrelated scan notices.
//
// The arm driven here is the one where the model is refusing again, which is
// also the one where the retirement is most clearly real.
func TestNoteModelGone_ARetirementThatStandsIsStillAnnounced(t *testing.T) {
	// Not parallel: it subscribes to the process-wide event bus.
	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// A success lands inside the commit window and fresh refusals rebuild the
	// case behind it, so the revert stands down and the model stays retired.
	repo.afterConfirm = func() {
		h.noteModelServed(m, endpointTypeChat)
		for range goneStrikeThreshold {
			h.noteModelGone(cand, endpointTypeChat)
		}
	}

	ch := events.Subscribe()
	defer events.Unsubscribe(ch)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "model.auto_disabled_gone" || ev.Metadata["model_uuid"] != m.ID.String() {
				continue
			}
			return
		case <-deadline:
			t.Fatal("a retirement that was never reverted was never announced")
		}
	}
}

// TestNoteModelGone_TheRetirementEventReportsTheRealStrikeCount pins that the
// number an operator reads is the number the decision was made on.
//
// Reporting goneStrikeThreshold would be accurate only if a retirement could
// only be triggered by the caller that saw exactly three. Under claimProbe a
// write can equally stand on a single refusal that re-claimed a streak parked
// since the last cooldown, or on a streak already past the threshold.
//
// The sequence below produces three different plausible answers, which is the
// point of its shape: the constant would say 3, a count read back after the
// probe and the write would say 14, and the count the claiming refusal actually
// saw is 4. Only the last one answers "how was this decision reached".
func TestNoteModelGone_TheRetirementEventReportsTheRealStrikeCount(t *testing.T) {
	// Not parallel: it subscribes to the process-wide event bus.
	const extraRefusals = 10

	repo := &mockModelRepo{
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	// Rate limited first, so the first probe establishes nothing and parks the
	// streak at the threshold instead of retiring on it.
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "Google AI Studio (Gemini)", srv.URL)

	ch := events.Subscribe()
	defer events.Unsubscribe(ch)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	waitForProbes(t, script, 1)

	// The incident passes and the model turns out to be genuinely gone. One
	// refusal now claims the probe, and the count it sees is one past the
	// threshold — a number no constant could produce.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)

	select {
	case <-repo.setEnabledEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the disable write never started")
	}

	// The client keeps retrying the dead model while the write is held open.
	// None of these buys a probe (the cooldown is running again) and none of
	// them was part of the decision, but every one is a strike.
	for range extraRefusals {
		h.noteModelGone(cand, endpointTypeChat)
	}
	close(repo.setEnabledGate)

	want := int64(goneStrikeThreshold + 1)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			// The bus fans out to every subscriber, so other tests' events
			// interleave; filter to this model's retirement.
			if ev.Type != "model.auto_disabled_gone" || ev.Metadata["model_uuid"] != m.ID.String() {
				continue
			}
			if got := ev.Metadata["strikes"]; got != want {
				t.Fatalf("event reported strikes = %v (%T), want %d", got, got, want)
			}
			return
		case <-deadline:
			t.Fatal("the retirement was never announced")
		}
	}
}

// TestProbeForRetirement_NilCandidatePostponesInsteadOfPanicking pins that the
// guard is where the dereferences are.
//
// probeModel checks the same two fields, but every field probeForRetirement
// touches on the way there — the provider's id for the semaphore, the model's id
// for the postpone log — would already have panicked, so the downstream guard
// was promising an outcome it could never deliver. The panic is caught by the
// disable goroutine's recover and reported as a panic, which is the wrong answer
// to "is this model still served": nothing was established, and that is what
// probeInconclusive means.
func TestProbeForRetirement_NilCandidatePostponesInsteadOfPanicking(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	cases := []struct {
		name      string
		candidate modelCandidate
	}{
		{"no provider", modelCandidate{model: &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}}},
		{"no model", modelCandidate{provider: &provider.Provider{ID: uuid.New(), Name: "Google AI Studio (Gemini)"}}},
		{"neither", modelCandidate{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := h.probeForRetirement(tc.candidate, endpointTypeChat); got != probeInconclusive {
				t.Fatalf("verdict = %s, want inconclusive", got)
			}
		})
	}
}

// TestGoneStreak_ASuccessBetweenTheStrikeAndTheClaimWins pins the window between
// the two lock acquisitions noteModelGone makes.
//
// A refusal strikes and then claims, and a success can land in between: it
// clears the count and sets the tombstone, and the claim would then proceed on
// strikes that no longer exist — spending an upstream request, wiping the
// tombstone that success set, and reporting a strike count the streak does not
// hold. The claim reads the count for exactly this reason, which is what makes
// the comment's "a claim starts the next decision, on strikes the success is
// older than" true rather than merely usual.
func TestGoneStreak_ASuccessBetweenTheStrikeAndTheClaimWins(t *testing.T) {
	t.Parallel()

	s := &goneStreak{}
	now := time.Now()
	for range goneStrikeThreshold {
		s.strike(now)
	}

	// The success lands after the last strike and before the claim.
	s.supersede()

	if ok, reason := s.canClaimProbe(now); ok {
		t.Error("the cheap read admitted a claim on evidence a success had already cleared")
	} else if !strings.Contains(reason, "cleared") {
		t.Errorf("reason = %q, want the cleared-strikes reason rather than the cooldown one", reason)
	}
	if s.claimProbe(now) {
		t.Fatal("claimed a probe on evidence a success had already cleared")
	}
	if !s.cancelled.Load() {
		t.Fatal("the claim wiped the tombstone the success set, so the queued disable would not stand down")
	}

	// Fresh refusals rebuild the case, and then the claim is real again.
	for range goneStrikeThreshold {
		s.strike(now)
	}
	if !s.claimProbe(now) {
		t.Fatal("three fresh strikes must buy a claim")
	}
	if s.cancelled.Load() {
		t.Error("a claim on strikes newer than the success must clear the tombstone")
	}
}

// TestGoneStreak_CanClaimProbeDoesNotTakeTheClaim pins the read that lets the
// cheap per-model reason to stop be checked before the shared per-provider one.
//
// It has to agree with claimProbe and it has to take nothing. If it stamped, a
// refusal that only asked whether a probe was possible would extend the cooldown
// it was asking about; if it disagreed, a model would either be denied a probe
// it had earned or reach the semaphore on every refusal, which is what put the
// per-provider slots in the path of traffic that was never going to spend one.
func TestGoneStreak_CanClaimProbeDoesNotTakeTheClaim(t *testing.T) {
	t.Parallel()

	s := &goneStreak{}
	now := time.Now()
	// A claim needs evidence as well as an expired cooldown, so the streak has
	// to be at the threshold before either can be asked about.
	for range goneStrikeThreshold {
		s.strike(now)
	}

	if ok, _ := s.canClaimProbe(now); !ok {
		t.Fatal("a streak at the threshold that has never been probed must admit a claim")
	}
	if ok, _ := s.canClaimProbe(now); !ok {
		t.Fatal("asking twice was refused, so the read spent the claim it was asking about")
	}
	if !s.claimProbe(now) {
		t.Fatal("the real claim must still be available after the read")
	}

	// And now both must see the cooldown.
	if ok, reason := s.canClaimProbe(now); ok {
		t.Fatal("the read did not see the claim that was just taken")
	} else if !strings.Contains(reason, "cooldown") {
		t.Errorf("reason = %q, want the cooldown reason", reason)
	}
	if s.claimProbe(now) {
		t.Fatal("claimProbe granted a second claim inside the cooldown")
	}

	// They agree on the far side of it too.
	lapsed := now.Add(goneProbeCooldown)
	if ok, _ := s.canClaimProbe(lapsed); !ok {
		t.Fatal("the read must admit a claim once the cooldown has lapsed")
	}
	if !s.claimProbe(lapsed) {
		t.Fatal("claimProbe must grant one once the cooldown has lapsed")
	}
}

// TestNoteStreamOutcome_NilLogDataIsIgnored pins a guard that has to live at the
// dereference. producedOutput checks for nil as well, but noteStreamOutcome
// builds its arguments from logData in the same expression that calls it, and Go
// evaluates all of them first — so on this path the helper's check could never
// run and a nil would panic before reaching it.
func TestNoteStreamOutcome_NilLogDataIsIgnored(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}

	h.noteStreamOutcome(nil, goneCandidateFor(t, m, "Google AI Studio (Gemini)"))

	if calls := repo.disableCalls(); len(calls) != 0 {
		t.Fatalf("a stream with no log entry establishes nothing, got %+v", calls)
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
	waitForStreakCount(t, h, m.ID, probeChatEndpoint, 0)

	// An ordinary request to the same model succeeds, exactly as the request
	// path reports it.
	h.noteModelServed(m, endpointTypeChat)

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
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
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

	// The postponement KEEPS the evidence. Dropping the streak to stay reachable
	// would leave the probe rate unbounded — three fresh refusals buying another
	// probe, forever, at a provider already rate limiting us — so the claim gates
	// the retry instead and the strikes stay where they are.
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold {
		t.Fatalf("a postponed retirement must keep the strikes it was built on, got a streak of %d", n)
	}

	// The incident passes and the model turns out to be genuinely gone. One
	// refusal is now enough, precisely because the earlier strikes survived.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
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
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
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
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold {
		t.Fatalf("the strikes must survive so the retry is reachable, got a streak of %d", n)
	}
	// And the model's own claim was not spent on it. A refused slot costs
	// nothing: no upstream request was made, so there is nothing for the
	// five-minute cooldown to be bounding. Charging it anyway is what turned a
	// provider-wide nomination event into four models per cooldown — the very
	// burst the semaphore exists for, converging in hours instead of minutes.
	if next := probeClaimAt(t, h, m.ID, probeChatEndpoint); !next.IsZero() {
		t.Fatalf("a refused slot burnt the probe cooldown until %s", next)
	}

	// A slot frees up: the very next refusal probes, with no cooldown to wait
	// out, and the retirement lands on the evidence that was already there.
	<-full
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

// TestNoteModelGone_AnOpenCircuitCostsNoClaim pins that the cooldown is only
// spent on a probe that can actually leave the process.
//
// probeModel asks the breaker too, but it asks from inside the detached
// goroutine — by which point the five minutes are gone. A model whose third
// strike landed in the gap between its provider's last answer and its circuit
// opening would buy nothing with them and wait out the full cooldown before it
// could be adjudicated, once the provider came back.
//
// The strikes must survive either way: an open circuit says something about the
// provider and nothing about the model.
func TestNoteModelGone_AnOpenCircuitCostsNoClaim(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	h.circuitBreaker = failover.NewCircuitBreaker(nil)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("claude-sonnet-4"))
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	// Driven through the breaker's own API rather than by reaching into its
	// state: this check has to agree with what the routing path sees, and the
	// threshold is the breaker's to define.
	for range h.circuitBreaker.Threshold {
		h.circuitBreaker.RecordFailure(cand.provider.ID, cand.provider.Name)
	}
	if state := h.circuitBreaker.GetState(cand.provider.ID); state != failover.StateOpen {
		t.Fatalf("fixture: the circuit did not open, state = %v", state)
	}

	// The provider WOULD refuse this model, so a probe that went out would
	// retire it.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("nothing may be retired on a probe that was never sent, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 0 {
		t.Fatalf("a sidelined provider must not be asked, got %v", paths)
	}
	if next := probeClaimAt(t, h, m.ID, probeChatEndpoint); !next.IsZero() {
		t.Fatalf("the claim was spent on a probe that never left the process, cooldown until %s", next)
	}
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold {
		t.Fatalf("the strikes must survive: an open circuit is evidence about the provider, got %d", n)
	}
	// Between them, those two assertions are the whole property: the evidence is
	// intact and nothing stands between it and the next refusal, so the
	// retirement is adjudicated as soon as the provider is routable again rather
	// than a cooldown later. The recovery itself is not driven here because the
	// breaker only leaves the open state through its own cooldown, which is the
	// breaker's business and not this path's.
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

// TestNoteModelGone_AModelServingBothSurfacesIsNeverRetired pins the gate that
// is about the ACTION rather than the evidence.
//
// Strikes, probes and successes are per surface, because a provider can retire
// one surface of a model and keep serving the other. The disable is not: it
// turns the model ROW off. For a model the catalog says serves chat AND
// embeddings those two scopes disagree, and no probe can catch it — the probe is
// right, the model really is gone on that surface, and the disable is simply
// broader than the finding. So nothing is written at all, and the model stays
// enabled until discovery drops it or an operator does.
//
// The refusals here would otherwise retire it: the fixture refuses by name, and
// the same sequence against a chat-only model disables it (see
// TestNoteModelGone_DisablesAfterThreshold).
func TestNoteModelGone_AModelServingBothSurfacesIsNeverRetired(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol", InputModalities: `["text"]`, OutputModalities: `["text","embedding"]`}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gpt-5.6-sol"))
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	// Well past the threshold on both surfaces: the gate is unconditional, not
	// a delay.
	for range goneStrikeThreshold + 3 {
		h.noteModelGone(cand, endpointTypeChat)
		h.noteModelGone(cand, endpointTypeEmbeddings)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a disable cannot separate the surfaces, so it must not be written, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 0 {
		t.Fatalf("a refusal that can never be acted on must not spend an upstream request, got %v", paths)
	}
	for _, endpoint := range []string{probeChatEndpoint, probeEmbeddingsEndpoint} {
		if _, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: endpoint}); ok {
			t.Errorf("a streak that can never fire is not evidence, and one was recorded on %s", endpoint)
		}
	}
}

// TestNoteModelServed_ClearsOnlyItsOwnSurface pins the other side of the split.
//
// A streak is about one surface because a model can be served on one and refused
// on another. Clearing every surface on any success answered both questions as
// one, and the direction it failed in is the one that matters: a provider that
// has retired a model's chat surface while still serving its embeddings would
// have every embeddings success wipe the chat streak, so the dead surface could
// never reach three consecutive strikes and would never be adjudicated at all.
//
// Narrowing it cannot retire anything wrongly, which is why it is safe to do:
// the surviving streak still has to be refused by a real PROBE to that same
// surface before a disable is written.
func TestNoteModelServed_ClearsOnlyItsOwnSurface(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	// A chat model. Nothing filters a request by modality on the way in, so it
	// can still be sent to /v1/embeddings and can still be served there — the
	// refusals that arrive on that surface are ignored, but a SUCCESS on it is
	// what this test is about.
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol", InputModalities: `["text"]`, OutputModalities: `["text"]`}
	cand := goneCandidateFor(t, m, "OpenAI")

	h.noteModelGone(cand, endpointTypeChat)

	// An embeddings request to the same model succeeds. It says nothing about
	// the chat surface, which is the one accused.
	h.noteModelServed(m, endpointTypeEmbeddings)
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 1 {
		t.Errorf("chat streak = %d, want the strike kept: an embeddings success is not evidence about chat", n)
	}

	// Nor does traffic on a surface that is never auto-retired.
	h.noteModelServed(m, endpointTypeImage)
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 1 {
		t.Errorf("chat streak = %d, want the strike kept after an image success", n)
	}

	// And the mirror: /v1/messages resolves to the chat surface, so a success
	// there clears the streak a /v1/chat/completions refusal built.
	h.noteModelServed(m, endpointTypeMessages)
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 0 {
		t.Errorf("chat streak = %d, want 0 after a success on the same surface", n)
	}
}

// TestNoteModelServed_AnUnprobeableFamilyClearsNothing pins the rule the strike
// side already follows, applied to the success side: a family that cannot be
// adjudicated does not get to speak about one that can. An image or TTS response
// says no more about the chat surface than an image refusal does, and crediting
// it would let traffic on an unprobeable surface hold a genuinely dead chat
// surface open indefinitely.
func TestNoteModelServed_AnUnprobeableFamilyClearsNothing(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.5-flash-image"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	h.noteModelGone(cand, endpointTypeChat)

	for _, family := range []string{endpointTypeImage, endpointTypeTTS, endpointTypeSTT, endpointTypeRerank, ""} {
		h.noteModelServed(m, family)
		if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != 1 {
			t.Fatalf("a %q success cleared the chat streak (now %d)", family, n)
		}
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
	// An embeddings model as the catalog describes one: a refusal on
	// /embeddings only counts against a model that says it produces embeddings.
	m := &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small", OutputModalities: `["embedding"]`}
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
		// Every surface a streak can be keyed on, so this cannot pass by
		// looking under a key nothing would have written.
		for _, endpoint := range []string{probeChatEndpoint, probeEmbeddingsEndpoint} {
			if _, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: endpoint}); ok {
				t.Errorf("%s: a family that can never be adjudicated must not record a streak (%s)", f.name, endpoint)
			}
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

// TestNoteModelGone_BurstIssuesOneProbeAndLeaksNoSlots pins the two things a
// concurrent burst against a dead model must not do.
//
// A dead model is exactly the one that gets hammered: clients retry it and a
// failover group can try it on several requests at once. Every one of those
// refusals arrives at the threshold and asks for a probe, so the claim decides
// which single caller may spend an upstream request.
//
// The second assertion is the one with teeth. The provider's slot is taken
// BEFORE the model's claim, so a caller that loses the claim is holding a slot
// it must give back on a path that returns without ever probing. A leak there
// is permanent and silent: after goneProbeMaxConcurrent losers the provider has
// no slots left, every later nomination reports "too many retirement probes are
// already in flight", and no model on that provider is ever adjudicated again
// for the life of the process. Nothing else in the suite would notice, because
// every other test issues one probe at a time.
//
// Its sensitivity is deliberately stated rather than assumed, because the loser
// path is a race and cannot be entered on demand: the window is between
// canClaimProbe and claimProbe, and the seeding plus the start barrier below are
// what widen it enough to be entered at all. Measured by deleting the release():
// caught 5 runs in 6 at this burst size, 4 in 5 at a quarter of it, and 0 in 5
// before the streak was pre-seeded (the burst spent itself building the count
// and never reached the claim).
//
// The error is one-sided, which is what makes that acceptable. Correct code
// always passes — the slots always come back — so this can miss a regression but
// cannot invent one. The first two assertions are exact and hold every run.
func TestNoteModelGone_BurstIssuesOneProbeAndLeaksNoSlots(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "burst-model-1"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("burst-model-1"))
	candidate := goneCandidateAt(m, "Burst Provider", srv.URL)

	// Three strikes first, so every goroutine below arrives at a streak that is
	// already at the threshold and reaches the claim. Without this the burst
	// spends most of itself building the count, and the contended window this
	// test exists for is never entered.
	raw, _ := h.goneStrikes.LoadOrStore(goneStreakKey{model: m.ID, endpoint: probeChatEndpoint}, &goneStreak{})
	seeded, ok := raw.(*goneStreak)
	if !ok {
		t.Fatal("the streak map holds something that is not a streak")
	}
	for range goneStrikeThreshold {
		seeded.strike(time.Now())
	}

	// Released together, so the callers hit canClaimProbe at the same moment
	// rather than in the staggered order goroutine startup gives. That is what
	// puts more than one of them past the cooldown read and into the claim,
	// which is the only path that takes a provider slot and can then lose.
	const burst = 256
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(burst)
	for range burst {
		go func() {
			defer wg.Done()
			<-start
			h.noteModelGone(candidate, endpointTypeChat)
		}()
	}
	close(start)
	wg.Wait()

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("recorded %d disables, want exactly 1", len(calls))
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Errorf("provider was probed %d times, want exactly 1: %v", len(paths), paths)
	}

	// Every slot must be back. Taking all of them proves it; the acquires are
	// non-blocking, so a leaked slot fails here rather than hanging.
	var releases []func()
	for i := range goneProbeMaxConcurrent {
		release, ok := h.acquireProbeSlot(candidate.provider.ID)
		if !ok {
			t.Fatalf("slot %d of %d could not be acquired after the burst: a loser kept its slot", i+1, goneProbeMaxConcurrent)
		}
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}
