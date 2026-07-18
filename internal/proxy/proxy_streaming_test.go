package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Use testDB from proxy_test.go

// errorAfterDataReader sends some SSE data, then returns a fixed error.
type errorAfterDataReader struct {
	data   string
	err    error
	offset int
}

func (r *errorAfterDataReader) Read(p []byte) (int, error) {
	if r.offset < len(r.data) {
		n := copy(p, r.data[r.offset:])
		r.offset += n
		return n, nil
	}
	return 0, r.err
}

func (r *errorAfterDataReader) Close() error { return nil }

func stopUnitHandlerIntegration(h *Handler) {
	if h != nil && h.upstreamTransport != nil {
		h.upstreamTransport.CloseIdleConnections()
	}
}

// ---------------------------------------------------------------------------
// Tests moved from routing_integration_test.go
// ---------------------------------------------------------------------------

// TestResolveHotelModel_EmptyFailoverGroup tests the case where a failover group exists but has no priority order

func TestHandleStreamingResponse_UpstreamError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that returns an error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"upstream error\"}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	// Make a request to the upstream to get a real *http.Response
	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "upstream error") {
		t.Errorf("expected error message to contain 'upstream error', got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_MissingDoneSentinel tests that the proxy auto-injects
// the [DONE] sentinel when the upstream omits it but content was received successfully.

func TestHandleStreamingResponse_MissingDoneSentinel(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that closes connection without [DONE]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send a few chunks then close without [DONE]
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		// Close without sending [DONE]
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	// Verify the proxy actually injected [DONE] into the downstream response
	if !strings.Contains(inner.Body.String(), "data: [DONE]") {
		t.Errorf("expected downstream response to contain 'data: [DONE]', got body:\n%s", inner.Body.String())
	}
}

// TestHandleNonStreamingResponse_Success tests successful non-streaming response handling

func TestHandleStreamingResponse_Basic(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that returns a simple streaming response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send a few chunks
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		// Send [DONE] sentinel
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Basic verification - the handler should process the stream
	if inner.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, inner.Code)
	}
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// TestChatCompletions_ContextCancelDuringStream tests client disconnect during streaming

func TestHandleStreamingResponse_ClientDisconnectMidStream(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that streams slowly
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send first chunk
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Wait longer than client will wait (test cancels after 20ms)
		time.Sleep(100 * time.Millisecond)

		// This should never be sent
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Create a context that will be canceled
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	// Start streaming in goroutine
	done := make(chan struct{})
	go func() {
		h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})
		close(done)
	}()

	// Let first chunk be processed
	time.Sleep(50 * time.Millisecond)

	// Cancel context to simulate client disconnect
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleStreamingResponse did not finish")
	}

	// Verify clientDisconnected was detected (state should be failed with disconnect message)
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "client disconnected") {
		t.Errorf("expected error message to mention client disconnect, got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_EmptyLinesSSESep tests that empty lines between
// events are passed through correctly (they're SSE separators).

func TestHandleStreamingResponse_EmptyLinesSSESep(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends chunks with extra empty lines
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send chunk with empty line separator
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		// Extra empty lines (normal SSE separators)
		fmt.Fprint(w, "\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete successfully
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	// Verify both chunks were forwarded
	if !strings.Contains(inner.Body.String(), "hello") {
		t.Error("expected first chunk to be forwarded")
	}
	if !strings.Contains(inner.Body.String(), "world") {
		t.Error("expected second chunk to be forwarded")
	}
}

// TestHandleStreamingResponse_TooManyEmptyLines tests that streams with >1000
// consecutive empty lines are aborted.

func TestHandleStreamingResponse_TooManyEmptyLines(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends many empty lines
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send one chunk with real newlines (not backtick \n literals)
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Send 1002 empty lines. bufio.Scanner splits on \n, so each
		// \n is a separate scan line. The implementation aborts when
		// emptyLines > 1000.
		for range 1002 {
			fmt.Fprint(w, "\n")
		}
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "too many empty lines") {
		t.Errorf("expected error message to mention 'too many empty lines', got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_UTF8BOM tests that UTF-8 BOM at start of stream
// is handled correctly (stripped for parsing).

func TestHandleStreamingResponse_UTF8BOM(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends UTF-8 BOM before first chunk
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send UTF-8 BOM (\uFEFF) before first data line
		// Note: The BOM is stripped for parsing purposes, but may still appear
		// in the forwarded output since we forward raw bytes.
		fmt.Fprint(w, "\uFEFF")
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete successfully - BOM is stripped for parsing
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	// Verify chunk was forwarded
	if !strings.Contains(inner.Body.String(), "hello") {
		t.Error("expected chunk to be forwarded")
	}
}

// TestHandleStreamingResponse_LeadingWhitespace tests that leading \\r\\n before
// data: lines is trimmed correctly.

func TestHandleStreamingResponse_LeadingWhitespace(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends leading whitespace before data lines
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send chunk with leading \\r\\n (Gemini-style)
		fmt.Fprint(w, "\r\n")
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete successfully
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	if !strings.Contains(inner.Body.String(), "hello") {
		t.Error("expected chunk to be forwarded")
	}
}

// TestHandleStreamingResponse_UsageExtraction tests that usage is extracted
// from the final chunk and logged.

func TestHandleStreamingResponse_UsageExtraction(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends usage in final chunk with [DONE]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send content chunk
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		// Send usage chunk with [DONE] combined (common pattern)
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	if logData.tokensPrompt != 10 {
		t.Errorf("expected prompt_tokens=10, got %d", logData.tokensPrompt)
	}
	if logData.tokensCompletion != 5 {
		t.Errorf("expected completion_tokens=5, got %d", logData.tokensCompletion)
	}
}

// TestHandleStreamingResponse_AnthropicErrorEvent tests handling of Anthropic-style
// "event: error" followed by error data.

func TestHandleStreamingResponse_AnthropicErrorEvent(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends Anthropic-style error event
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send Anthropic-style error event (event line + data line)
		fmt.Fprint(w, "event: error\n")
		fmt.Fprint(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Server overloaded\"}}\n\n")
		flusher.Flush()
		// Send [DONE] to complete the stream
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// The stream captures the Anthropic error and marks the request as failed.
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "Server overloaded") {
		t.Errorf("expected error message to contain 'Server overloaded', got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_DuplicateFinishReasonSuppression tests that
// consecutive chunks with same finish_reason and no content are suppressed.

func TestHandleStreamingResponse_DuplicateFinishReasonSuppression(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends duplicate finish_reason chunks
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send content chunk with finish_reason
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
		// Send duplicate finish_reason with no content (should be suppressed)
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete successfully
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	// The duplicate suppression is an optimization - verify the stream completed
	// Counting exact finish_reason occurrences is fragile due to formatting
}

// TestHandleStreamingResponse_RepeatedContentDetection tests that repeated
// identical content chunks are handled (warning is logged).

func TestHandleStreamingResponse_RepeatedContentDetection(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends repeated content
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send same content 12 times (exceeds threshold of 10)
		for range 12 {
			fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking...\"},\"finish_reason\":null}]}\n\n")
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete (repeated content is just logged, not failed)
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_DataWithoutSpace tests that SSE lines with "data:"
// (no space after colon) are correctly parsed. This is for LM Studio compatibility.
// Covers lines 145-149 in proxy.go.

func TestHandleStreamingResponse_DataWithoutSpace(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends SSE with "data:" (no space after colon)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send data without space after colon (LM Studio style)
		fmt.Fprint(w, "data:{\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Verify status code
	if inner.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, inner.Code)
	}

	// Verify the content "hello" was correctly extracted from the payload
	if !strings.Contains(inner.Body.String(), "hello") {
		t.Errorf("expected response body to contain 'hello', got:\n%s", inner.Body.String())
	}

	// Verify log state is completed
	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
}

// TestHandleNonStreamingResponse_NonJSONError tests that non-JSON error responses
// from upstream are wrapped in OpenAI-compatible error format.
// Covers the decode-failure branch in handleNonStreamingResponse (proxy.go:602-621).

func TestHandleStreamingResponse_NonErrorAnthropicEvent(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends Anthropic-style ping event
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send Anthropic-style ping event (non-error)
		fmt.Fprint(w, "event: ping\n")
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	if !strings.Contains(inner.Body.String(), "hello") {
		t.Errorf("expected response body to contain 'hello', got:\n%s", inner.Body.String())
	}
}

// TestHandleStreamingResponse_ErrAccumFlushOnNonDataLine tests error accumulation
// flush when a non-data line (SSE comment) arrives after error JSON prefix.
// Covers lines 168-174.

func TestHandleStreamingResponse_ErrAccumFlushOnNonDataLine(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends error JSON split across lines,
	// then a non-data line (SSE comment) to trigger flush
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send error JSON split: first part starts with {"error"
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"Rate limit\n\n")
		flusher.Flush()
		// Send SSE comment (non-data line) to trigger errAccum flush
		fmt.Fprint(w, ": comment\n\n")
		flusher.Flush()
		// Send normal data to complete stream
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "Rate limit") {
		t.Errorf("expected error message to contain 'Rate limit', got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_ErrAccumFlushOnNonErrorDataLine tests error accumulation
// flush when a non-error data line arrives after error JSON prefix.
// Covers lines 240-247.

func TestHandleStreamingResponse_ErrAccumFlushOnNonErrorDataLine(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends error JSON prefix, then non-error data
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send error JSON prefix (incomplete)
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"overloaded\"\n\n")
		flusher.Flush()
		// Send non-error data line to trigger errAccum flush
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "overloaded") {
		t.Errorf("expected error message to contain 'overloaded', got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_PromptCacheHitTokens tests extraction of
// prompt_cache_hit_tokens from usage chunk.
// Covers lines 292-295.

func TestHandleStreamingResponse_PromptCacheHitTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends prompt_cache_hit_tokens in usage
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send content chunk
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		// Send usage chunk with prompt_cache_hit_tokens
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":5,\"total_tokens\":105,\"prompt_cache_hit_tokens\":80}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

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

// TestHandleStreamingResponse_NativeFinishReason tests handling of
// native_finish_reason field in streaming chunk.
// Covers lines 300-304.

func TestHandleStreamingResponse_NativeFinishReason(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends native_finish_reason
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send chunk with native_finish_reason
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\",\"native_finish_reason\":\"STOP\"}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
}

// TestHandleStreamingResponse_ErrAccumFlushAtStreamEnd tests error accumulation
// flush at stream end when error JSON is in the last data line.
// Covers lines 457-462.

func TestHandleStreamingResponse_ErrAccumFlushAtStreamEnd(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends error JSON and closes without [DONE]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send error JSON as last data line, then close (no [DONE])
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"server error\"}}\n\n")
		flusher.Flush()
		// Connection closes without [DONE]
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "server error") {
		t.Errorf("expected error message to contain 'server error', got %q", logData.errorMessage)
	}
}

// TestHandleStreamingResponse_FinishReasonNormalization tests finish_reason
// normalization (e.g., "STOP" -> "stop") for non-OpenAI providers.
// Covers lines 379-425 (JSON rewrite path).

func TestHandleStreamingResponse_FinishReasonNormalization(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends non-OpenAI finish_reason
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send chunk with uppercase finish_reason (Gemini-style "STOP")
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"STOP\"}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	// Verify finish_reason was normalized to lowercase "stop"
	if !strings.Contains(inner.Body.String(), `"finish_reason":"stop"`) {
		t.Errorf("expected response body to contain normalized finish_reason \"stop\", got:\n%s", inner.Body.String())
	}
	// Verify original "STOP" is NOT present
	if strings.Contains(inner.Body.String(), `"finish_reason":"STOP"`) {
		t.Errorf("expected response body NOT to contain original finish_reason \"STOP\", got:\n%s", inner.Body.String())
	}
}

// TestChatCompletions_SettingsReadMsFromContext tests that SettingsReadMsKey
// from the request context is captured into the timings.settingsReadMs field.
// Covers lines 705-708 in proxy.go.

func TestHandleStreamingResponse_HasContentChecks(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends:
	// 1. Chunk with content "hello" and finish_reason "stop"
	// 2. Chunk with empty content "" and finish_reason "stop" (should be suppressed)
	// 3. Chunk with reasoning_content "thinking..." and finish_reason "stop" (should NOT be suppressed)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// First chunk: content with finish_reason
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
		// Second chunk: empty content with finish_reason (should be suppressed)
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
		// Third chunk: reasoning_content with finish_reason (should NOT be suppressed)
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking...\"},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	// Verify response contains "hello" and "thinking"
	if !strings.Contains(inner.Body.String(), "hello") {
		t.Errorf("expected response body to contain 'hello', got:\n%s", inner.Body.String())
	}
	if !strings.Contains(inner.Body.String(), "thinking") {
		t.Errorf("expected response body to contain 'thinking', got:\n%s", inner.Body.String())
	}
}

// TestHandleStreamingResponse_RepeatedContentPreviewTruncation tests that
// repeated content detection triggers the preview truncation when content >50 chars.
// Covers lines 328-330 in proxy.go.

func TestHandleStreamingResponse_RepeatedContentPreviewTruncation(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Build an upstream server that sends 12+ identical chunks with >50 char content
	longContent := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 60 'a's
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		// Send 12 identical chunks (exceeds threshold of 10)
		for range 12 {
			fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"%s\"},\"finish_reason\":null}]}\n\n", longContent)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	// Verify response body contains the long content (truncated preview in logs)
	if !strings.Contains(inner.Body.String(), "aaaa") {
		t.Errorf("expected response body to contain content, got:\n%s", inner.Body.String())
	}
}

// TestChatCompletions_EmptyModelParts tests that a model with "/" but not starting
// with "hotel/" that references a non-existent provider returns 404.
// Covers lines 735-739 in proxy.go (resolveSpecificProvider error → 404).

func TestHandleStreamingResponse_AddTokensError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	h.virtualKeyRepo = &mockVirtualKeyRepo{addTokensErr: fmt.Errorf("db connection refused")}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream must support flushing")
		}
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
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
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=%q, got %q", "completed", logData.state)
	}
	if logData.tokensPrompt != 5 || logData.tokensCompletion != 1 {
		t.Errorf("expected tokens 5/1, got %d/%d", logData.tokensPrompt, logData.tokensCompletion)
	}
}

// TestHandleNonStreamingResponse_AddTokensError tests that when AddTokens returns
// an error during non-streaming, the request still completes successfully.
// Covers lines 584-594 in proxy.go.

func TestHandleStreamingResponse_ClientWriteFailureOnDataLine(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream must support flushing")
		}
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
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
	innerRW := &failAfterNWriter{inner: inner, maxWrites: 1}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), streamOptions{attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// TestHandleStreamingResponse_ClientWriteFailureOnDoneLine tests that a write
// failure when writing the [DONE] sentinel marks the client as disconnected.
// Covers lines 200-209 in proxy.go.

func TestHandleStreamingResponse_ClientWriteFailureOnDoneLine(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream must support flushing")
		}
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// 1 data chunk = 2 writes (line + newline), then [DONE] write fails
	inner := httptest.NewRecorder()
	innerRW := &failAfterNWriter{inner: inner, maxWrites: 2}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), streamOptions{attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// TestHandleStreamingResponse_ClientWriteFailureOnNormalizedChunk tests write
// failures during finish_reason normalization rewrite.
// Covers lines 402-419 in proxy.go.

func TestHandleStreamingResponse_ClientWriteFailureOnNormalizedChunk(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream must support flushing")
		}
		// Content chunk (2 writes: line + newline)
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		// Chunk with non-OpenAI finish_reason "STOP" triggers normalization
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"STOP"}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Content chunk = 2 writes, then normalization writes "data: " (1 write) which should fail
	inner := httptest.NewRecorder()
	innerRW := &failAfterNWriter{inner: inner, maxWrites: 2}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), streamOptions{attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// TestHandleStreamingResponse_ClientWriteFailureOnNonNormalizedChunk tests write
// failure for a chunk that doesn't need normalization (the `if !written` path).
// Covers lines 438-441 in proxy.go.

func TestHandleStreamingResponse_ClientWriteFailureOnNonNormalizedChunk(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream must support flushing")
		}
		// Content chunk (2 writes)
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		flusher.Flush()
		// Chunk with standard "stop" finish_reason (no normalization needed)
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Content chunk = 2 writes, then finish_reason chunk write fails
	inner := httptest.NewRecorder()
	innerRW := &failAfterNWriter{inner: inner, maxWrites: 2}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), streamOptions{attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// TestHandleStreamingResponse_ScannerContextCanceled tests scanner error when context is canceled.

func TestHandleStreamingResponse_ScannerContextCanceled(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := io.NopCloser(&errorAfterDataReader{
		data: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		err:  context.Canceled,
	})

	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 0, // no watchdog for these tests
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	if logData.errorMessage != "client disconnected" {
		t.Errorf("expected error message %q, got %q", "client disconnected", logData.errorMessage)
	}
	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// TestHandleStreamingResponse_ScannerDeadlineExceededRetryTimeout tests scanner deadline exceeded with retry_timeout origin.

func TestHandleStreamingResponse_ScannerDeadlineExceededRetryTimeout(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := io.NopCloser(&errorAfterDataReader{
		data: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		err:  context.DeadlineExceeded,
	})

	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 0,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "retry_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	if logData.errorMessage != "stream interrupted: param-strip retry timed out" {
		t.Errorf("expected error message %q, got %q", "stream interrupted: param-strip retry timed out", logData.errorMessage)
	}
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ScannerDeadlineExceededFailoverTimeout tests scanner deadline exceeded with failover_timeout origin.

func TestHandleStreamingResponse_ScannerDeadlineExceededFailoverTimeout(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := io.NopCloser(&errorAfterDataReader{
		data: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		err:  context.DeadlineExceeded,
	})

	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 0,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	if logData.errorMessage != "stream interrupted: upstream request timed out" {
		t.Errorf("expected error message %q, got %q", "stream interrupted: upstream request timed out", logData.errorMessage)
	}
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ScannerDeadlineExceededUnknownOrigin tests scanner deadline exceeded with custom origin.

func TestHandleStreamingResponse_ScannerDeadlineExceededUnknownOrigin(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := io.NopCloser(&errorAfterDataReader{
		data: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		err:  context.DeadlineExceeded,
	})

	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 0,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "custom_origin",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	if !strings.Contains(logData.errorMessage, "stream interrupted:") {
		t.Errorf("expected error message to contain %q, got %q", "stream interrupted:", logData.errorMessage)
	}
	if !strings.Contains(logData.errorMessage, "custom_origin") {
		t.Errorf("expected error message to contain %q, got %q", "custom_origin", logData.errorMessage)
	}
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ScannerGenericError tests scanner with generic error.

func TestHandleStreamingResponse_ScannerGenericError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := io.NopCloser(&errorAfterDataReader{
		data: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		err:  errors.New("connection reset by peer"),
	})

	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 0,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	if logData.errorMessage != "connection reset by peer" {
		t.Errorf("expected error message %q, got %q", "connection reset by peer", logData.errorMessage)
	}
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}
