package proxy

import (
	"encoding/json"
	"fmt"
	"io"
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
		response := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test-model",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hello world"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{
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
		response := map[string]any{
			"id":     "chatcmpl-test",
			"object": "chat.completion",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{
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
	var response map[string]any
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

// zenChatShapedBody is what OpenCode Zen and OpenCode Go answer some failed
// requests with: a complete chat.completion envelope, no error object, no
// content, delivered under a non-2xx status.
const zenChatShapedBody = `{"id":"chatcmpl_u5tt67g6rmf","object":"chat.completion","created":1787135446,"model":"gpt-5.1-codex","choices":[{"index":0,"message":{"role":"assistant"},"finish_reason":null}]}`

// A non-2xx upstream is a failure whatever its body decodes as. The client must
// get an OpenAI error envelope it can read `.error.message` off, not the
// success-shaped body the provider sent.
func TestHandleNonStreamingResponse_ChatShapedNon2xxGetsErrorEnvelope(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(zenChatShapedBody)),
		Header:     make(http.Header),
	}

	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "gpt-5.1-codex",
		providerName:    "opencode-zen",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	if inner.Code != http.StatusBadRequest {
		t.Errorf("expected upstream status %d to be forwarded, got %d", http.StatusBadRequest, inner.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(inner.Body.Bytes(), &body); err != nil {
		t.Fatalf("client body is not JSON: %v (%s)", err, inner.Body.String())
	}
	rawErr, present := body["error"]
	if !present {
		t.Fatalf("client body has no error object: %s", inner.Body.String())
	}
	var errObj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rawErr, &errObj); err != nil {
		t.Fatalf("re-parse error object: %v", err)
	}
	if errObj.Message == "" {
		t.Errorf("error envelope carries no message: %s", inner.Body.String())
	}
	if _, present := body["choices"]; present {
		t.Errorf("success-shaped choices forwarded on a non-2xx: %s", inner.Body.String())
	}

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
	if logData.errorKind == "" {
		t.Error("expected a classified error_kind, got empty")
	}
	if !strings.Contains(logData.errorMessage, "chatcmpl_u5tt67g6rmf") {
		t.Errorf("upstream body not recoverable from error_message: %q", logData.errorMessage)
	}
}

// The same body under a 200 is an ordinary completion and must pass through
// untouched: same choices, same metering, same log state.
func TestHandleNonStreamingResponse_ChatShaped2xxPassesThrough(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(zenChatShapedBody)),
		Header:     make(http.Header),
	}

	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "gpt-5.1-codex",
		providerName:    "opencode-zen",
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
	var body map[string]json.RawMessage
	if err := json.Unmarshal(inner.Body.Bytes(), &body); err != nil {
		t.Fatalf("client body is not JSON: %v (%s)", err, inner.Body.String())
	}
	if _, present := body["error"]; present {
		t.Errorf("2xx completion wrapped in an error envelope: %s", inner.Body.String())
	}
	if _, present := body["choices"]; !present {
		t.Errorf("2xx completion lost its choices: %s", inner.Body.String())
	}
	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
}

// Metering is the other half of the contract: a failed request has no
// completion to charge for, so neither the log counters nor the virtual key's
// token balance may move even when the upstream attaches a usage block.
func TestHandleNonStreamingResponse_ChatShapedNon2xxDoesNotMeter(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	const withUsage = `{"id":"chatcmpl_u5tt67g6rmf","object":"chat.completion","created":1787135446,"model":"gpt-5.1-codex","choices":[{"index":0,"message":{"role":"assistant"},"finish_reason":null}],"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}`

	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(withUsage)),
		Header:     make(http.Header),
	}

	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "gpt-5.1-codex",
		providerName:    "opencode-zen",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	if logData.tokensPrompt != 0 || logData.tokensCompletion != 0 {
		t.Errorf("metered a failed request: prompt=%d completion=%d", logData.tokensPrompt, logData.tokensCompletion)
	}
	if len(vkRepo.addTokensCalls) != 0 {
		t.Errorf("charged the virtual key for a failed request: %+v", vkRepo.addTokensCalls)
	}
	if logData.tokensPerSecond != 0 {
		t.Errorf("recorded a throughput figure for a failed request: %v", logData.tokensPerSecond)
	}
	// The usage block is what makes this assertion bite: chatAnswerCarriesContent
	// falls back to Usage.CompletionTokens > 0 when no choice carries content, so
	// a body with completion_tokens 22 is the one shape that would mark a failed
	// request as having delivered content. deliveredContent feeds producedOutput
	// at the call site, where it clears a model's gone-strike streak.
	if logData.deliveredContent {
		t.Error("a non-2xx must not count as the model having served content")
	}
}
