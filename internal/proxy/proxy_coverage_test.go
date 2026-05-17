package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 1)

	// Encode error is logged but not propagated; state was already set to "completed" before the write attempt
	// State should be completed since JSON parsed successfully
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions tests
// ---------------------------------------------------------------------------

func TestChatCompletions_RequestBodyNotCached(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Create a mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	// Create provider + model
	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-uncached", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{
		ID:               uuid.New(),
		ProviderID:       prov.ID,
		ModelID:          "test-model",
		Name:             "Test Model",
		DisplayName:      "Test Model Display",
		Description:      "A test model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Request WITHOUT RequestBodyKey in context (normal path)
	body := `{"model":"test-provider-uncached/test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)
	// Do NOT add ctxkeys.RequestBodyKey to context

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_ReadBodyError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Create request with body reader that errors
	errReader := &errorReader{err: errors.New("body read error")}
	req := httptest.NewRequest("POST", "/v1/chat/completions", errReader)
	req = withAuthContext(req)
	// Ensure RequestBodyKey is NOT in context

	w := httptest.NewRecorder()

	h.ChatCompletions(w, req)

	// Should return 400 with error message
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "failed to read request body") {
		t.Errorf("expected error about reading body, got %q", body)
	}
}

func TestChatCompletions_NoCandidatesAfterResolve(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Create provider WITHOUT any model
	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	_, err = h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-nomodel", BaseURL: "http://localhost:9999", APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	// No model created for this provider

	body := `{"model":"test-provider-nomodel/nonexistent-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestChatCompletions_NewRequestWithContextError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-reqerr", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Override newRequestWithContext to fail
	origNewReq := newRequestWithContext
	defer func() { newRequestWithContext = origNewReq }()
	newRequestWithContext = func(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
		return nil, errors.New("request creation failed")
	}

	body := `{"model":"test-provider-reqerr/test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_ContextErrorHandling(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Server that delays response to trigger timeout
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-ctxerr", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Set very short request timeout
	if err := h.settingsRepo.Set(ctx, "request_timeout", "100ms"); err != nil {
		t.Fatalf("failed to set timeout: %v", err)
	}
	defer func() {
		_ = h.settingsRepo.Set(ctx, "request_timeout", "60000")
	}()
	h.settingsRepo.InvalidateCache("request_timeout")

	body := `{"model":"test-provider-ctxerr/test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_RetryRequestCreationError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retrycreate", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Override newRequestWithContext: succeed on first call, fail on retry
	origNewReq := newRequestWithContext
	defer func() { newRequestWithContext = origNewReq }()
	reqCallCount := 0
	newRequestWithContext = func(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
		reqCallCount++
		if reqCallCount > 1 {
			return nil, errors.New("retry request creation failed")
		}
		return http.NewRequestWithContext(ctx, method, url, body)
	}

	body := `{"model":"test-provider-retrycreate/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// When retry request creation fails, all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_RetryRequestDoError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Second request (retry): hijack and close connection to cause Do error
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retrydo", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-retrydo/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// When retry Do fails, all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestChatCompletions_RetryCancelFailoverPath(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Retry: return 500 (failover-eligible status)
		// With single candidate, failover won't trigger but non-200 path
		// will call retryCancel at L1052-1054
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal server error"}}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retry-failover", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-retry-failover/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	// Retry returns 500, single candidate so no failover, 500 forwarded
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_RetryCancelNon200Path(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Retry: return 400 (non-failover-eligible, non-200)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request after retry"}}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-retrycancel-non200", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-retrycancel-non200/test-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletions_RetryCancelStreamingPath(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First request: 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("{\"error\":{\"message\":\"Unrecognized parameter \\\"temperature\\\" is not supported\",\"type\":\"invalid_request_error\"}}"))
			return
		}
		// Retry: return 200 streaming
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	ctx := context.Background()
	kp, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name: "test-provider-stream", BaseURL: upstream.URL, APIKey: "test-api-key",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := h.modelRepo.Upsert(ctx, &model.Model{ID: uuid.New(), ProviderID: prov.ID, ModelID: "test-model", Name: "Test Model", DisplayName: "Test Model Display", Description: "A test model", Capabilities: "{}", Params: "{}", Modality: "text", InputModalities: "[]", OutputModalities: "[]", Enabled: true, CreatedAt: time.Now(), LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	body := `{"model":"test-provider-stream/test-model","messages":[{"role":"user","content":"hi"}],"stream":true,"temperature":0.7}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Additional edge case tests for streaming
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

	if logData.tokensPrompt != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", logData.tokensPrompt)
	}
	if logData.tokensCompletion != 5 {
		t.Errorf("expected completion_tokens=5, got %d", logData.tokensCompletion)
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

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
	// Data line writes: w.Write(line) then w.Write([]byte("\n")) = 2 writes
	// Then injected [DONE] is the 3rd Write
	streamData := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}]}

`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamData)),
		Header:     make(http.Header),
	}

	w := &failingResponseWriter{
		failAfter: 2, // first 2 Writes succeed (data line + newline), 3rd (injected [DONE]) fails
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

	// The injected [DONE] write failure is logged but benign - state should still be completed
	// because the stream content was successfully written
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
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
	h.handleNonStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, vkHash, 1)

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
// Reasoning field normalization tests (streaming)
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

	body := w.Body.String()
	// Assert: response body contains reasoning_content with the value
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

	body := w.Body.String()
	// Assert: response body contains reasoning_content with concatenated text
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

	body := w.Body.String()
	// Assert: response body contains reasoning_content with extracted thinking
	// and content with remaining text
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
	h.handleStreamingResponse(w, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, "", 0)

	body := w.Body.String()
	// Assert: response body contains reasoning_content unchanged
	if !strings.Contains(body, "reasoning_content") {
		t.Error("expected response to contain reasoning_content")
	}
	if !strings.Contains(body, "Already here") {
		t.Errorf("expected reasoning_content to contain 'Already here', got: %s", body)
	}
}
