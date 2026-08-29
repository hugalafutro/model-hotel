package proxy

import (
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
// unmetered, with the row written as failed. It is now an error.
//
// The caller asked for a chat completion; `accepted` is not one, and an OpenAI
// client cannot parse it either. Forwarding an unreadable success is also
// exactly how the unmetered path stayed invisible. 204/205 remain the one
// exception, because their status promises no body — see
// TestAttemptCandidate_A204IsForwardedWithoutAnInventedBody.
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
// erased the charge recordAnswerOutcome was about to make — so a relay
// answering 201 with no completion could never open its circuit. That is the
// #805 hole re-opened on the statuses the routing had just started admitting.
func TestAttemptCandidate_AnEmptySuccessChargesTheBreakerWhateverIts2xx(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })
			withBreakerThresholdOne(t, h)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[]}`)
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

			if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
				t.Errorf("a %d carrying no answer was credited instead of charged", status)
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

// The Anthropic ingress wraps the writer, and that wrapper decided verbatim vs
// error-envelope on `status == 200`. Preserving the provider's 201 therefore
// dropped a perfectly good completion into an Anthropic error envelope — with
// the model's own text inside error.message — and the metering fix meant the
// caller was now CHARGED for it.
func TestAnthropicResponseWriter_TreatsEverySuccessAsASuccess(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			aw := newAnthropicResponseWriter(rec, "msg_1", "claude-x")
			aw.WriteHeader(status)
			_, _ = aw.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`))
			aw.Finalize()

			if got := rec.Body.String(); strings.Contains(got, `"type":"error"`) {
				t.Errorf("a %d completion was delivered as an Anthropic error: %s", status, got[:min(200, len(got))])
			}
		})
	}
}
