package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// A 200 is a status, not an answer. recordBreakerOutcome credited the provider
// the moment the response headers arrived, before a byte of the body was read —
// so a provider answering {"choices":[]} to every request recorded a success
// every time, and RecordSuccess resets consecutiveFails, so its circuit could
// never open however long it went on returning nothing.
//
// That is #805's charge, on the path #805 did not cover: it fixed the streaming
// side, where finalizeStream judges the stream after it ends, and left the
// completion side crediting on headers.
func TestHandleNonStreamingResponse_EmptyAnswerChargesTheBreaker(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantCharge bool
	}{
		{"no choices at all", `{"id":"x","object":"chat.completion","choices":[]}`, true},
		{"a choice with empty content", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":""}}]}`, true},
		{"a null content", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null}}]}`, true},
		// Anything the model actually produced clears it.
		{"text", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"}}]}`, false},
		{"content as parts", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}]}`, false},
		{"reasoning only", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`, false},
		{"a tool call and no text", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`, false},
		{"usage reporting completion tokens", `{"id":"x","object":"chat.completion","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":7}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })
			withBreakerThresholdOne(t, h)

			providerID := uuid.New()
			st := &requestState{circuitBreakerEnabled: true, startTime: time.Now()}
			candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "empty-answer-provider"}}
			logData := &requestLogData{
				modelID:        "gpt-test",
				providerID:     providerID,
				providerName:   "empty-answer-provider",
				virtualKeyName: "k",
				virtualKeyID:   "00000000-0000-0000-0000-000000000001",
				state:          "pending",
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(tc.body)), Header: make(http.Header)}
			req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))

			h.handleNonStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)
			h.recordAnswerOutcome(st, candidate, logData, false)

			charged := h.circuitBreaker.GetState(providerID) == failover.StateOpen
			if charged != tc.wantCharge {
				t.Errorf("circuit open = %v, want %v (state %q)", charged, tc.wantCharge, logData.state)
			}
		})
	}
}

// The credit has to keep working, or every completion stops clearing the
// provider's failure history and old failures accumulate until an unrelated one
// opens the circuit.
func TestRecordAnswerOutcome_AnAnswerClearsTheFailureCount(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThreshold(t, h, "2")

	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true, startTime: time.Now()}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}
	h.circuitBreaker.RecordFailure(providerID, "p")

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "completed", deliveredContent: true, providerID: providerID, providerName: "p"}, false)

	h.circuitBreaker.RecordFailure(providerID, "p")
	if h.circuitBreaker.GetState(providerID) == failover.StateOpen {
		t.Error("an answered completion recorded no success, so an old failure was still on the clock")
	}
}

// A completion the gateway could not read is the provider's fault, not a
// success: the old code had already credited it at header time.
func TestRecordAnswerOutcome_AFailedAttemptIsCharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true, startTime: time.Now()}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", errorKind: KindProviderError, providerID: providerID, providerName: "p"}, false)

	if h.circuitBreaker.GetState(providerID) != failover.StateOpen {
		t.Error("a completion that failed after the headers must be charged")
	}
}

// And it stays a no-op when the breaker is off.
func TestRecordAnswerOutcome_NoOpWhenTheBreakerIsDisabled(t *testing.T) {
	cb := failover.NewCircuitBreaker(nil)
	h := &Handler{circuitBreaker: cb}
	providerID := uuid.New()

	h.recordAnswerOutcome(&requestState{circuitBreakerEnabled: false}, modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}},
		&requestLogData{state: "failed", errorKind: KindProviderError, providerID: providerID, providerName: "p"}, false)

	if _, seen := cbConsecutiveFails(cb, providerID); seen {
		t.Error("the disabled breaker was touched")
	}
}

// A 200 whose body this gateway cannot turn into a completion is a provider
// fault — the comment at each of those three sites already says so — and the
// old code credited every one of them at header time, because the credit
// happened before the translation was even attempted.
func TestChargeBreaker_UntranslatableBodyIsCharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}

	h.chargeBreaker(st, candidate, "upstream body could not be translated")

	if h.circuitBreaker.GetState(providerID) != failover.StateOpen {
		t.Error("a body the gateway could not translate must be charged to the provider")
	}
}

// And the charge has to happen where it is produced, not just in the helper.
// Driven through attemptCandidate with a Gemini candidate whose 200 is not a
// Gemini answer, which is the shape translateEgressResponseBody rejects — the
// case each adapter's own comment already calls a provider fault, and the case
// the old code credited before the translation was attempted.
//
// One of the three adapters by fixture; all three by code path, since they share
// rejectUntranslatableBody.
func TestAttemptCandidate_UntranslatableBodyChargesTheBreaker(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `<html>502 Bad Gateway</html>`)
	}))
	defer srv.Close()
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	// The hostname decides the dialect; the transport dials the test server.
	cand := goneCandidateAt(m, "Vertex Express", "http://us-central1-aiplatform.googleapis.com/v1")

	st := &requestState{
		startTime:             time.Now(),
		reqModel:              "gemini-2.0-flash",
		bodyBytes:             []byte(`{"model":"gemini-2.0-flash","messages":[{"role":"user","content":"hi"}]}`),
		failoverTimeout:       30 * time.Second,
		circuitBreakerEnabled: true,
		logData:               &requestLogData{modelID: "gemini-2.0-flash", endpointType: endpointTypeChat},
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

	// Two candidates, so the attempt is free to reject this one.
	if got := h.attemptCandidate(httptest.NewRecorder(), r, st, cand, 0, 2); got != outcomeFailover {
		t.Fatalf("outcome = %v, want a failover on an untranslatable 200", got)
	}
	if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
		t.Error("a 200 whose body could not be translated was not charged to the provider")
	}
}

// A client that hangs up mid-read is not a provider failure. The upstream body
// is read under the caller's context, so a user pressing stop — or a load
// balancer's client-side timeout — surfaces here as a failed read, and charging
// for it opens the circuit for every tenant after five impatient cancels.
//
// doUpstream, judgeStreamForBreaker, classifyProbeError and the pass-through
// first-byte probe all refuse to charge for this. This verdict is the one that
// did not.
func TestRecordAnswerOutcome_AClientHangingUpIsNotAProviderFailure(t *testing.T) {
	cb := failover.NewCircuitBreaker(nil)
	h := &Handler{circuitBreaker: cb}
	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", errorKind: KindProviderError, providerID: providerID, providerName: "p"}, true)

	if _, seen := cbConsecutiveFails(cb, providerID); seen {
		t.Error("an abandoned request was charged to the provider")
	}
}

// A 2xx this gateway could not decode says nothing about whether the provider is
// up: it is classified provider_bad_request and, as nonStreamingFailureDetail
// says, it still holds the model's generated text — a relay quoting its token
// counts is the named cause. judgeStreamForBreaker calls recording nothing the
// honest verdict for its own unreadable frames, and it is the honest verdict
// here.
func TestRecordAnswerOutcome_OnlyAProviderFaultIsCharged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind       ErrorKind
		wantCharge bool
	}{
		{KindProviderError, true},
		{KindProviderTimeout, true},
		{KindProviderModelGone, true},
		{KindProviderBadRequest, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			cb := failover.NewCircuitBreaker(nil)
			h := &Handler{circuitBreaker: cb}
			providerID := uuid.New()
			st := &requestState{circuitBreakerEnabled: true}
			candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}

			h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", errorKind: tc.kind, providerID: providerID, providerName: "p"}, false)

			_, seen := cbConsecutiveFails(cb, providerID)
			if seen != tc.wantCharge {
				t.Errorf("charged = %v, want %v", seen, tc.wantCharge)
			}
		})
	}
}

// The bar for a breaker charge has to be generous, because a false negative
// darkens a provider for every tenant after five requests while a false positive
// merely fails to strike a model. These are all the model answering, and all of
// them read as nothing before: refusal, audio and function_call ride in the
// unmodelled overflow, and reasoning_details was read only AFTER the
// normalisation that folds it into reasoning_content — which runs later than the
// judgement.
func TestChatAnswerCarriesContent_TheShapesAnAnswerCanTake(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"a safety refusal", `{"choices":[{"message":{"role":"assistant","content":null,"refusal":"I can't help with that."}}]}`, true},
		{"an audio answer", `{"choices":[{"message":{"role":"assistant","content":null,"audio":{"id":"a","data":"UklGRg==","transcript":"hello"}}}]}`, true},
		{"reasoning_details only", `{"choices":[{"message":{"role":"assistant","content":"","reasoning_details":[{"type":"reasoning.text","text":"thinking"}]}}]}`, true},
		{"a legacy function_call", `{"choices":[{"message":{"role":"assistant","content":null,"function_call":{"name":"f","arguments":"{}"}}}]}`, true},
		// Still nothing: an unmodelled member that carries nothing is not an
		// answer, and neither is a choice list with nothing in it.
		{"no choices at all", `{"choices":[]}`, false},
		{"an empty message", `{"choices":[{"message":{"role":"assistant","content":""}}]}`, false},
		{"empty unmodelled members", `{"choices":[{"message":{"role":"assistant","content":"","refusal":null,"annotations":[]}}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out ChatCompletionResponse
			if err := json.Unmarshal([]byte(tc.body), &out); err != nil {
				t.Fatalf("fixture did not decode: %v", err)
			}
			if got := chatAnswerCarriesContent(out); got != tc.want {
				t.Errorf("chatAnswerCarriesContent = %v, want %v", got, tc.want)
			}
		})
	}
}

// The pass-through surface had the same defect and the detection function
// already written for it: passthroughAnswered exists precisely to spot
// 200 {"data":[]}, and it was consulted for the gone-strike streak only. The
// breaker success was recorded the moment the buffered read succeeded, so an
// embeddings provider answering nothing to every request recorded a success
// every time and its circuit could never open.
func TestServeBufferedJSONPassthrough_EmptyEmbeddingsChargesTheBreaker(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantCharge bool
	}{
		{"no data at all", `{"object":"list","data":[]}`, true},
		{"a real embedding", `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })
			withBreakerThresholdOne(t, h)

			providerID := uuid.New()
			st := &requestState{
				circuitBreakerEnabled: true,
				startTime:             time.Now(),
				logData: &requestLogData{
					modelID: "text-embedding-3-small", providerID: providerID, providerName: "p",
					endpointType: endpointTypeEmbeddings, state: "pending",
					virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
				},
			}
			candidate := modelCandidate{
				model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"},
				provider: &provider.Provider{ID: providerID, Name: "p"},
			}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(tc.body)), Header: make(http.Header)}

			h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, candidate, resp, "application/json", 1, 5)

			charged := h.circuitBreaker.GetState(providerID) == failover.StateOpen
			if charged != tc.wantCharge {
				t.Errorf("circuit open = %v, want %v", charged, tc.wantCharge)
			}
		})
	}
}

// And an abandoned pass-through read is not the provider's doing either. The
// first-byte probe on the streamed twin has always had this guard; the buffered
// branch did not.
func TestServeBufferedJSONPassthrough_AClientHangingUpIsNotCharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	st := &requestState{
		circuitBreakerEnabled: true,
		startTime:             time.Now(),
		logData: &requestLogData{
			modelID: "text-embedding-3-small", providerID: providerID, providerName: "p",
			endpointType: endpointTypeEmbeddings, state: "pending",
			virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
		},
	}
	candidate := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"},
		provider: &provider.Provider{ID: providerID, Name: "p"},
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&errorReader{err: errors.New("connection reset by peer")}), Header: make(http.Header)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/v1/embeddings", http.NoBody).WithContext(ctx)

	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), req, st, candidate, resp, "application/json", 1, 5)

	if h.circuitBreaker.GetState(providerID) == failover.StateOpen {
		t.Error("an abandoned pass-through read was charged to the provider")
	}
}

// End to end through attemptCandidate, which is what proves the verdict is
// WIRED and not merely correct. Both call sites could be deleted without a test
// noticing: the unit tests above compose handleNonStreamingResponse and
// recordAnswerOutcome by hand, and the pre-existing breaker-sequence tests only
// ever exercise the credit direction.
func TestAttemptCandidate_EmptyAnswerChargesTheBreaker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		native bool
		body   string
	}{
		{"openai shaped", false, `{"id":"x","object":"chat.completion","choices":[]}`},
		{"native anthropic", true, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[],"usage":{"input_tokens":3,"output_tokens":0}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })
			withBreakerThresholdOne(t, h)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			h.upstreamTransport = dialToTestServer(t, srv)

			m := &model.Model{ID: uuid.New(), ModelID: "claude-x"}
			cand := goneCandidateAt(m, "Anthropic", "http://api.anthropic.com")

			st := &requestState{
				startTime:        time.Now(),
				reqModel:         "claude-x",
				bodyBytes:        []byte(`{"model":"claude-x","messages":[{"role":"user","content":"hi"}]}`),
				anthropicRawBody: []byte(`{"model":"claude-x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`),
				// buildCandidateRequest recomputes anthropicNativeAttempt from
				// anthropicIn and the provider family, so setting the flag
				// directly is overwritten — as this test found by surviving a
				// mutation of the native call site.
				anthropicIn:           tc.native,
				failoverTimeout:       30 * time.Second,
				circuitBreakerEnabled: true,
				vkHash:                "test-hash",
				logData: &requestLogData{
					modelID: "claude-x", providerID: cand.provider.ID, providerName: "Anthropic",
					endpointType: endpointTypeChat, state: "pending",
					virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
				},
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

			h.attemptCandidate(w, r, st, cand, 0, 1)

			if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
				t.Errorf("a 200 carrying no answer was not charged (state %q, body %q)", st.logData.state, w.Body.String())
			}
		})
	}
}
