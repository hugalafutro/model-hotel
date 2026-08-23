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
