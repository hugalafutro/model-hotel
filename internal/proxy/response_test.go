package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockVirtualKeyRepo is a simple in-memory mock for testing AddTokens calls
type mockVirtualKeyRepo struct {
	addTokensCalls []addTokensCall
}

type addTokensCall struct {
	keyHash string
	tokens  int
}

func (m *mockVirtualKeyRepo) AddTokens(ctx context.Context, keyHash string, tokens int) error {
	m.addTokensCalls = append(m.addTokensCalls, addTokensCall{keyHash: keyHash, tokens: tokens})
	return nil
}

func (m *mockVirtualKeyRepo) TouchLastUsed(ctx context.Context, keyHash string) error {
	return nil
}

func (m *mockVirtualKeyRepo) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKeyInfo, error) {
	return &VirtualKeyInfo{ID: "test-id", Name: "test-key"}, nil
}

func (m *mockVirtualKeyRepo) Create(ctx context.Context, name, keyHash, keyPreview string) (*VirtualKeyInfo, error) {
	return &VirtualKeyInfo{ID: "test-id", Name: name, KeyHash: keyHash, KeyPreview: keyPreview}, nil
}

func (m *mockVirtualKeyRepo) Delete(ctx context.Context, id string) error {
	return nil
}

// TestHandleNonStreamingResponse_Success tests the happy path with a valid
// ChatCompletionResponse JSON body
func TestHandleNonStreamingResponse_Success(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 1)

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
	if h == nil {
		t.Skip("database not available")
	}
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
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 1)

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
	if h == nil {
		t.Skip("database not available")
	}
	defer stopUnitHandler(h)

	upstreamBody := "invalid json response"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 1)

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
	if h == nil {
		t.Skip("database not available")
	}
	defer stopUnitHandler(h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString("")),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 1)

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
	if h == nil {
		t.Skip("database not available")
	}
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
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, vkHash, 1)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)

	require.Len(t, mockVKRepo.addTokensCalls, 1, "AddTokens should be called once")
	assert.Equal(t, vkHash, mockVKRepo.addTokensCalls[0].keyHash)
	assert.Equal(t, 15, mockVKRepo.addTokensCalls[0].tokens)

	assert.Equal(t, "completed", logData.state)
}
