package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		// The only shape charged: nothing came back at all. This is what an
		// aggregator in front of a retired model returns between its refusals.
		{"no choices at all", `{"id":"x","object":"chat.completion","choices":[]}`, true},
		// A choice whose message holds literally nothing is an empty answer too,
		// and it has to be: every egress translator synthesises exactly this
		// shape for an emptied Gemini, Anthropic or Responses answer, so a rule
		// that stopped at "a choice exists" could never charge any of them.
		{"a choice with empty content", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":""}}]}`, true},
		{"a null content", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null}}]}`, true},
		// What that rule must NOT catch is a choice carrying something this
		// gateway does not model, or a stop reason about the provider's own
		// output. Those follow.
		{"a safety refusal", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"refusal":"I can't help with that."}}]}`, false},
		{"an audio answer", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"audio":{"id":"a","data":"UklGRg==","transcript":"hi"}}}]}`, false},
		{"an azure content filter block", `{"id":"x","object":"chat.completion","choices":[{"index":0,"finish_reason":"content_filter","content_filter_results":{"hate":{"filtered":true}},"message":{"role":"assistant","content":null}}]}`, false},
		{"a legacy function_call", `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"function_call":{"name":"f","arguments":"{}"}}}]}`, false},
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
			h.recordAnswerOutcome(st, candidate, logData, nil)

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

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "completed", emptyCompletion: false, providerID: providerID, providerName: "p"}, nil)

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

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", errorKind: KindProviderError, providerID: providerID, providerName: "p"}, nil)

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
		&requestLogData{state: "failed", errorKind: KindProviderError, providerID: providerID, providerName: "p"}, nil)

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

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", errorKind: KindProviderError, providerID: providerID, providerName: "p"}, cancelledRequest())

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
		// An unclassified failure escapes too. Documented rather than changed:
		// every path into this branch sets a kind, and inventing a charge for
		// one that did not would be guessing at whose fault it was.
		{"", false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			cb := failover.NewCircuitBreaker(nil)
			h := &Handler{circuitBreaker: cb}
			providerID := uuid.New()
			st := &requestState{circuitBreakerEnabled: true}
			candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}

			h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", errorKind: tc.kind, providerID: providerID, providerName: "p"}, nil)

			_, seen := cbConsecutiveFails(cb, providerID)
			if seen != tc.wantCharge {
				t.Errorf("charged = %v, want %v", seen, tc.wantCharge)
			}
		})
	}
}

// The bar the BREAKER reads is narrower than !deliveredContent on purpose, and
// the two questions are not the same question.
//
// deliveredContent backs the retirement verdict, where missing an answer merely
// fails to clear a streak — and `refusal` is the single likeliest field for an
// aggregator to write "this model is gone" into behind a 200, so widening THAT
// would let such a provider clear its gone-strikes forever. A breaker charge
// costs the opposite way round: five of them take a provider out of rotation for
// every tenant. So the breaker charges only a completion with nothing in it.
func TestEmptyCompletion_IsOnlyACompletionWithNothingInIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"no choices at all", `{"choices":[]}`, true},
		// What every egress translator synthesises for an emptied answer.
		{"a synthesised empty message", `{"choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`, true},
		{"a null content", `{"choices":[{"message":{"role":"assistant","content":null}}]}`, true},
		{"no choices but usage", `{"choices":[],"usage":{"completion_tokens":7}}`, false},
		{"real text", `{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`, false},
		{"content as parts", `{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}]}`, false},
		{"reasoning only", `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`, false},
		{"reasoning_details only", `{"choices":[{"message":{"role":"assistant","content":"","reasoning_details":[{"type":"reasoning.text","text":"t"}]}}]}`, false},
		{"a tool call", `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`, false},
		// Unmodelled, and unmistakably the answer.
		{"a safety refusal", `{"choices":[{"message":{"role":"assistant","content":null,"refusal":"no"}}]}`, false},
		{"an audio answer", `{"choices":[{"message":{"role":"assistant","content":null,"audio":{"data":"UklGRg=="}}}]}`, false},
		{"a legacy function_call", `{"choices":[{"message":{"role":"assistant","content":null,"function_call":{"name":"f"}}}]}`, false},
		// A stop reason about the provider's own output. Gemini's SAFETY maps here.
		{"a content filter block", `{"choices":[{"finish_reason":"content_filter","message":{"role":"assistant","content":null}}]}`, false},
		{"a length cut", `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":null}}]}`, false},
		// An allowlist, not "any member that carries": a relay stamping a
		// bookkeeping field must not silently make the breaker inert.
		{"a bookkeeping field only", `{"choices":[{"message":{"role":"assistant","content":"","name":"assistant","provider":"acme"}}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out ChatCompletionResponse
			if err := json.Unmarshal([]byte(tc.body), &out); err != nil {
				t.Fatalf("fixture did not decode: %v", err)
			}
			if got := !answerCarriesSomething(out); got != tc.want {
				t.Errorf("emptyCompletion = %v, want %v", got, tc.want)
			}
		})
	}
}

// And the retirement bar stays where it was: a refusal envelope must not clear a
// model's gone-strike streak.
func TestChatAnswerCarriesContent_StaysStrictForRetirement(t *testing.T) {
	t.Parallel()
	var out ChatCompletionResponse
	body := `{"choices":[{"message":{"role":"assistant","content":null,"refusal":"this model is no longer available"}}]}`
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("fixture did not decode: %v", err)
	}
	if chatAnswerCarriesContent(out) {
		t.Error("a refusal envelope cleared the retirement streak: an aggregator writes its gone-marker there")
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

// A Gemini prompt blocked by its safety filter comes back 200 with an empty
// candidate list. BuildChatCompletion cannot turn that into a completion, but
// the body is a perfectly good Gemini object and the provider is plainly alive —
// so charging for it took a healthy provider out of rotation for every tenant
// after five blocked prompts, which is exactly what a client retries.
func TestAttemptCandidate_AGeminiSafetyBlockIsNotAProviderFault(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantCharge bool
	}{
		{"prompt blocked by the safety filter", `{"promptFeedback":{"blockReason":"SAFETY"},"candidates":[]}`, false},
		// No candidates and no stated reason is not Gemini declining to answer,
		// it is a body that does not carry one — {} , null and an aggregator's
		// 200 {"error":…} all unmarshal into the Gemini shape without complaint,
		// so exempting candidate-absence left no body a Gemini provider could
		// return that would ever charge its breaker.
		{"no candidates and no reason", `{"candidates":[]}`, true},
		{"an aggregator error envelope", `{"error":{"code":404,"message":"model gone"}}`, true},
		{"a bare null", `null`, true},
		// Still a fault: these bytes are not a Gemini response at all.
		{"not a gemini object", `<html>502 Bad Gateway</html>`, true},
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

			m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
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

			h.attemptCandidate(httptest.NewRecorder(), r, st, cand, 0, 2)

			charged := h.circuitBreaker.GetState(cand.provider.ID) == failover.StateOpen
			if charged != tc.wantCharge {
				t.Errorf("charged = %v, want %v", charged, tc.wantCharge)
			}
		})
	}
}

// The third charge site created by this change needed the client-gone guard the
// other two got. Both translators read the body with an unbounded ReadAll under
// the caller's context, so an abandoned request arrives as a translation
// failure.
func TestRejectUntranslatableBody_AClientHangingUpIsNotCharged(t *testing.T) {
	cb := failover.NewCircuitBreaker(nil)
	h := &Handler{circuitBreaker: cb}
	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}

	h.rejectUntranslatableBody(st, candidate, &requestLogData{modelID: "m", providerName: "p"}, "gemini", errors.New("read: connection reset"), 0, cancelledRequest())

	if _, seen := cbConsecutiveFails(cb, providerID); seen {
		t.Error("an abandoned request was charged to the provider")
	}
}

// A body that died on the wire is not a body this gateway could not parse, and
// the two were collapsed into one error kind — so the transport failure carried
// the parse failure's classification, which providerAtFault excludes, and it was
// never charged.
func TestHandleNonStreamingResponse_ABodyThatDiedOnTheWireIsCharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true, startTime: time.Now()}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}
	logData := &requestLogData{
		modelID: "gpt-test", providerID: providerID, providerName: "p", state: "pending",
		virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
	}
	body := io.MultiReader(bytes.NewBufferString(`{"id":"x","choices":[{"message":{"content":"par`), &errorReader{err: errors.New("connection reset by peer")})
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body), Header: make(http.Header)}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))

	h.handleNonStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)
	h.recordAnswerOutcome(st, candidate, logData, nil)

	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %q, want provider_error: a dead read is not a parse failure", logData.errorKind)
	}
	if h.circuitBreaker.GetState(providerID) != failover.StateOpen {
		t.Error("a provider that broke after committing its status was not charged")
	}
}

// cancelledRequest is a request whose caller has already gone.
func cancelledRequest() *http.Request {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).WithContext(ctx)
}

// A 204 or 202 legitimately carries an empty body and the provider is plainly
// alive. The streamed pass-through twin says exactly that and credits it, so
// charging here would have the two disagree about one response.
func TestServeBufferedJSONPassthrough_AnEmptyNon200IsCredited(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThreshold(t, h, "2")

	providerID := uuid.New()
	h.circuitBreaker.RecordFailure(providerID, "p")
	st := passthroughState(providerID)
	candidate := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"},
		provider: &provider.Provider{ID: providerID, Name: "p"},
	}
	resp := &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(bytes.NewBufferString("")), Header: make(http.Header)}

	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, candidate, resp, "application/json", 1, 5)

	h.circuitBreaker.RecordFailure(providerID, "p")
	if h.circuitBreaker.GetState(providerID) == failover.StateOpen {
		t.Error("an empty 204 recorded no success, so an old failure was still on the clock")
	}
}

// And a body that READ fine under a caller who has gone is not the provider's
// doing either. The existing abandoned-read test exits at the read-error guard
// and never reaches this arm.
func TestServeBufferedJSONPassthrough_AnAbandonedRequestIsNotCharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	st := passthroughState(providerID)
	candidate := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"},
		provider: &provider.Provider{ID: providerID, Name: "p"},
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"object":"list","data":[]}`)), Header: make(http.Header)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/v1/embeddings", http.NoBody).WithContext(ctx)

	h.serveBufferedJSONPassthrough(httptest.NewRecorder(), req, st, candidate, resp, "application/json", 1, 5)

	if h.circuitBreaker.GetState(providerID) == failover.StateOpen {
		t.Error("an abandoned pass-through request was charged to the provider")
	}
}

func passthroughState(providerID uuid.UUID) *requestState {
	return &requestState{
		circuitBreakerEnabled: true,
		startTime:             time.Now(),
		logData: &requestLogData{
			modelID: "text-embedding-3-small", providerID: providerID, providerName: "p",
			endpointType: endpointTypeEmbeddings, state: "pending",
			virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
		},
	}
}

// The hole the narrow bar left, driven end to end. Every egress translator
// synthesises a one-element Choices literal on success, so an emptied Gemini,
// Anthropic-egress or Responses answer always arrives with a choice — and a rule
// that charged only `len(Choices) == 0` could never fire for any of them. The
// same upstream emptiness charged on the native path and credited here, chosen
// by nothing but which dialect the request came in on.
func TestAttemptCandidate_AnEmptiedEgressAnswerIsCharged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		baseURL string
		body    string
	}{
		{"gemini egress", "http://us-central1-aiplatform.googleapis.com/v1", `{"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"STOP"}]}`},
		{"anthropic egress", "http://api.anthropic.com", `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":3,"output_tokens":0}}`},
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

			m := &model.Model{ID: uuid.New(), ModelID: "m"}
			cand := goneCandidateAt(m, "egress", tc.baseURL)

			st := &requestState{
				startTime:             time.Now(),
				reqModel:              "m",
				bodyBytes:             []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`),
				failoverTimeout:       30 * time.Second,
				circuitBreakerEnabled: true,
				vkHash:                "test-hash",
				logData: &requestLogData{
					modelID: "m", providerID: cand.provider.ID, providerName: "egress",
					endpointType: endpointTypeChat, state: "pending",
					virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
				},
			}
			r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

			h.attemptCandidate(httptest.NewRecorder(), r, st, cand, 0, 1)

			if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
				t.Errorf("an emptied %s answer was credited (state %q)", tc.name, st.logData.state)
			}
		})
	}
}

// A caller that hangs up is not the provider failing, and the row has to say so:
// the body is read under the request context, so a client cancel arrives as a
// read error and was being reported as the provider dying.
func TestNonStreamingFailureDetail_ClassifiesTheReadFailure(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	for _, tc := range []struct {
		name    string
		readErr error
		want    ErrorKind
	}{
		{"the client cancelled", context.Canceled, KindClientDisconnect},
		{"the provider stopped sending", errors.New("connection reset by peer"), KindProviderError},
		{"a body this gateway could not parse", nil, KindProviderBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decodeErr := tc.readErr
			if decodeErr == nil {
				decodeErr = errors.New("invalid character")
			}
			_, _, kind, _ := nonStreamingFailureDetail(resp, []byte("{"), tc.readErr, decodeErr, "m")
			if kind != tc.want {
				t.Errorf("kind = %q, want %q", kind, tc.want)
			}
		})
	}
}

// JSON is self-delimiting, so a provider that sent the whole document and then
// dropped the connection without its terminal chunk yields ErrUnexpectedEOF from
// a body that parses perfectly. Discarding it threw away a complete answer, and
// once the breaker started reading the outcome it charged for one too.
func TestHandleNonStreamingResponse_ACompleteBodyBehindAnUncleanCloseIsServed(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	st := &requestState{circuitBreakerEnabled: true, startTime: time.Now()}
	candidate := modelCandidate{provider: &provider.Provider{ID: providerID, Name: "p"}}
	logData := &requestLogData{
		modelID: "m", providerID: providerID, providerName: "p", state: "pending",
		virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
	}
	complete := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"}}]}`
	body := io.MultiReader(bytes.NewBufferString(complete), &errorReader{err: io.ErrUnexpectedEOF})
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body), Header: make(http.Header)}
	w := httptest.NewRecorder()

	h.handleNonStreamingResponse(w, withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)), logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)
	h.recordAnswerOutcome(st, candidate, logData, nil)

	if !strings.Contains(w.Body.String(), "hello") {
		t.Errorf("a complete answer was discarded: %q", w.Body.String())
	}
	if h.circuitBreaker.GetState(providerID) == failover.StateOpen {
		t.Error("a provider that delivered a complete answer was charged")
	}
}
