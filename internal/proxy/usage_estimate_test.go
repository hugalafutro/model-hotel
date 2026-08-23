package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// The metering fallback for streams and responses that end without a usage
// report. Token counts normally come only from the provider's usage chunk, which
// is the LAST chunk of an OpenAI stream: a client that hangs up after the
// content but before it, or a provider that never sends one, used to leave the
// TPM budget and the key's tokens_used counter uncharged while the provider
// still billed the operator. The fallback charges an estimate derived from the
// prompt text size and the delivered output bytes (4 bytes ≈ 1 token).

func TestEstimateTokens(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 3: 1, 4: 1, 5: 2, 40: 10, 41: 11}
	for textBytes, want := range cases {
		assert.Equal(t, want, estimateTokens(textBytes), "bytes=%d", textBytes)
	}
}

// streamUsageHarness runs one upstream SSE body through handleStreamingResponse
// with a recording virtual-key repo and a 40-byte prompt on the log row.
func streamUsageHarness(t *testing.T, upstreamBody string, cancelAfterFirstChunk bool) (*mockVirtualKeyRepo, *requestLogData) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var body io.ReadCloser
	var served chan struct{}
	if cancelAfterFirstChunk {
		// Deliver the first event, then block until the client context is
		// cancelled so the rest (usage chunk included) never arrives.
		first, _, _ := strings.Cut(upstreamBody, "\n\n")
		served = make(chan struct{})
		body = &blockingReader{first: first + "\n\n", ctx: ctx, served: served}
	} else {
		body = io.NopCloser(strings.NewReader(upstreamBody))
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).WithContext(ctx))

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 40,
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		h.handleStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})
		close(done)
	}()
	if cancelAfterFirstChunk {
		<-served
		cancel()
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleStreamingResponse did not finish")
	}
	return vkRepo, logData
}

// blockingReader serves `first` (closing `served` once it is fully read), then
// blocks until ctx is cancelled and returns context.Canceled, the error the
// transport surfaces when the client goes away.
type blockingReader struct {
	first  string
	ctx    context.Context
	served chan struct{}
	off    int
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if r.off < len(r.first) {
		n := copy(p, r.first[r.off:])
		r.off += n
		if r.off == len(r.first) {
			close(r.served)
		}
		return n, nil
	}
	<-r.ctx.Done()
	return 0, context.Canceled
}

func (r *blockingReader) Close() error { return nil }

// singleAddTokens returns the one charge recorded against the key.
// recordTokenUsage always reaches the repo, with 0 when nothing is owed, so a
// zero here means "nothing charged", not "nothing recorded".
func singleAddTokens(t *testing.T, repo *mockVirtualKeyRepo) int {
	t.Helper()
	require.Len(t, repo.addTokensCalls, 1, "AddTokens should be called exactly once")
	assert.Equal(t, "test-hash", repo.addTokensCalls[0].keyHash)
	return repo.addTokensCalls[0].tokens
}

const (
	contentChunk = `data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n"
	usageChunk   = `data: {"id":"c","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":220,"total_tokens":232}}` + "\n\n"
	doneChunk    = "data: [DONE]\n\n"
)

func TestHandleStreamingResponse_EstimatesUsageWhenClientDisconnectsBeforeUsageChunk(t *testing.T) {
	repo, logData := streamUsageHarness(t, contentChunk+usageChunk+doneChunk, true)

	require.Contains(t, logData.errorMessage, "client disconnected")
	// 40 prompt bytes → 10 prompt tokens; "hello" (5 bytes) → 2 completion tokens.
	assert.Equal(t, 12, singleAddTokens(t, repo))
}

func TestHandleStreamingResponse_EstimatesUsageWhenProviderOmitsUsageChunk(t *testing.T) {
	body := contentChunk +
		`data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" world","reasoning_content":"think"},"finish_reason":"stop"}]}` + "\n\n" +
		doneChunk
	repo, logData := streamUsageHarness(t, body, false)

	assert.Equal(t, "completed", logData.state)
	// 40 prompt bytes → 10; "hello"+" world"+"think" = 16 bytes → 4.
	assert.Equal(t, 14, singleAddTokens(t, repo))
	// The request log keeps the provider's (absent) figures: estimates charge
	// the quota, they are not reported as measured usage.
	assert.Equal(t, 0, logData.tokensPrompt)
	assert.Equal(t, 0, logData.tokensCompletion)
}

// Agent traffic is mostly tool calls, whose output lives in
// delta.tool_calls[].function.arguments rather than delta.content.
func TestHandleStreamingResponse_EstimatesUsageFromToolCallArguments(t *testing.T) {
	body := `data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		doneChunk
	repo, _ := streamUsageHarness(t, body, false)

	// 40 prompt bytes → 10; "get_weather" (11) + {"city":"Paris"} (16) = 27 bytes → 7.
	assert.Equal(t, 17, singleAddTokens(t, repo))
}

// Every choice of an n>1 stream is delivered output; the estimate must not
// stop at choices[0] the way the content observers do.
func TestHandleStreamingResponse_EstimatesUsageAcrossAllChoices(t *testing.T) {
	body := `data: {"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null},{"index":1,"delta":{"content":"bonjour","tool_calls":[{"index":0,"function":{"name":"f","arguments":"{}"}}]},"finish_reason":null}]}` + "\n\n" +
		doneChunk
	repo, _ := streamUsageHarness(t, body, false)

	// 40 prompt bytes → 10; "hello" (5) + "bonjour" (7) + "f" (1) + "{}" (2) = 15 bytes → 4.
	assert.Equal(t, 14, singleAddTokens(t, repo))
}

// A usage chunk whose counts are all zero is no usage at all.
func TestHandleStreamingResponse_EstimatesUsageWhenReportedUsageIsZero(t *testing.T) {
	body := contentChunk +
		`data: {"id":"c","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}` + "\n\n" +
		doneChunk
	repo, _ := streamUsageHarness(t, body, false)
	assert.Equal(t, 12, singleAddTokens(t, repo))
}

func TestHandleStreamingResponse_ReportedUsageWinsOverEstimate(t *testing.T) {
	repo, _ := streamUsageHarness(t, contentChunk+usageChunk+doneChunk, false)
	assert.Equal(t, 232, singleAddTokens(t, repo))
}

func TestHandleStreamingResponse_NoEstimateWithoutDeliveredContent(t *testing.T) {
	body := `data: {"error":{"message":"upstream exploded"}}` + "\n\n" + doneChunk
	repo, logData := streamUsageHarness(t, body, false)

	assert.Equal(t, "failed", logData.state)
	assert.Equal(t, 0, singleAddTokens(t, repo))
}

// The security property itself: the estimate reaches the TPM bucket, so a key
// that disconnects before the usage chunk still burns its budget and the next
// request is refused once the budget is spent.
func TestHandleStreamingResponse_EstimateDebitsTPMBudget(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = &mockVirtualKeyRepo{}

	const tpm = 10
	// Admission, as the TPM middleware performs it: creates the bucket and
	// reserves the 1-token placeholder.
	require.True(t, h.tpmLimiter.Allow("test-hash", tpm))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	served := make(chan struct{})
	resp := &http.Response{StatusCode: http.StatusOK, Body: &blockingReader{first: contentChunk, ctx: ctx, served: served}}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).WithContext(ctx))
	logData := &requestLogData{id: uuid.New().String(), modelID: "test-model", streaming: true, virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming", promptTextBytes: 40}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		h.handleStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})
		close(done)
	}()
	<-served
	cancel()
	<-done

	// 12 estimated tokens against a 10-token budget: the bucket is in debt.
	assert.False(t, h.tpmLimiter.Allow("test-hash", tpm), "next request must be refused after the estimated debit")
}

// Native Anthropic passthrough: message_start reports input_tokens up front, but
// output_tokens only arrive on message_delta at the end, so a disconnect after
// the text deltas leaves output unmetered. The fallback estimates the output
// from the delivered delta text and keeps the reported input figure.
func TestNativeStream_EstimatesOutputWhenTruncatedBeforeMessageDelta(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	head, _, _ := strings.Cut(nativeStreamHead, "event: message_delta")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(head))}
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "claude-test",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 40,
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)
	h.handleStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, rawPassthrough: true})

	// input_tokens 12 reported by message_start; "Hello" (5 bytes) → 2 estimated.
	assert.Equal(t, 14, singleAddTokens(t, vkRepo))
}

// A tool call whose input is empty delivers only its name before the stream is
// cut; the name alone must keep the estimate from reading as "nothing delivered".
func TestNativeStream_EstimatesOutputFromToolStartName(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	head, _, _ := strings.Cut(nativeStreamHead, "event: content_block_start")
	body := head + "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t1\",\"name\":\"lookup\",\"input\":{}}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	logData := &requestLogData{id: uuid.New().String(), modelID: "claude-test", streaming: true, virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming", promptTextBytes: 40}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)
	h.handleStreamingResponse(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", http.NoBody), logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, rawPassthrough: true})

	// input_tokens 12 reported by message_start; "lookup" (6 bytes) → 2 estimated.
	assert.Equal(t, 14, singleAddTokens(t, vkRepo))
}

func TestHandleNativeNonStreaming_EstimatesUsageWhenOmitted(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	anthropicBody := `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"Hello, world!"},{"type":"tool_use","id":"t1","name":"f","input":{"a":1}}],"stop_reason":"end_turn"}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(anthropicBody)), Header: make(http.Header)}
	native := true
	aw := newAnthropicResponseWriter(httptest.NewRecorder(), "msg_ignored", "m")
	aw.bindNativeFlag(&native)
	logData := &requestLogData{id: uuid.New().String(), modelID: "claude-x", virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming", promptTextBytes: 40}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleNativeNonStreaming(aw, httptest.NewRequest("POST", "/v1/messages", http.NoBody), st, resp, 1, 5)

	// 40 prompt bytes → 10; "Hello, world!" (13) + "f" (1) + {"a":1} (7) = 21 bytes → 6.
	assert.Equal(t, 16, singleAddTokens(t, vkRepo))
}

func TestHandleNonStreamingResponse_EstimatesUsageWhenProviderOmitsIt(t *testing.T) {
	vkRepo := &mockVirtualKeyRepo{}
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = vkRepo

	upstreamBody := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Hello, world!","reasoning_content":"hmm"}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(upstreamBody)), Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := &requestLogData{
		modelID:         "gpt-test",
		providerID:      uuid.New(),
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "pending",
		promptTextBytes: 40,
	}
	h.handleNonStreamingResponse(w, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	require.Equal(t, http.StatusOK, w.Code)
	// 40 prompt bytes → 10; "Hello, world!" (13) + "hmm" (3) = 16 bytes → 4.
	assert.Equal(t, 14, singleAddTokens(t, vkRepo))
	assert.Equal(t, 0, logData.tokensPrompt, "estimates must not be reported as measured usage")
}

func TestHandleNonStreamingResponse_NoEstimateForEmptyAnswer(t *testing.T) {
	vkRepo := &mockVirtualKeyRepo{}
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = vkRepo

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"id":"x","object":"chat.completion","choices":[]}`)), Header: make(http.Header)}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := &requestLogData{modelID: "gpt-test", providerID: uuid.New(), virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "pending", promptTextBytes: 40}
	h.handleNonStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	assert.Equal(t, 0, singleAddTokens(t, vkRepo))
}

func TestChatAnswerBytes_CountsEveryOutputShape(t *testing.T) {
	out := ChatCompletionResponse{Choices: []Choice{
		{Message: Message{Content: "abc", Reasoning: "de"}},
		{Message: Message{Content: []any{map[string]any{"type": "text", "text": "fghi"}, map[string]any{"type": "image_url"}}, ReasoningContent: "j"}},
		{Message: Message{ReasoningDetails: []ReasoningDetail{{Type: "reasoning.text", Text: "kl"}}, ToolCalls: []ToolCall{{Function: ToolCallFunc{Name: "m", Arguments: `{}`}}}}},
	}}
	assert.Equal(t, 15, chatAnswerBytes(out))
}

// The prompt is sized from its text, never from the raw body: a vision request
// carries base64 media that is ~1000x larger in bytes than in tokens, and
// sizing it by bytes would charge millions of phantom tokens for one request
// (locking the key out for hours).
func TestPromptTextBytes_SkipsInlineMedia(t *testing.T) {
	media := strings.Repeat("A", 1<<20)
	body := fmt.Sprintf(`{"model":"m","messages":[{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}},{"type":"input_audio","input_audio":{"data":"%s","format":"wav"}}]}],"tools":[{"type":"function","function":{"name":"f"}}]}`, media, media)

	// "be brief" (8) + "what is this" (12) + the tools JSON (45).
	assert.Equal(t, 65, promptTextBytes([]byte(body)))
	assert.Equal(t, 0, promptTextBytes([]byte("not json")))
}

// The prompt size reaches the log row at ingest so every response path can
// estimate from it.
func TestIngestRequest_RecordsPromptTextBytes(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	body := `{"model":"m","stream":false,"messages":[{"role":"user","content":"hi there"}]}`
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	st, ok := h.ingestRequest(httptest.NewRecorder(), req, endpointTypeChat)
	require.True(t, ok)
	assert.Equal(t, 8, st.logData.promptTextBytes)
}

// Multimodal SSE passthrough: usage scraped from the SSE tail is charged even
// when the copy to the client is interrupted, since the provider billed it.
func TestAudioSpeech_SSEUsageMeteredWhenCopyInterrupted(t *testing.T) {
	sse := "data: {\"type\":\"speech.audio.delta\",\"audio\":\"cGFydA==\"}\n\ndata: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":12,\"output_tokens\":34,\"total_tokens\":46}}\n\n"
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		require.True(t, ok, "test server must support hijacking")
		conn, buf, err := hj.Hijack()
		require.NoError(t, err)
		// A Content-Length longer than the bytes sent makes the copy end in
		// an unexpected EOF after the whole usage tail has been delivered.
		_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nContent-Length: %d\r\n\r\n%s", len(sse)+100, sse)
		_ = buf.Flush()
		_ = conn.Close()
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy","stream_format":"sse"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	env.handler.AudioSpeech(httptest.NewRecorder(), req)

	vk, err := virtualkey.NewRepository(testDB.Pool()).FindByKeyHash(context.Background(), env.keyHash)
	require.NoError(t, err)
	assert.Equal(t, int64(46), vk.TokensUsed, "interrupted copy must still charge the scraped usage")
}
