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

// ---------------------------------------------------------------------------
// What a failed decode may put in the logs
// ---------------------------------------------------------------------------

// paddingReader is an endless source of one filler byte, so an oversized
// upstream body can be produced without allocating it test-side.
type paddingReader struct{}

func (paddingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func nonStreamingLogData() *requestLogData {
	return &requestLogData{
		modelID:         "test-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}
}

// A 2xx whose body fails to decode is still a completion: it holds the model's
// generated text. No prompt or response content is ever logged, so the row may
// carry only the decode diagnostics. errorMessage is dashboard-visible and
// stored, and it is the same string handed to the debug log, so asserting on it
// covers both sinks.
// Every 2xx, not just 200: the metering fix is what made 201/202 reach this
// handler at all, so the no-content rule became load-bearing for statuses it
// had never had to cover.
func TestHandleNonStreamingResponse_Undecodable2xxDoesNotLogContent(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			undecodable2xxDoesNotLogContent(t, status)
		})
	}
}

func undecodable2xxDoesNotLogContent(t *testing.T, status int) {
	t.Helper()
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// A real shape from a relay: valid completion, "created" sent as a string.
	// The decode fails on the type; the assistant's answer is right beside it.
	const secret = "the-user-private-answer-text"
	body := `{"id":"chatcmpl-1","object":"chat.completion","created":"1699999999","model":"m",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"` + secret + `"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`

	resp := &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := nonStreamingLogData()

	h.handleNonStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	if logData.state != "failed" {
		t.Fatalf("state = %q, want failed", logData.state)
	}
	if strings.Contains(logData.errorMessage, secret) {
		t.Fatalf("response content leaked into the request log: %q", logData.errorMessage)
	}
	if strings.Contains(logData.errorMessage, "chatcmpl-1") {
		t.Fatalf("response body leaked into the request log: %q", logData.errorMessage)
	}
	for _, want := range []string{"response decode error", fmt.Sprintf("body_bytes=%d", len(body)), "application/json"} {
		if !strings.Contains(logData.errorMessage, want) {
			t.Errorf("errorMessage missing diagnostic %q: %q", want, logData.errorMessage)
		}
	}
	if logData.errorKind != KindProviderBadRequest {
		t.Errorf("error kind = %v, want %v", logData.errorKind, KindProviderBadRequest)
	}
}

// The other half of the same rule: a non-2xx carries no completion, so its
// body is the provider's error text and that text is what makes the row worth
// reading. It must keep landing in the log.
func TestHandleNonStreamingResponse_Non2xxKeepsUpstreamErrorText(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const upstreamText = "model overloaded, try again shortly"
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"` + upstreamText + `"}}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := nonStreamingLogData()

	h.handleNonStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	if logData.state != "failed" {
		t.Fatalf("state = %q, want failed", logData.state)
	}
	if !strings.Contains(logData.errorMessage, upstreamText) {
		t.Fatalf("upstream error text lost from the request log: %q", logData.errorMessage)
	}
}

// A non-2xx that fails to decode as well (an HTML error page from a proxy in
// front of the provider) keeps its text too — it is still an error document.
func TestHandleNonStreamingResponse_UndecodableNon2xxKeepsBody(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const page = "<html><body>504 Gateway Time-out (nginx)</body></html>"
	resp := &http.Response{
		StatusCode: http.StatusGatewayTimeout,
		Body:       io.NopCloser(strings.NewReader(page)),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := nonStreamingLogData()

	h.handleNonStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	if !strings.Contains(logData.errorMessage, "504 Gateway Time-out") {
		t.Fatalf("upstream error page lost from the request log: %q", logData.errorMessage)
	}
}

// ---------------------------------------------------------------------------
// The non-streaming body read is bounded
// ---------------------------------------------------------------------------

// Past the cap the request fails instead of buffering the whole body — and it
// fails rather than silently forwarding a truncated completion.
func TestHandleNonStreamingResponse_OversizedBodyRefused(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const head = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(io.MultiReader(
			strings.NewReader(head),
			io.LimitReader(paddingReader{}, nonStreamingBodyCap),
		)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := nonStreamingLogData()
	rec := httptest.NewRecorder()

	h.handleNonStreamingResponse(rec, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	if logData.state != "failed" {
		t.Fatalf("state = %q, want failed for an oversized body", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "non-streaming body cap") {
		t.Fatalf("errorMessage does not name the cap: %q", logData.errorMessage)
	}
	if strings.Contains(rec.Body.String(), "aaaa") {
		t.Fatalf("truncated body forwarded to the client: %.200q", rec.Body.String())
	}
	if rec.Body.Len() > 4096 {
		t.Fatalf("oversized body echoed to the client (%d bytes)", rec.Body.Len())
	}
}

// A body of exactly the cap is legitimate and must still decode and reach the
// client whole: the read takes cap+1 bytes precisely so the boundary case is
// not mistaken for an overflow.
func TestHandleNonStreamingResponse_BodyAtCapDecodesIntact(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const head = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"`
	const tail = `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	pad := nonStreamingBodyCap - len(head) - len(tail)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(io.MultiReader(
			strings.NewReader(head),
			io.LimitReader(paddingReader{}, int64(pad)),
			strings.NewReader(tail),
		)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := nonStreamingLogData()
	rec := httptest.NewRecorder()

	h.handleNonStreamingResponse(rec, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	if logData.state != "completed" {
		t.Fatalf("state = %q (%s), want completed for a body exactly at the cap", logData.state, logData.errorMessage)
	}
	var got ChatCompletionResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("client response did not decode: %v", err)
	}
	content, _ := got.Choices[0].Message.Content.(string)
	if len(content) != pad {
		t.Fatalf("content forwarded truncated: %d bytes, want %d", len(content), pad)
	}
}

// Everything unmodelled in a completion has to reach the client: a client that
// asked for logprobs gets them, aggregator routing/cost fields survive, and the
// normalisation this package does still wins over the raw copy.
func TestHandleNonStreamingResponse_PreservesUnmodelledFields(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const body = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m",
		"system_fingerprint":"fp_abc","provider":"Together",
		"choices":[{"index":0,"logprobs":{"content":[{"token":"hi","logprob":-0.25}]},
		  "native_finish_reason":"STOP",
		  "message":{"role":"assistant","content":"hi","reasoning":"pondered"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"cost":0.000123}}`

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	rec := httptest.NewRecorder()

	h.handleNonStreamingResponse(rec, req, nonStreamingLogData(), resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("client response did not decode: %v (%s)", err, rec.Body.String())
	}
	if got["system_fingerprint"] != "fp_abc" || got["provider"] != "Together" {
		t.Errorf("top-level provider fields dropped: %s", rec.Body.String())
	}
	choice, ok := got["choices"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("no choice in response: %s", rec.Body.String())
	}
	if _, has := choice["logprobs"]; !has {
		t.Errorf("logprobs dropped: %s", rec.Body.String())
	}
	if choice["native_finish_reason"] != "STOP" {
		t.Errorf("native_finish_reason dropped: %s", rec.Body.String())
	}
	usage, ok := got["usage"].(map[string]any)
	if !ok {
		t.Fatalf("no usage in response: %s", rec.Body.String())
	}
	if usage["cost"] != 0.000123 {
		t.Errorf("usage.cost dropped: %s", rec.Body.String())
	}
	if usage["total_tokens"] != float64(3) {
		t.Errorf("modelled usage field lost: %s", rec.Body.String())
	}
	msg := choice["message"].(map[string]any)
	if msg["reasoning_content"] != "pondered" {
		t.Errorf("reasoning normalisation lost: %s", rec.Body.String())
	}
}

// The content rule at the unit level, both directions at once.
func TestNonStreamingFailureDetail(t *testing.T) {
	t.Parallel()

	jsonHeader := http.Header{"Content-Type": []string{"application/json"}}
	decodeErr := errors.New("json: cannot unmarshal string into Go struct field ChatCompletionResponse.created of type int64")

	t.Run("2xx keeps the completion out of the logs", func(t *testing.T) {
		t.Parallel()
		body := []byte(`{"choices":[{"message":{"role":"assistant","content":"private answer"}}],"created":"1"}`)
		resp := &http.Response{StatusCode: http.StatusOK, Header: jsonHeader}

		logMsg, detail, kind, reason := nonStreamingFailureDetail(context.Background(), resp, body, nil, decodeErr, "m")

		for _, s := range []string{logMsg, detail} {
			if strings.Contains(s, "private answer") || strings.Contains(s, "choices") {
				t.Fatalf("body content leaked: %q", s)
			}
			if !strings.Contains(s, fmt.Sprintf("body_bytes=%d", len(body))) || !strings.Contains(s, "application/json") {
				t.Errorf("diagnostics missing: %q", s)
			}
		}
		if kind != KindProviderBadRequest {
			t.Errorf("kind = %v, want %v", kind, KindProviderBadRequest)
		}
		if !strings.Contains(reason, "could not decode") || strings.Contains(reason, "200") {
			t.Errorf("reason = %q", reason)
		}
	})

	t.Run("non-2xx keeps the upstream error text", func(t *testing.T) {
		t.Parallel()
		body := []byte(`{"error":{"message":"rate limit reached for gpt-4o"}}`)
		resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: jsonHeader}

		logMsg, detail, kind, _ := nonStreamingFailureDetail(context.Background(), resp, body, nil, nil, "gpt-4o")

		if !strings.Contains(logMsg, "upstream HTTP 429") || !strings.Contains(logMsg, "rate limit reached") {
			t.Errorf("logMsg = %q", logMsg)
		}
		if !strings.Contains(detail, "rate limit reached") {
			t.Errorf("detail = %q", detail)
		}
		if kind == "" {
			t.Error("non-2xx must be classified")
		}
	})

	t.Run("non-2xx that also fails to decode is named as such", func(t *testing.T) {
		t.Parallel()
		body := []byte(`<html>502 Bad Gateway</html>`)
		resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"text/html"}}}

		logMsg, _, _, _ := nonStreamingFailureDetail(context.Background(), resp, body, nil, decodeErr, "m")

		if !strings.Contains(logMsg, "response decode error") || !strings.Contains(logMsg, "502 Bad Gateway") {
			t.Errorf("logMsg = %q", logMsg)
		}
	})
}
