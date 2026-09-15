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
		"reasoning delta":   "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"hmm\"}}]}\n\n",
		"tool call delta":   "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"f\"}}]}}]}\n\n",
		"explicit null err": "data: {\"error\":null,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n",
		// An empty boilerplate error member alongside a real token: the frame
		// delivered content, so failing it over would throw away an answer.
		"empty err + content": "data: {\"error\":{},\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n",
		// The native Anthropic passthrough is probed like any other stream, and
		// its first content block must keep winning.
		"anthropic content_block_delta": "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"unparseable json":              "data: {not json at all\n\n",
		"word error inside":             "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"the error was mine\"}}]}\n\n",
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
	if got := h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID); got != failover.StateOpen {
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
// A stream that commits and THEN delivers nothing is still a provider failure.
// The probe guard above only covers the first frame; an empty answer (a role
// opener and then [DONE]) passes the probe as a completion, is forwarded with
// its own finish reason, and arrives once the rivals are already cancelled, so
// the breaker is the only thing left that can keep the next request away.
// ---------------------------------------------------------------------------

func TestDispatchStreaming_EmptyAnswerAfterCommitOpensTheCircuit(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Two things about this test are load-bearing.
	//
	// It goes through dispatchStreaming, NOT straight to handleStreamingResponse,
	// so the TTFT probe really runs: the charge being pinned depends on what the
	// probe tells the breaker, and a test that skips the probe cannot see it.
	//
	// And it runs at the PRODUCTION default threshold of 5. A probe success that
	// zeroed consecutiveFails on every request would let each failure bring the
	// count back only to 1, which a threshold of 1 cannot tell from working.
	providerID := uuid.New()
	// Hoisted so the assertion below can name the very model the charges were
	// routed to, rather than a second copy of the id that could drift from it.
	cand := modelCandidate{
		model:    &model.Model{ModelID: "test-model"},
		provider: &provider.Provider{ID: providerID, Name: "empty-answer-provider"},
		apiKey:   "sk-test",
	}
	const attempts = 5
	for i := range attempts {
		// A role opener and then the terminator: the probe commits it as an
		// empty answer, and the caller receives exactly that.
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" + emptyStreamSSE)),
		}
		// providerID is deliberately left off logData: the request-log row has a
		// foreign key to providers and this provider exists only in the breaker.
		logData := streamingLog()
		logData.providerName = "empty-answer-provider"
		h.insertRequestLogAsync(logData)

		st := &requestState{
			startTime:             time.Now(),
			reqModel:              "test-model",
			isStreaming:           true,
			circuitBreakerEnabled: true,
			logData:               logData,
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		if got := h.dispatchStreaming(w, req, st, cand, resp, 1, 10, "failover_timeout"); got != outcomeServed {
			t.Fatalf("attempt %d: outcome = %v, want served (an empty answer commits, it does not fail over)", i, got)
		}
		if body := w.Body.String(); !strings.Contains(body, "\"role\":\"assistant\"") || !strings.Contains(body, "[DONE]") {
			t.Fatalf("attempt %d: the caller must receive the empty answer as the provider sent it, got %q", i, body)
		}
	}

	if got := h.circuitBreaker.GetState(providerID, cand.model.ModelID); got != failover.StateOpen {
		t.Errorf("circuit = %s after %d empty answers, want open", got, attempts)
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
	if got := h.circuitBreaker.GetState(providerID, ""); got == failover.StateOpen {
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

	if got := h.circuitBreaker.GetState(providerID, ""); got == failover.StateOpen {
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
	withBreakerThreshold(t, h, "1")
}

// withBreakerThreshold sets the consecutive-failure threshold for one test. A
// threshold above one is what makes a recorded SUCCESS observable: it is the
// only thing that resets the counter, so whether a later failure opens the
// circuit reports whether the stream credited the provider.
func withBreakerThreshold(t *testing.T, h *Handler, threshold string) {
	t.Helper()
	if err := h.settingsRepo.Set(context.Background(), "circuit_breaker_threshold", threshold); err != nil {
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
// It is driven from errorFrameCorpus rather than a table of its own. The whole
// point of the shared emptiness rule is that the probe and the stream observer
// cannot disagree about a shape, and two hand-maintained tables is precisely
// how they would: a shape added to one and not the other reintroduces the drift
// by the back door. Only payloads that are not JSON objects at all are listed
// here, because the corpus is a corpus of frames.
func TestErrorEnvelopeMessage(t *testing.T) {
	for _, tc := range errorFrameCorpus {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := errorEnvelopeMessage(tc.payload)
			if ok != tc.isError {
				t.Fatalf("ok = %v, want %v (msg %q)", ok, tc.isError, msg)
			}
			if msg != tc.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tc.wantMsg)
			}
		})
	}

	// A frame with no object to hold a member at all.
	for _, frame := range []string{`{not json`, `[1,2,3]`, `"a string"`, ``} {
		t.Run("not an object: "+frame, func(t *testing.T) {
			if msg, ok := errorEnvelopeMessage(frame); ok {
				t.Errorf("ok = true (msg %q), want false", msg)
			}
		})
	}
}

// The Anthropic wrapper is the same member one level in, and the probe reads it
// through the same rule. It is not in the corpus because the observer sees this
// shape through the P1-C branch instead, keyed on the preceding "event: error"
// line rather than on the member.
func TestErrorEnvelopeMessage_AnthropicWrapper(t *testing.T) {
	msg, ok := errorEnvelopeMessage(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`)
	if !ok || msg != "overloaded" {
		t.Errorf("got (%q, %v), want (overloaded, true)", msg, ok)
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
	re, charged := classifyProbeError(&upstreamFrameError{msg: msg}, "prov-A", newCredentialMasker(apiKey), nil, false, time.Second, 30*time.Second, 60*time.Second, 1)

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
	_, charged := classifyProbeError(&upstreamFrameError{msg: "boom"}, "prov-A", newCredentialMasker("sk-x"), nil, true, time.Millisecond, 30*time.Second, 60*time.Second, 1)
	if !charged {
		t.Error("an error envelope must be charged to the provider even when the client is gone")
	}
	// The contrast that makes the case above meaningful: a zero-token stall
	// with a fast client close is NOT charged.
	if _, chargedStall := classifyProbeError(errors.New("TTFT timeout"), "prov-A", newCredentialMasker("sk-x"), nil, true, time.Millisecond, 30*time.Second, 60*time.Second, 1); chargedStall {
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
			resp, err := srv.Client().Get(srv.URL)
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

// A frame that carries no output is not a first token, but a stream that ENDS
// behind one is an empty answer: the provider completed having said nothing,
// which commits and is forwarded whole (every byte is in the replay buffer)
// with its own finish reason, and the finalizer charges it once. Only a
// stream that stays open behind such frames fails the probe, by timeout.
func TestProbeFirstToken_EmptyAnswerCommits(t *testing.T) {
	h := &Handler{}
	for name, body := range map[string]string{
		"role delta then done":     "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" + emptyStreamSSE,
		"empty choices then done":  "data: {\"choices\":[]}\n\n" + emptyStreamSSE,
		"usage-only then done":     "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3}}\n\n" + emptyStreamSSE,
		"finish only then done":    "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"content_filter\"}]}\n\n" + emptyStreamSSE,
		"role delta then body end": "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n",
		// The native Anthropic stream ends on message_stop with no [DONE].
		"anthropic empty answer": "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			probeBuf, ttft, err := h.probeFirstToken(context.Background(), makeSSEBody(t, body), 5*time.Second, time.Now())
			if err != nil {
				t.Fatalf("an empty answer must commit, got %v", err)
			}
			if ttft <= 0 {
				t.Errorf("ttft = %.1f, want > 0", ttft)
			}
			if got := bufString(probeBuf); got != body {
				t.Errorf("replay buffer must hold the whole stream, got %q", got)
			}
		})
	}
}

// The guard stays narrow: a token behind frames carrying no output still wins,
// and the bytes the probe read ahead of it (keepalives, the role opener) are
// all in the replay buffer so the client loses nothing.
func TestProbeFirstToken_TokenBehindNonOutputFramesWins(t *testing.T) {
	h := &Handler{}
	for name, body := range map[string]string{
		"keepalive then frame":   ": ping\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" + emptyStreamSSE,
		"role opener then frame": "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" + emptyStreamSSE,
	} {
		t.Run(name, func(t *testing.T) {
			probeBuf, _, err := h.probeFirstToken(context.Background(), makeSSEBody(t, body), 5*time.Second, time.Now())
			if err != nil {
				t.Fatalf("a stream that produced a frame must win the probe, got %v", err)
			}
			if got := bufString(probeBuf); !strings.HasPrefix(body, got) || !strings.Contains(got, "\"content\":\"hi\"") {
				t.Errorf("replay buffer must hold every byte up to the first token, got %q", got)
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

// An empty stream loses the race exactly as an error frame does, and is charged
// exactly as one too: a zero-token answer is not a valid answer.
func TestProbeStreamingCandidate_EmptyStreamLosesAndIsCharged(t *testing.T) {
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
	// Threshold is 1 here, so a single charge shows immediately.
	if got := h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID); got != failover.StateOpen {
		t.Errorf("circuit = %s, want open: a provider that produced nothing is charged", got)
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
		{"role only", `{"choices":[{"delta":{"role":"assistant"}}]}`, probeFrameNotAToken, ""},
		{"empty choices", `{"choices":[]}`, probeFrameNotAToken, ""},
		{"anthropic message_start", `{"type":"message_start"}`, probeFrameNotAToken, ""},
		{"anthropic message_stop is the terminator", `{"type":"message_stop"}`, probeFrameEmptyStream, ""},
		{"anthropic content_block_delta", `{"type":"content_block_delta","delta":{"text":"hi"}}`, probeFrameToken, ""},
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

// frameCarriesOutput is the probe's reading of "did the model say something"
// (the stall watchdog deliberately counts bytes, not output: an Anthropic
// keepalive across a tool pause must keep a committed stream alive), so every
// shape it must accept and every shape it must reject is pinned here.
func TestFrameCarriesOutput(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{"content", `{"choices":[{"delta":{"content":"hi"}}]}`, true},
		{"content as parts", `{"choices":[{"delta":{"content":[{"type":"text","text":"hi"}]}}]}`, true},
		{"reasoning_content", `{"choices":[{"delta":{"reasoning_content":"hmm"}}]}`, true},
		{"reasoning (ollama)", `{"choices":[{"delta":{"reasoning":"hmm"}}]}`, true},
		{"reasoning_details", `{"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":"hmm"}]}}]}`, true},
		{"tool_calls", `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":""}}]}}]}`, true},
		{"function_call", `{"choices":[{"delta":{"function_call":{"name":"f"}}}]}`, true},
		{"images", `{"choices":[{"delta":{"images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}}]}`, true},
		{"refusal", `{"choices":[{"delta":{"refusal":"no"}}]}`, true},
		{"second choice only", `{"choices":[{"delta":{"role":"assistant"}},{"delta":{"content":"hi"}}]}`, true},
		{"legacy completion text", `{"choices":[{"text":"hi","index":0}]}`, true},
		{"whitespace token", `{"choices":[{"delta":{"content":"\n"}}]}`, true},
		{"tool call header without a name yet", `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function"}]}}]}`, false},
		{"content-as-parts opener", `{"choices":[{"delta":{"content":[{"type":"text","text":""}]}}]}`, false},
		{"role opener", `{"choices":[{"delta":{"role":"assistant","content":""}}]}`, false},
		{"empty markers", `{"choices":[{"delta":{"content":"","reasoning":"","reasoning_details":[],"tool_calls":null}}]}`, false},
		{"empty choices", `{"choices":[]}`, false},
		{"usage only", `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":0}}`, false},
		{"finish only", `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, false},
		{"null delta", `{"choices":[{"delta":null}]}`, false},
		{"anthropic empty text block opener", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, false},
		{"anthropic tool_use block opener", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`, true},
		{"anthropic content_block_stop", `{"type":"content_block_stop","index":0}`, false},
		{"anthropic content_block_delta", `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}`, true},
		{"anthropic message_start", `{"type":"message_start","message":{"usage":{"input_tokens":3}}}`, false},
		{"anthropic ping", `{"type":"ping"}`, false},
		{"anthropic message_delta", `{"type":"message_delta","usage":{"output_tokens":3}}`, false},
		{"anthropic message_stop", `{"type":"message_stop"}`, false},
		{"usage only without a choices member", `{"id":"x","usage":{"prompt_tokens":3,"completion_tokens":0}}`, false},
		{"metadata-only chunk opener", `{"id":"x","object":"chat.completion.chunk","model":"m","created":1}`, false},
		{"null usage and nothing else", `{"usage":null}`, false},
		{"unknown dialect beside a usage member", `{"usage":{"prompt_tokens":3},"result":{"text":"hi"}}`, true},
		{"anthropic redacted thinking opener", `{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"EmwKAhgB"}}`, true},
		{"terminal chunk without a delta member", `{"choices":[{"index":0,"finish_reason":"stop","logprobs":null}]}`, false},
		{"anthropic relay text on the block opener", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`, true},
		{"usage only with null choices", `{"choices":null,"usage":{"prompt_tokens":3}}`, false},
		// Shapes this gateway does not model are never cut for being unknown.
		{"not json", `{not json`, true},
		{"no choices member, unknown object", `{"id":"x","result":{"text":"hi"}}`, true},
		{"unknown event type", `{"type":"response.output_text.delta","delta":"hi"}`, true},
		{"choice without a delta", `{"choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}]}`, true},
		{"delta that is not an object", `{"choices":[{"index":0,"delta":"hi"}]}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := frameCarriesOutput(tc.payload); got != tc.want {
				t.Errorf("frameCarriesOutput(%s) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}

// Every OpenAI-shaped output the probe commits a stream on must also count as
// delivery once the stream is committed, or a stream whose whole answer is
// that output is charged as a completion with nothing in it.
func TestStreamDelivery_CountsEveryOutputTheProbeAccepts(t *testing.T) {
	for name, payload := range map[string]string{
		"content":       `{"choices":[{"delta":{"content":"hi"}}]}`,
		"reasoning":     `{"choices":[{"delta":{"reasoning":"hmm"}}]}`,
		"tool call":     `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":""}}]}}]}`,
		"function_call": `{"choices":[{"delta":{"function_call":{"name":"f","arguments":"{}"}}}]}`,
		"refusal":       `{"choices":[{"delta":{"refusal":"no"}}]}`,
		"audio":         `{"choices":[{"delta":{"audio":{"id":"a","data":"UklGRg==","transcript":"hi"}}}]}`,
		"legacy text":   `{"choices":[{"text":"hi","index":0}]}`,
		"image":         `{"choices":[{"delta":{"images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if !frameCarriesOutput(payload) {
				t.Fatalf("test assumption broken: the probe must accept %s", payload)
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			st := &streamState{}
			st.observeDataChunk(chunk, false, 1, &requestLogData{})
			if !streamDeliveredOutput(st) {
				t.Errorf("the probe commits on %s but the finalizer would charge it as empty", payload)
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
		{"empty field then token", "data:\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n", probeFrameToken, "", true},
		{"role opener then token", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n", probeFrameToken, "", true},
		{"role opener only", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n", probeFrameNotAToken, "", false},
		// An empty answer: the terminator behind a frame commits, as in the main loop.
		{"role opener then terminator", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\ndata: [DONE]\n", probeFrameToken, "", true},
		{"empty field then terminator", "data:\ndata: [DONE]\n", probeFrameEmptyStream, "", true},
		{"keepalive then token", ": ping\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n", probeFrameToken, "", true},
		// A mid-line network fragment has no trailing newline in the buffer and
		// must not be mistaken for a complete frame.
		{"partial line rejected", "data: {\"choices\":[{\"delta\":", probeFrameNotAToken, "", false},
		{"nothing usable", ": ping\nevent: message\n", probeFrameNotAToken, "", false},
		{"empty buffer", "", probeFrameNotAToken, "", false},
		// The first meaningful frame decides, not the last.
		{"first frame wins", "data: [DONE]\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n", probeFrameEmptyStream, "", true},
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

// ---------------------------------------------------------------------------
// A completely empty response counts against the provider. A zero-token answer
// is not a valid one in almost any real use, and a caller deliberately coercing
// a model into silence is not a case worth protecting a provider from. Decided
// 2026-08-28, reversing the narrower call made when the empty-stream guard
// first shipped.
//
// Two paths produce a completely empty response and both charge:
//   - the stream never produced a chunk at all (caught by the probe)
//   - the stream produced frames but delivered no output (caught by the
//     finalizer, after the provider was already committed to)
// ---------------------------------------------------------------------------

func TestClassifyProbeError_ChargesAnEmptyStream(t *testing.T) {
	re, charged := classifyProbeError(&emptyStreamError{}, "prov-A", newCredentialMasker("sk-x"), nil, false, time.Second, 30*time.Second, 60*time.Second, 1)
	if !charged {
		t.Error("a stream that produced nothing must be charged to the provider")
	}
	if re.Kind != KindProviderError {
		t.Errorf("kind = %s, want %s", re.Kind, KindProviderError)
	}
	// A client hanging up cannot excuse it either: the provider had already
	// finished saying nothing.
	if _, chargedGone := classifyProbeError(&emptyStreamError{}, "prov-A", newCredentialMasker("sk-x"), nil, true, time.Millisecond, 30*time.Second, 60*time.Second, 1); !chargedGone {
		t.Error("an empty stream must be charged even when the client is gone")
	}
}

// The finalizer half: a stream that got past the probe, completed cleanly, and
// still handed the caller nothing. Previously recorded as a SUCCESS, which
// actively cleared any accumulated failures for that provider.
func TestJudgeStreamForBreaker_CompletedButEmptyIsCharged(t *testing.T) {
	st := &streamState{sawDone: true}
	logData := &requestLogData{}
	v := judgeStreamForBreaker(st, logData, "", true)
	if v.failureReason == "" {
		t.Error("a stream that completed having delivered nothing must be charged")
	}
	if v.success {
		t.Error("an empty stream must not be recorded as a success")
	}
}

// The guard that keeps the above from swallowing every ordinary stream: any
// evidence the caller actually received output means success, and the three
// signals are checked because no single one is available on every path.
func TestJudgeStreamForBreaker_DeliveredStreamsStillSucceed(t *testing.T) {
	for name, tc := range map[string]struct {
		st      *streamState
		logData *requestLogData
	}{
		"content seen":    {&streamState{sawDone: true, sawContent: true}, &requestLogData{}},
		"bytes delivered": {&streamState{sawDone: true, deliveredBytes: 42}, &requestLogData{}},
		"usage reported":  {&streamState{sawDone: true, completionTokens: 7}, &requestLogData{}},
		// The native Anthropic path never runs observeDataChunk, so sawContent
		// is unreachable there; deliveredBytes (from the event's TextBytes) is
		// what proves it answered.
		"anthropic delivered": {&streamState{sawMessageStop: true, deliveredBytes: 120}, &requestLogData{deliveredContent: true}},
	} {
		t.Run(name, func(t *testing.T) {
			v := judgeStreamForBreaker(tc.st, tc.logData, "", true)
			if v.failureReason != "" {
				t.Errorf("charged %q, want a success", v.failureReason)
			}
			if !v.success {
				t.Error("a stream that delivered output must be recorded as a success")
			}
		})
	}
}

// A client that hangs up before anything arrives is not the provider's doing,
// and must not charge it under the new rule either.
func TestJudgeStreamForBreaker_EmptyOnClientHangupIsNotCharged(t *testing.T) {
	for name, st := range map[string]*streamState{
		"client hung up":     {clientDisconnected: true},
		"gateway restarting": {interrupted: true},
	} {
		t.Run(name, func(t *testing.T) {
			v := judgeStreamForBreaker(st, &requestLogData{}, "", true)
			if v.failureReason != "" {
				t.Errorf("charged %q, want no charge", v.failureReason)
			}
		})
	}
}

// End to end at the production default threshold, through the real probe: five
// providers-that-say-nothing take the provider out of rotation.
func TestDispatchStreaming_EmptyStreamsOpenTheCircuit(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	providerID := uuid.New()
	// Hoisted so the assertion below reads the model the charges were routed to.
	cand := modelCandidate{
		model:    &model.Model{ModelID: "test-model"},
		provider: &provider.Provider{ID: providerID, Name: "empty-provider"},
		apiKey:   "sk-test",
	}
	const attempts = 5
	for i := range attempts {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(emptyStreamSSE)),
		}
		logData := streamingLog()
		logData.providerName = "empty-provider"
		h.insertRequestLogAsync(logData)

		st := &requestState{
			startTime:             time.Now(),
			reqModel:              "test-model",
			isStreaming:           true,
			circuitBreakerEnabled: true,
			logData:               logData,
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		if got := h.dispatchStreaming(w, req, st, cand, resp, 1, 10, "failover_timeout"); got != outcomeFailover {
			t.Fatalf("attempt %d: outcome = %v, want failover", i, got)
		}
	}

	if got := h.circuitBreaker.GetState(providerID, cand.model.ModelID); got != failover.StateOpen {
		t.Errorf("circuit = %s after %d empty streams, want open", got, attempts)
	}
}

// A tool call IS output. A completion whose only product is a function call has
// answered — that is the whole point of tool use — and must never be charged as
// an empty response, or a provider answering correctly would be taken out of
// rotation for every tenant after five such requests.
//
// Driven through the real streaming pipeline rather than a hand-built
// streamState, because the thing under test is whether the pipeline's own
// accounting (observeDataChunk -> deliveredBytes) registers tool calls at all.
func TestHandleStreamingResponse_ToolCallOnlyIsNotAnEmptyResponse(t *testing.T) {
	streams := map[string]string{
		// Tool calls arrive incrementally: the name in one chunk, argument
		// fragments after it. No content delta appears anywhere.
		"tool call in fragments": "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"function\":{\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"function\":{\"arguments\":\"\\\"Prague\\\"}\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n",
		// Reasoning with no visible content is output too.
		"reasoning only": "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking about it\"}}]}\n\ndata: [DONE]\n\n",
	}
	for name, body := range streams {
		t.Run(name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandlerIntegration(h)
			withBreakerThresholdOne(t, h)

			providerID := uuid.New()
			logData := streamingLog()
			logData.providerName = "tool-provider"
			h.insertRequestLogAsync(logData)

			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
				responseHeaderMs: 10,
				providerID:       providerID,
				providerName:     "tool-provider",
				circuitBreakerOn: true,
				vkHash:           "test-hash",
				attempt:          1,
			})

			if logData.state != "completed" {
				t.Errorf("state = %q, want completed", logData.state)
			}
			// Threshold is 1, so a single stray charge shows immediately.
			if got := h.circuitBreaker.GetState(providerID, ""); got == failover.StateOpen {
				t.Error("a completion whose only output is a tool call or reasoning is not empty and must not break the circuit")
			}
		})
	}
}

// The counterpart: a stream carrying frames that deliver nothing at all really
// is empty, and is charged. Without this the guard above could be satisfied by
// simply never charging.
func TestHandleStreamingResponse_FramesWithNoOutputAreCharged(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	logData := streamingLog()
	logData.providerName = "silent-provider"
	h.insertRequestLogAsync(logData)

	// A role delta and a finish reason: well-formed frames, zero output.
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       providerID,
		providerName:     "silent-provider",
		circuitBreakerOn: true,
		vkHash:           "test-hash",
		attempt:          1,
	})

	if got := h.circuitBreaker.GetState(providerID, ""); got != failover.StateOpen {
		t.Errorf("circuit = %s, want open: the caller received nothing", got)
	}
}

// message_stop is a TERMINATION signal, not a delivery one: it is present on
// every native stream that ends cleanly, including one that ended having
// produced nothing. Treating it as delivery let a completely empty
// /v1/messages response escape the charge entirely.
func TestJudgeStreamForBreaker_EmptyNativeStreamIsCharged(t *testing.T) {
	st := &streamState{sawMessageStop: true}
	// What finalizeStream derives for the retirement verdict, where
	// message_stop IS allowed to stand in for "the model answered". The breaker
	// verdict must not inherit that.
	logData := &requestLogData{deliveredContent: true}

	v := judgeStreamForBreaker(st, logData, "", true)
	if v.failureReason == "" {
		t.Error("a native stream that terminated having delivered nothing must be charged")
	}
	if v.success {
		t.Error("message_stop alone must not be recorded as a success")
	}
}

// A frame this gateway could not read is not evidence the provider sent nothing
// — it may have answered in a shape our types do not cover, whether that frame
// was dropped as broken bytes or forwarded verbatim. The contents are unknown,
// so with nothing else delivered the verdict is neither a charge nor a credit.
// A stream that DID deliver is credited regardless; see
// TestJudgeStreamForBreaker_UntypeableFrames.
func TestJudgeStreamForBreaker_UnparseableFramesWithholdTheVerdict(t *testing.T) {
	st := &streamState{sawDone: true, unparsedChunks: 1}
	v := judgeStreamForBreaker(st, &requestLogData{}, "", true)
	if v.failureReason != "" {
		t.Errorf("charged %q: emptiness cannot be pinned on the provider when our own parser dropped a frame", v.failureReason)
	}
	if v.success {
		t.Error("an unreadable stream is not evidence of health either")
	}
}

// The reasoning spellings the observers never saw. normalizeReasoningChunk
// rewrites "reasoning" and "reasoning_details" into reasoning_content for the
// client, but it runs AFTER the observers — so the caller received a real
// answer while the accounting saw nothing, and the provider was charged for it.
func TestHandleStreamingResponse_ReasoningSpellingsAreDelivery(t *testing.T) {
	for name, body := range map[string]string{
		"reasoning_content": "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\ndata: [DONE]\n\n",
		"reasoning":         "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"thinking hard\"}}]}\n\ndata: [DONE]\n\n",
		"reasoning_details": "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.text\",\"text\":\"thinking\"}]}}]}\n\ndata: [DONE]\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandlerIntegration(h)
			withBreakerThresholdOne(t, h)

			providerID := uuid.New()
			logData := streamingLog()
			logData.providerName = "reasoning-provider"
			h.insertRequestLogAsync(logData)

			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
				responseHeaderMs: 10,
				providerID:       providerID,
				providerName:     "reasoning-provider",
				circuitBreakerOn: true,
				vkHash:           "test-hash",
				attempt:          1,
			})

			if got := h.circuitBreaker.GetState(providerID, ""); got == failover.StateOpen {
				t.Errorf("a reasoning answer spelled %q is output and must not break the circuit", name)
			}
		})
	}
}

// A chunk this gateway's types cannot represent: the provider answered, we
// dropped the frame, and the provider must not be charged for our parser.
func TestHandleStreamingResponse_UnparseableChunkDoesNotCharge(t *testing.T) {
	for name, body := range map[string]string{
		"tool arguments as an object": "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"f\",\"arguments\":{\"city\":\"Prague\"}}}]}}]}\n\ndata: [DONE]\n\n",
		"content as parts":            "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}]}\n\ndata: [DONE]\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandlerIntegration(h)
			withBreakerThresholdOne(t, h)

			providerID := uuid.New()
			logData := streamingLog()
			logData.providerName = "wide-shape-provider"
			h.insertRequestLogAsync(logData)

			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
				responseHeaderMs: 10,
				providerID:       providerID,
				providerName:     "wide-shape-provider",
				circuitBreakerOn: true,
				vkHash:           "test-hash",
				attempt:          1,
			})

			if got := h.circuitBreaker.GetState(providerID, ""); got == failover.StateOpen {
				t.Error("a frame our own parser dropped must not be charged to the provider")
			}
		})
	}
}

// The stream verdict's CREDIT has to land on the circuit its charges land on.
// finalizeStream reads opts.model, filled from the candidate by whichever
// dispatch built the options; a credit under any other key leaves the real
// charge on the clock and the model opens on a count it never reached.
//
// Threshold 2, where an erased credit is visible: at 1 the first charge opens
// the circuit on its own. Charge, serve a complete stream, charge — the circuit
// must still be closed.
func TestDispatchStreaming_TheStreamCreditLandsOnTheServedModel(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThreshold(t, h, "2")

	providerID := uuid.New()
	cand := modelCandidate{
		model:    &model.Model{ModelID: "streaming-model"},
		provider: &provider.Provider{ID: providerID, Name: "streaming-provider"},
		apiKey:   "sk-test",
	}
	h.circuitBreaker.RecordFailure(providerID, cand.provider.Name, cand.model.ModelID, failover.Cause{})

	logData := streamingLog()
	logData.providerName = cand.provider.Name
	h.insertRequestLogAsync(logData)
	st := &requestState{
		startTime:             time.Now(),
		reqModel:              "streaming-model",
		isStreaming:           true,
		circuitBreakerEnabled: true,
		logData:               logData,
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")),
	}
	if got := h.dispatchStreaming(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody),
		st, cand, resp, 1, 10, "failover_timeout"); got != outcomeServed {
		t.Fatalf("outcome = %v, want served", got)
	}

	h.circuitBreaker.RecordFailure(providerID, cand.provider.Name, cand.model.ModelID, failover.Cause{})
	if got := h.circuitBreaker.GetState(providerID, cand.model.ModelID); got == failover.StateOpen {
		t.Errorf("circuit = %s, want closed: the completed stream credited a circuit other than the model it served", got)
	}
}

// The hedged winner builds its own streamOptions, so it carries its own copy of
// the model the stream verdict is keyed on. A winner that commits and then fails
// mid-stream must charge the model it was serving.
//
// The sequential dispatch's twin cannot cover this: the two option literals are
// separate lines, and the hedged one going empty leaves every hedged stream's
// verdict landing on a circuit nothing routes by.
func TestRunHedgedStreaming_TheWinnersVerdictLandsOnItsModel(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	withBreakerThresholdOne(t, h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// A valid opening frame so the TTFT probe passes and this candidate wins,
		// then the error the finalizer charges for.
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"+errorFrameSSE)
	}))
	defer srv.Close()

	st, logData := newHedgeState(25 * time.Millisecond)
	st.circuitBreakerEnabled = true
	st.reqModel = "orig-model"
	st.bodyBytes = []byte(`{"model":"orig-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	logData.endpointType = endpointTypeChat

	cand := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "hedged-model"},
		provider: &provider.Provider{ID: uuid.New(), Name: "hedge-winner", BaseURL: srv.URL},
	}

	h.runHedgedStreaming(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody),
		st, []modelCandidate{cand}, h.probeStreamingCandidate)

	if got := h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID); got != failover.StateOpen {
		t.Errorf("circuit = %s, want open: the hedged winner's stream verdict missed its own model", got)
	}
}
