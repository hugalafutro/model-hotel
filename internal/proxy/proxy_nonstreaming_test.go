package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Use testDB from proxy_test.go

func TestHandleNonStreamingResponse_Success_Integration(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that returns a successful response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test-model",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello world"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 7,
				"total_tokens":      12,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":false,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	if inner.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, inner.Code)
	}
	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	if logData.tokensPrompt != 5 {
		t.Errorf("expected prompt tokens 5, got %d", logData.tokensPrompt)
	}
}

// TestHandleNonStreamingResponse_PromptCacheHitTokens tests that prompt_cache_hit_tokens
// in the usage object is correctly extracted and cache miss is calculated.
// Covers lines 572-575 in proxy.go.

func TestHandleNonStreamingResponse_PromptCacheHitTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that returns a response with prompt_cache_hit_tokens
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"id":     "chatcmpl-test",
			"object": "chat.completion",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":           100,
				"completion_tokens":       5,
				"total_tokens":            105,
				"prompt_cache_hit_tokens": 80,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":false,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	if logData.tokensPromptCacheHit != 80 {
		t.Errorf("expected tokensPromptCacheHit=80, got %d", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 20 {
		t.Errorf("expected tokensPromptCacheMiss=20, got %d", logData.tokensPromptCacheMiss)
	}
}

// TestHandleStreamingResponse_Basic tests basic streaming response handling
// This test verifies that the streaming handler processes chunks and updates logs

func TestHandleNonStreamingResponse_NonJSONError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that returns a non-JSON 500 error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal Server Error")
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":false,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	// Verify status code is 500 (preserved from upstream)
	if inner.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, inner.Code)
	}

	// Verify response is valid JSON (OpenAI error format)
	var response map[string]interface{}
	if err := json.Unmarshal(inner.Body.Bytes(), &response); err != nil {
		t.Errorf("expected response to be valid JSON, got:\n%s\nerror: %v", inner.Body.String(), err)
	}

	// Verify it has the OpenAI error structure
	if response["error"] == nil {
		t.Errorf("expected response to have 'error' field, got:\n%s", inner.Body.String())
	}

	// Verify log state is failed
	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// TestHandleStreamingResponse_NonErrorAnthropicEvent tests handling of
// "event: ping" (non-error event) followed by normal data chunk.
// Covers lines 162-164 where lastAnthropicEvent = "" for non-"error" events.

func TestHandleNonStreamingResponse_AddTokensError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	h.virtualKeyRepo = &mockVirtualKeyRepo{addTokensErr: fmt.Errorf("db connection refused")}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":1234567890,"model":"gpt-3.5-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`)
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	if logData.tokensPrompt != 5 || logData.tokensCompletion != 7 {
		t.Errorf("expected tokens 5/7, got %d/%d", logData.tokensPrompt, logData.tokensCompletion)
	}
}

// TestHandleStreamingResponse_ClientWriteFailureOnDataLine tests that a write
// failure on a data line (not [DONE]) marks the client as disconnected.
// Covers lines 178-187 in proxy.go.
