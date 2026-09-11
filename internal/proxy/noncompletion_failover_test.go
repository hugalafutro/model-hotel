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

// A body past the gateway's own cap fails over like any other 2xx that is not a
// completion, but the provider is not charged for it: the limit is this
// gateway's, not the provider's failure.
func TestOversizedBodyIsNotChargedToTheProvider(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", nonStreamingBodyCap+1))),
		Header:     make(http.Header),
	}
	ans := readNonStreamingBody(resp, credentialMasker{})

	err := ans.completionFault(context.Background(), http.StatusOK)
	if err == nil {
		t.Fatal("an oversized body is not a completion and must fail over")
	}
	if !errors.Is(err, httpx.ErrBodyTooLarge) {
		t.Fatalf("err = %v, want it to wrap httpx.ErrBodyTooLarge", err)
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

// A body past the gateway's own cap goes to the sibling like any other answer
// that is not a completion, and the provider is not charged on the way: the two
// halves of the oversized rule, proven on the path rather than on the helper.
func TestOversizedBody_FailsOverUncharged(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	oversized := strings.Repeat("x", nonStreamingBodyCap+1)

	t.Run("chat", func(t *testing.T) {
		st, candidate := nonCompletionState(t)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(oversized)), Header: make(http.Header)}
		w := httptest.NewRecorder()
		req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))

		if outcome := h.dispatchNonStreaming(w, req, st, candidate, resp, 0, 1.0, true); outcome != outcomeFailover {
			t.Fatalf("outcome = %v, want outcomeFailover", outcome)
		}
		if w.Body.Len() != 0 {
			t.Errorf("the client was written to before the sibling was tried: %s", w.Body.String())
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

		if outcome := h.dispatchNonStreaming(aw, req, st, candidate, resp, 0, 1.0, true); outcome != outcomeFailover {
			t.Fatalf("outcome = %v, want outcomeFailover", outcome)
		}
		aw.Finalize()
		if rec.Body.Len() != 0 {
			t.Errorf("the client was written to before the sibling was tried: %s", rec.Body.String())
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
