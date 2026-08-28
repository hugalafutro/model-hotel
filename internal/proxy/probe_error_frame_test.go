package proxy

import (
	"context"
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

// ---------------------------------------------------------------------------
// A provider that answers 200 and then puts an error envelope in its FIRST SSE
// data frame must not be credited with a first token. The TTFT probe is what
// picks the winner of a hedged race, so counting an error frame as a token
// rewards the fastest FAILURE: the broken provider wins, every healthy rival
// still in flight is cancelled as superseded, and the request dies with no
// second chance. Observed in production 2026-08-28, 36 consecutive dead
// requests on hotel/glm52 while a working provider was cancelled each time.
// ---------------------------------------------------------------------------

const errorFrameSSE = "data: {\"error\":{\"message\":\"Unterminated string starting at: line 1 column 9777 (char 9776)\"}}\n\n"

func TestProbeFirstToken_ErrorEnvelopeIsNotAToken(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, errorFrameSSE)

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, time.Now())

	if err == nil {
		t.Fatalf("an error envelope must fail the probe, got a token at ttft=%.1fms buf=%q", trueTtftMs, bufString(probeBuf))
	}
	if !strings.Contains(err.Error(), "Unterminated string") {
		t.Errorf("probe error = %q, want it to carry the provider's own message", err)
	}
}

// The error frame is still an error when it arrives behind the keepalives and
// event: lines the probe skips, which is how a real provider frames it.
func TestProbeFirstToken_ErrorEnvelopeBehindKeepalive(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, ": keepalive\n\nevent: error\n"+errorFrameSSE)

	if _, _, err := h.probeFirstToken(context.Background(), body, 5*time.Second, time.Now()); err == nil {
		t.Fatal("an error envelope behind a keepalive must fail the probe")
	}
}

// The guard must be narrow: an ordinary first token is still a first token.
// Without this a false positive would fail over every healthy stream.
func TestProbeFirstToken_ContentFrameStillWins(t *testing.T) {
	h := &Handler{}
	for name, frame := range map[string]string{
		"content delta":     "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n",
		"empty choices":     "data: {\"choices\":[]}\n\n",
		"role-only delta":   "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n",
		"explicit null err": "data: {\"error\":null,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n",
		"unparseable json":  "data: {not json at all\n\n",
		"word error inside": "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"the error was mine\"}}]}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			h2 := h
			if _, ttft, err := h2.probeFirstToken(context.Background(), makeSSEBody(t, frame), 5*time.Second, time.Now()); err != nil {
				t.Fatalf("a healthy first frame must win the probe, got %v", err)
			} else if ttft <= 0 {
				t.Errorf("ttft = %f, want > 0", ttft)
			}
		})
	}
}

func bufString(b interface{ String() string }) string {
	if b == nil {
		return ""
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// The hedged path: an error frame must lose the race and be charged.
// ---------------------------------------------------------------------------

func TestProbeStreamingCandidate_ErrorFrameLosesAndIsChargedToTheProvider(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	withBreakerThresholdOne(t, h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, errorFrameSSE)
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.circuitBreakerEnabled = true

	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
	if res.resp != nil {
		_ = res.resp.Body.Close()
	}
	if res.won {
		t.Fatal("a stream whose first frame is an error must not win the race")
	}
	if res.reqErr.Kind != KindProviderError {
		t.Errorf("kind = %s, want %s: the provider answered, it just answered with an error",
			res.reqErr.Kind, KindProviderError)
	}
	// Charged to the breaker, which is the half that stops the next 35
	// requests walking into the same provider.
	if got := h.circuitBreaker.GetState(cand.provider.ID); got != failover.StateOpen {
		t.Errorf("circuit = %s, want open: an error-frame probe is a provider failure", got)
	}
}

// The incident, end to end. Two candidates race: the broken one answers with an
// error frame FIRST, the healthy one answers with a real token later. Before
// the fix the broken one won at 100ms and the healthy one was cancelled as
// hedge_superseded, so the caller got nothing.
func TestRunHedgedStreaming_HealthyCandidateBeatsAFasterErrorFrame(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, errorFrameSSE)
	}))
	defer broken.Close()

	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(250 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer healthy.Close()

	st, logData := newHedgeState(10 * time.Millisecond)
	st.bodyBytes = []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	cands := []modelCandidate{
		liveCandidate("broken", broken.URL),
		liveCandidate("healthy", healthy.URL),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.runHedgedStreaming(w, req, st, cands, h.probeStreamingCandidate)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `"content":"hi"`) {
		t.Fatalf("the caller must receive the healthy provider's token, got: %s", body)
	}
	if logData.providerName != "healthy" {
		t.Errorf("winner = %q, want %q: the error frame must not win the race",
			logData.providerName, "healthy")
	}
}

// ---------------------------------------------------------------------------
// A stream that commits and THEN fails without delivering anything is still a
// provider failure. The probe guard above only covers the first frame; an error
// on a later frame arrives after the rivals are already cancelled, so the
// breaker is the only thing left that can keep the next request away.
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_ErrorWithNoContentChargesTheBreaker(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThresholdOne(t, h)

	// A valid opening frame (so the probe would pass), then the error. Nothing
	// is ever delivered to the caller.
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" + errorFrameSSE)),
	}

	providerID := uuid.New()
	// providerID is deliberately left off logData: the request-log row has a
	// foreign key to providers and this provider exists only in the breaker.
	logData := streamingLog()
	logData.providerName = "error-frame-provider"
	h.insertRequestLogAsync(logData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       providerID,
		providerName:     "error-frame-provider",
		circuitBreakerOn: true,
		vkHash:           "test-hash",
		attempt:          1,
	})

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed", logData.state)
	}
	if got := h.circuitBreaker.GetState(providerID); got != failover.StateOpen {
		t.Errorf("circuit = %s, want open: the stream errored having delivered nothing", got)
	}
}

// The counterpart guard: a provider that streamed real content before failing
// did part of its job, and must NOT be broken for it. Without this the fix
// above would open a circuit on every truncated-but-useful stream.
func TestHandleStreamingResponse_ErrorAfterContentDoesNotChargeTheBreaker(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThresholdOne(t, h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial answer\"}}]}\n\n" + errorFrameSSE)),
	}

	providerID := uuid.New()
	logData := streamingLog()
	logData.providerName = "partial-provider"
	h.insertRequestLogAsync(logData)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       providerID,
		providerName:     "partial-provider",
		circuitBreakerOn: true,
		vkHash:           "test-hash",
		attempt:          1,
	})

	if got := h.circuitBreaker.GetState(providerID); got == failover.StateOpen {
		t.Error("a provider that delivered content before failing must not be broken for it")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// liveCandidate builds a hedge candidate pointed at a real test server.
func liveCandidate(name, baseURL string) modelCandidate {
	return modelCandidate{
		model:    &model.Model{ModelID: "m-" + name},
		provider: &provider.Provider{ID: uuid.New(), Name: name, BaseURL: baseURL},
		apiKey:   "sk-test",
	}
}

// withBreakerThresholdOne makes a single RecordFailure open the circuit, so a
// test can observe "was the breaker charged" through GetState.
func withBreakerThresholdOne(t *testing.T, h *Handler) {
	t.Helper()
	if err := h.settingsRepo.Set(context.Background(), "circuit_breaker_threshold", "1"); err != nil {
		t.Fatalf("set circuit_breaker_threshold: %v", err)
	}
	h.settingsRepo.InvalidateCache("circuit_breaker_threshold")
	t.Cleanup(func() {
		_ = h.settingsRepo.Set(context.Background(), "circuit_breaker_threshold", "5")
		h.settingsRepo.InvalidateCache("circuit_breaker_threshold")
	})
}

// errorEnvelopeMessage is unit-tested directly because the probe's scanner-error
// recovery branch calls it too, and that branch is defence-in-depth for a race
// the existing tests document as impossible to trigger deterministically
// (see TestProbeFirstToken_ScannerErrorRecovery_PipeRace). Testing the decision
// covers both callers of it.
func TestErrorEnvelopeMessage(t *testing.T) {
	tests := []struct {
		name    string
		frame   string
		wantMsg string
		wantOk  bool
	}{
		{"provider error", `{"error":{"message":"boom"}}`, "boom", true},
		{"error with no message", `{"error":{}}`, "provider reported an error with no message", true},
		{"anthropic error event", `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`, "overloaded", true},
		{"ordinary token", `{"choices":[{"delta":{"content":"hi"}}]}`, "", false},
		{"explicit null error", `{"error":null,"choices":[]}`, "", false},
		{"unparseable frame", `{not json`, "", false},
		{"error as a string", `{"error":"boom"}`, "", false},
		{"json array", `[1,2,3]`, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := errorEnvelopeMessage(tc.frame)
			if ok != tc.wantOk {
				t.Fatalf("ok = %v, want %v (msg %q)", ok, tc.wantOk, msg)
			}
			if msg != tc.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tc.wantMsg)
			}
		})
	}
}

// The provider's own text reaches the request log through reqError.Underlying,
// so it goes through the same sanitizer every other upstream body does.
func TestClassifyProbeError_SanitizesTheProvidersMessage(t *testing.T) {
	msg := "tenant 793ac38b-0211-43e6-baa7-aa7054c39931 exceeded quota"
	re, charged := classifyProbeError(&upstreamFrameError{msg: msg}, "prov-A", false, time.Second, 30*time.Second, 60*time.Second, 1)

	if !charged {
		t.Error("an error envelope is always the provider's fault and must be charged")
	}
	if re.Kind != KindProviderError {
		t.Errorf("kind = %s, want %s", re.Kind, KindProviderError)
	}
	if strings.Contains(re.Underlying, "793ac38b") {
		t.Errorf("underlying = %q, want the UUID redacted", re.Underlying)
	}
}

// A client that hangs up cannot excuse an error envelope: the provider had
// already answered with a failure, so the charge does not depend on the
// downstream connection the way a zero-token stall does.
func TestClassifyProbeError_ChargesEvenWhenTheClientIsGone(t *testing.T) {
	_, charged := classifyProbeError(&upstreamFrameError{msg: "boom"}, "prov-A", true, time.Millisecond, 30*time.Second, 60*time.Second, 1)
	if !charged {
		t.Error("an error envelope must be charged to the provider even when the client is gone")
	}
	// The contrast that makes the case above meaningful: a zero-token stall
	// with a fast client close is NOT charged.
	if _, chargedStall := classifyProbeError(errors.New("TTFT timeout"), "prov-A", true, time.Millisecond, 30*time.Second, 60*time.Second, 1); chargedStall {
		t.Error("a fast client cancel with zero tokens must still not be charged")
	}
}
