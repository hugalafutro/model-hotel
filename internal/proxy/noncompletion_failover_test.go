package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/httpx"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// A 2xx that carries no completion must not end the candidate loop. The client
// is not committed to a provider until the answer is in hand, so an upstream
// that answers 200 with something this gateway cannot decode is failed over to
// the sibling instead of being turned into the gateway's own 502.

const siblingCompletion = `{"id":"chatcmpl-sibling","object":"chat.completion","created":1,"model":"shared-model",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"the sibling answered"},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`

// replayUpstream answers the first provider in the group with firstBody under
// firstStatus and every later one with a valid completion. The two providers
// are told apart by Host: buildReplayEnv gives them different base URLs and
// dials both to this one server.
func replayUpstream(t *testing.T, firstStatus int, firstBody string) *replayEnv {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Host, "one-slot") {
			w.WriteHeader(firstStatus)
			_, _ = io.WriteString(w, firstBody)
			return
		}
		_, _ = io.WriteString(w, siblingCompletion)
	}))
	t.Cleanup(upstream.Close)
	return buildReplayEnv(t, upstream)
}

// The motivating bug: a relay answering 200 with an HTML error page used to be
// rendered as a 502 by the first candidate, with the healthy sibling behind it
// never asked.
func TestNonCompletion2xx_FailsOverToTheSibling(t *testing.T) {
	env := replayUpstream(t, http.StatusOK, "<html><body>502 Bad Gateway (from the relay)</body></html>")

	w := replayRequest(t, env)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the sibling can serve this); body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "the sibling answered") {
		t.Fatalf("the client did not get the sibling's completion: %s", w.Body.String())
	}

	trail := waitForTrail(t, "hotel/"+env.group, 2)
	if len(trail) != 2 {
		t.Fatalf("trail has %d attempts, want 2 (the garbled 200 and the sibling): %+v", len(trail), trail)
	}
	if trail[0].Status != http.StatusOK {
		t.Errorf("attempt 0 status = %d, want the 200 the relay actually sent", trail[0].Status)
	}
	// The amplification brake: a relay that does this on every request is
	// charged every time, so its circuit opens and it stops being a candidate.
	if trail[0].Breaker != breakerCharge {
		t.Errorf("attempt 0 breaker = %q, want %q; an uncharged failover would burn the candidate list on every request", trail[0].Breaker, breakerCharge)
	}
	if trail[0].ErrorKind != string(KindProviderError) {
		t.Errorf("attempt 0 error_kind = %q, want %q", trail[0].ErrorKind, KindProviderError)
	}
	if trail[1].Breaker != breakerSuccess {
		t.Errorf("attempt 1 breaker = %q, want %q", trail[1].Breaker, breakerSuccess)
	}
}

// The asymmetry with the streaming path, closed: a stream that ends without a
// single chunk has always gone to the sibling, so a completion object carrying
// nothing does too. The last candidate still forwards it, since by then there is
// nowhere else to ask.
func TestEmptyCompletion_FailsOverToTheSibling(t *testing.T) {
	env := replayUpstream(t, http.StatusOK, `{"id":"chatcmpl-empty","object":"chat.completion","created":1,"model":"shared-model","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`)

	w := replayRequest(t, env)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "the sibling answered") {
		t.Fatalf("the empty answer was served instead of the sibling's: %s", w.Body.String())
	}
	trail := waitForTrail(t, "hotel/"+env.group, 2)
	if len(trail) != 2 {
		t.Fatalf("trail has %d attempts, want 2: %+v", len(trail), trail)
	}
	if trail[0].Breaker != breakerCharge {
		t.Errorf("attempt 0 breaker = %q, want %q: an empty answer is charged whether it is served or failed over", trail[0].Breaker, breakerCharge)
	}
}

// 204 promises no body, so the empty one it carries is the whole answer and the
// decode failure behind it is expected. Failing over there would re-send the
// prompt to a second provider for a request the first one completed.
func TestNoContent2xx_IsServedRatherThanFailedOver(t *testing.T) {
	env := replayUpstream(t, http.StatusNoContent, "")

	w := replayRequest(t, env)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (the first provider's own answer); body: %s", w.Code, w.Body.String())
	}
	trail := waitForTrail(t, "hotel/"+env.group, 1)
	if len(trail) != 1 {
		t.Fatalf("trail has %d attempts, want 1: a 204 is an answer, not a failover: %+v", len(trail), trail)
	}
}

// The last candidate has nobody behind it, so the same garbled 200 is still
// rendered as the gateway's 502 rather than being lost.
func TestNonCompletion2xx_LastCandidateStillAnswersTheClient(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("<html>not a completion</html>")),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
	}
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "shared-model",
		providerName:   "relay",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "pending",
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	w := httptest.NewRecorder()
	h.handleNonStreamingResponse(w, req, logData, resp, readNonStreamingBody(resp, logData.masker), time.Now(), 0, 0, resolveTimings{}, 0, "", 1)

	if w.Code != http.StatusBadGateway {
		t.Errorf("client status = %d, want 502", w.Code)
	}
	if logData.state != "failed" {
		t.Errorf("state = %q, want failed", logData.state)
	}
}

// completionFault is the verdict the whole change turns on, so each way out of
// it is pinned here rather than only through the paths above.
func TestCompletionFault(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	answered := nonStreamingAnswer{chat: ChatCompletionResponse{
		Choices: []Choice{{Message: Message{Role: "assistant", Content: "hello"}}},
	}}
	refused := nonStreamingAnswer{chat: ChatCompletionResponse{
		Choices: []Choice{{Message: Message{Role: "assistant", Extra: jsonExtras{"refusal": json.RawMessage(`"I cannot help with that"`)}}}},
	}}
	lengthFinish := "length"

	for _, tc := range []struct {
		name    string
		ctx     context.Context
		status  int
		ans     nonStreamingAnswer
		wantErr bool
	}{
		{"a completion is served", context.Background(), http.StatusOK, answered, false},
		{"a 201 completion is served", context.Background(), http.StatusCreated, answered, false},
		// The asymmetry with the streaming probe, closed: a stream that ends
		// without a chunk fails over, so an answer object carrying nothing does
		// too.
		{"a completion carrying nothing is a fault", context.Background(), http.StatusOK, nonStreamingAnswer{}, true},
		// The bar is answerCarriesSomething, not len(choices): a refusal, a
		// filtered answer and a reported token count are all the model answering.
		{"a refusal is an answer", context.Background(), http.StatusOK, refused, false},
		{"reported completion tokens are an answer", context.Background(), http.StatusOK, nonStreamingAnswer{chat: ChatCompletionResponse{Usage: Usage{CompletionTokens: 7}}}, false},
		// A stated finish_reason is the provider saying how its own generation
		// ended, so an emptied answer that hit the output ceiling is served. The
		// native twin reads stop_reason for the same claim.
		{"a stated finish_reason is an answer", context.Background(), http.StatusOK, nonStreamingAnswer{chat: ChatCompletionResponse{Choices: []Choice{{FinishReason: &lengthFinish}}}}, false},
		{"an undecodable 200 is a fault", context.Background(), http.StatusOK, nonStreamingAnswer{decodeErr: errors.New("invalid character '<'")}, true},
		{"a body that died on the wire is a fault", context.Background(), http.StatusOK, nonStreamingAnswer{readErr: io.ErrUnexpectedEOF, decodeErr: io.ErrUnexpectedEOF}, true},
		// The whole document arrived and parsed, and only then did the
		// connection drop. That answer is complete: failing over on it would
		// discard it and re-bill the prompt on a sibling.
		{"a completion that parsed despite a broken read is served", context.Background(), http.StatusOK, nonStreamingAnswer{chat: answered.chat, readErr: io.ErrUnexpectedEOF}, false},
		{"a non-2xx belongs to forwardUpstreamError", context.Background(), http.StatusInternalServerError, nonStreamingAnswer{decodeErr: errors.New("invalid character '<'")}, false},
		{"204 carries no body by contract", context.Background(), http.StatusNoContent, nonStreamingAnswer{decodeErr: io.EOF}, false},
		{"205 carries no body by contract", context.Background(), http.StatusResetContent, nonStreamingAnswer{decodeErr: io.EOF}, false},
		{"an interrupted read is nobody's answer to serve", cancelled, http.StatusOK, nonStreamingAnswer{readErr: context.Canceled, decodeErr: context.Canceled}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.ans.completionFault(tc.ctx, tc.status); (err != nil) != tc.wantErr {
				t.Errorf("completionFault = %v, want fault: %v", err, tc.wantErr)
			}
		})
	}
}

// A body past the gateway's own cap is a fault, but not one a sibling can
// answer any better, and not one the provider is charged for: the limit is this
// gateway's, and the size of an answer is a function of the request.
func TestOversizedBodyIsNotChargedToTheProvider(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", nonStreamingBodyCap+1))),
		Header:     make(http.Header),
	}
	ans := readNonStreamingBody(resp, credentialMasker{})

	err := ans.completionFault(context.Background(), http.StatusOK)
	if err == nil {
		t.Fatal("an oversized body is not a completion")
	}
	if !errors.Is(err, httpx.ErrBodyTooLarge) {
		t.Fatalf("err = %v, want it to wrap httpx.ErrBodyTooLarge", err)
	}
	if answerFaultIsRoutable(err) {
		t.Error("every later candidate regenerates the same oversized answer; the cap refusal must not walk the group")
	}
	if translationIsProviderFault(err) {
		t.Error("the gateway's own body cap must not charge the provider's circuit")
	}
}

// nonCompletionState is the minimum a dispatch needs: a log row to stamp and a
// breaker to charge.
func nonCompletionState(t *testing.T) (*requestState, modelCandidate) {
	t.Helper()
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "shared-model",
		providerName:   "relay",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "pending",
	}
	st := &requestState{startTime: time.Now(), logData: logData, circuitBreakerEnabled: true}
	return st, goneCandidateAt(&model.Model{ID: uuid.New(), ModelID: "shared-model"}, "relay", "http://relay.test")
}

// A body past the gateway's own cap ends the request where it stands, even with
// a sibling waiting, and the provider is not charged for it. The answer's size
// is a function of the request, so every later candidate would regenerate the
// same oversized body and be refused for it: failing over re-bills the prompt at
// every stop to render the 502 the first candidate already owed the client.
func TestOversizedBody_IsTerminalAndUncharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	oversized := strings.Repeat("x", nonStreamingBodyCap+1)

	t.Run("chat", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(oversized)), Header: make(http.Header)}
		w := httptest.NewRecorder()
		req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))

		if outcome := h.dispatchNonStreaming(w, req, st, candidate, resp, 0, 1.0, true); outcome == outcomeFailover {
			t.Fatal("the cap refusal walked to the sibling, which would regenerate the same oversized answer")
		}
		if w.Code != http.StatusBadGateway {
			t.Errorf("client status = %d, want 502 rendered on the spot", w.Code)
		}
		if st.logData.errorKind != KindProviderBadRequest {
			t.Errorf("error kind = %q, want %q: the cap is this gateway's limit", st.logData.errorKind, KindProviderBadRequest)
		}
		if st.logData.attemptBreaker == breakerCharge {
			t.Error("the gateway's own body cap charged the provider's circuit")
		}
	})

	t.Run("native anthropic", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		st.anthropicNativeAttempt = true
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(oversized)), Header: make(http.Header)}
		rec := httptest.NewRecorder()
		native := true
		aw := newAnthropicResponseWriter(rec, "msg_o", "m")
		aw.bindNativeFlag(&native)
		req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)

		if outcome := h.dispatchNonStreaming(aw, req, st, candidate, resp, 0, 1.0, true); outcome == outcomeFailover {
			t.Fatal("the cap refusal walked to the sibling, which would regenerate the same oversized answer")
		}
		aw.Finalize()
		if rec.Code != http.StatusBadGateway {
			t.Errorf("client status = %d, want 502 rendered on the spot", rec.Code)
		}
		if st.logData.errorKind != KindProviderBadRequest {
			t.Errorf("error kind = %q, want %q: the cap is this gateway's limit", st.logData.errorKind, KindProviderBadRequest)
		}
		if st.logData.attemptBreaker == breakerCharge {
			t.Error("the gateway's own body cap charged the provider's circuit")
		}
	})
}

// The pass-through families (embeddings, images, audio) had the same hole: a 2xx
// whose body never arrives used to end the loop with this gateway's 502.
func TestPassthrough2xxWithoutABody_FailsOverToTheSibling(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	t.Run("buffered json", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		st.logData.endpointType = endpointTypeEmbeddings
		resp := &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{}, Header: make(http.Header)}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/embeddings", http.NoBody)

		if outcome := h.serveBufferedJSONPassthrough(w, req, st, candidate, resp, "application/json", 0, 1.0, true); outcome != outcomeFailover {
			t.Fatalf("outcome = %v, want outcomeFailover", outcome)
		}
		if w.Body.Len() != 0 {
			t.Errorf("the client was written to before the sibling was tried: %s", w.Body.String())
		}
		if st.logData.state == "failed" {
			t.Error("the row was finalized on a candidate the loop has not finished with")
		}
		if st.logData.attemptBreaker != breakerCharge {
			t.Errorf("breaker note = %q, want %q", st.logData.attemptBreaker, breakerCharge)
		}
	})

	t.Run("streamed", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		st.logData.endpointType = endpointTypeTTS
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/audio/speech", http.NoBody)

		if outcome := h.serveStreamedPassthrough(w, req, st, candidate, resp, "audio/mpeg", false, 0, 1.0, true); outcome != outcomeFailover {
			t.Fatalf("outcome = %v, want outcomeFailover", outcome)
		}
		if w.Body.Len() != 0 {
			t.Errorf("the client was written to before the sibling was tried: %s", w.Body.String())
		}
		if st.logData.attemptBreaker != breakerCharge {
			t.Errorf("breaker note = %q, want %q", st.logData.attemptBreaker, breakerCharge)
		}
	})
}

// The pass-through loop's own proof, through the real handler rather than its
// sub-handlers: the first provider answers 200 with nothing at all, which is
// what an embeddings answer carrying no vectors is, and the client gets the
// sibling's list. Without the hasMoreCandidates argument reaching the two serve
// halves correctly, this is the gateway's 502 with the second provider never
// contacted.
func TestEmbeddings_EmptyBodyFailsOverToTheSibling(t *testing.T) {
	var emptyCalls, goodCalls atomic.Int32
	envEmpty := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
	}))
	t.Cleanup(goodUpstream.Close)
	_, _, goodModelUUID, _ := createMultimodalProvider(t, goodUpstream.URL)

	groupName := envEmpty.modelName
	failoverRepo := failover.NewRepository(testDB.Pool())
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{envEmpty.modelUUID, goodModelUUID},
		map[string]bool{envEmpty.modelUUID.String(): true, goodModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	req := envEmpty.request("/v1/embeddings", "application/json", strings.NewReader(fmt.Sprintf(`{"model":"hotel/%s","input":"hi"}`, groupName)))
	w := httptest.NewRecorder()
	envEmpty.handler.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after failover; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"embedding"`) {
		t.Errorf("body = %q, want the sibling's vectors", w.Body.String())
	}
	if emptyCalls.Load() != 1 || goodCalls.Load() != 1 {
		t.Errorf("upstream calls: empty = %d, good = %d, want 1 each", emptyCalls.Load(), goodCalls.Load())
	}
}

// A request the caller or this gateway's own timeout ended is never failed over:
// the second provider would be answering nobody, and the prompt would be billed
// twice for it. Each path that can now fail over is asked separately, since each
// classifies the interruption itself.
func TestInterruptedAttempt_IsNotFailedOver(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	gone := func(method, path string) *http.Request {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return httptest.NewRequest(method, path, http.NoBody).WithContext(ctx)
	}

	t.Run("chat, a body that decoded into nothing", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[]}`)), Header: make(http.Header)}
		if outcome := h.dispatchNonStreaming(httptest.NewRecorder(), gone("POST", "/v1/chat/completions"), st, candidate, resp, 0, 1.0, true); outcome == outcomeFailover {
			t.Error("an interrupted attempt was sent to a second provider")
		}
	})

	t.Run("passthrough, buffered", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		st.logData.endpointType = endpointTypeEmbeddings
		resp := &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{}, Header: make(http.Header)}
		if outcome := h.serveBufferedJSONPassthrough(httptest.NewRecorder(), gone("POST", "/v1/embeddings"), st, candidate, resp, "application/json", 0, 1.0, true); outcome == outcomeFailover {
			t.Error("an interrupted attempt was sent to a second provider")
		}
		if st.logData.attemptBreaker == breakerCharge {
			t.Error("an interrupted read charged the provider's circuit")
		}
	})

	t.Run("passthrough, streamed", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		st.logData.endpointType = endpointTypeTTS
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}
		if outcome := h.serveStreamedPassthrough(httptest.NewRecorder(), gone("POST", "/v1/audio/speech"), st, candidate, resp, "audio/mpeg", false, 0, 1.0, true); outcome == outcomeFailover {
			t.Error("an interrupted attempt was sent to a second provider")
		}
		if st.logData.attemptBreaker == breakerCharge {
			t.Error("an interrupted read charged the provider's circuit")
		}
	})
}

// The native twin of the empty-completion rule: a message with no content blocks
// and no output tokens goes to the sibling.
func TestNativeEmptyMessage_FailsOverToTheSibling(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	st, candidate := nonCompletionState(t)
	st.anthropicNativeAttempt = true
	body := `{"id":"msg_empty","type":"message","role":"assistant","content":[],"usage":{"input_tokens":3,"output_tokens":0}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_empty", "m")
	aw.bindNativeFlag(&native)

	outcome := h.dispatchNonStreaming(aw, httptest.NewRequest("POST", "/v1/messages", http.NoBody), st, candidate, resp, 0, 1.0, true)
	aw.Finalize()

	if outcome != outcomeFailover {
		t.Fatalf("outcome = %v, want outcomeFailover", outcome)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("the empty message was written to the client: %s", rec.Body.String())
	}
	if st.logData.state == "completed" {
		t.Error("the row was finalized as completed on a candidate that answered nothing")
	}
	if st.logData.attemptBreaker != breakerCharge {
		t.Errorf("breaker note = %q, want %q", st.logData.attemptBreaker, breakerCharge)
	}
}

// The native Anthropic twin: its body is forwarded verbatim rather than decoded,
// so the only way it can answer 2xx without an answer is a read that died. That
// is failed over to the sibling too, and nothing is written to the client.
func TestNativeNonStreaming_ReadFailureFailsOverWhenASiblingRemains(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{}, Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_e", "m")
	aw.bindNativeFlag(&native)
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-x",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	st := &requestState{startTime: time.Now(), logData: logData, circuitBreakerEnabled: true}
	candidate := goneCandidateAt(&model.Model{ID: uuid.New(), ModelID: "claude-x"}, "Anthropic", "http://api.anthropic.test")
	h.deferAnswerJudgement(st, candidate, logData, http.StatusOK)

	outcome := h.handleNativeNonStreaming(aw, req, st, candidate, resp, 1, 10.0, true)

	if outcome != outcomeFailover {
		t.Fatalf("outcome = %v, want outcomeFailover", outcome)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("the client was written to before the sibling was tried: %s", rec.Body.String())
	}
	if logData.state == "failed" {
		t.Error("the row was finalized on a candidate the loop has not finished with")
	}
	if logData.attemptBreaker != breakerCharge {
		t.Errorf("breaker note = %q, want %q", logData.attemptBreaker, breakerCharge)
	}
	// The reject path took this attempt's verdict, so the one armed for the
	// handler must not still be waiting to fire on a later candidate's row.
	if logData.judgeAnswer != nil {
		t.Error("the deferred breaker judgement is still armed after the attempt failed over")
	}
}

// withRequestTimeout narrows the per-attempt deadline for one test and puts the
// setting back afterwards. The cache is invalidated on both edges: the handler
// reads request_timeout once per request, from the cache.
func withRequestTimeout(t *testing.T, h *Handler, value string) {
	t.Helper()
	ctx := context.Background()
	previous, hadRow, err := h.settingsRepo.GetChecked(ctx, "request_timeout")
	if err != nil {
		t.Fatalf("read request_timeout: %v", err)
	}
	if err := h.settingsRepo.Set(ctx, "request_timeout", value); err != nil {
		t.Fatalf("set request_timeout: %v", err)
	}
	h.settingsRepo.InvalidateCache("request_timeout")
	// Put back what was there, including nothing: writing the default in
	// place of an absent row would leave every later test reading an explicit
	// value where it expects the built-in one.
	t.Cleanup(func() {
		if hadRow {
			_ = h.settingsRepo.Set(ctx, "request_timeout", previous)
		} else {
			_ = h.settingsRepo.DeleteKey(ctx, "request_timeout")
		}
		h.settingsRepo.InvalidateCache("request_timeout")
	})
}

// A provider that answers 200 headers and then says nothing until this gateway's
// own per-attempt deadline has stalled; it has not been cancelled by anyone who
// stopped caring. The streaming half has always sent that event to the sibling
// from its TTFT probe and charged the provider for it, while the non-streaming
// half read its own timeout as an interruption and ended the request with a
// terminal error, siblings untouched.
func TestStalled2xx_FailsOverToTheSibling(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Host, "one-slot") {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			// Headers and then silence, until the gateway gives up on the body.
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, siblingCompletion)
	}))
	t.Cleanup(upstream.Close)
	env := buildReplayEnv(t, upstream)
	withRequestTimeout(t, env.h, "500ms")

	w := replayRequest(t, env)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the sibling can serve this); body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "the sibling answered") {
		t.Fatalf("the stall ended the request instead of reaching the sibling: %s", w.Body.String())
	}
	trail := waitForTrail(t, "hotel/"+env.group, 2)
	if len(trail) != 2 {
		t.Fatalf("trail has %d attempts, want 2 (the stall and the sibling): %+v", len(trail), trail)
	}
	// Charged exactly as the TTFT probe charges its own stall. A provider that
	// answers headers and then goes quiet on every request must open its circuit
	// rather than cost every caller a full deadline before the real answer.
	if trail[0].Breaker != breakerCharge {
		t.Errorf("attempt 0 breaker = %q, want %q", trail[0].Breaker, breakerCharge)
	}
	// The same kind the last candidate records for a stall, so the trail reads
	// the event the same way wherever in the group it happened.
	if trail[0].ErrorKind != string(KindProviderTimeout) {
		t.Errorf("attempt 0 error_kind = %q, want %q", trail[0].ErrorKind, KindProviderTimeout)
	}
	if trail[1].Breaker != breakerSuccess {
		t.Errorf("attempt 1 breaker = %q, want %q", trail[1].Breaker, breakerSuccess)
	}
}

// rowPromptTokens reads the prompt-token column of the request's own row.
func rowPromptTokens(t *testing.T, modelID string) int {
	t.Helper()
	var tokens int
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT COALESCE(tokens_prompt, 0) FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`,
		modelID).Scan(&tokens); err != nil {
		t.Fatalf("read tokens_prompt: %v", err)
	}
	return tokens
}

// keyTokensUsed reads the virtual key's own usage counter.
func keyTokensUsed(t *testing.T, keyHash string) int {
	t.Helper()
	var tokens int
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT tokens_used FROM virtual_keys WHERE key_hash = $1`, keyHash).Scan(&tokens); err != nil {
		t.Fatalf("read tokens_used: %v", err)
	}
	return tokens
}

// A candidate whose 2xx is rejected still generated that answer, and the
// provider billed the prompt it read to do so. Only the provider that finally
// served was metered, so a request that walked the group cost the operator
// several prompts and charged the tenant for one.
func TestRejected2xx_ItsPromptIsStillMetered(t *testing.T) {
	env := replayUpstream(t, http.StatusOK,
		`{"id":"chatcmpl-empty","object":"chat.completion","created":1,"model":"shared-model",`+
			`"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":0,"total_tokens":11}}`)

	w := replayRequest(t, env)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	waitForTrail(t, "hotel/"+env.group, 2)
	// 11 from the candidate that answered nothing, 1 from the sibling that
	// answered: the row reports what the request cost, not what its last hop did.
	if got := rowPromptTokens(t, "hotel/"+env.group); got != 12 {
		t.Errorf("row tokens_prompt = %d, want 12 (the rejected candidate's 11 plus the sibling's 1)", got)
	}
	// The key's counter and the TPM bucket take the same charge through
	// recordTokenUsage: 11 for the rejected prompt, 3 for the served answer.
	if got := keyTokensUsed(t, env.keyHash); got != 14 {
		t.Errorf("virtual key tokens_used = %d, want 14 (11 rejected prompt + 3 served)", got)
	}
}

// finishAttemptAdmission fixes the slot's clean flag from the 2xx headers, and a
// 2xx that carried no answer is precisely where that flag is wrong: counting the
// attempt toward the consecutive clean completions that widen the provider's
// learned in-flight window lets a relay answering 200 with nothing earn more
// concurrency the more often it does it.
//
// The clean-run count is what the assertion reads, because the body's own EOF
// settles the slot before this path can tell a served answer from a rejected
// one: what matters is the credit the attempt ends up holding, not which of the
// two writes got there first.
func TestRejected2xx_DoesNotEarnACleanRun(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	limiter := newInflightLimiter()
	h.inflight = limiter

	st, candidate := nonCompletionState(t)
	st.inflightEnabled = true
	pid := candidate.provider.ID
	// A capped window one clean completion short of the grow threshold: only a
	// capped window counts runs at all, and this is the run where the difference
	// shows in the allowance itself rather than just the counter. A correction
	// applied after the fact cannot reach the allowance, which is why the slot
	// has to be held until the verdict instead.
	limiter.cut(pid, 0)
	for range defaultInflightGrowAfter - 1 {
		if !limiter.tryAcquire(pid, 0) {
			t.Fatal("setup: slot not acquired")
		}
		limiter.release(pid, true, 0, 0)
	}
	before := *limiter.windowFor(t, pid)
	if before.goodRuns != defaultInflightGrowAfter-1 {
		t.Fatalf("setup: clean-run count = %d, want %d", before.goodRuns, defaultInflightGrowAfter-1)
	}

	if !h.admitCandidate(st, candidate) {
		t.Fatal("setup: the candidate was not admitted")
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[]}`)), Header: make(http.Header)}
	h.finishAttemptAdmission(st, candidate, resp)
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))

	if outcome := h.dispatchNonStreaming(httptest.NewRecorder(), req, st, candidate, resp, 0, 1.0, true); outcome != outcomeFailover {
		t.Fatalf("outcome = %v, want outcomeFailover", outcome)
	}

	after := *limiter.windowFor(t, pid)
	if after.goodRuns != 0 {
		t.Errorf("clean-run count = %d, want 0: a 2xx the client never saw counted toward widening the provider's window", after.goodRuns)
	}
	if after.limit != before.limit {
		t.Errorf("allowance = %d, want %d: a 2xx the client never saw was the clean run that widened the window", after.limit, before.limit)
	}
}

// The native message bar and the translated completion bar must be the same bar.
// `content: [], stop_reason: max_tokens` is a generation that finished at the
// output ceiling, which completionCarriesAnswer serves; rejecting it on
// /v1/messages alone re-bills the prompt on a sibling for an answer the same
// upstream already produced, decided by nothing but the caller's dialect.
func TestNativeStopReason_IsAnAnswer(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	st, candidate := nonCompletionState(t)
	st.anthropicNativeAttempt = true
	body := `{"id":"msg_capped","type":"message","role":"assistant","content":[],"stop_reason":"max_tokens","usage":{"input_tokens":9,"output_tokens":0}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_capped", "m")
	aw.bindNativeFlag(&native)

	outcome := h.dispatchNonStreaming(aw, httptest.NewRequest("POST", "/v1/messages", http.NoBody), st, candidate, resp, 0, 1.0, true)
	aw.Finalize()

	if outcome == outcomeFailover {
		t.Fatal("a message that states how its generation ended was sent to a sibling")
	}
	if !strings.Contains(rec.Body.String(), "max_tokens") {
		t.Errorf("the message was not forwarded to the client: %s", rec.Body.String())
	}
	// The content bar is untouched and stays narrow: no block arrived, so the
	// breaker still charges the emptied answer and the retirement streak is not
	// cleared by it.
	if !st.logData.emptyCompletion {
		t.Error("a message with no content block was recorded as carrying content")
	}
}

// The same undecodable 2xx at both positions in the group. With a sibling it
// goes through rejectUntranslatableBody; on the last candidate the handler
// renders the client's 502 instead. Both are the same fault, so both report the
// same kind and both charge: routing order alone must not decide whether a
// provider's circuit hears about it.
func TestUndecodable2xx_ChargesTheSameAtEitherPosition(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	for _, tc := range []struct {
		name    string
		hasMore bool
	}{
		{"a sibling remains", true},
		{"the last candidate", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, candidate := nonCompletionState(t)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("<html>not a completion</html>")),
				Header:     http.Header{"Content-Type": []string{"text/html"}},
			}
			req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))

			h.dispatchNonStreaming(httptest.NewRecorder(), req, st, candidate, resp, 0, 1.0, tc.hasMore)

			if st.logData.attemptBreaker != breakerCharge {
				t.Errorf("breaker note = %q, want %q", st.logData.attemptBreaker, breakerCharge)
			}
			kind := st.lastReqErr.Kind
			if !tc.hasMore {
				kind = st.logData.errorKind
			}
			if kind != KindProviderError {
				t.Errorf("error kind = %q, want %q", kind, KindProviderError)
			}
		})
	}
}

// streamedPassthroughPair builds a two-provider text-to-speech failover group:
// the first provider runs firstHandler, the second answers real audio bytes.
// Text-to-speech is the shape that reaches serveStreamedPassthrough, where the
// commit point is the first body byte rather than a buffered read.
func streamedPassthroughPair(t *testing.T, firstHandler http.HandlerFunc) (*multimodalTestEnv, string, *atomic.Int32) {
	t.Helper()
	env := newMultimodalEnv(t, firstHandler)
	var siblingCalls atomic.Int32
	sibling := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		siblingCalls.Add(1)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "ID3 the sibling's audio")
	}))
	t.Cleanup(sibling.Close)
	_, _, siblingModelUUID, _ := createMultimodalProvider(t, sibling.URL)

	group := env.modelName
	if _, err := failover.NewRepository(testDB.Pool()).UpsertWithConfig(context.Background(), group,
		[]uuid.UUID{env.modelUUID, siblingModelUUID},
		map[string]bool{env.modelUUID.String(): true, siblingModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	return env, group, &siblingCalls
}

// speechRequest asks the group for audio, which is what puts the answer on the
// streamed half of the pass-through.
func speechRequest(env *multimodalTestEnv, group string) *http.Request {
	return env.request("/v1/audio/speech", "application/json",
		strings.NewReader(fmt.Sprintf(`{"model":"hotel/%s","input":"hi","voice":"alloy"}`, group)))
}

// The pass-through twin of the stalled chat 2xx, and the reason the attempt's
// own context has to reach these handlers. A provider that answers audio headers
// and then sends no byte until this gateway's per-attempt deadline has stalled,
// and the sibling behind it can serve. Handed the bare client request instead,
// the deadline arrives carrying no cancel origin, resolveCancelOrigin calls it a
// client disconnect, and the request ends there with the provider uncharged.
func TestStreamedPassthroughStall_FailsOverToTheSibling(t *testing.T) {
	env, group, siblingCalls := streamedPassthroughPair(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Headers and then silence, until the gateway gives up on the body.
		<-r.Context().Done()
	})
	// Long-running endpoints get ten times the request timeout, so this is a
	// one-second per-attempt budget and a two-second budget for the request.
	withRequestTimeout(t, env.handler, "100ms")
	withBreakerThresholdOne(t, env.handler)

	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, speechRequest(env, group))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after failover; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "the sibling's audio") {
		t.Fatalf("the stall ended the request instead of reaching the sibling: %q", w.Body.String())
	}
	if got := siblingCalls.Load(); got != 1 {
		t.Errorf("sibling called %d times, want 1", got)
	}
	if env.handler.circuitBreaker.GetState(env.providerID, env.modelName) != failover.StateOpen {
		t.Error("the stalled provider was not charged, so a provider that does this on every request stays a candidate")
	}
}

// The other half of the same rule on the same path: a caller that hangs up is
// not a stall. Nobody is waiting for the answer a second provider would produce,
// so the sibling is not asked and the provider is not charged for someone else's
// cancellation.
func TestStreamedPassthroughDisconnect_IsNotFailedOverOrCharged(t *testing.T) {
	reached := make(chan struct{})
	env, group, siblingCalls := streamedPassthroughPair(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(reached)
		<-r.Context().Done()
	})
	withBreakerThresholdOne(t, env.handler)

	req := speechRequest(env, group)
	ctx, cancel := context.WithCancel(req.Context())
	t.Cleanup(cancel)
	go func() {
		<-reached
		// The headers are out and the gateway is on the first-byte read.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	env.handler.AudioSpeech(httptest.NewRecorder(), req.WithContext(ctx))

	if got := siblingCalls.Load(); got != 0 {
		t.Errorf("sibling called %d times, want 0: the prompt was re-billed for an answer nobody was waiting for", got)
	}
	if env.handler.circuitBreaker.GetState(env.providerID, env.modelName) == failover.StateOpen {
		t.Error("a caller that hung up was charged to the provider")
	}
}
