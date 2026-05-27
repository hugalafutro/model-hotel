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

func (m *mockVirtualKeyRepo) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*VirtualKeyInfo, error) {
	return &VirtualKeyInfo{ID: "test-id", Name: name, KeyHash: keyHash, KeyPreview: keyPreview}, nil
}

func (m *mockVirtualKeyRepo) Delete(ctx context.Context, id string) error {
	return nil
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

	var responseBody map[string]interface{}
	err := json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	errorObj, ok := responseBody["error"].(map[string]interface{})
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

	var responseBody map[string]interface{}
	err := json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	errorObj, ok := responseBody["error"].(map[string]interface{})
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// clientDisconnected should be set due to write failure
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// scanner.Err() should be captured
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "scanner read error") {
		t.Errorf("expected error message to contain 'scanner read error', got %q", logData.errorMessage)
	}
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// Error should be captured from errAccum
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	body := w.Body.String()
	// BOM should be stripped, response should be valid
	if !strings.Contains(body, "[DONE]") {
		t.Error("expected [DONE] sentinel")
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPrompt != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", logData.tokensPrompt)
	}
	if logData.tokensCompletion != 5 {
		t.Errorf("expected completion_tokens=5, got %d", logData.tokensCompletion)
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// When generationDuration <= 0, should fallback to totalDuration
	// TPS = 5 / ~100 * 1000 ≈ 50 (approximate, just verify positive)
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS with no TTFT, got %f", logData.tokensPerSecond)
	}
	if logData.tokensCompletionReasoning != 0 {
		t.Errorf("expected reasoning_tokens=0, got %d", logData.tokensCompletionReasoning)
	}
}

func TestHandleStreamingResponse_PromptCacheTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 80 {
		t.Errorf("expected prompt_cache_hit=80, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 20 {
		t.Errorf("expected prompt_cache_miss=20, got %d", logData.tokensPromptCacheMiss)
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// The injected [DONE] write failure is logged but benign - state should still be completed
	// because the stream content was successfully written
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	}
	if !called {
		t.Error("expected injectable function to be called")
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	body := w.Body.String()
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected response to contain reasoning_content")
	}
	if !strings.Contains(body, "Already here") {
		t.Errorf("expected reasoning_content to contain 'Already here', got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Anthropic-native cache field fallback tests
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_AnthropicCacheTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"cache_read_input_tokens":60,"cache_creation_input_tokens":10}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 60 {
		t.Errorf("expected prompt_cache_hit=60, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 40 {
		t.Errorf("expected prompt_cache_miss=40, got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_AnthropicCacheOpenAITakesPrecedence(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Both OpenAI and Anthropic cache fields present - OpenAI should win
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"cache_read_input_tokens":60}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 80 {
		t.Errorf("expected prompt_cache_hit=80 (OpenAI takes precedence), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 20 {
		t.Errorf("expected prompt_cache_miss=20, got %d", logData.tokensPromptCacheMiss)
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

// Anthropic cache with cache_read > prompt_tokens (negative miss clamped to 0)

func TestHandleStreamingResponse_AnthropicCacheNegativeMiss(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// cache_read_input_tokens (120) > prompt_tokens (100) → miss clamped to 0
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"cache_read_input_tokens":120,"cache_creation_input_tokens":10}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 120 {
		t.Errorf("expected prompt_cache_hit=120, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 0 {
		t.Errorf("expected prompt_cache_miss=0 (clamped), got %d", logData.tokensPromptCacheMiss)
	}
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

// ---------------------------------------------------------------------------
// PromptTokensDetails.cached_tokens fallback tests (tier 3)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_PromptTokensDetailsCachedTokens(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// OpenAI's official nested format: prompt_tokens_details.cached_tokens
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"prompt_tokens_details":{"cached_tokens":1984},"completion_tokens_details":{"reasoning_tokens":261}}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 1984 {
		t.Errorf("expected prompt_cache_hit=1984 (from prompt_tokens_details.cached_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 24 {
		t.Errorf("expected prompt_cache_miss=24 (2008-1984), got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_AllCacheFormatsPrecedence(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// All three cache formats present - tier 1 (PromptCacheHitTokens) should win
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"prompt_cache_hit_tokens":500,"cache_read_input_tokens":300,"prompt_tokens_details":{"cached_tokens":1984}}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// Tier 1 (PromptCacheHitTokens) should take precedence
	if logData.tokensPromptCacheHit != 500 {
		t.Errorf("expected prompt_cache_hit=500 (tier 1 takes precedence), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 1508 {
		t.Errorf("expected prompt_cache_miss=1508 (2008-500), got %d", logData.tokensPromptCacheMiss)
	}
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

// ---------------------------------------------------------------------------
// Negative cache miss clamping tests (max(0, prompt_tokens - cache_hit))
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_PromptTokensDetailsNegativeMiss(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Upstream returns prompt_tokens: 100 but prompt_tokens_details.cached_tokens: 150
	// (more cached than total prompt). Verify miss is clamped to 0.
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":150}}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 150 {
		t.Errorf("expected prompt_cache_hit=150 (from prompt_tokens_details.cached_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 0 {
		t.Errorf("expected prompt_cache_miss=0 (clamped by max(0, 100-150)), got %d", logData.tokensPromptCacheMiss)
	}
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

func TestHandleStreamingResponse_Tier2OverridesTier3(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Both tier 2 (cache_read_input_tokens) and tier 3 (prompt_tokens_details.cached_tokens) present.
	// Tier 1 is NOT present. Verify tier 2 wins.
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":2008,"completion_tokens":266,"total_tokens":2274,"cache_read_input_tokens":300,"prompt_tokens_details":{"cached_tokens":1984}}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	// Tier 2 (cache_read_input_tokens) should take precedence over tier 3
	if logData.tokensPromptCacheHit != 300 {
		t.Errorf("expected prompt_cache_hit=300 (tier 2: cache_read_input_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 1708 {
		t.Errorf("expected prompt_cache_miss=1708 (2008-300), got %d", logData.tokensPromptCacheMiss)
	}
}

func TestHandleStreamingResponse_Tier1NegativeMiss(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Tier 1 (prompt_cache_hit_tokens: 500) > prompt_tokens: 400.
	// Verify miss is clamped to 0 by max(0, ...).
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":400,"completion_tokens":5,"total_tokens":405,"prompt_cache_hit_tokens":500}}

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "failover_timeout")

	if logData.tokensPromptCacheHit != 500 {
		t.Errorf("expected prompt_cache_hit=500 (from prompt_cache_hit_tokens), got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 0 {
		t.Errorf("expected prompt_cache_miss=0 (clamped by max(0, 400-500)), got %d", logData.tokensPromptCacheMiss)
	}
}
