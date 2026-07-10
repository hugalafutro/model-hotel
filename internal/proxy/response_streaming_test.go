package proxy

import (
	"encoding/json"
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
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// handleStreamingResponse tests - write error paths
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_WriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 0, // fails on first Write
		failErr:   errors.New("client write error"),
	}

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

	// clientDisconnected should be set, state should reflect the error
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_NewlineWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with one data line - first Write succeeds, newline Write fails
	streamData := "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 1, // first Write succeeds, second (newline) fails
		failErr:   errors.New("newline write error"),
	}

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

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_DoneWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with [DONE] - fails on newline after [DONE]
	// Code writes: "data: [DONE]" then "\n" then breaks (doesn't write second "\n")
	// So we need to fail on the "\n" write (second Write)
	streamData := "data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// Fail on the newline Write after [DONE] (second Write call)
	w := &failingResponseWriter{
		failAfter: 1,
		failErr:   errors.New("done newline write error"),
	}

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

	// clientDisconnected should be set due to write failure
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_NormalizedWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Chunk with non-OpenAI finish_reason that needs normalization.
	// "end_turn" (Anthropic) normalizes to "stop".
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"end_turn"}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// The rewrite path writes: "data: ", then newPayload, then "\n"
	// Fail on the newline Write (third Write: "data: "=0, newPayload=1, "\n"=2)
	w := &failingResponseWriter{
		failAfter: 2,
		failErr:   errors.New("normalized newline write error"),
	}

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

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_NormalizedPayloadWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Chunk with non-OpenAI finish_reason that needs normalization.
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"end_turn"}]}

data: [DONE]

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// The rewrite path writes: "data: " (Write 0), newPayload (Write 1), "\n" (Write 2)
	// Fail on the newPayload Write (second Write)
	w := &failingResponseWriter{
		failAfter: 1,
		failErr:   errors.New("normalized payload write error"),
	}

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

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_ErrAccumAtEnd(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream that ends with accumulated error bytes (no non-error line to flush)
	streamData := `data: {"error":{"message":"rate limit"}}

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

	// Error should be logged and state should be failed
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "rate limit") {
		t.Errorf("expected error message to contain 'rate limit', got %q", logData.errorMessage)
	}
}

func TestHandleStreamingResponse_ScannerError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Body reader that returns an error
	errReader := &errorReader{err: errors.New("scanner read error")}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errReader),
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

	// scanner.Err() should be captured
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "scanner read error") {
		t.Errorf("expected error message to contain 'scanner read error', got %q", logData.errorMessage)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions tests
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_NonDataLineFlushesErrAccum(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Error line followed by non-data line (comment)
	streamData := `data: {"error":{"message":"rate limit"}}

: comment line

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

	// Error should be captured from errAccum
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_LeadingWhitespaceTrimmed(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with leading whitespace on data lines (Gemini-style)
	streamData := "\r\n\r\ndata: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

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
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}

func TestHandleStreamingResponse_UsageCaptured(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

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

	if logData.tokensPrompt != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", logData.tokensPrompt)
	}
	if logData.tokensCompletion != 5 {
		t.Errorf("expected completion_tokens=5, got %d", logData.tokensCompletion)
	}
}

func TestHandleStreamingResponse_TPSFallbackWhenNoTTFT(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Usage with no reasoning tokens, TTFT=0
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}
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

	startTime := time.Now().Add(-100 * time.Millisecond)
	h.handleStreamingResponse(w, req, logData, resp, startTime, streamOptions{cancelOrigin: "failover_timeout"})

	// When generationDuration <= 0, should fallback to totalDuration
	// TPS = 5 / ~100 * 1000 ≈ 50 (approximate, just verify positive)
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS with no TTFT, got %f", logData.tokensPerSecond)
	}
	if logData.tokensCompletionReasoning != 0 {
		t.Errorf("expected reasoning_tokens=0, got %d", logData.tokensCompletionReasoning)
	}
}

func TestHandleStreamingResponse_InjectsDoneSentinel(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream without [DONE] sentinel
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}]}

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
	// [DONE] should be injected
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] to be injected")
	}
	// State should be completed (injected sentinel is benign)
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_NonDataLineWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with non-data line (SSE comment) followed by [DONE]
	// Scanner splits on \n, so lines are: ": comment", "" (empty), "data: [DONE]", "" (empty)
	// First non-empty non-data line is ": comment". L181 does w.Write(line) where line = ": comment"
	streamData := ": comment\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 0, // fails on first Write (the comment line)
		failErr:   errors.New("non-data line write error"),
	}

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

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_NonDataLineNewlineWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Same stream as above, but fail on the newline Write after the comment line
	// L187 does w.Write([]byte("\n")) after successfully writing the comment line
	streamData := ": comment\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 1, // first Write (comment line) succeeds, second Write (newline) fails
		failErr:   errors.New("non-data line newline write error"),
	}

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

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_ContentHasContent(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Two chunks with same finish_reason. Second has non-empty content,
	// so it should NOT be suppressed (hasContent=true via L368-370).
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"first"},"finish_reason":"stop"}]}

data: {"id":"2","choices":[{"index":0,"delta":{"content":"second"},"finish_reason":"stop"}]}

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
	// Should contain both chunks (second not suppressed because hasContent=true)
	if !strings.Contains(body, "first") {
		t.Error("expected first chunk with content")
	}
	if !strings.Contains(body, "second") {
		t.Error("expected second chunk with content (not suppressed)")
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}

func TestHandleStreamingResponse_ErrAccumAtStreamEnd(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with malformed error JSON that starts with {"error" but is invalid
	// so chunk.Error doesn't fire, leaving errAccum non-empty at stream end (L460-465)
	streamData := `data: {"error":{"message":"rate`

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

	// Error should be captured from errAccum at stream end
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "rate") {
		t.Errorf("expected error message to contain 'rate', got %q", logData.errorMessage)
	}
}

func TestHandleStreamingResponse_InjectedDoneWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream with content but no [DONE] sentinel - handler will inject [DONE]
	// Data line writes: w.Write(line) then w.Write([]byte("\n\n")) = 2 writes
	// Empty line forwarded: w.Write([]byte("\n")) = 1 write
	// Then injected [DONE] is the 4th Write
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}]}

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 3, // first 3 Writes succeed (data line + \n\n + blank line), 4th (injected [DONE]) fails
		failErr:   errors.New("injected done write error"),
	}

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

	// The injected [DONE] write failure is logged but benign - state should still be completed
	// because the stream content was successfully written
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_BlankLineWriteFailure tests the write error path
// for blank lines between SSE events (line 234-237 in proxy.go).
//
// The blank line write at line 234 is only reached when the stream starts
// with empty lines before any data line (skipNextEmptyLine starts as false).
// With triggerAfterBytes=0 the very first write (the "\n" blank line) fails.
func TestHandleStreamingResponse_BlankLineWriteFailure(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Stream starting with an empty line, then data chunk.
	// The empty line is processed first and reaches line 234.
	streamData := "\ndata: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	// Empty line write at line 234: w.Write([]byte("\n")) = 1 byte.
	// We want THIS write to fail. Set triggerAfterBytes=0 so the very
	// first write (the "\n" blank line) fails.
	w := &contentTriggeredWriter{
		triggerAfterBytes: 0,
		failErr:           errors.New("blank line write error"),
	}

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

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

func TestHandleStreamingResponse_SSEEventSeparators(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Standard SSE format: each event is a data line followed by a blank line.
	// eventsource-parser dispatches events on blank lines; without them,
	// all data lines get concatenated into one invalid event.
	streamData := "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n"
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

	// Verify each data line is followed by \n\n (SSE event separator).
	// Split on \n\n to get individual events.
	lines := strings.Split(body, "\n\n")
	dataEvents := 0
	for _, event := range lines {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		if strings.HasPrefix(event, "data: ") {
			dataEvents++
		}
	}
	if dataEvents < 3 {
		t.Errorf("expected at least 3 data events (2 content + 1 [DONE]), got %d; body=%q", dataEvents, body)
	}

	// Verify no two consecutive data lines without a blank line separator.
	// This would indicate the bug where empty lines were being skipped.
	if strings.Contains(body, "}\ndata:") {
		t.Errorf("found consecutive data lines without blank line separator; body=%q", body)
	}
}

func TestHandleStreamingResponse_SSENoTripleNewlines(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Regression test: when the proxy forwarded SSE data, it emitted
	// triple newlines (\n\n\n) between events because both the data-line
	// write path (which writes \n\n) and the empty-line handler (which
	// writes \n) fired for the same upstream separator. Warp.dev's Go
	// backend (openai-go ssestream) breaks on the extra empty event.
	streamData := "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n"
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

	// The output must NOT contain triple newlines between events.
	if strings.Contains(body, "\n\n\n") {
		t.Errorf("SSE output contains triple newlines (\\n\\n\\n), which breaks openai-go ssestream; body=%q", body)
	}

	// Verify each data line is terminated with exactly \n\n.
	// Split the body by \n\n and verify each segment is either a
	// valid data line or empty.
	events := strings.Split(body, "\n\n")
	dataEvents := 0
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		if strings.HasPrefix(event, "data: ") {
			dataEvents++
		} else {
			t.Errorf("unexpected non-data segment in SSE output: %q", event)
		}
	}
	if dataEvents < 3 {
		t.Errorf("expected at least 3 data events (2 content + 1 [DONE]), got %d; body=%q", dataEvents, body)
	}
}

// ---------------------------------------------------------------------------
// Tests for "skip invalid JSON chunks" logic in streaming handler
// ---------------------------------------------------------------------------

// TestHandleStreamingResponse_InvalidJSONChunkSkipped tests that truncated JSON
// in a data line is skipped and NOT forwarded to the client.
func TestHandleStreamingResponse_InvalidJSONChunkSkipped(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: valid chunk, truncated chunk, another valid chunk, then [DONE]
	streamData := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"World"},"finish_reason":null}]}

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

	// Verify stream completed successfully
	assert.Equal(t, "completed", logData.state)

	// Verify both valid chunks appear in output
	assert.Contains(t, body, `"content":"Hello"`)
	assert.Contains(t, body, `"content":"World"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "data: [DONE]")

	// Verify the truncated chunk is NOT in output
	assert.NotContains(t, body, `{"choices":[{"delta":`)
}

// TestHandleStreamingResponse_InvalidJSONChunkMidStream tests the Warp.dev
// scenario: upstream truncates a chunk mid-stream.
func TestHandleStreamingResponse_InvalidJSONChunkMidStream(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: 3 valid chunks with content, then a truncated chunk, then [DONE]
	streamData := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-3","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content"

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

	// Verify stream completed successfully
	assert.Equal(t, "completed", logData.state)

	// Verify the 3 valid chunks are in output
	assert.Contains(t, body, `"content":"Hello"`)
	assert.Contains(t, body, `"content":" world"`)
	assert.Contains(t, body, `"content":"!"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "data: [DONE]")

	// Verify the truncated chunk is NOT in output
	assert.NotContains(t, body, `"choices":[{"delta":{"content"`)

	// Verify no broken JSON in output (every data: line contains valid JSON or [DONE])
	lines := strings.Split(body, "\n\n")
	for _, event := range lines {
		event = strings.TrimSpace(event)
		if event == "" || !strings.HasPrefix(event, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(event, "data: ")
		if payload == "[DONE]" {
			continue
		}
		assert.True(t, json.Valid([]byte(payload)), "Every data line should contain valid JSON, got: %s", payload)
	}
}

// TestHandleStreamingResponse_MultipleInvalidChunksAllSkipped tests that multiple
// consecutive invalid chunks are all skipped.
func TestHandleStreamingResponse_MultipleInvalidChunksAllSkipped(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: valid chunk, 3 consecutive truncated chunks, another valid chunk, then [DONE]
	streamData := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"First"},"finish_reason":null}]}

data: {"id":"x","choices":[{"delta"

data: {"choices":[{"delta":

data: {"id":"y","choices":[{"delta":{"content"

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Last"},"finish_reason":null}]}

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

	// Verify stream completed successfully
	assert.Equal(t, "completed", logData.state)

	// Verify only the 2 valid chunks and [DONE] appear in output
	assert.Contains(t, body, `"content":"First"`)
	assert.Contains(t, body, `"content":"Last"`)
	assert.Contains(t, body, "data: [DONE]")

	// Verify none of the truncated chunks appear in output
	assert.NotContains(t, body, `"choices":[{"delta"`)
}

// TestHandleStreamingResponse_InvalidChunkWithoutDataPrefix tests that non-data
// lines (e.g., event:, comments) are NOT affected by the invalid JSON skip logic.
func TestHandleStreamingResponse_InvalidChunkWithoutDataPrefix(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: valid chunk, SSE comment line, valid chunk, then [DONE]
	streamData := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

: this is a comment

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"World"},"finish_reason":null}]}

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

	// Verify stream completed successfully
	assert.Equal(t, "completed", logData.state)

	// Verify both valid chunks and the comment line appear in output
	assert.Contains(t, body, `"content":"Hello"`)
	assert.Contains(t, body, `"content":"World"`)
	assert.Contains(t, body, ": this is a comment")
	assert.Contains(t, body, "data: [DONE]")
}

// TestHandleStreamingResponse_AllChunksInvalid tests the edge case where all
// data chunks are invalid JSON (e.g., upstream crashes immediately).
func TestHandleStreamingResponse_AllChunksInvalid(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: 3 truncated data lines, then [DONE]
	streamData := `data: {"choices":[{"delta":

data: {"id":"x","choices":[{"delta"

data: {"id":"y","choices":[{"delta":{"content"

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

	// Verify stream completes (it has [DONE])
	assert.Equal(t, "completed", logData.state)

	// Verify output only has [DONE], no broken JSON
	assert.Contains(t, body, "data: [DONE]")

	// Verify no truncated chunks appear in output
	assert.NotContains(t, body, `"choices":[{"delta":`)
	assert.NotContains(t, body, `"id":"x"`)
	assert.NotContains(t, body, `"id":"y"`)

	// Verify no content data events (only [DONE])
	assert.NotContains(t, body, `"content"`)
}

// TestHandleStreamingResponse_ValidJSONForwardedUnchanged is a regression test:
// valid JSON that doesn't need normalization still gets forwarded via the !written path.
func TestHandleStreamingResponse_ValidJSONForwardedUnchanged(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: a valid chunk with standard format (no normalization needed), then [DONE]
	validChunk := `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`
	streamData := "data: " + validChunk + "\n\n" + "data: [DONE]\n\n"

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

	// Verify stream completed successfully
	assert.Equal(t, "completed", logData.state)

	// Verify the chunk appears in output identically (data prefix + JSON + \n\n separators)
	assert.Contains(t, body, "data: "+validChunk)

	// Verify [DONE] is present
	assert.Contains(t, body, "data: [DONE]")

	// Verify JSON is valid
	var chunk map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(validChunk), &chunk))
}

// TestHandleStreamingResponse_InvalidErrorChunkSkippedButLogged tests the
// interaction between P1-B error accumulation and the invalid JSON skip logic.
// When a truncated error-prefix chunk arrives, it should be accumulated into
// errAccum (for logging) and NOT forwarded to the client. When a subsequent
// non-error line arrives, the accumulated error should be flushed and logged.
func TestHandleStreamingResponse_InvalidErrorChunkSkippedButLogged(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream sends: valid chunk, truncated error chunk (P1-B), valid chunk, then [DONE].
	// The truncated error chunk starts with {"error" so P1-B accumulates it,
	// but json.Unmarshal fails because it's truncated. The skip block should
	// prevent it from being forwarded while P1-B still captures the error.
	streamData := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"error":{"message":"Rate limit

data: {"id":"chatcmpl-2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"World"},"finish_reason":null}]}

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

	// Verify stream ended in "failed" state because the upstream error was
	// captured by P1-B accumulation (even though the stream itself completed).
	assert.Equal(t, "failed", logData.state)

	// Verify both valid chunks appear in output
	assert.Contains(t, body, `"content":"Hello"`)
	assert.Contains(t, body, `"content":"World"`)

	// Verify [DONE] is present
	assert.Contains(t, body, "data: [DONE]")

	// Verify the truncated error chunk is NOT forwarded to the client
	assert.NotContains(t, body, `"error":{"message":"Rate limit`)

	// Verify the P1-B error accumulation captured the error message.
	// parseAccumulatedError uses a heuristic for incomplete JSON,
	// so we check that errorMessage is non-empty.
	assert.NotEmpty(t, logData.errorMessage, "P1-B should have captured the truncated error chunk")
}
