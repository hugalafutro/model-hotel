package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestHandleStreamingResponse_ReasoningContentHasContent(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Two chunks with same finish_reason. Second has reasoning_content,
	// so it should NOT be suppressed (hasContent=true).
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}

data: {"id":"2","choices":[{"index":0,"delta":{"reasoning_content":"thinking..."},"finish_reason":"stop"}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	result := w.Result()
	defer result.Body.Close()

	body := w.Body.String()
	// Should contain both chunks (not suppressed)
	if !strings.Contains(body, "content") {
		t.Error("expected first chunk with content")
	}
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected second chunk with reasoning_content (not suppressed)")
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}

func TestHandleStreamingResponse_BOMStripped(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with UTF-8 BOM at start
	streamData := "\uFEFFdata: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	// BOM should be stripped, response should be valid
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}

func TestHandleStreamingResponse_ReasoningTokensCaptured(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":800,"completion_tokens_details":{"reasoning_tokens":650}}}
data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensCompletion != 50 {
		t.Errorf("expected completion_tokens=50, got %d", logData.tokensCompletion)
	}
	if logData.tokensCompletionReasoning != 650 {
		t.Errorf("expected reasoning_tokens=650, got %d", logData.tokensCompletionReasoning)
	}
}

func TestHandleStreamingResponse_TPSWithReasoningTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Simulate a thinking model: 650 reasoning + 50 completion = 700 total output
	// TTFT includes reasoning time, generationDuration = totalDuration - ttft
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hello world"}}],"usage":{"prompt_tokens":89000,"completion_tokens":50,"total_tokens":89700,"completion_tokens_details":{"reasoning_tokens":650}}}
data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	// Start time 19 seconds ago → totalDuration ≈ 19000ms, no TTFT measured
	// generationDuration = totalDuration since ttft=0, so TPS = 700/19000*1000 ≈ 36.8
	startTime := time.Now().Add(-19 * time.Second)
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	// TPS should use (50 + 650) / totalDuration * 1000 since no TTFT was measured
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS, got %f", logData.tokensPerSecond)
	}
	// The old (buggy) formula would give: 50/19000*1000 ≈ 2.6 TPS
	// The new formula includes reasoning tokens (700 total output vs 50)
	if logData.tokensPerSecond < 10 {
		t.Errorf("TPS seems too low (%.1f), reasoning tokens may not be included in calculation", logData.tokensPerSecond)
	}
}

// TestHandleStreamingResponse_ReasoningStripWriteFailure tests write failure
// during reasoning_content stripping (line 540-543 in proxy.go).
//
// Stream: chunk with reasoning_content that gets stripped. After the stripping
// logic reconstructs the chunk, writeSSEDataChunk (line 540) fails.
func TestHandleStreamingResponse_ReasoningStripWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hello","reasoning_content":"thinking..."}}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// With stripReasoning=true, the first data chunk is processed through the
	// reasoning strip path. The stripped payload is written via writeSSEDataChunk
	// (3 writes: "data: " + payload + "\n\n"). We want the writeSSEDataChunk to fail.
	// The initial "data: " (6 bytes) succeeds, then payload write triggers failure.
	w := &contentTriggeredWriter{
		triggerAfterBytes: 6, // "data: " succeeds, payload write fails
		failErr:           errors.New("reasoning strip write error"),
	}

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_EmptyContentStripWriteFailure tests write failure
// when stripping empty content from reasoning chunks (line 648-651 in proxy.go).
// The empty-content-strip block runs regardless of stripReasoning, but with
// stripReasoning=true the reasoning strip block would modify the delta first
// (deleting content), preventing the empty-content check from matching.
// Using stripReasoning=false lets the original chunk reach line 629 unmodified.
func TestHandleStreamingResponse_EmptyContentStripWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"","reasoning_content":"thinking..."}}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// Empty content strip path: "data: " write succeeds, payload write fails
	w := &contentTriggeredWriter{
		triggerAfterBytes: 6,
		failErr:           errors.New("empty content strip write error"),
	}

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req) // no stripReasoning — chunk reaches line 629 unmodified

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ReasoningStrip_DeltaHasRole tests that chunks
// with role field (but no content) are forwarded, not stripped (line 461-463).
func TestHandleStreamingResponse_ReasoningStrip_DeltaHasRole(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with role field but no content or reasoning - should be forwarded
	// Note: the handler normalizes empty deltas, but role field triggers deltaHasContent = true
	streamData := `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	// Chunk should be forwarded (not stripped - it has role + finish_reason fields)
	// The delta may be normalized but the chunk itself should be present
	if !strings.Contains(body, "data:") {
		t.Errorf("expected chunk to be forwarded, got: %q", body)
	}
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ReasoningStrip_DeltaHasToolCalls tests that chunks
// with tool_calls field (but no content) are forwarded, not stripped (line 464-466).
func TestHandleStreamingResponse_ReasoningStrip_DeltaHasToolCalls(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with tool_calls field but no content or reasoning - should be forwarded
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_1","function":{"name":"test","arguments":"{}"}}]}}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	// Chunk should be forwarded (not stripped) because it has tool_calls field
	if !strings.Contains(body, `"tool_calls"`) {
		t.Errorf("expected chunk with tool_calls to be forwarded, got: %q", body)
	}
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ReasoningStrip_KeepAliveWriteFailure tests write
// failure during keep-alive write when reasoning is stripped (line 510-520).
func TestHandleStreamingResponse_ReasoningStrip_KeepAliveWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with only reasoning_content (no content) - triggers keep-alive
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"reasoning_content":"thinking..."}}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// Fail on the keep-alive write (after initial writes)
	w := &failingResponseWriter{
		failAfter: 0,
		failErr:   errors.New("keep-alive write error"),
	}

	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	// Write failure should set state to failed
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// ---------------------------------------------------------------------------
// Streaming reasoning normalization tests (moved from proxy_coverage_test.go)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_ReasoningFieldNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Ollama-style: delta.reasoning → reasoning_content
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning":"Let me think"},"finish_reason":null}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected response to contain reasoning_content")
	}
	if !strings.Contains(body, "Let me think") {
		t.Errorf("expected reasoning_content to contain 'Let me think', got: %s", body)
	}
}

func TestHandleStreamingResponse_ReasoningDetailsNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// OpenRouter-style: delta.reasoning_details with reasoning.text → reasoning_content
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_details":[{"type":"reasoning.text","text":"Step 1","format":"google-gemini-v1"}]},"finish_reason":null}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected response to contain reasoning_content")
	}
	if !strings.Contains(body, "Step 1") {
		t.Errorf("expected reasoning_content to contain 'Step 1', got: %s", body)
	}
}

func TestHandleStreamingResponse_ThinkingTagsNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// MiniMax native-style: <thinking> tags in delta.content → reasoning_content
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"<thinking>My reasoning</thinking>The answer"},"finish_reason":null}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected response to contain reasoning_content")
	}
	if !strings.Contains(body, "My reasoning") {
		t.Errorf("expected reasoning_content to contain 'My reasoning', got: %s", body)
	}
	if !strings.Contains(body, "The answer") {
		t.Errorf("expected content to contain 'The answer', got: %s", body)
	}
}

func TestHandleStreamingResponse_ReasoningContentAlreadyPresent(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// DeepSeek-style: already has reasoning_content, no double-normalization
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"Already here"},"finish_reason":null}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected response to contain reasoning_content")
	}
	if !strings.Contains(body, "Already here") {
		t.Errorf("expected reasoning_content to contain 'Already here', got: %s", body)
	}
}

// TestHandleStreamingResponse_StripReasoning_EmptyDeltasSkipped tests that
// reasoning-only chunks (with content: "" and reasoning_content) are stripped
// and NOT forwarded to the client when strip_reasoning is enabled.
func TestHandleStreamingResponse_StripReasoning_EmptyDeltasSkipped(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"Let me think"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"more thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Hello world"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning_content is NOT in output
	assert.NotContains(t, body, "reasoning_content")

	// Verify role is also NOT in output (stripped when no content)
	assert.NotContains(t, body, `"role":"assistant"`)

	// Verify content chunk IS in output
	assert.Contains(t, body, `"content":"Hello world"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify finish_reason is forwarded
	assert.Contains(t, body, "finish_reason")

	// Verify NO SSE keep-alive comments are sent (they break Warp's Go backend)
	assert.NotContains(t, body, ": thinking")

	// Verify the stream completed successfully
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_WarpThinkingModelScenario simulates
// the actual Warp.dev issue: a deep thinking model that sends 5+ reasoning chunks
// before any content. Only role, content, and [DONE] should appear in output.
func TestHandleStreamingResponse_StripReasoning_WarpThinkingModelScenario(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		// 5 reasoning-only chunks
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":"","reasoning_content":"step 1"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":"","reasoning_content":"step 2"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":"","reasoning_content":"step 3"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":"","reasoning_content":"step 4"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":"","reasoning_content":"step 5"}}]}`,
		// Content chunks
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":"Here"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":" is"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":" the"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{"content":" answer"}}]}`,
		`{"id":"warp-1","object":"chat.completion.chunk","created":1,"model":"deep-think","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "deep-think",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify no reasoning_content in output
	assert.NotContains(t, body, "reasoning_content")

	// Verify role is also NOT in output (stripped when no content)
	assert.NotContains(t, body, `"role":"assistant"`)

	// Verify all content chunks are present
	assert.Contains(t, body, `"content":"Here"`)
	assert.Contains(t, body, `"content":" is"`)
	assert.Contains(t, body, `"content":" the"`)
	assert.Contains(t, body, `"content":" answer"`)

	// Verify [DONE]
	assert.Contains(t, body, "[DONE]")

	// Verify finish_reason is forwarded
	assert.Contains(t, body, "finish_reason")

	// Verify no leading empty lines before first content (SSE separators
	// from stripped chunks must also be suppressed, not just data lines)
	assert.True(t, strings.HasPrefix(body, "data: "), "output should start with 'data: ' — no leading empty lines from stripped reasoning chunks")

	// Verify NO SSE keep-alive comments (they break Warp's Go backend)
	assert.NotContains(t, body, ": thinking")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_RoleStrippedWhenNoContent tests that
// a role chunk with reasoning_content but no content has both reasoning AND role
// stripped when strip_reasoning is enabled, without SSE keep-alive comments.
func TestHandleStreamingResponse_StripReasoning_RoleStrippedWhenNoContent(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"response"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning_content is removed
	assert.NotContains(t, body, "reasoning_content")

	// Verify role is also stripped (no longer preserved when no content)
	assert.NotContains(t, body, `"role":"assistant"`)

	// Verify NO keep-alive SSE comments are sent for the stripped role+reasoning chunk
	assert.NotContains(t, body, ": thinking")

	// Verify content is present
	assert.Contains(t, body, `"content":"response"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify finish_reason is forwarded
	assert.Contains(t, body, "finish_reason")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_FinishAndUsagePassThrough tests that
// finish_reason chunks and usage chunks pass through even with strip_reasoning enabled.
func TestHandleStreamingResponse_StripReasoning_FinishAndUsagePassThrough(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"think"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"more thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Answer"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning_content is removed
	assert.NotContains(t, body, "reasoning_content")

	// Verify role is also stripped (no content in first chunk after stripping reasoning)
	assert.NotContains(t, body, `"role":"assistant"`)

	// Verify content is present
	assert.Contains(t, body, `"content":"Answer"`)

	// Verify finish_reason and usage are forwarded
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.Contains(t, body, `"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`)

	// Verify NO SSE keep-alive comments are sent (they break Warp's Go backend)
	assert.NotContains(t, body, ": thinking")

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_Disabled tests that when
// strip_reasoning is false, reasoning_content fields pass through normally.
func TestHandleStreamingResponse_StripReasoning_Disabled(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"Let me think"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, false)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning_content IS present (not stripped)
	assert.Contains(t, body, `"reasoning_content":"Let me think"`)

	// Verify content is present
	assert.Contains(t, body, `"content":"Hello"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify finish_reason is forwarded
	assert.Contains(t, body, "finish_reason")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_JSONKeepAlive tests that
// valid JSON data keep-alive chunks (not SSE comments) are sent for
// skipped reasoning-only chunks when strip_reasoning is enabled.
// SSE comments (": thinking") break Warp's openai-go ssestream parser,
// and sending nothing causes client timeouts during long thinking phases.
func TestHandleStreamingResponse_StripReasoning_JSONKeepAlive(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"more"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"still thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"almost done"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify NO SSE comment keep-alives (they break Warp's openai-go ssestream)
	assert.NotContains(t, body, ": thinking")

	// Verify valid JSON keep-alive chunks ARE sent for stripped reasoning.
	// The keep-alive reuses the stream's real completion ID.
	// json.Marshal produces deterministic alphabetical keys.
	assert.Contains(t, body, `data: {"choices":[{"delta":{},"index":0}],"id":"c1","object":"chat.completion.chunk"}`)

	// Verify reasoning_content is NOT in output
	assert.NotContains(t, body, "reasoning_content")

	// Verify content is in output
	assert.Contains(t, body, `"content":"Hello"`)

	// Verify [DONE] is in output
	assert.Contains(t, body, "[DONE]")

	// Verify finish_reason is in output
	assert.Contains(t, body, "finish_reason")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_FinishReasonNormalized tests that
// provider-specific finish_reason values (e.g., "end_turn" from Anthropic,
// "STOP" from Gemini) are normalized to OpenAI equivalents when strip_reasoning
// is active. Without the fix, the continue in the strip_reasoning path would
// skip the finish_reason normalization block, leaking non-standard values.
func TestHandleStreamingResponse_StripReasoning_FinishReasonNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"final thought"},"finish_reason":"end_turn"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning_content is stripped
	assert.NotContains(t, body, "reasoning_content")

	// Verify content is present
	assert.Contains(t, body, `"content":"Hello"`)

	// Verify non-standard "end_turn" is normalized to "stop"
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.NotContains(t, body, `"finish_reason":"end_turn"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_StripReasoning_GeminiStopNormalized tests that
// Gemini's "STOP" finish_reason is normalized to "stop" when strip_reasoning
// is active, even when the chunk also carries content alongside reasoning.
func TestHandleStreamingResponse_StripReasoning_GeminiStopNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Answer","reasoning_content":"hmm"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"STOP"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withStripReasoningContext(req, true)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning_content is stripped
	assert.NotContains(t, body, "reasoning_content")

	// Verify content is present
	assert.Contains(t, body, `"content":"Answer"`)

	// Verify "STOP" is normalized to "stop"
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.NotContains(t, body, `"finish_reason":"STOP"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_EmptyContentStrip_FinishReasonNormalized tests that
// the always-on empty-content strip block normalizes finish_reason in-place.
// Without the fix, a chunk with reasoning_content + empty content + non-standard
// finish_reason (e.g., "end_turn") would have finish_reason leaked unnormalized
// because written=true skips the normalization block later in the loop.
func TestHandleStreamingResponse_EmptyContentStrip_FinishReasonNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"thinking"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"","reasoning_content":"final thought"},"finish_reason":"end_turn"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify "end_turn" is normalized to "stop" even through the
	// always-on empty-content strip path
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.NotContains(t, body, `"finish_reason":"end_turn"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}

// TestHandleStreamingResponse_ReasoningNormalization_FinishReasonNormalized tests that
// the reasoning field normalization block (always-on, handles Ollama reasoning→reasoning_content
// mapping) normalizes finish_reason in-place. Without the fix, written=true would skip the
// normalization block, leaking non-standard finish_reason values.
func TestHandleStreamingResponse_ReasoningNormalization_FinishReasonNormalized(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstream := buildSSEBody(
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning":"thinking via Ollama format"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"Answer"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"STOP"}]}`,
		`[DONE]`,
	)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(upstream),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{})

	body := w.Body.String()

	// Verify reasoning was normalized to reasoning_content (Ollama → standard)
	assert.Contains(t, body, "reasoning_content")

	// Verify "STOP" is normalized to "stop" — the reasoning normalization
	// block sets written=true which would skip the main normalization.
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.NotContains(t, body, `"finish_reason":"STOP"`)

	// Verify content is present
	assert.Contains(t, body, `"content":"Answer"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "[DONE]")

	// Verify stream completed
	assert.Equal(t, "completed", logData.state)
}
