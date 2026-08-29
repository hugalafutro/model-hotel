package proxy

import (
	"bytes"
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
			h.recordAnswerOutcome(st, candidate, logData)

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

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "completed", deliveredContent: true, providerID: providerID, providerName: "p"})

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

	h.recordAnswerOutcome(st, candidate, &requestLogData{state: "failed", providerID: providerID, providerName: "p"})

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
		&requestLogData{state: "failed", providerID: providerID, providerName: "p"})

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

// And the charge has to happen at the three sites that produce it, not just in
// the helper. Driven through attemptCandidate with a Gemini candidate whose 200
// is not a Gemini answer, which is the shape translateEgressResponseBody
// rejects — the case each adapter's own comment already calls a provider fault,
// and the case the old code credited before the translation was attempted.
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
