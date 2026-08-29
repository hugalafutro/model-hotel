package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// serveStatus drives one candidate attempt against an upstream that answers
// with the given status and body, and hands back what the client saw.
func serveStatus(t *testing.T, status int, upstreamBody string) (*httptest.ResponseRecorder, *requestState, *mockVirtualKeyRepo) {
	w, st, vk, _ := serveStatusOutcome(t, status, upstreamBody, 1)
	return w, st, vk
}

func serveStatusOutcome(t *testing.T, status int, upstreamBody string, totalCandidates int) (*httptest.ResponseRecorder, *requestState, *mockVirtualKeyRepo, candidateOutcome) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if upstreamBody != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		if upstreamBody != "" {
			_, _ = io.WriteString(w, upstreamBody)
		}
	}))
	t.Cleanup(srv.Close)
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "relay-model"}
	cand := goneCandidateAt(m, "Relay", "http://relay.example.com")

	logData := &requestLogData{
		id: uuid.New().String(), modelID: "relay-model",
		providerID: cand.provider.ID, providerName: "Relay",
		endpointType: endpointTypeChat, state: "pending",
		virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
	}
	st := &requestState{
		startTime: time.Now(), reqModel: "relay-model",
		bodyBytes:       []byte(`{"model":"relay-model","messages":[{"role":"user","content":"hi"}]}`),
		failoverTimeout: 30 * time.Second,
		vkHash:          "test-hash",
		logData:         logData,
	}
	h.insertRequestLogAsync(logData)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	outcome := h.attemptCandidate(w, r, st, cand, 0, totalCandidates)
	return w, st, vkRepo, outcome
}

// A 2xx that is not a bare 200 is a served answer and must be metered like one.
//
// attemptCandidate routed on `!= 200`, so a 201 or 202 — what an
// OpenAI-compatible relay or aggregator can answer with — was diverted to
// forwardUpstreamError. That wrote the row as state="failed" and never called
// recordTokenUsage, then forwarded the provider's body to the client anyway. The
// caller kept a complete answer that charged no tokens against the virtual key
// and debited no TPM bucket, which is a quota bypass a client controls by
// picking such a provider.
//
// The pass-through families already had this right: multimodal routes on the
// 2xx RANGE and only sends a real non-2xx to forwardUpstreamError.
func TestAttemptCandidate_ASuccessStatusOtherThan200IsMetered(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`
			w, st, vkRepo := serveStatus(t, status, body)

			if st.logData.state != "completed" {
				t.Errorf("row state = %q, want completed: a served answer was recorded as a failure", st.logData.state)
			}
			if st.logData.statusCode != status {
				t.Errorf("row status = %d, want %d", st.logData.statusCode, status)
			}
			if got := singleAddTokens(t, vkRepo); got != 12 {
				t.Errorf("metered %d tokens, want 12: the answer was served free", got)
			}
			if st.logData.tokensPrompt != 7 || st.logData.tokensCompletion != 5 {
				t.Errorf("row usage = %d/%d, want 7/5", st.logData.tokensPrompt, st.logData.tokensCompletion)
			}
			if w.Code != status {
				t.Errorf("client status = %d, want %d", w.Code, status)
			}
		})
	}
}

// And the reason the routing could not simply be widened without care: a 204 is
// a success with no body, and the gateway must not invent one. An error
// envelope written under No Content would be a body the provider did not send,
// under a status that forbids one.
func TestAttemptCandidate_A204IsForwardedWithoutAnInventedBody(t *testing.T) {
	w, st, vkRepo := serveStatus(t, http.StatusNoContent, "")

	if w.Code != http.StatusNoContent {
		t.Errorf("client status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 carried a body the provider never sent: %q", w.Body.String())
	}
	if st.logData.state != "completed" {
		t.Errorf("row state = %q, want completed", st.logData.state)
	}
	// Nothing was delivered, so nothing is charged.
	if n := len(vkRepo.addTokensCalls); n != 0 {
		t.Errorf("charged %d times for a No Content answer, want 0", n)
	}
}

// A success is not a reason to try the next provider. The old routing sent
// every non-200 to forwardUpstreamError, whose eligibility gate a 2xx had to be
// checked ahead of so a success could never be rewritten into an envelope. With
// the routing fixed that ordering is no longer what protects it — this is.
func TestAttemptCandidate_ASuccessIsNeverFailedOver(t *testing.T) {
	body := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	_, _, _, outcome := serveStatusOutcome(t, http.StatusCreated, body, 2)
	if outcome != outcomeServed {
		t.Errorf("outcome = %v, want served: a 201 answer sent the request to the next candidate", outcome)
	}
}

// The deliberate narrowing that came with the fix. A 2xx carrying something
// that is not a completion used to be forwarded to the client verbatim,
// unmetered, with the row written as failed. It now gets an error BODY.
//
// The caller asked for a chat completion; `accepted` is not one, and an OpenAI
// client cannot parse it either. Forwarding an unreadable success is also
// exactly how the unmetered path stayed invisible. 204/205 remain the one
// exception, because their status promises no body — see
// TestAttemptCandidate_A204IsForwardedWithoutAnInventedBody.
//
// The STATUS stays the upstream's 2xx, which an OpenAI SDK will not raise on:
// it unmarshals and finds no choices. That is long-standing behaviour for a 200
// with an undecodable body (TestHandleNonStreamingResponse_EmptyBody pins it),
// and this change widens the set of statuses it applies to rather than
// introducing it. Answering 502 instead, as the multimodal twin already does,
// is a separate change to a contract clients depend on.
func TestAttemptCandidate_A2xxThatIsNotACompletionIsAnError(t *testing.T) {
	w, st, vkRepo := serveStatus(t, http.StatusAccepted, `accepted`)

	if st.logData.state != "failed" {
		t.Errorf("row state = %q, want failed: a 202 that carried no completion was recorded as served", st.logData.state)
	}
	if n := len(vkRepo.addTokensCalls); n != 0 {
		t.Errorf("charged %d times for a body that is not a completion, want 0", n)
	}
	if got := w.Body.String(); !strings.Contains(got, `"error"`) {
		t.Errorf("client got %q, want an error envelope it can parse", got)
	}
}

// The breaker must judge a 201 by its answer, not by its headers.
//
// recordBreakerOutcome credited a success for any non-200 at header time, which
// was harmless only while a non-200 success could never reach the body readers.
// Once the routing let 201 through, that credit reset consecutiveFails and
// erased the charge recordAnswerOutcome was about to make.
//
// Threshold TWO, and two requests, because that is the only shape that shows it.
// At a threshold of one the erased credit is invisible: the charge that follows
// microseconds later opens the circuit by itself, so the test passes with the
// bug in place — which is exactly what the first version of this test did while
// its own comment said the circuit "could never open above a threshold of one".
func TestAttemptCandidate_AnEmptySuccessChargesTheBreakerWhateverIts2xx(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })
			withBreakerThreshold(t, h, "2")

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[]}`)
			}))
			t.Cleanup(srv.Close)
			h.upstreamTransport = dialToTestServer(t, srv)

			m := &model.Model{ID: uuid.New(), ModelID: "relay-model"}
			cand := goneCandidateAt(m, "Relay", "http://relay.example.com")
			for range 2 {
				st := &requestState{
					startTime: time.Now(), reqModel: "relay-model",
					bodyBytes:             []byte(`{"model":"relay-model","messages":[{"role":"user","content":"hi"}]}`),
					failoverTimeout:       30 * time.Second,
					circuitBreakerEnabled: true,
					vkHash:                "test-hash",
					logData: &requestLogData{
						id: uuid.New().String(), modelID: "relay-model",
						providerID: cand.provider.ID, providerName: "Relay",
						endpointType: endpointTypeChat, state: "pending",
						virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
					},
				}
				h.insertRequestLogAsync(st.logData)
				h.attemptCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cand, 0, 1)
			}

			if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
				t.Errorf("two %d answers carrying nothing did not open the circuit: "+
					"a header-time credit erased each charge as it was made", status)
			}
		})
	}
}

// Delivering nothing is not delivering. A 204 is a legitimate HTTP success, but
// for a chat completion it means the model produced no answer, so it must not
// buy the provider a breaker credit — a relay answering 204 to everything would
// otherwise black-hole a whole failover group with its circuit permanently shut.
func TestAttemptCandidate_A204DoesNotCreditTheBreaker(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThresholdOne(t, h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "relay-model"}
	cand := goneCandidateAt(m, "Relay", "http://relay.example.com")
	st := &requestState{
		startTime: time.Now(), reqModel: "relay-model",
		bodyBytes:             []byte(`{"model":"relay-model","messages":[{"role":"user","content":"hi"}]}`),
		failoverTimeout:       30 * time.Second,
		circuitBreakerEnabled: true,
		vkHash:                "test-hash",
		logData: &requestLogData{
			id: uuid.New().String(), modelID: "relay-model",
			providerID: cand.provider.ID, providerName: "Relay",
			endpointType: endpointTypeChat, state: "pending",
			virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
		},
	}
	h.insertRequestLogAsync(st.logData)
	h.attemptCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), st, cand, 0, 1)

	if !st.logData.emptyCompletion {
		t.Error("a 204 was recorded as having delivered something")
	}
	if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
		t.Error("a 204 bought the provider a breaker credit")
	}
}

// 205 Reset Content is the other status HTTP forbids a body on. Untested, it
// was one character away from being dropped from bodilessSuccessStatus without
// anything noticing.
func TestAttemptCandidate_A205IsAlsoBodiless(t *testing.T) {
	w, st, _ := serveStatus(t, http.StatusResetContent, "")

	if w.Code != http.StatusResetContent {
		t.Errorf("client status = %d, want 205", w.Code)
	}
	if st.logData.state != "completed" || st.logData.statusCode != http.StatusResetContent {
		t.Errorf("row = %s/%d, want completed/205", st.logData.state, st.logData.statusCode)
	}
}

// The Anthropic ingress wraps the writer, and that wrapper decided every one of
// its three output modes on `status == 200`. Preserving the provider's 201
// therefore dropped a good completion into an Anthropic error envelope — with
// the model's own text inside error.message — and the metering fix meant the
// caller was now CHARGED for it.
//
// All three modes are covered, because the first version of this test drove only
// the buffered one and two of the three mutations survived it.
func TestAnthropicResponseWriter_TreatsEverySuccessAsASuccess(t *testing.T) {
	const completion = `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`

	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run("buffered/"+http.StatusText(status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			aw := newAnthropicResponseWriter(rec, "msg_1", "claude-x")
			aw.WriteHeader(status)
			_, _ = aw.Write([]byte(completion))
			aw.Finalize()

			if got := rec.Body.String(); strings.Contains(got, `"type":"error"`) {
				t.Errorf("a %d completion was delivered as an Anthropic error: %s", status, got)
			}
		})

		// Native passthrough: the upstream is already Anthropic-shaped, so the
		// bytes must go out untouched rather than through the translator.
		t.Run("native/"+http.StatusText(status), func(t *testing.T) {
			const native = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}`
			rec := httptest.NewRecorder()
			aw := newAnthropicResponseWriter(rec, "msg_1", "claude-x")
			isNative := true
			aw.bindNativeFlag(&isNative)
			aw.WriteHeader(status)
			_, _ = aw.Write([]byte(native))
			aw.Finalize()

			if got := rec.Body.String(); got != native {
				t.Errorf("a %d native answer was not forwarded verbatim:\n got: %s\nwant: %s", status, got, native)
			}
		})

		// Streaming: an SSE success must be translated incrementally, not
		// collected into a buffered error.
		t.Run("stream/"+http.StatusText(status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			aw := newAnthropicResponseWriter(rec, "msg_1", "claude-x")
			aw.Header().Set("Content-Type", "text/event-stream")
			aw.WriteHeader(status)
			_, _ = aw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
			aw.Finalize()

			got := rec.Body.String()
			if strings.Contains(got, `"type":"error"`) {
				t.Errorf("a %d SSE success was delivered as an Anthropic error: %s", status, got)
			}
			if !strings.Contains(got, "message_start") {
				t.Errorf("a %d SSE success was not translated as a stream: %s", status, got)
			}
		})
	}
}

// A status that forbids a body has nothing to translate, and running the
// translator on nothing fails and rewrites the answer to 502. That turned a
// provider's legitimate No Content into a gateway error on the Anthropic
// ingress, while the OpenAI ingress and the request log both said 204.
func TestAnthropicResponseWriter_BodilessSuccessIsPassedThrough(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			aw := newAnthropicResponseWriter(rec, "msg_1", "claude-x")
			aw.WriteHeader(status)
			aw.Finalize()

			if rec.Code != status {
				t.Errorf("client status = %d, want %d", rec.Code, status)
			}
			if got := rec.Body.String(); got != "" {
				t.Errorf("%d answered with an invented body: %s", status, got)
			}
		})
	}
}

// The retirement probe judged any non-200 a probe FAILURE, so a relay that
// answers 201 could push a live model toward retirement — the probe exists
// precisely to stop a model being retired while it is still answering.
func TestProbeModel_ASuccessStatusOtherThan200IsNotAProbeFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newProbeHandler(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"still here"},"finish_reason":"stop"}]}`)
			}))
			t.Cleanup(srv.Close)
			h.upstreamTransport = dialToTestServer(t, srv)

			if got := runProbe(t, h, probeCandidateFor(srv.URL, "relay-model"), endpointTypeChat); got != probeServed {
				t.Errorf("a %d answer gave verdict %v, want probeServed: a model that answers must never be retired", status, got)
			}
		})
	}
}

// The hedge race dropped any non-200 candidate, so with hedging on, the same
// 201 the sequential path serves and meters was thrown away — and if it was the
// only candidate the request failed outright.
func TestProbeStreamingCandidate_ASuccessStatusOtherThan200CanWin(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}))
			t.Cleanup(srv.Close)
			h.upstreamTransport = dialToTestServer(t, srv)

			st, cand := probeStateForServer(srv.URL)
			res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 2*time.Second, time.Second)
			if !res.won {
				t.Errorf("a %d stream lost the hedge race: %+v", status, res.reqErr)
			}
		})
	}
}

// The native Anthropic path recorded and returned a hardcoded 200 for whatever
// the provider sent, so a native 201 was logged as a status no upstream ever
// returned — the same flattening the OpenAI path was just fixed not to do.
func TestAttemptCandidate_NativeAnthropicKeepsItsSuccessStatus(t *testing.T) {
	const native = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":3,"output_tokens":2}}`
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, native)
			}))
			t.Cleanup(srv.Close)
			h.upstreamTransport = dialToTestServer(t, srv)

			m := &model.Model{ID: uuid.New(), ModelID: "claude-x"}
			cand := goneCandidateAt(m, "Anthropic", "http://api.anthropic.com")
			st := &requestState{
				startTime: time.Now(), reqModel: "claude-x",
				bodyBytes:        []byte(`{"model":"claude-x","messages":[{"role":"user","content":"hi"}]}`),
				anthropicRawBody: []byte(`{"model":"claude-x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`),
				anthropicIn:      true,
				failoverTimeout:  30 * time.Second,
				vkHash:           "test-hash",
				logData: &requestLogData{
					id: uuid.New().String(), modelID: "claude-x",
					providerID: cand.provider.ID, providerName: "Anthropic",
					endpointType: endpointTypeChat, state: "pending",
					virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
				},
			}
			h.insertRequestLogAsync(st.logData)
			w := httptest.NewRecorder()
			h.attemptCandidate(w, httptest.NewRequest("POST", "/v1/messages", http.NoBody), st, cand, 0, 1)

			if st.logData.statusCode != status {
				t.Errorf("row status = %d, want %d: the native path flattened it", st.logData.statusCode, status)
			}
			if w.Code != status {
				t.Errorf("client status = %d, want %d", w.Code, status)
			}
		})
	}
}
