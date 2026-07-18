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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// failingResponseWriter implements http.ResponseWriter and http.Flusher
// that fails after N successful writes.
type failingResponseWriter struct {
	header    http.Header
	code      int
	writes    int
	failAfter int
	failErr   error
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingResponseWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes > w.failAfter {
		return 0, w.failErr
	}
	return len(b), nil
}

func (w *failingResponseWriter) WriteHeader(code int) {
	w.code = code
}

func (w *failingResponseWriter) Flush() {}

// errorReader implements io.Reader that always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

// mockVirtualKeyRepo is a simple in-memory mock for testing AddTokens calls
type mockVirtualKeyRepo struct {
	addTokensCalls []addTokensCall
	addTokensErr   error // if set, AddTokens returns this error
}

type addTokensCall struct {
	keyHash string
	tokens  int
}

func (m *mockVirtualKeyRepo) AddTokens(ctx context.Context, keyHash string, tokens int) error {
	m.addTokensCalls = append(m.addTokensCalls, addTokensCall{keyHash: keyHash, tokens: tokens})
	if m.addTokensErr != nil {
		return m.addTokensErr
	}
	return nil
}

func (m *mockVirtualKeyRepo) TouchLastUsed(ctx context.Context, keyHash string) error {
	return nil
}

func (m *mockVirtualKeyRepo) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
	return &VirtualKeyInfo{ID: "test-id", Name: "test-key"}, nil
}

func (m *mockVirtualKeyRepo) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKeyInfo, error) {
	return &VirtualKeyInfo{ID: "test-id", Name: name, KeyHash: keyHash, KeyPreview: keyPreview}, nil
}

func (m *mockVirtualKeyRepo) Delete(ctx context.Context, id string) error {
	return nil
}

// withStripReasoningContext adds auth context and strip_reasoning flag to request
func withStripReasoningContext(r *http.Request, enabled bool) *http.Request {
	r = withAuthContext(r)
	ctx := context.WithValue(r.Context(), ctxkeys.VirtualKeyStripReasoningKey, enabled)
	return r.WithContext(ctx)
}

// buildSSEBody joins SSE data lines with \n\n separators
func buildSSEBody(lines ...string) io.Reader {
	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString("data: ")
		sb.WriteString(line)
		sb.WriteString("\n\n")
	}
	return strings.NewReader(sb.String())
}

// TestHandleNonStreamingResponse_Success tests the happy path with a valid
// ChatCompletionResponse JSON body
func TestHandleNonStreamingResponse_Success(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Create a successful upstream response
	upstreamBody := `{
		"id": "chatcmpl-test",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-3.5-turbo",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello, world!"
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-3.5-turbo",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-test", decodedResp.ID)
	assert.Equal(t, "gpt-3.5-turbo", decodedResp.Model)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello, world!", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, 10, decodedResp.Usage.PromptTokens)
	assert.Equal(t, 5, decodedResp.Usage.CompletionTokens)

	assert.Equal(t, "completed", logData.state)
	assert.Equal(t, http.StatusOK, logData.statusCode)
}

// TestHandleNonStreamingResponse_Non200Status tests handling of upstream
// responses with non-200 status codes. The JSON parses successfully but
// represents an error response from the upstream.
func TestHandleNonStreamingResponse_Non200Status(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Non-200 status with valid JSON structure (upstream error response)
	upstreamBody := `{
		"error": {
			"message": "Invalid request",
			"type": "invalid_request_error"
		}
	}`
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-3.5-turbo",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	// The function writes the response as-is when JSON parses successfully
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err)

	// The response will have empty fields since upstream returned error format
	assert.Equal(t, "", decodedResp.ID)
	assert.Equal(t, "", decodedResp.Model)

	// Log should show completed state (JSON parsed successfully)
	assert.Equal(t, "completed", logData.state)
	assert.Equal(t, http.StatusBadRequest, logData.statusCode)
}

// TestHandleNonStreamingResponse_InvalidJSON tests handling of 200 status
// with invalid JSON body
func TestHandleNonStreamingResponse_InvalidJSON(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := "invalid json response"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-3.5-turbo",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var responseBody map[string]any
	err := json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	errorObj, ok := responseBody["error"].(map[string]any)
	require.True(t, ok, "Should have error object in response")
	assert.Contains(t, errorObj["message"], "upstream provider returned HTTP 200")

	assert.Equal(t, "failed", logData.state)
	assert.Contains(t, logData.errorMessage, "response decode error")
	// Note: "invalid json response" may be truncated/omitted by SanitizeLogBody
	// depending on the exact error message format
}

// TestHandleNonStreamingResponse_EmptyBody tests handling of 200 status
// with empty body
func TestHandleNonStreamingResponse_EmptyBody(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString("")),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-3.5-turbo",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var responseBody map[string]any
	err := json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	errorObj, ok := responseBody["error"].(map[string]any)
	require.True(t, ok, "Should have error object in response")
	assert.Contains(t, errorObj["message"], "upstream provider returned HTTP 200")

	assert.Equal(t, "failed", logData.state)
	assert.Contains(t, logData.errorMessage, "response decode error")
}

// TestHandleNonStreamingResponse_WithVirtualKeyHash tests that AddTokens is
// called when vkHash is non-empty
func TestHandleNonStreamingResponse_WithVirtualKeyHash(t *testing.T) {
	mockVKRepo := &mockVirtualKeyRepo{}
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	// Replace virtualKeyRepo with mock for this test
	h.virtualKeyRepo = mockVKRepo

	upstreamBody := `{
		"id": "chatcmpl-test",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-3.5-turbo",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello, world!"
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-3.5-turbo",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	vkHash := "test-vk-hash-abc123"
	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, vkHash, 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)

	require.Len(t, mockVKRepo.addTokensCalls, 1, "AddTokens should be called once")
	assert.Equal(t, vkHash, mockVKRepo.addTokensCalls[0].keyHash)
	assert.Equal(t, 15, mockVKRepo.addTokensCalls[0].tokens)

	assert.Equal(t, "completed", logData.state)
}

// TestHandleNonStreamingResponse_WithReasoningContent tests that
// reasoning_content in the message is preserved through re-serialization.
func TestHandleNonStreamingResponse_WithReasoningContent(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{
		"id": "chatcmpl-reasoning",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "deepseek-reasoner",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The answer is 42.",
				"reasoning_content": "Let me think about this step by step..."
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "deepseek-reasoner",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "deepseek-reasoner", decodedResp.Model)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "The answer is 42.", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, "Let me think about this step by step...", decodedResp.Choices[0].Message.ReasoningContent)

	assert.Equal(t, "completed", logData.state)
}

// ---------------------------------------------------------------------------
// Reasoning field normalization tests (non-streaming)
// ---------------------------------------------------------------------------

// TestHandleNonStreamingResponse_ReasoningFieldNormalized tests that
// message.reasoning (Ollama-style) is normalized to reasoning_content.
func TestHandleNonStreamingResponse_ReasoningFieldNormalized(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{
		"id": "chatcmpl-ollama",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "llama3",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The answer is 42.",
				"reasoning": "I thought about it"
			}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "llama3",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-ollama", decodedResp.ID)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "The answer is 42.", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, "I thought about it", decodedResp.Choices[0].Message.ReasoningContent)

	assert.Equal(t, "completed", logData.state)
}

// TestHandleNonStreamingResponse_ReasoningDetailsNormalized tests that
// message.reasoning_details text entries (OpenRouter-style) are normalized
// to reasoning_content.
func TestHandleNonStreamingResponse_ReasoningDetailsNormalized(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{
		"id": "chatcmpl-openrouter",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gemini-2.5-pro",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The answer.",
				"reasoning_details": [
					{"type": "reasoning.text", "text": "Step 1: Analyze", "format": "google-gemini-v1"},
					{"type": "reasoning.encrypted", "text": "", "format": "anthropic-claude-v1"}
				]
			}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gemini-2.5-pro",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-openrouter", decodedResp.ID)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "The answer.", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, "Step 1: Analyze", decodedResp.Choices[0].Message.ReasoningContent)

	assert.Equal(t, "completed", logData.state)
}

// TestHandleNonStreamingResponse_ThinkingTagsNormalized tests that
// <thinking> tags in message.content (MiniMax native-style) are extracted
// to reasoning_content with remaining text in content.
func TestHandleNonStreamingResponse_ThinkingTagsNormalized(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{
		"id": "chatcmpl-minimax",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "MiniMax-Text-01",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "<thinking>Hidden reasoning</thinking>Visible answer"
			}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "MiniMax-Text-01",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-minimax", decodedResp.ID)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "Hidden reasoning", decodedResp.Choices[0].Message.ReasoningContent)
	assert.Equal(t, "Visible answer", decodedResp.Choices[0].Message.Content)

	assert.Equal(t, "completed", logData.state)
}

// ---------------------------------------------------------------------------
// handleNonStreamingResponse tests
// ---------------------------------------------------------------------------

func TestHandleNonStreamingResponse_EncodeError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{
		"id": "chatcmpl-test",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-3.5-turbo",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello"
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 0,
		failErr:   errors.New("encode write error"),
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "gpt-3.5-turbo",
		providerID:     uuid.New(),
		streaming:      false,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	// Encode error is logged but not propagated; state was already set to "completed" before the write attempt
	// State should be completed since JSON parsed successfully
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// contentTriggeredWriter succeeds on all writes until it has written a
// cumulative total of triggerAfterBytes bytes, then fails.
// Note: when a write crosses the threshold, the full len(b) is counted
// against w.written but (0, err) is returned. This differs from real
// writers that may return n < len(b) on partial writes. The proxy code
// never retries writes or uses the returned n after an error, so this
// simplification is safe for the current test scenarios.
type contentTriggeredWriter struct {
	header            http.Header
	code              int
	written           int
	triggerAfterBytes int
	failErr           error
}

func (w *contentTriggeredWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *contentTriggeredWriter) Write(b []byte) (int, error) {
	w.written += len(b)
	if w.written > w.triggerAfterBytes {
		return 0, w.failErr
	}
	return len(b), nil
}

func (w *contentTriggeredWriter) WriteHeader(code int) { w.code = code }
func (w *contentTriggeredWriter) Flush()               {}

func TestHandleNonStreamingResponse_AddTokensCalled(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	mockVKRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = mockVKRepo

	upstreamBody := `{
		"id": "chatcmpl-test",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-3.5-turbo",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello"
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:        "gpt-3.5-turbo",
		providerID:     uuid.New(),
		streaming:      false,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	logData.insertWg.Add(1)

	vkHash := "test-vk-hash"
	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, vkHash, 1)

	if len(mockVKRepo.addTokensCalls) != 1 {
		t.Errorf("expected AddTokens to be called once, got %d calls", len(mockVKRepo.addTokensCalls))
	} else {
		if mockVKRepo.addTokensCalls[0].keyHash != vkHash {
			t.Errorf("expected keyHash=%q, got %q", vkHash, mockVKRepo.addTokensCalls[0].keyHash)
		}
		if mockVKRepo.addTokensCalls[0].tokens != 15 {
			t.Errorf("expected tokens=15, got %d", mockVKRepo.addTokensCalls[0].tokens)
		}
	}
}

func TestNewRequestWithContextVar(t *testing.T) {
	// Test that the injectable var can be overridden
	origNewRequestWithContext := newRequestWithContext
	defer func() { newRequestWithContext = origNewRequestWithContext }()

	called := false
	newRequestWithContext = func(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
		called = true
		return http.NewRequestWithContext(ctx, method, url, body)
	}

	req, err := newRequestWithContext(context.Background(), "GET", "http://example.com", http.NoBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req == nil {
		t.Fatal("expected non-nil request")
		return
	}
	if !called {
		t.Error("expected injectable function to be called")
	}
}

func TestHandleNonStreamingResponse_AnthropicCacheTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{
		"id": "chatcmpl-anthropic",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "claude-3-5-sonnet-20241022",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello from Claude!"
			}
		}],
		"usage": {
			"prompt_tokens": 200,
			"completion_tokens": 10,
			"total_tokens": 210,
			"cache_read_input_tokens": 150,
			"cache_creation_input_tokens": 20
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "claude-3-5-sonnet-20241022",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-anthropic", decodedResp.ID)
	assert.Equal(t, "claude-3-5-sonnet-20241022", decodedResp.Model)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello from Claude!", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, 200, decodedResp.Usage.PromptTokens)
	assert.Equal(t, 10, decodedResp.Usage.CompletionTokens)
	assert.Equal(t, 150, decodedResp.Usage.CacheReadInputTokens)
	assert.Equal(t, 20, decodedResp.Usage.CacheCreationInputTokens)

	assert.Equal(t, "completed", logData.state)
	assert.Equal(t, http.StatusOK, logData.statusCode)
	assert.Equal(t, 150, logData.tokensPromptCacheHit)
	assert.Equal(t, 50, logData.tokensPromptCacheMiss)
}

func TestHandleNonStreamingResponse_AnthropicCacheNegativeMiss(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// cache_read_input_tokens (300) > prompt_tokens (200) → miss clamped to 0
	upstreamBody := `{
		"id": "chatcmpl-anthropic",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "claude-3-5-sonnet-20241022",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello!"
			}
		}],
		"usage": {
			"prompt_tokens": 200,
			"completion_tokens": 10,
			"total_tokens": 210,
			"cache_read_input_tokens": 300,
			"cache_creation_input_tokens": 20
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "claude-3-5-sonnet-20241022",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, 300, logData.tokensPromptCacheHit)
	assert.Equal(t, 0, logData.tokensPromptCacheMiss)
}

func TestHandleNonStreamingResponse_AnthropicCacheOpenAITakesPrecedence(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Both OpenAI and Anthropic cache fields present - OpenAI should win
	upstreamBody := `{
		"id": "chatcmpl-anthropic",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "claude-3-5-sonnet-20241022",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello!"
			}
		}],
		"usage": {
			"prompt_tokens": 200,
			"completion_tokens": 10,
			"total_tokens": 210,
			"prompt_cache_hit_tokens": 80,
			"cache_read_input_tokens": 150,
			"cache_creation_input_tokens": 20
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "claude-3-5-sonnet-20241022",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, 80, logData.tokensPromptCacheHit, "OpenAI cache hit should take precedence")
	assert.Equal(t, 120, logData.tokensPromptCacheMiss, "miss = prompt_tokens - openai_cache_hit = 200 - 80")
}

func TestUsageAnthropicCacheFieldsDeserialization(t *testing.T) {
	// OpenAI-style cache fields
	openaiJSON := `{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}`
	var openaiUsage Usage
	require.NoError(t, json.Unmarshal([]byte(openaiJSON), &openaiUsage))
	assert.Equal(t, 80, openaiUsage.PromptCacheHitTokens)
	assert.Equal(t, 20, openaiUsage.PromptCacheMissTokens)
	assert.Equal(t, 0, openaiUsage.CacheReadInputTokens)

	// Anthropic-native cache fields
	anthropicJSON := `{"prompt_tokens":100,"completion_tokens":5,"cache_read_input_tokens":60,"cache_creation_input_tokens":10}`
	var anthropicUsage Usage
	require.NoError(t, json.Unmarshal([]byte(anthropicJSON), &anthropicUsage))
	assert.Equal(t, 0, anthropicUsage.PromptCacheHitTokens)
	assert.Equal(t, 60, anthropicUsage.CacheReadInputTokens)
	assert.Equal(t, 10, anthropicUsage.CacheCreationInputTokens)

	// Both present - all fields deserialize independently
	bothJSON := `{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":80,"cache_read_input_tokens":60,"cache_creation_input_tokens":10}`
	var bothUsage Usage
	require.NoError(t, json.Unmarshal([]byte(bothJSON), &bothUsage))
	assert.Equal(t, 80, bothUsage.PromptCacheHitTokens)
	assert.Equal(t, 60, bothUsage.CacheReadInputTokens)
	assert.Equal(t, 10, bothUsage.CacheCreationInputTokens)
}

func TestHandleNonStreamingResponse_PromptTokensDetailsCachedTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Non-streaming response with OpenAI's official nested format
	upstreamBody := `{
		"id": "chatcmpl-openai-cache",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello from cached model!"
			}
		}],
		"usage": {
			"prompt_tokens": 2008,
			"completion_tokens": 266,
			"total_tokens": 2274,
			"prompt_tokens_details": {
				"cached_tokens": 1984
			},
			"completion_tokens_details": {
				"reasoning_tokens": 261
			}
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-4o",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-openai-cache", decodedResp.ID)
	assert.Equal(t, "gpt-4o", decodedResp.Model)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello from cached model!", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, 2008, decodedResp.Usage.PromptTokens)
	assert.Equal(t, 266, decodedResp.Usage.CompletionTokens)
	assert.Equal(t, 1984, decodedResp.Usage.PromptTokensDetails.CachedTokens)

	assert.Equal(t, "completed", logData.state)
	assert.Equal(t, http.StatusOK, logData.statusCode)
	assert.Equal(t, 1984, logData.tokensPromptCacheHit)
	assert.Equal(t, 24, logData.tokensPromptCacheMiss)
}

func TestUsagePromptTokensDetailsDeserialization(t *testing.T) {
	// OpenAI nested format: prompt_tokens_details.cached_tokens
	openaiNestedJSON := `{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"prompt_tokens_details":{"cached_tokens":1984}}`
	var openaiNestedUsage Usage
	require.NoError(t, json.Unmarshal([]byte(openaiNestedJSON), &openaiNestedUsage))
	assert.Equal(t, 1984, openaiNestedUsage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 0, openaiNestedUsage.PromptCacheHitTokens)
	assert.Equal(t, 0, openaiNestedUsage.CacheReadInputTokens)

	// Top-level format: prompt_cache_hit_tokens
	topLevelJSON := `{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}`
	var topLevelUsage Usage
	require.NoError(t, json.Unmarshal([]byte(topLevelJSON), &topLevelUsage))
	assert.Equal(t, 80, topLevelUsage.PromptCacheHitTokens)
	assert.Nil(t, topLevelUsage.PromptTokensDetails)

	// Both present - all fields deserialize independently
	bothJSON := `{"prompt_tokens":2008,"completion_tokens":266,"prompt_cache_hit_tokens":500,"prompt_tokens_details":{"cached_tokens":1984}}`
	var bothUsage Usage
	require.NoError(t, json.Unmarshal([]byte(bothJSON), &bothUsage))
	assert.Equal(t, 500, bothUsage.PromptCacheHitTokens)
	assert.Equal(t, 1984, bothUsage.PromptTokensDetails.CachedTokens)
}

func TestHandleNonStreamingResponse_PromptTokensDetailsNegativeMiss(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Non-streaming response: prompt_tokens: 100, prompt_tokens_details.cached_tokens: 150
	// Verify miss is clamped to 0.
	upstreamBody := `{
		"id": "chatcmpl-negative-miss",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello!"
			}
		}],
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 5,
			"total_tokens": 105,
			"prompt_tokens_details": {
				"cached_tokens": 150
			}
		}
	}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req = withAuthContext(req)

	logData := &requestLogData{
		modelID:         "gpt-4o",
		providerID:      uuid.New(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	startTime := time.Now()
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	var decodedResp ChatCompletionResponse
	err := json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, "chatcmpl-negative-miss", decodedResp.ID)
	assert.Equal(t, "gpt-4o", decodedResp.Model)
	assert.Len(t, decodedResp.Choices, 1)
	assert.Equal(t, "assistant", decodedResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello!", decodedResp.Choices[0].Message.Content)
	assert.Equal(t, 100, decodedResp.Usage.PromptTokens)
	assert.Equal(t, 5, decodedResp.Usage.CompletionTokens)
	assert.Equal(t, 150, decodedResp.Usage.PromptTokensDetails.CachedTokens)

	assert.Equal(t, "completed", logData.state)
	assert.Equal(t, http.StatusOK, logData.statusCode)
	assert.Equal(t, 150, logData.tokensPromptCacheHit)
	assert.Equal(t, 0, logData.tokensPromptCacheMiss)
}
