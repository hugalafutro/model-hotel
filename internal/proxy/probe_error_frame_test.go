package proxy

import (
	"bytes"
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
		// An empty boilerplate error member alongside a real token: the frame
		// delivered content, so failing it over would throw away an answer.
		"empty err + content": "data: {\"error\":{},\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n",
		// The native Anthropic passthrough is probed like any other stream, and
		// its first frame must keep winning.
		"anthropic message_start": "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10}}}\n\n",
		"unparseable json":        "data: {not json at all\n\n",
		"word error inside":       "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"the error was mine\"}}]}\n\n",
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

func TestDispatchStreaming_ErrorAfterTheFirstFrameOpensTheCircuit(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Two things about this test are load-bearing, and an earlier version of it
	// had neither.
	//
	// It goes through dispatchStreaming, NOT straight to handleStreamingResponse,
	// so the TTFT probe really runs. The bug being pinned lives in what the probe
	// tells the breaker, so a test that skips the probe cannot see it — the first
	// version of this test passed with the bug fully restored.
	//
	// And it runs at the PRODUCTION default threshold of 5. The bug is that a
	// probe success zeroes consecutiveFails on every request, so each failure can
	// only bring the count back to 1. A threshold of 1 is the single value at
	// which that is invisible.
	providerID := uuid.New()
	const attempts = 5
	for i := range attempts {
		// A valid opening frame, so the probe passes and the provider is
		// committed to; then the error. Nothing reaches the caller.
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" + errorFrameSSE)),
		}
		// providerID is deliberately left off logData: the request-log row has a
		// foreign key to providers and this provider exists only in the breaker.
		logData := streamingLog()
		logData.providerName = "error-frame-provider"
		h.insertRequestLogAsync(logData)

		st := &requestState{
			startTime:             time.Now(),
			reqModel:              "test-model",
			isStreaming:           true,
			circuitBreakerEnabled: true,
			logData:               logData,
		}
		cand := modelCandidate{
			model:    &model.Model{ModelID: "test-model"},
			provider: &provider.Provider{ID: providerID, Name: "error-frame-provider"},
			apiKey:   "sk-test",
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		if got := h.dispatchStreaming(w, req, st, cand, resp, 1, 10, "failover_timeout"); got != outcomeServed {
			t.Fatalf("attempt %d: outcome = %v, want served (the probe must pass on a healthy first frame)", i, got)
		}
		if logData.state != "failed" {
			t.Fatalf("attempt %d: state = %q, want failed", i, logData.state)
		}
	}

	if got := h.circuitBreaker.GetState(providerID); got != failover.StateOpen {
		t.Errorf("circuit = %s after %d committed-then-failed streams, want open", got, attempts)
	}
}

// A stream that completes is what lets a provider recover, and it is now
// recorded by the finalizer rather than the probe. Without it a provider would
// accumulate failures forever with nothing able to clear them.
func TestHandleStreamingResponse_CompletedStreamClearsTheFailureCount(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	providerID := uuid.New()
	opts := func() streamOptions {
		return streamOptions{
			responseHeaderMs: 10,
			providerID:       providerID,
			providerName:     "recovering-provider",
			circuitBreakerOn: true,
			vkHash:           "test-hash",
			attempt:          1,
		}
	}
	fail := func() {
		logData := streamingLog()
		logData.providerName = "recovering-provider"
		h.insertRequestLogAsync(logData)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(errorFrameSSE))}
		h.handleStreamingResponse(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), logData, resp, time.Now(), opts())
	}
	succeed := func() {
		logData := streamingLog()
		logData.providerName = "recovering-provider"
		h.insertRequestLogAsync(logData)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))}
		h.handleStreamingResponse(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), logData, resp, time.Now(), opts())
	}

	// Four failures is one short of the default threshold; a completed stream
	// must reset the count so the fifth failure does not open the circuit.
	for range 4 {
		fail()
	}
	succeed()
	fail()
	if got := h.circuitBreaker.GetState(providerID); got == failover.StateOpen {
		t.Errorf("circuit = %s, want closed: a completed stream must clear the failure count", got)
	}
}

// A gateway-authored failure is not the provider's fault and must not darken it.
// deriveStreamError produces these for the param-strip retry budget, the
// per-attempt failover timeout, and internal cancels.
func TestJudgeStreamForBreaker_DoesNotChargeGatewayAuthoredFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind ErrorKind
		want bool // charged
	}{
		{"provider error", KindProviderError, true},
		{"provider timeout", KindProviderTimeout, true},
		{"model gone", KindProviderModelGone, true},
		{"gateway internal", KindInternal, false},
		{"param-strip retry budget", KindRetryTimeout, false},
		{"failover deadline", KindFailoverTimeout, false},
		{"validation", KindValidation, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &streamState{}
			logData := &requestLogData{errorKind: tc.kind}
			got := judgeStreamForBreaker(st, logData, "something went wrong", true).failureReason != ""
			if got != tc.want {
				t.Errorf("charged = %v, want %v for kind %s", got, tc.want, tc.kind)
			}
		})
	}
}

// Neither a shutdown nor a client hangup is the provider's fault, and neither is
// evidence that it is healthy: both record nothing at all.
func TestJudgeStreamForBreaker_ShutdownAndClientHangupRecordNothing(t *testing.T) {
	for name, st := range map[string]*streamState{
		"gateway restarting": {interrupted: true},
		"client hung up":     {clientDisconnected: true},
	} {
		t.Run(name, func(t *testing.T) {
			logData := &requestLogData{errorKind: KindProviderError}
			v := judgeStreamForBreaker(st, logData, "stream interrupted", true)
			if v.failureReason != "" {
				t.Errorf("charged %q, want no charge", v.failureReason)
			}
			if v.success {
				t.Error("recorded a success, want nothing at all")
			}
		})
	}
}

// The native Anthropic passthrough never runs observeDataChunk, so sawContent is
// structurally unreachable there. A truncated native stream that delivered
// thousands of tokens must not be charged as though it delivered nothing.
func TestJudgeStreamForBreaker_NativePassthroughCountsDeliveredBytes(t *testing.T) {
	logData := &requestLogData{errorKind: KindProviderError}
	truncated := "stream truncated: upstream closed before message_stop"

	delivered := &streamState{deliveredBytes: 3900}
	if v := judgeStreamForBreaker(delivered, logData, truncated, true); v.failureReason != "" {
		t.Errorf("charged %q, want no charge: the caller received 3900 bytes", v.failureReason)
	}
	empty := &streamState{}
	if v := judgeStreamForBreaker(empty, logData, truncated, true); v.failureReason == "" {
		t.Error("a native stream that delivered nothing must be charged")
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
	// Threshold 1 here is deliberate and safe: it makes a single stray charge
	// visible, which is exactly what this negative test is looking for.

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
		{"anthropic error event", `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`, "overloaded", true},
		// Ollama answers with a bare string. carriesErrorObject has always
		// counted that as an error; the first version of this check unmarshalled
		// the frame into a streamChunk, which fails outright on this shape, so a
		// local Ollama box in a hedged group reproduced the whole incident.
		{"ollama bare string", `{"error":"model not found"}`, "model not found", true},
		{"object without a message", `{"error":{"code":500}}`, `{"code":500}`, true},
		{"ordinary token", `{"choices":[{"delta":{"content":"hi"}}]}`, "", false},
		{"explicit null error", `{"error":null,"choices":[]}`, "", false},
		{"unparseable frame", `{not json`, "", false},
		// Empty of every shape leaves a caller nothing to read, so it is not an
		// error frame — the same judgement carriesErrorObject makes.
		{"empty error object", `{"error":{}}`, "", false},
		{"empty error string", `{"error":""}`, "", false},
		{"empty error list", `{"error":[]}`, "", false},
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

// The provider's own text reaches request_logs.error_message through
// reqError.Underlying, where the virtual key's owner can read it. A provider is
// free to quote the api key back inside its error, so this goes through the same
// credential masking every other upstream error body does — not just the UUID
// redaction and the length cap.
func TestClassifyProbeError_MasksTheProvidersMessage(t *testing.T) {
	const apiKey = "sk-live-abc123def456ghi789"
	msg := "invalid api key " + apiKey + " for tenant 793ac38b-0211-43e6-baa7-aa7054c39931"
	re, charged := classifyProbeError(&upstreamFrameError{msg: msg}, "prov-A", newCredentialMasker(apiKey), false, time.Second, 30*time.Second, 60*time.Second, 1)

	if !charged {
		t.Error("an error envelope is always the provider's fault and must be charged")
	}
	if re.Kind != KindProviderError {
		t.Errorf("kind = %s, want %s", re.Kind, KindProviderError)
	}
	if strings.Contains(re.Underlying, apiKey) {
		t.Errorf("underlying = %q, still carries the provider credential", re.Underlying)
	}
	if strings.Contains(re.Underlying, "793ac38b") {
		t.Errorf("underlying = %q, want the UUID redacted", re.Underlying)
	}
}

// A client that hangs up cannot excuse an error envelope: the provider had
// already answered with a failure, so the charge does not depend on the
// downstream connection the way a zero-token stall does.
func TestClassifyProbeError_ChargesEvenWhenTheClientIsGone(t *testing.T) {
	_, charged := classifyProbeError(&upstreamFrameError{msg: "boom"}, "prov-A", newCredentialMasker("sk-x"), true, time.Millisecond, 30*time.Second, 60*time.Second, 1)
	if !charged {
		t.Error("an error envelope must be charged to the provider even when the client is gone")
	}
	// The contrast that makes the case above meaningful: a zero-token stall
	// with a fast client close is NOT charged.
	if _, chargedStall := classifyProbeError(errors.New("TTFT timeout"), "prov-A", newCredentialMasker("sk-x"), true, time.Millisecond, 30*time.Second, 60*time.Second, 1); chargedStall {
		t.Error("a fast client cancel with zero tokens must still not be charged")
	}
}

// A provider is free to quote the api key back inside its own error, and this
// branch made that text travel further than it used to: into the probe's
// warning, the sequential dispatcher's "TTFT probe failed" line and the hedged
// path's breaker warning.
//
// Masking reqError.Underlying is not sufficient on its own — the call sites
// were logging the RAW probeErr beside it — and key-SHAPE masking is not
// sufficient either, since an operator's credential need not look like a key.
// Nothing may carry the configured credential verbatim.
func TestProbeErrorFrame_NeverLogsTheProviderCredential(t *testing.T) {
	// Deliberately not key-shaped: no sk- prefix, no great length. A shape
	// heuristic passes this straight through.
	const apiKey = "hunter2-corp"
	frame := "data: {\"error\":{\"message\":\"auth failed for " + apiKey + "\"}}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, frame)
	}))
	defer srv.Close()

	for _, tc := range []struct {
		name string
		run  func(t *testing.T, h *Handler, st *requestState, cand modelCandidate)
	}{
		{"sequential", func(_ *testing.T, h *Handler, st *requestState, cand modelCandidate) {
			resp, err := srv.Client().Get(srv.URL) //nolint:noctx // test server, no context needed
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			h.dispatchStreaming(w, req, st, cand, resp, 0, 10, "failover_timeout")
		}},
		{"hedged", func(_ *testing.T, h *Handler, st *requestState, cand modelCandidate) {
			res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
			if res.resp != nil {
				_ = res.resp.Body.Close()
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandler(h)
			logs := captureProxyLogs(t)

			st, cand := probeStateForServer(srv.URL)
			st.circuitBreakerEnabled = true
			cand.apiKey = apiKey
			tc.run(t, h, st, cand)

			for _, r := range logs.all() {
				for k, v := range r.attrs {
					if strings.Contains(v, apiKey) {
						t.Errorf("log %q attr %s = %q leaks the provider credential", r.msg, k, v)
					}
				}
				if strings.Contains(r.msg, apiKey) {
					t.Errorf("log message %q leaks the provider credential", r.msg)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// An empty completion is as good as an error: it must not win a hedged race
// either. A provider whose FIRST data frame is the [DONE] sentinel produced no
// chunks at all, and letting it win cancels every healthy rival still in flight
// and hands the caller nothing. Same mechanism as the error frame above, same
// verdict. Decided 2026-08-28.
// ---------------------------------------------------------------------------

const emptyStreamSSE = "data: [DONE]\n\n"

func TestProbeFirstToken_ImmediateDoneIsNotAToken(t *testing.T) {
	h := &Handler{}

	if _, ttft, err := h.probeFirstToken(context.Background(), makeSSEBody(t, emptyStreamSSE), 5*time.Second, time.Now()); err == nil {
		t.Fatalf("a stream that ends at its first frame must fail the probe, got a token at ttft=%.1f", ttft)
	}
}

// The guard stays narrow. Only a BARE first [DONE] means "no chunks at all" —
// a provider that sends any real frame first and then finishes has answered,
// even if the answer is empty, and must keep winning.
func TestProbeFirstToken_DoneAfterAFrameStillWins(t *testing.T) {
	h := &Handler{}
	for name, body := range map[string]string{
		"role delta then done":    "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" + emptyStreamSSE,
		"empty choices then done": "data: {\"choices\":[]}\n\n" + emptyStreamSSE,
		"keepalive then frame":    ": ping\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" + emptyStreamSSE,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := h.probeFirstToken(context.Background(), makeSSEBody(t, body), 5*time.Second, time.Now()); err != nil {
				t.Fatalf("a stream that produced a frame must win the probe, got %v", err)
			}
		})
	}
}

// The race, end to end: a provider that finishes instantly with nothing must
// lose to one that takes longer and actually answers.
func TestRunHedgedStreaming_HealthyCandidateBeatsAFasterEmptyStream(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, emptyStreamSSE)
	}))
	defer empty.Close()

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
		liveCandidate("empty", empty.URL),
		liveCandidate("healthy", healthy.URL),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.runHedgedStreaming(w, req, st, cands, h.probeStreamingCandidate)

	if body := w.Body.String(); !strings.Contains(body, `"content":"hi"`) {
		t.Fatalf("the caller must receive the healthy provider's token, got: %s", body)
	}
	if logData.providerName != "healthy" {
		t.Errorf("winner = %q, want %q: an empty stream must not win the race", logData.providerName, "healthy")
	}
}

// An empty stream loses the race exactly as an error frame does, but is NOT
// charged to the breaker. Whether a model emits anything depends on the prompt,
// which the caller controls, so charging it would let one virtual key blackhole
// a healthy provider for every tenant.
func TestProbeStreamingCandidate_EmptyStreamLosesButIsNotCharged(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	withBreakerThresholdOne(t, h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, emptyStreamSSE)
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.circuitBreakerEnabled = true

	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
	if res.resp != nil {
		_ = res.resp.Body.Close()
	}
	if res.won {
		t.Fatal("a stream that produced no content must not win the race")
	}
	if res.reqErr.Kind != KindProviderError {
		t.Errorf("kind = %s, want %s", res.reqErr.Kind, KindProviderError)
	}
	// Threshold is 1 here, so a single charge would show immediately.
	if got := h.circuitBreaker.GetState(cand.provider.ID); got == failover.StateOpen {
		t.Error("an empty stream must not break the circuit: the prompt, not the provider, decides whether a model emits anything")
	}
}

// The empty-stream guard has to hold for every spelling of "carries nothing",
// not just the bare [DONE]. An empty data field won the probe while the stream
// reader counted zero chunks downstream — the same empty-stream bug reached by
// a different route, and the reason the two now share one classifier.
func TestProbeFirstToken_EmptyDataFieldIsNotAToken(t *testing.T) {
	h := &Handler{}
	for name, body := range map[string]string{
		"bare data colon":     "data:\n\n" + emptyStreamSSE,
		"data colon space":    "data: \n\n" + emptyStreamSSE,
		"data colon spaces":   "data:    \n\n" + emptyStreamSSE,
		"empty then terminat": "data:\n\ndata:\n\n" + emptyStreamSSE,
	} {
		t.Run(name, func(t *testing.T) {
			_, ttft, err := h.probeFirstToken(context.Background(), makeSSEBody(t, body), 5*time.Second, time.Now())
			if err == nil {
				t.Fatalf("an empty data field carries no token; probe must not win at ttft=%.3f", ttft)
			}
		})
	}
}

// An empty field is SKIPPED, not refused: a real frame after it still wins, the
// way a keepalive comment does. Refusing outright would fail over a stream that
// went on to answer perfectly well.
func TestProbeFirstToken_EmptyDataFieldIsSkippedNotFatal(t *testing.T) {
	h := &Handler{}
	body := "data:\n\ndata: \n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

	if _, ttft, err := h.probeFirstToken(context.Background(), makeSSEBody(t, body), 5*time.Second, time.Now()); err != nil {
		t.Fatalf("a real frame after empty fields must still win, got %v", err)
	} else if ttft <= 0 {
		t.Errorf("ttft = %f, want > 0", ttft)
	}
}

// classifyProbeFrame is unit-tested directly because the scanner-error recovery
// branch calls it too, and that branch is defence-in-depth for a race the
// existing tests document as impossible to trigger deterministically. Covering
// the decision covers both callers, which is the whole reason it is shared.
func TestClassifyProbeFrame(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    probeFrame
		wantMsg string
	}{
		{"real token", `{"choices":[{"delta":{"content":"hi"}}]}`, probeFrameToken, ""},
		{"role only", `{"choices":[{"delta":{"role":"assistant"}}]}`, probeFrameToken, ""},
		{"empty choices", `{"choices":[]}`, probeFrameToken, ""},
		{"anthropic message_start", `{"type":"message_start"}`, probeFrameToken, ""},
		{"unparseable is still a token", `{not json`, probeFrameToken, ""},
		{"terminator", "[DONE]", probeFrameEmptyStream, ""},
		{"empty field", "", probeFrameNotAToken, ""},
		{"error envelope", `{"error":{"message":"boom"}}`, probeFrameError, "boom"},
		{"ollama bare string", `{"error":"model not found"}`, probeFrameError, "model not found"},
		{"empty error member", `{"error":{}}`, probeFrameToken, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := classifyProbeFrame(tc.content)
			if got != tc.want {
				t.Errorf("verdict = %d, want %d", got, tc.want)
			}
			if msg != tc.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tc.wantMsg)
			}
		})
	}
}

// recoverProbeFrame is the scanner-error recovery branch's decision, lifted out
// so it can be tested. The branch itself needs the watchdog to close the body in
// the same instant the scanner yields a line, which no test can arrange
// deterministically — see TestProbeFirstToken_ScannerErrorRecovery_PipeRace.
// Before the extraction, every verdict that branch could reach was untested and
// a revert of it left the whole package green.
func TestRecoverProbeFrame(t *testing.T) {
	tests := []struct {
		name    string
		buf     string
		want    probeFrame
		wantMsg string
		found   bool
	}{
		{"a real token", "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n", probeFrameToken, "", true},
		{"terminator only", "data: [DONE]\n", probeFrameEmptyStream, "", true},
		{"error envelope", "data: {\"error\":{\"message\":\"boom\"}}\n", probeFrameError, "boom", true},
		{"empty field then token", "data:\ndata: {\"choices\":[]}\n", probeFrameToken, "", true},
		{"empty field then terminator", "data:\ndata: [DONE]\n", probeFrameEmptyStream, "", true},
		{"keepalive then token", ": ping\ndata: {\"choices\":[]}\n", probeFrameToken, "", true},
		// A mid-line network fragment has no trailing newline in the buffer and
		// must not be mistaken for a complete frame.
		{"partial line rejected", "data: {\"choices\":[{\"delta\":", probeFrameNotAToken, "", false},
		{"nothing usable", ": ping\nevent: message\n", probeFrameNotAToken, "", false},
		{"empty buffer", "", probeFrameNotAToken, "", false},
		// The first meaningful frame decides, not the last.
		{"first frame wins", "data: [DONE]\ndata: {\"choices\":[]}\n", probeFrameEmptyStream, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verdict, msg, found := recoverProbeFrame(tc.buf)
			if found != tc.found {
				t.Fatalf("found = %v, want %v", found, tc.found)
			}
			if verdict != tc.want {
				t.Errorf("verdict = %d, want %d", verdict, tc.want)
			}
			if msg != tc.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tc.wantMsg)
			}
		})
	}
}

// recoverFirstToken is the whole scanner-error recovery branch. Tested here
// because the branch cannot be reached from a test: it needs the watchdog to
// close the body in the same instant the scanner yields a line.
func TestRecoverFirstToken(t *testing.T) {
	scanErr := errors.New("body closed by timeout goroutine")

	t.Run("a recovered token is returned with its buffer", func(t *testing.T) {
		buf := bytes.NewBufferString("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n")
		probeBuf, ttft, err, recovered := recoverFirstToken(buf, time.Now().Add(-time.Millisecond), scanErr)
		if !recovered {
			t.Fatal("expected the frame to be recovered")
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probeBuf != buf {
			t.Error("expected the original buffer back for replay")
		}
		if ttft <= 0 {
			t.Errorf("ttft = %f, want > 0", ttft)
		}
	})

	t.Run("a recovered terminator is an empty stream", func(t *testing.T) {
		_, _, err, recovered := recoverFirstToken(bytes.NewBufferString("data: [DONE]\n"), time.Now(), scanErr)
		if !recovered {
			t.Fatal("expected the frame to be recovered")
		}
		var empty *emptyStreamError
		if !errors.As(err, &empty) {
			t.Errorf("err = %v, want an emptyStreamError", err)
		}
	})

	t.Run("a recovered error envelope carries the provider message", func(t *testing.T) {
		_, _, err, recovered := recoverFirstToken(bytes.NewBufferString("data: {\"error\":{\"message\":\"boom\"}}\n"), time.Now(), scanErr)
		if !recovered {
			t.Fatal("expected the frame to be recovered")
		}
		var frame *upstreamFrameError
		if !errors.As(err, &frame) {
			t.Fatalf("err = %v, want an upstreamFrameError", err)
		}
		if frame.msg != "boom" {
			t.Errorf("msg = %q, want %q", frame.msg, "boom")
		}
	})

	t.Run("nothing usable is not recovered", func(t *testing.T) {
		for name, b := range map[string]string{
			"only a keepalive": ": ping\n",
			"partial line":     "data: {\"choices\":[{\"delta\":",
			"empty fields":     "data:\ndata: \n",
			"empty buffer":     "",
		} {
			t.Run(name, func(t *testing.T) {
				probeBuf, ttft, err, recovered := recoverFirstToken(bytes.NewBufferString(b), time.Now(), scanErr)
				if recovered {
					t.Errorf("recovered = true (buf=%v ttft=%f err=%v), want the caller to fall through", probeBuf, ttft, err)
				}
			})
		}
	})
}
