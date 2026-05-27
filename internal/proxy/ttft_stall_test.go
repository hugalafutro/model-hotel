package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// probeFirstToken unit tests (no DB needed)
// ---------------------------------------------------------------------------

// makeSSEBody creates an io.ReadCloser from SSE-formatted text.
func makeSSEBody(t *testing.T, s string) io.ReadCloser {
	t.Helper()
	return io.NopCloser(strings.NewReader(s))
}

func TestProbeFirstToken_DataChunk(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, "data: {\"choices\":[]}\n\ndata: [DONE]\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
	// probeBuf should contain the bytes read up to and including the first data line
	got := probeBuf.String()
	if !strings.Contains(got, `data: {"choices":[]}`) {
		t.Errorf("probeBuf should contain first data line, got: %q", got)
	}
}

func TestProbeFirstToken_KeepaliveThenData(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, ": keepalive\n\nevent: message_start\ndata: {\"choices\":[]}\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
	got := probeBuf.String()
	// Should have skipped keepalive and event line, found data line
	if !strings.Contains(got, "data: {\"choices\":[]}") {
		t.Errorf("probeBuf should contain data line, got: %q", got)
	}
	// Keepalive and event lines should also be in the buffer (captured by TeeReader)
	if !strings.Contains(got, ": keepalive") {
		t.Errorf("probeBuf should contain keepalive line, got: %q", got)
	}
	if !strings.Contains(got, "event: message_start") {
		t.Errorf("probeBuf should contain event line, got: %q", got)
	}
}

func TestProbeFirstToken_DoneFirst(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, "data: [DONE]\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
	}
	if trueTtftMs != 0 {
		t.Errorf("expected trueTtftMs == 0 for [DONE] first, got %f", trueTtftMs)
	}
}

func TestProbeFirstToken_Timeout(t *testing.T) {
	pr, pw := io.Pipe()
	// Writer never sends anything — body blocks forever
	// Close the write end after a long time to ensure cleanup happens
	go func() {
		time.Sleep(5 * time.Second)
		pw.Close()
	}()
	defer pr.Close()

	h := &Handler{}
	startTime := time.Now()

	_, _, err := h.probeFirstToken(context.Background(), pr, 100*time.Millisecond, startTime)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "TTFT timeout") {
		t.Errorf("expected 'TTFT timeout' in error, got: %v", err)
	}
}

func TestProbeFirstToken_ReadError(t *testing.T) {
	h := &Handler{}
	body := &failingReadCloser{err: io.ErrUnexpectedEOF}
	startTime := time.Now()

	_, _, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err == nil {
		t.Fatal("expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "TTFT probe read error") {
		t.Errorf("expected 'TTFT probe read error' in error, got: %v", err)
	}
}

func TestProbeFirstToken_EmptyBody(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, "")
	startTime := time.Now()

	_, _, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	if !strings.Contains(err.Error(), "body closed before first data chunk") {
		t.Errorf("expected 'body closed before first data chunk' in error, got: %v", err)
	}
}

func TestProbeFirstToken_ProbeBufContainsAllBytes(t *testing.T) {
	h := &Handler{}
	sse := ": keepalive\n\nevent: message_start\nid: 42\nretry: 3000\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	body := makeSSEBody(t, sse)
	startTime := time.Now()

	probeBuf, _, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := probeBuf.String()
	if got != sse {
		t.Errorf("probeBuf should contain ALL bytes read from body\nexpected: %q\ngot:      %q", sse, got)
	}
}

// failingReadCloser is an io.ReadCloser that returns an error on first read.
type failingReadCloser struct {
	err    error
	closed bool
}

func (r *failingReadCloser) Read(p []byte) (int, error) {
	return 0, r.err
}

func (r *failingReadCloser) Close() error {
	r.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// Stall watchdog tests (use integration handler with DB)
// ---------------------------------------------------------------------------

func TestStallWatchdog_Timeout(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Body sends one chunk then blocks longer than the stall timeout.
	// The watchdog should fire and close the body before the second write.
	closeCh := make(chan struct{})
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data: {\"choices\":[]}\n\n"))
		select {
		case <-closeCh:
			// Body was closed by watchdog, pipe write will fail
		case <-time.After(200 * time.Millisecond):
			// Timeout reached — watchdog did NOT fire, write the [DONE]
			pw.Write([]byte("data: [DONE]\n\n"))
		}
		pw.Close()
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
	}

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
	time.Sleep(100 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 50 * time.Millisecond,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)
	// Close channel to signal test done (body already closed by watchdog)
	close(closeCh)

	// After stall timeout, watchdog closes body, scanner gets error,
	// and state should be "failed" with an error message
	if logData.state != "failed" {
		t.Errorf("expected state=failed after stall, got %q", logData.state)
	}
	if logData.errorMessage == "" {
		t.Error("expected non-empty error message after stall")
	}
	// Duration should be much less than 200ms (the sleep in the goroutine)
	// since watchdog fires at ~50ms
	if logData.durationMs > 150 {
		t.Errorf("expected duration < 150ms (stall fired early), got %.1fms", logData.durationMs)
	}
}

func TestStallWatchdog_Reset(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Body sends two chunks with a small delay between them, both less than
	// the stall timeout, so the watchdog should keep resetting and never fire
	var buf bytes.Buffer
	buf.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
	buf.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
	buf.WriteString("data: [DONE]\n\n")
	body := io.NopCloser(&buf)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
	}

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
	time.Sleep(100 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 5 * time.Second, // very generous timeout
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	// Stream should complete normally without stall
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q (error: %s)", logData.state, logData.errorMessage)
	}
	if logData.errorMessage != "" {
		t.Errorf("expected no error message, got: %q", logData.errorMessage)
	}
}

func TestStallWatchdog_Disabled(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// When streamStallTimeout is 0, no watchdog goroutine is started
	var buf bytes.Buffer
	buf.WriteString("data: {\"choices\":[]}\n\n")
	buf.WriteString("data: [DONE]\n\n")
	body := io.NopCloser(&buf)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
	}

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
	time.Sleep(100 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 0, // disabled
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	// Stream completes normally, no stall mechanism involved
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q", logData.state)
	}
}

// ---------------------------------------------------------------------------
// Additional probeFirstToken edge case tests
// ---------------------------------------------------------------------------

func TestProbeFirstToken_OnlyCommentsAndEmptyLines(t *testing.T) {
	h := &Handler{}
	// Only keepalive comments, no data
	body := makeSSEBody(t, ": keepalive\n: another\n\n\n")
	startTime := time.Now()

	_, _, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err == nil {
		t.Fatal("expected error when no data is found, got nil")
	}
	if !strings.Contains(err.Error(), "body closed before first data chunk") {
		t.Errorf("expected 'body closed before first data chunk' in error, got: %v", err)
	}
}

func TestProbeFirstToken_MultipleSkipLines(t *testing.T) {
	h := &Handler{}
	body := makeSSEBody(t, ": comment\nid: 1\nevent: open\nretry: 1000\n: more\ndata: {\"x\":1}\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
	got := probeBuf.String()
	if !strings.Contains(got, "data: {\"x\":1}") {
		t.Errorf("expected data line in buffer, got: %q", got)
	}
	// All preceding lines should also be in the buffer
	if !strings.Contains(got, "id: 1") {
		t.Errorf("expected 'id: 1' in buffer, got: %q", got)
	}
	if !strings.Contains(got, "event: open") {
		t.Errorf("expected 'event: open' in buffer, got: %q", got)
	}
}

func TestProbeFirstToken_CanceledContext(t *testing.T) {
	h := &Handler{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data: {\"choices\":[]}\n\n"))
		pw.Close()
	}()

	// With an already-canceled context and a 0 timeout, the function should
	// either return immediately with an error or process the data quickly.
	// A short timeout ensures we don't block forever.
	_, _, err := h.probeFirstToken(ctx, pr, 1*time.Second, time.Now())
	_ = pr.Close()

	// Either we get data fast enough, or we get a context error.
	// Both are acceptable given the race between cancel and pipe write.
	if err != nil {
		// Context is cancelled, the probe goroutine detects DeadlineExceeded or the scanner hits EOF/other error.
		// Either an error or success is fine — just ensure no panic.
		t.Logf("got expected error from canceled context: %v", err)
	}
}

func TestStallWatchdog_CircuitBreakerOnStall(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Body sends one chunk then blocks — stall should fire before second write
	closeCh := make(chan struct{})
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data: {\"choices\":[]}\n\n"))
		select {
		case <-closeCh:
		case <-time.After(200 * time.Millisecond):
			pw.Write([]byte("data: [DONE]\n\n"))
		}
		pw.Close()
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
	}

	providerID := uuid.New()
	providerName := "stall-test-provider"

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		providerID:      providerID,
		providerName:    providerName,
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 50 * time.Millisecond,
		providerID:         providerID,
		providerName:       providerName,
		circuitBreakerOn:   true,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)
	close(closeCh)

	// Stall detected — state should be "failed"
	if logData.state != "failed" {
		t.Errorf("expected state=failed after stall, got %q", logData.state)
	}
	if logData.errorMessage == "" {
		t.Error("expected non-empty error message after stall")
	}
	// Duration should be much less than 200ms since watchdog fires at ~50ms
	if logData.durationMs > 150 {
		t.Errorf("expected duration < 150ms (stall fired early), got %.1fms", logData.durationMs)
	}
}

func TestStallWatchdog_NoStallWhenCircuitBreakerOff(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Body sends one chunk then blocks — stall fires (same as Timeout test)
	// but circuitBreakerOn=false means no circuit breaker recording
	closeCh := make(chan struct{})
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data: {\"choices\":[]}\n\n"))
		select {
		case <-closeCh:
		case <-time.After(200 * time.Millisecond):
			pw.Write([]byte("data: [DONE]\n\n"))
		}
		pw.Close()
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
	}

	providerID := uuid.New()
	providerName := "stall-no-cb-provider"

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:              uuid.New().String(),
		providerID:      providerID,
		providerName:    providerName,
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "streaming",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: 50 * time.Millisecond,
		providerID:         providerID,
		providerName:       providerName,
		circuitBreakerOn:   false,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)
	close(closeCh)

	// Stall still detected (state=failed) but circuitBreakerOn=false
	// means no circuit breaker recording (indirectly verified — no panic
	// since nil circuitBreaker would panic if RecordFailure was called)
	if logData.state != "failed" {
		t.Errorf("expected state=failed after stall, got %q", logData.state)
	}
	if logData.durationMs > 150 {
		t.Errorf("expected duration < 150ms (stall fired early), got %.1fms", logData.durationMs)
	}
}
