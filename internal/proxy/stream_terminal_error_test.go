package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// streamingLog builds the interim "streaming" log row the finalize path needs.
func streamingLog() *requestLogData {
	return &requestLogData{
		id:             uuid.New().String(),
		modelID:        "test-model",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
}

// lastSSEError returns the error object of the last `data:` line carrying one.
func lastSSEError(t *testing.T, body string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, line := range strings.Split(body, "\n") {
		p, ok := strings.CutPrefix(line, "data: ")
		if !ok || strings.TrimSpace(p) == "[DONE]" {
			continue
		}
		var chunk struct {
			Error map[string]any `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(p)), &chunk) == nil && chunk.Error != nil {
			found = chunk.Error
		}
	}
	return found
}

// A stall after the first byte cannot fail over, so the stream must end with a
// terminal OpenAI error frame plus [DONE] rather than a bare connection close.
func TestHandleStreamingResponse_StallEndsWithErrorFrame(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// One content chunk, then the reader blocks forever so the watchdog fires.
	body := newBlockUntilClosedReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := streamingLog()
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	opts := streamOptions{responseHeaderMs: 10, streamStallTimeout: 30 * time.Millisecond, vkHash: "test-hash", attempt: 1}
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), opts)

	out := w.Body.String()
	if !strings.Contains(out, `"content":"hi"`) {
		t.Fatalf("content chunk should still forward: %s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n")+"\n", "data: [DONE]\n") {
		t.Errorf("stream must end with [DONE]: %q", out)
	}
	e := lastSSEError(t, out)
	if e == nil {
		t.Fatalf("expected a terminal error frame, got: %s", out)
	}
	if e["code"] != string(KindProviderTimeout) {
		t.Errorf("code = %v, want %q", e["code"], KindProviderTimeout)
	}
	if logData.state != "failed" {
		t.Errorf("state = %q, want failed", logData.state)
	}
}

// A process shutdown mid-stream ends the client stream with a restart error
// frame instead of a cut connection, and is not charged to the provider.
func TestHandleStreamingResponse_ShutdownEndsWithErrorFrame(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	h.shutdown = make(chan struct{})
	// Cut quickly instead of waiting the production grace.
	prevGrace := shutdownStreamGrace
	shutdownStreamGrace = 20 * time.Millisecond
	defer func() { shutdownStreamGrace = prevGrace }()

	body := newBlockUntilClosedReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := streamingLog()
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	go func() {
		time.Sleep(30 * time.Millisecond)
		close(h.shutdown)
	}()

	opts := streamOptions{responseHeaderMs: 10, streamStallTimeout: 0, vkHash: "test-hash", attempt: 1, circuitBreakerOn: true}
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), opts)

	out := w.Body.String()
	e := lastSSEError(t, out)
	if e == nil {
		t.Fatalf("expected a terminal error frame, got: %s", out)
	}
	if e["code"] != string(KindInternal) {
		t.Errorf("code = %v, want %q", e["code"], KindInternal)
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "restarting") {
		t.Errorf("message = %q, want a restart notice", msg)
	}
	if logData.errorKind != KindInternal {
		t.Errorf("errorKind = %v, want internal (not a provider fault)", logData.errorKind)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n")+"\n", "data: [DONE]\n") {
		t.Errorf("stream must end with [DONE]: %q", out)
	}
}

// A provider that sends its own error frame keeps that as the one error the
// client sees; no second terminal frame is appended.
func TestHandleStreamingResponse_ProviderErrorFrameNotDoubled(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"upstream boom\",\"type\":\"server_error\"}}\n\n")
	}))
	defer upstream.Close()

	req, _ := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("contact upstream: %v", err)
	}
	defer resp.Body.Close()

	w := httptest.NewRecorder()
	logData := streamingLog()
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1})

	if n := strings.Count(w.Body.String(), `"error"`); n != 1 {
		t.Errorf("expected exactly one error frame (the provider's), got %d:\n%s", n, w.Body.String())
	}
}

// blockUntilClosedReader emits its data once, then blocks every subsequent
// Read until Close is called (by the watchdog on stall or shutdown),
// simulating a provider that goes silent mid-stream.
type blockUntilClosedReader struct {
	data   string
	offset int
	closed chan struct{}
}

func newBlockUntilClosedReader(data string) *blockUntilClosedReader {
	return &blockUntilClosedReader{data: data, closed: make(chan struct{})}
}

func (r *blockUntilClosedReader) Read(p []byte) (int, error) {
	if r.offset < len(r.data) {
		n := copy(p, r.data[r.offset:])
		r.offset += n
		return n, nil
	}
	<-r.closed
	return 0, io.EOF
}

func (r *blockUntilClosedReader) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

// A raw scanner/transport error (which can embed the gateway's own address and
// the upstream's) must not reach the client: the terminal frame carries a
// coarse gateway-authored message while the log keeps the detail.
func TestHandleStreamingResponse_TransportErrorClientMessageIsCoarse(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const raw = "read tcp 172.18.0.2:44322->10.0.0.50:11434: read: connection reset by peer"
	body := io.NopCloser(&errorAfterDataReader{
		data: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n",
		err:  &stringError{raw},
	})
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := streamingLog()
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1})

	out := w.Body.String()
	if strings.Contains(out, "172.18.0.2") || strings.Contains(out, "10.0.0.50") || strings.Contains(out, "connection reset") {
		t.Fatalf("raw transport detail reached the client:\n%s", out)
	}
	e := lastSSEError(t, out)
	if e == nil || !strings.Contains(e["message"].(string), "upstream connection error") {
		t.Fatalf("expected a coarse terminal frame, got: %s", out)
	}
	if !strings.Contains(logData.errorMessage, "connection reset") {
		t.Errorf("the log must keep the transport detail, got %q", logData.errorMessage)
	}
}

// A native Anthropic stream that emitted message_stop and then went silent is a
// real completion: no error event is appended, and it is not charged.
func TestNativeStream_MessageStopThenStallNoErrorFrame(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	sse := nativeStreamHead +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	body := newBlockUntilClosedReader(sse) // blocks after message_stop instead of EOF
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := streamingLog()
	logData.modelID = "claude-test"
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	opts := streamOptions{responseHeaderMs: 10, streamStallTimeout: 30 * time.Millisecond, vkHash: "test-hash", attempt: 1, rawPassthrough: true, circuitBreakerOn: true}
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), opts)

	if strings.Contains(w.Body.String(), "\"type\":\"error\"") {
		t.Fatalf("no error event should follow message_stop:\n%s", w.Body.String())
	}
	if logData.state == "failed" {
		t.Errorf("a stream that saw message_stop is complete, not failed")
	}
}

// Close broadcasts the shutdown signal exactly once.
func TestHandlerClose_ClosesShutdownOnce(t *testing.T) {
	h := &Handler{shutdown: make(chan struct{})}
	h.Close()
	select {
	case <-h.shutdown:
	default:
		t.Fatal("Close did not close the shutdown channel")
	}
	h.Close() // must not panic on a second close
}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

// A stream that finishes on its own during the shutdown grace completes
// normally — no "gateway restarting" frame, its real [DONE] stands.
func TestHandleStreamingResponse_ShutdownGraceLetsStreamFinish(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	h.shutdown = make(chan struct{})
	prevGrace := shutdownStreamGrace
	shutdownStreamGrace = 2 * time.Second // long enough for the stream to end first
	defer func() { shutdownStreamGrace = prevGrace }()

	// Shutdown fires immediately, but the body is fully readable to [DONE],
	// so the scanner reaches EOF well within the grace window.
	close(h.shutdown)
	body := io.NopCloser(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := streamingLog()
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1})

	out := w.Body.String()
	if strings.Contains(out, "restarting") || strings.Contains(out, "\"error\"") {
		t.Fatalf("a stream that finished in the grace must not get a restart frame:\n%s", out)
	}
	if logData.state != "completed" {
		t.Errorf("state = %q, want completed", logData.state)
	}
}
