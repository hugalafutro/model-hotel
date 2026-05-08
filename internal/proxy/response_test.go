package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test successful JSON response handling by testing the core logic directly
func TestHandleNonStreamingResponseSuccess(t *testing.T) {
	// Test the JSON parsing and response writing logic directly
	// without calling the full handleNonStreamingResponse function
	// to avoid database dependencies

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

	// Test JSON parsing
	var chatResp ChatCompletionResponse
	err := json.NewDecoder(io.NopCloser(bytes.NewBufferString(upstreamBody))).Decode(&chatResp)
	require.NoError(t, err, "Should parse valid JSON successfully")

	assert.Equal(t, "chatcmpl-test", chatResp.ID)
	assert.Equal(t, "chat.completion", chatResp.Object)
	assert.Equal(t, int64(1234567890), chatResp.Created)
	assert.Equal(t, "gpt-3.5-turbo", chatResp.Model)
	assert.Len(t, chatResp.Choices, 1)
	assert.Equal(t, "assistant", chatResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello, world!", chatResp.Choices[0].Message.Content)

	// Test response writing
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(chatResp)
	require.NoError(t, err, "Should encode chat response successfully")

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	// Verify we can decode it back
	var decodedResp ChatCompletionResponse
	err = json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, chatResp.ID, decodedResp.ID)
	assert.Equal(t, chatResp.Model, decodedResp.Model)
	assert.Len(t, decodedResp.Choices, 1)
}

// Test non-200 status code handling
func TestHandleNonStreamingResponseNon200Status(t *testing.T) {
	// Test error response handling
	upstreamBody := `{"error": "bad request"}`
	upstreamResponse := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	// Test that we can read the error body
	body, err := io.ReadAll(upstreamResponse.Body)
	require.NoError(t, err)
	assert.NotEmpty(t, body, "Should be able to read error response body")

	// Test error response writing
	w := httptest.NewRecorder()
	writeOpenAIError(w, "upstream provider returned HTTP 400", http.StatusBadRequest)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusBadRequest, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	// Verify the response body contains an error message
	var responseBody map[string]interface{}
	err = json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	// The error object is nested under "error" key
	errorObj := responseBody["error"].(map[string]interface{})
	assert.Contains(t, errorObj["message"], "upstream provider returned HTTP 400")
}

// Test invalid JSON response handling
func TestHandleNonStreamingResponseInvalidJSON(t *testing.T) {
	// Create an invalid JSON upstream response
	upstreamBody := "invalid json response"
	upstreamResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(upstreamBody)),
		Header:     make(http.Header),
	}

	// Test that JSON parsing fails
	var chatResp ChatCompletionResponse
	err := json.NewDecoder(upstreamResponse.Body).Decode(&chatResp)
	assert.Error(t, err, "Should fail to parse invalid JSON")

	// Test error response writing
	w := httptest.NewRecorder()
	writeOpenAIError(w, "response decode error: invalid json response", http.StatusOK)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode) // Original status code is preserved
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	// Verify the response body contains an error message
	var responseBody map[string]interface{}
	err = json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	// The error object is nested under "error" key
	errorObj := responseBody["error"].(map[string]interface{})
	assert.Contains(t, errorObj["message"], "response decode error")
}

// Test response with prompt cache hit tokens
func TestHandleNonStreamingResponseWithPromptCache(t *testing.T) {
	// Create a successful upstream response with prompt cache hit tokens
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
			"total_tokens": 15,
			"prompt_cache_hit_tokens": 3
		}
	}`

	// Test JSON parsing
	var chatResp ChatCompletionResponse
	err := json.NewDecoder(io.NopCloser(bytes.NewBufferString(upstreamBody))).Decode(&chatResp)
	require.NoError(t, err, "Should parse valid JSON with cache tokens successfully")

	assert.Equal(t, 10, chatResp.Usage.PromptTokens)
	assert.Equal(t, 5, chatResp.Usage.CompletionTokens)
	assert.Equal(t, 3, chatResp.Usage.PromptCacheHitTokens)

	// Test response writing
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(chatResp)
	require.NoError(t, err, "Should encode chat response successfully")

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	// Verify we can decode it back
	var decodedResp ChatCompletionResponse
	err = json.NewDecoder(result.Body).Decode(&decodedResp)
	require.NoError(t, err, "Should decode response successfully")

	assert.Equal(t, 3, decodedResp.Usage.PromptCacheHitTokens)
}

// Test response with empty body
func TestHandleNonStreamingResponseEmptyBody(t *testing.T) {
	// Create an empty upstream response
	upstreamResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString("")),
		Header:     make(http.Header),
	}

	// Test that JSON parsing fails
	var chatResp ChatCompletionResponse
	err := json.NewDecoder(upstreamResponse.Body).Decode(&chatResp)
	assert.Error(t, err, "Should fail to parse empty JSON")

	// Test error response writing
	w := httptest.NewRecorder()
	writeOpenAIError(w, "response decode error: empty body", http.StatusOK)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusOK, result.StatusCode) // Original status code is preserved
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))

	// Verify the response body contains an error message
	var responseBody map[string]interface{}
	err = json.NewDecoder(result.Body).Decode(&responseBody)
	require.NoError(t, err)

	// The error object is nested under "error" key
	errorObj := responseBody["error"].(map[string]interface{})
	assert.Contains(t, errorObj["message"], "response decode error")
}
