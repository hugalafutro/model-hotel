package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// buildNativeAnthropicRequest forwards the original Messages body (model
// rewritten) to the provider's native /v1/messages with anthropic auth headers.
func TestBuildNativeAnthropicRequest(t *testing.T) {
	h := &Handler{}
	st := &requestState{anthropicRawBody: []byte(`{"model":"hotel/claude-x","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)}
	cand := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "claude-opus-4-8"},
		provider: &provider.Provider{ID: uuid.New(), Name: "Anthropic", BaseURL: "https://api.anthropic.com"},
		apiKey:   "sk-ant-test",
	}
	req, ptype, url, err := h.buildNativeAnthropicRequest(context.Background(), st, cand, "anthropic")
	if err != nil {
		t.Fatalf("buildNativeAnthropicRequest: %v", err)
	}
	if ptype != "anthropic" {
		t.Errorf("ptype = %q, want anthropic", ptype)
	}
	if !strings.HasSuffix(url, "/v1/messages") {
		t.Errorf("url = %q, want suffix /v1/messages", url)
	}
	body, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(body), `"claude-opus-4-8"`) || strings.Contains(string(body), "hotel/claude-x") {
		t.Errorf("body model not rewritten: %s", body)
	}
	if req.Header.Get("x-api-key") == "" {
		t.Error("missing x-api-key header")
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", req.Header.Get("Content-Type"))
	}
}

// handleNativeNonStreaming forwards the Anthropic message verbatim and meters
// from its usage block.
func TestHandleNativeNonStreaming(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	anthropicBody := `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":3}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(anthropicBody)), Header: make(http.Header)}

	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_ignored", "m")
	aw.bindNativeFlag(&native)

	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-x",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	st := &requestState{startTime: time.Now(), logData: logData}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0)
	aw.Finalize()

	if outcome != outcomeServed {
		t.Errorf("outcome = %v, want outcomeServed", outcome)
	}
	if logData.state != "completed" {
		t.Errorf("state = %q, want completed", logData.state)
	}
	if logData.tokensPrompt != 9 || logData.tokensCompletion != 3 {
		t.Errorf("usage = (%d,%d), want (9,3)", logData.tokensPrompt, logData.tokensCompletion)
	}
	if rec.Body.String() != anthropicBody {
		t.Errorf("verbatim body mismatch:\n got %s\nwant %s", rec.Body.String(), anthropicBody)
	}
}

// errorReadCloser fails on Read, simulating an upstream body that drops
// mid-transfer after a 200 header.
type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (errorReadCloser) Close() error             { return nil }

// A read failure on the native non-streaming 200 body must finalize the log row
// (state=failed) instead of leaving it orphaned in the in-flight state.
func TestHandleNativeNonStreaming_ReadErrorFinalizesLog(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{}, Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_e", "m")
	aw.bindNativeFlag(&native)
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-x",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	st := &requestState{startTime: time.Now(), logData: logData}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0)
	aw.Finalize()

	if outcome != outcomeFatal {
		t.Errorf("outcome = %v, want outcomeFatal", outcome)
	}
	if logData.state != "failed" {
		t.Errorf("state = %q, want failed (log row must not orphan)", logData.state)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %v, want KindProviderError", logData.errorKind)
	}
	if logData.statusCode != http.StatusBadGateway {
		t.Errorf("statusCode = %d, want 502", logData.statusCode)
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("client status = %d, want 502", rec.Code)
	}
}

// runNativeStream drives a complete Anthropic SSE body through the real streaming
// pipeline with rawPassthrough enabled (the native passthrough path), returning
// the forwarded client bytes and the finalized log row. It mirrors the harness in
// ttft_stall_test.go.
func runNativeStream(t *testing.T, sseBody string) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sseBody))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-test",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	opts := streamOptions{
		responseHeaderMs: 10.0,
		vkHash:           "test-hash",
		attempt:          1,
		rawPassthrough:   true,
	}
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), opts)
	return w, logData
}

const nativeStreamHead = `event: message_start
data: {"type":"message_start","message":{"id":"msg_up","type":"message","role":"assistant","model":"claude","content":[],"usage":{"input_tokens":12,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

`

// Happy path: a full stream ending in message_stop logs completed, meters usage
// from message_start/message_delta, and forwards the Anthropic frames verbatim.
func TestNativeStream_CompletedWithUsage(t *testing.T) {
	body := nativeStreamHead + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	w, logData := runNativeStream(t, body)

	if logData.state != "completed" {
		t.Errorf("state = %q, want completed (err: %s)", logData.state, logData.errorMessage)
	}
	if logData.tokensPrompt != 12 || logData.tokensCompletion != 5 {
		t.Errorf("usage = (%d,%d), want (12,5)", logData.tokensPrompt, logData.tokensCompletion)
	}
	out := w.Body.String()
	// Verbatim framing reconstruction: event lines AND data lines pass through,
	// and no OpenAI [DONE] sentinel is injected.
	for _, want := range []string{"event: message_start", `"type":"text_delta"`, "Hello", "event: message_stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("forwarded body missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "[DONE]") {
		t.Errorf("native stream must not inject [DONE]:\n%s", out)
	}
}

// A clean EOF that never delivered message_stop is a mid-stream truncation: it
// must log failed (not completed), or the partial output would be billed as a
// complete response.
func TestNativeStream_TruncatedBeforeMessageStop(t *testing.T) {
	_, logData := runNativeStream(t, nativeStreamHead) // no message_stop

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed (truncated before message_stop)", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "message_stop") {
		t.Errorf("errorMessage = %q, want it to mention message_stop", logData.errorMessage)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %v, want KindProviderError", logData.errorKind)
	}
}

// A provider-sent error event must be both forwarded to the client (verbatim)
// AND recorded so the request logs failed with the provider's message.
func TestNativeStream_ProviderErrorEvent(t *testing.T) {
	body := nativeStreamHead + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	w, logData := runNativeStream(t, body)

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed (provider error event)", logData.state)
	}
	if logData.errorMessage != "Overloaded" {
		t.Errorf("errorMessage = %q, want Overloaded", logData.errorMessage)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %v, want KindProviderError", logData.errorKind)
	}
	// The client still sees the error frame.
	if !strings.Contains(w.Body.String(), "overloaded_error") {
		t.Errorf("error frame not forwarded to client:\n%s", w.Body.String())
	}
}
