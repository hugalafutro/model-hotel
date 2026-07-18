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

func TestHandleStreamingResponse_ClientWriteFailureMarksDisconnected(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Build an upstream SSE server that streams ~50 chunks then [DONE].
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}

		for range 50 {
			chunk := fmt.Sprintf(`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}` + "\n\n")
			fmt.Fprint(w, chunk)
			flusher.Flush()
		}
		// Send usage chunk
		fmt.Fprint(w, `data: {"id":"chatcmpl-test","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":50,"total_tokens":60}}`+"\n\n")
		flusher.Flush()
		// Send [DONE]
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	// Make a request to the upstream to get a real *http.Response
	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	// Add auth context values needed by the proxy
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Wrap a real ResponseRecorder in our failing writer.
	// Allow only 3 writes before failing — the client should disconnect early.
	inner := httptest.NewRecorder()
	innerRW := &failAfterNWriter{
		inner:     inner,
		maxWrites: 3,
	}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	// Insert initial log entry so updateRequestLog has a row to update.
	// Insert initial log entry (async, but ID is set synchronously)
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond) // wait for async DB insert

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
	if logData.errorMessage != "client disconnected" {
		t.Errorf("expected errorMessage=%q, got %q", "client disconnected", logData.errorMessage)
	}
	// The stream should have been interrupted before consuming [DONE].
	// With maxWrites=3, we get at most 2 data lines written (line + newline = 2 writes per chunk).
	// The key assertion is that state is failed, not completed.
	if logData.state == "completed" {
		t.Error("stream should not show completed when client disconnected mid-stream")
	}
}

// TestHandleStreamingResponse_EmptyStream tests when upstream sends no data
func TestHandleStreamingResponse_EmptyStream(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Build an upstream SSE server that sends no data, just closes
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// No data sent, body just closes
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
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete without error even with empty stream
	if logData.state != "failed" {
		t.Errorf("expected state=failed for empty stream (no [DONE] sentinel), got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ErrorChunk tests when upstream sends error chunks
func TestHandleStreamingResponse_ErrorChunk(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Build an upstream SSE server that sends an error chunk
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream response writer must support flushing")
		}
		// Send error chunk
		fmt.Fprint(w, `data: {"error":{"message":"upstream error"}}`+"\n\n")
		flusher.Flush()
		// Send [DONE]
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
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	// Should complete but track error chunks
	if logData.state != "failed" {
		t.Errorf("expected state=failed (error chunk), got %q", logData.state)
	}
}

// ---------------------------------------------------------------------------
// handleStreamingResponse [DONE] write failure (line 228)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_DoneSentinelWriteFailure(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Upstream sends one chunk with reasoning (triggers normalization = 3 writes)
	// then [DONE]. We want the [DONE] line write to fail.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// Send a chunk with reasoning that triggers normalization.
		// Normalization writes: "data: " + payload + "\n\n" = 3 Write calls.
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"reasoning":"think step","reasoning_content":"","content":""}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Allow 4 writes: 3 for reasoning-normalized chunk (data: + payload + \n\n)
	// plus 1 for the empty SSE separator line (\n).
	// The [DONE] write at line 228 is write #5 and should fail.
	inner := httptest.NewRecorder()
	failWriter := &failAfterNWriter{inner: inner, maxWrites: 4}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(failWriter, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if logData.errorMessage != "client disconnected" {
		t.Errorf("expected errorMessage='client disconnected', got %q", logData.errorMessage)
	}
}

// ---------------------------------------------------------------------------
// handleStreamingResponse reasoning normalization write failure (lines 393-410)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_ReasoningNormWriteFailure(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Upstream sends a chunk with reasoning that triggers normalization.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"reasoning":"thinking step 1","reasoning_content":"","content":""}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Allow 0 writes — the first write in reasoning normalization is
	// w.Write([]byte("data: ")) at line 391. With maxWrites=0, that fails.
	inner := httptest.NewRecorder()
	failWriter := &failAfterNWriter{inner: inner, maxWrites: 0}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(failWriter, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ReasoningNormPayloadWriteFailure covers the
// second write in reasoning normalization (line 398: w.Write(newPayload)).
func TestHandleStreamingResponse_ReasoningNormPayloadWriteFailure(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"reasoning":"think","reasoning_content":"","content":""}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Allow 1 write ("data: " prefix succeeds), fail on newPayload write.
	inner := httptest.NewRecorder()
	failWriter := &failAfterNWriter{inner: inner, maxWrites: 1}

	logData := &requestLogData{
		modelID: "test-model", streaming: true, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(failWriter, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// TestHandleStreamingResponse_ReasoningNormNewlineWriteFailure covers the
// third write in reasoning normalization (line 405: w.Write([]byte("\n\n"))).
func TestHandleStreamingResponse_ReasoningNormNewlineWriteFailure(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"reasoning":"think","reasoning_content":"","content":""}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Allow 2 writes ("data: " + payload succeed), fail on "\n\n" write.
	inner := httptest.NewRecorder()
	failWriter := &failAfterNWriter{inner: inner, maxWrites: 2}

	logData := &requestLogData{
		modelID: "test-model", streaming: true, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(failWriter, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// ---------------------------------------------------------------------------
// handleStreamingResponse finish_reason normalization write failure (line 540)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_FinishReasonNormWriteFailure(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Upstream sends a chunk with non-OpenAI finish_reason (e.g. "end_turn")
	// that triggers normalization rewrite.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"content":"hi"},"finish_reason":"end_turn"}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	// Allow 0 writes — first write in finish_reason normalization is
	// w.Write([]byte("data: ")) at line 538. Fails immediately.
	inner := httptest.NewRecorder()
	failWriter := &failAfterNWriter{inner: inner, maxWrites: 0}

	logData := &requestLogData{
		modelID: "test-model", streaming: true, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleStreamingResponse(failWriter, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
}

// ---------------------------------------------------------------------------
// handleStreamingResponse TPS fallback (line 613-615)
// ---------------------------------------------------------------------------

func TestHandleStreamingResponse_TPSFallbackWhenTTFTExceedsDuration(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"content":"x"}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, `data: {"id":"1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID: "test-model", streaming: true, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	// Use a startTime in the past so totalDuration is positive,
	// but set ttft equal to totalDuration so generationDuration = 0,
	// triggering the else-if fallback at line 613.
	// We add a small delay so totalDuration > 0.
	startTime := time.Now()
	time.Sleep(2 * time.Millisecond)
	// ttft will be set to totalDuration, making generationDuration = 0
	h.handleStreamingResponse(inner, req, logData, resp, startTime, streamOptions{responseHeaderMs: 999999.0, vkHash: "test-hash", attempt: 1, cancelOrigin: "failover_timeout"})

	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	// TPS should be computed via the fallback path (totalDuration > 0)
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS from fallback path, got %f", logData.tokensPerSecond)
	}
}

// ---------------------------------------------------------------------------
// handleNonStreamingResponse TPS fallback (line 719-721)
// ---------------------------------------------------------------------------

func TestHandleNonStreamingResponse_TPSFallbackWhenTTFTExceedsDuration(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ChatCompletionResponse{
			ID:      "chatcmpl-1",
			Object:  "chat.completion",
			Created: 1234,
			Model:   "test-model",
			Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "hello"}}},
			Usage: Usage{
				PromptTokens:            10,
				CompletionTokens:        20,
				TotalTokens:             30,
				CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 5},
			},
		})
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test"}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID: "test-model", streaming: false, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	// Use a startTime in the past so totalDuration is positive,
	// but set ttft very large so generationDuration = totalDuration - ttft <= 0,
	// triggering the else-if fallback at line 719.
	startTime := time.Now()
	time.Sleep(2 * time.Millisecond)
	h.handleNonStreamingResponse(inner, req, logData, resp, startTime, 0, 0, 0, 0, 0, 0, 0, 0, 999999.0, "", 1)

	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
	// TPS should be computed via the fallback (totalDuration path)
	if logData.tokensPerSecond <= 0 {
		t.Errorf("expected positive TPS from fallback path, got %f", logData.tokensPerSecond)
	}
	if logData.tokensCompletionReasoning != 5 {
		t.Errorf("expected tokensCompletionReasoning=5, got %d", logData.tokensCompletionReasoning)
	}
}

// ---------------------------------------------------------------------------
// handleNonStreamingResponse <thinking> tags merge with existing reasoning_content (line 792-794)
// ---------------------------------------------------------------------------

func TestHandleNonStreamingResponse_ThinkingTagsAppendToReasoningContent(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ChatCompletionResponse{
			ID:      "chatcmpl-1",
			Object:  "chat.completion",
			Created: 1234,
			Model:   "test-model",
			Choices: []Choice{{
				Index: 0,
				Message: Message{
					Role:             "assistant",
					Content:          "<thinking>reasoning here</thinking>The answer is 42.",
					ReasoningContent: "prior reasoning",
				},
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		})
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test"}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID: "test-model", streaming: false, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	var result ChatCompletionResponse
	if err := json.Unmarshal(inner.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	// Should contain both "prior reasoning" and "reasoning here" (appended, not replaced)
	rc := result.Choices[0].Message.ReasoningContent
	if !strings.Contains(rc, "prior reasoning") {
		t.Errorf("expected reasoning_content to contain 'prior reasoning', got %q", rc)
	}
	if !strings.Contains(rc, "reasoning here") {
		t.Errorf("expected reasoning_content to contain 'reasoning here', got %q", rc)
	}
	// Content should have <thinking> tags stripped
	if c, ok := result.Choices[0].Message.Content.(string); ok {
		if strings.Contains(c, "<thinking>") {
			t.Errorf("expected content without thinking tags, got %q", c)
		}
		if !strings.Contains(c, "The answer is 42.") {
			t.Errorf("expected remaining content, got %q", c)
		}
	}
}

// ---------------------------------------------------------------------------
// handleNonStreamingResponse decode error (line 804-826)
// ---------------------------------------------------------------------------

func TestHandleNonStreamingResponse_UpstreamNonJSONResponse(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Non-JSON body — will fail json.Decode
		w.Write([]byte("not valid json at all"))
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL, strings.NewReader(`{"model":"test"}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID: "test-model", streaming: false, virtualKeyName: "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0, state: "pending",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.handleNonStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 1)

	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "response decode error") {
		t.Errorf("expected errorMessage containing 'response decode error', got %q", logData.errorMessage)
	}
	// Non-JSON upstream body on a 200 response causes handleNonStreamingResponse
	// to wrap it in an OpenAI error envelope at proxy.go line 825.
	// The upstream status (200) is forwarded, so client gets 200 with error JSON.
	if inner.Code != http.StatusOK {
		t.Errorf("expected 200 (upstream status forwarded), got %d", inner.Code)
	}
}
