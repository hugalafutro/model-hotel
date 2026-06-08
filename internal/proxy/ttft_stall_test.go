package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
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
		return
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
		return
	}
	if trueTtftMs != 0 {
		t.Errorf("expected trueTtftMs == 0 for [DONE] first, got %f", trueTtftMs)
	}
}

// TestProbeFirstToken_SingleToken verifies that the probe correctly captures
// a single SSE data chunk and returns the probe buffer with its contents.
func TestProbeFirstToken_SingleToken(t *testing.T) {
	h := &Handler{}
	sse := "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"
	body := makeSSEBody(t, sse)
	startTime := time.Now()

	probeBuf, ttftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
	}
	if ttftMs <= 0 {
		t.Errorf("expected ttftMs > 0, got %f", ttftMs)
	}
	got := probeBuf.String()
	if !strings.Contains(got, `data: {"id":"chatcmpl-1"`) {
		t.Errorf("probeBuf should contain the first data line, got: %q", got)
	}
}

// TestProbeFirstToken_MultipleDataChunks verifies that the probe returns
// immediately upon finding the FIRST data chunk, even when more follow.
// The probe buffer should contain the preamble and first data line but
// not subsequent chunks (they haven't been read yet).
func TestProbeFirstToken_MultipleDataChunks(t *testing.T) {
	h := &Handler{}
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\ndata: [DONE]\n\n"
	body := makeSSEBody(t, sse)
	startTime := time.Now()

	probeBuf, ttftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ttftMs <= 0 {
		t.Errorf("expected ttftMs > 0, got %f", ttftMs)
	}
	got := probeBuf.String()
	// Should contain first data chunk
	if !strings.Contains(got, `"content":"a"`) {
		t.Errorf("probeBuf should contain first chunk content 'a', got: %q", got)
	}
}

// TestProbeFirstToken_MixedSSELines verifies the probe correctly skips
// non-data SSE lines (keepalive comments, event/id/retry directives)
// while still capturing them in the probe buffer for replay.
func TestProbeFirstToken_MixedSSELines(t *testing.T) {
	h := &Handler{}
	sse := ": ping\n\nevent: message_start\nid: msg-1\nretry: 5000\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	body := makeSSEBody(t, sse)
	startTime := time.Now()

	probeBuf, ttftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ttftMs <= 0 {
		t.Errorf("expected ttftMs > 0, got %f", ttftMs)
	}
	got := probeBuf.String()
	// All lines should be captured by TeeReader for replay
	if !strings.Contains(got, ": ping") {
		t.Error("probeBuf should contain keepalive comment")
	}
	if !strings.Contains(got, "event: message_start") {
		t.Error("probeBuf should contain event directive")
	}
	if !strings.Contains(got, "id: msg-1") {
		t.Error("probeBuf should contain id directive")
	}
	if !strings.Contains(got, "retry: 5000") {
		t.Error("probeBuf should contain retry directive")
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
		return
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
		return
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
		return
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

	// Use io.Pipe with timed writes to verify watchdog timer resets.
	// Stall timeout is 200ms. We send chunks at 0ms, 50ms, 100ms — all
	// within the timeout window — so the watchdog should keep resetting
	// and never fire. Stream completes at ~150ms.
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		time.Sleep(50 * time.Millisecond)
		pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n"))
		time.Sleep(50 * time.Millisecond)
		pw.Write([]byte("data: [DONE]\n\n"))
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
		streamStallTimeout: 200 * time.Millisecond,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	// Stream should complete normally without stall — watchdog was reset
	// by each chunk and never fired.
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
		return
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

	// Set circuit breaker threshold to 1 so a single failure opens the circuit.
	if err := h.settingsRepo.Set(context.Background(), "circuit_breaker_threshold", "1"); err != nil {
		t.Fatalf("failed to set circuit_breaker_threshold: %v", err)
	}
	defer func() {
		_ = h.settingsRepo.Set(context.Background(), "circuit_breaker_threshold", "5")
	}()
	h.settingsRepo.InvalidateCache("circuit_breaker_threshold")

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
	// Verify circuit breaker was actually called: with threshold=1, a
	// single RecordFailure should transition the provider to StateOpen.
	cbState := h.circuitBreaker.GetState(providerID)
	if cbState != failover.StateOpen {
		t.Errorf("expected circuit breaker StateOpen after stall, got %s", cbState)
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

// TestStallWatchdog_RapidChunksNoPanic exercises the stall watchdog by sending
// chunks rapidly to verify it handles rapid resets without panicking.
func TestStallWatchdog_RapidChunksNoPanic(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Send chunks very rapidly with a short stall timeout to exercise
	// the watchdog timer reset logic without panicking.
	stallTimeout := 10 * time.Millisecond
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 50; i++ {
			pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
			time.Sleep(3 * time.Millisecond) // much faster than stallTimeout
		}
		pw.Write([]byte("data: [DONE]\n\n"))
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
		streamStallTimeout: stallTimeout,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	// Either the stream completes normally (watchdog reset kept pace)
	// or a stall is detected (timer fired between chunks).
	// Both are acceptable outcomes — this test primarily exercises
	// the timer.Reset code path without panicking.
	if logData.state != "completed" && logData.state != "failed" {
		t.Errorf("expected completed or failed, got %q", logData.state)
	}
}

// TestProbeFirstToken_RaceRecovery verifies that probeFirstToken returns
// success when the scanner has already read a complete SSE data line into the
// TeeReader buffer but the body returns an error on the next read. This
// exercises the race-recovery path in the scanner error handler that scans
// the buffer for a captured data line.
// TestProbeFirstToken_RaceRecovery verifies that probeFirstToken returns
// success when the scanner has already read a complete SSE data line into the
// TeeReader buffer but the body returns an error on the next read. This
// exercises the race-recovery path in the scanner error handler that scans
// the buffer for a captured data line.
// TestProbeFirstToken_RaceRecovery verifies that probeFirstToken returns
// success when the scanner has already read a complete SSE data line into the
// TeeReader buffer but the body returns an error on the next read. This
// exercises the race-recovery path in the scanner error handler that scans
// the buffer for a captured data line.
func TestProbeFirstToken_RaceRecovery(t *testing.T) {
	// Simulate a body that returns one complete SSE data line then errors.
	// This models the race: scanner reads "data: hello\n" (captured by
	// TeeReader into buf) → goroutine closes body → scanner.Scan() returns
	// false with an error → error handler finds data in buf.
	body := io.NopCloser(&errorAfterDataReader{
		data: "data: hello world\n",
		err:  errors.New("body closed by deadline goroutine"),
	})

	h := &Handler{}
	startTime := time.Now()

	probeBuf, ttft, err := h.probeFirstToken(
		context.Background(),
		body,
		5*time.Second, // generous timeout — the error comes from body, not timeout
		startTime,
	)

	if err != nil {
		t.Fatalf("expected race recovery success, got error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected non-nil probeBuf")
		return
	}
	if ttft <= 0 {
		t.Errorf("expected positive ttft, got %f", ttft)
	}
	if !strings.Contains(probeBuf.String(), "data: hello world") {
		t.Errorf("expected probeBuf to contain data line, got %q", probeBuf.String())
	}
}

// TestProbeFirstToken_PartialLineAccepted documents that bufio.Scanner's
// ScanLines split function returns the last non-empty line even without a
// trailing newline (atEOF rule). The scanner loop processes a partial
// "data: hel" as a valid data line and returns success — the recovery
// block is never reached. The recovery block's \n guard is defense-in-depth
// for any edge case where a partial line reaches it.
func TestProbeFirstToken_PartialLineAccepted(t *testing.T) {
	// The scanner's ScanLines split function returns the last non-empty line
	// even without a trailing newline when Read() returns an error. So the
	// scanner loop processes "data: hel" as a data line and returns success.
	// This is correct: the upstream DID send partial data, and the probe's
	// job is to confirm the provider is responsive.
	body := io.NopCloser(&errorAfterDataReader{
		data: "data: hel", // no trailing newline
		err:  errors.New("body closed by deadline goroutine"),
	})

	h := &Handler{}
	startTime := time.Now()

	probeBuf, ttft, err := h.probeFirstToken(
		context.Background(),
		body,
		5*time.Second,
		startTime,
	)

	// Scanner processes the partial line as valid (ScanLines last-line rule),
	// so the probe returns success. The recovery block's \n guard is only
	// reached when scanner.Scan() returns false — which doesn't happen here.
	if err != nil {
		t.Fatalf("expected success (scanner handles partial lines), got error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected non-nil probeBuf")
		return
	}
	if !strings.Contains(probeBuf.String(), "data: hel") {
		t.Errorf("expected probeBuf to contain partial data line, got %q", probeBuf.String())
	}
	if ttft <= 0 {
		t.Errorf("expected positive ttft, got %f", ttft)
	}
}

// slowReader delivers one byte at a time from its data source.
// Combined with a short probe timeout, this allows the timeout goroutine
// to close the body mid-scan, attempting to trigger the scanner error
// recovery path in probeFirstToken (lines 1909-1936).
//
// Note: In practice, bufio.Scanner's ScanLines split function returns the
// last line even when Read() returns an error (atEOF rule), so the scanner
// loop at line 1877 processes the data line before the recovery path is
// reached. The recovery path (1921-1934) is defense-in-depth for a race
// between TeeReader and scanner that is practically impossible to trigger
// deterministically.
type slowReader struct {
	data   string
	offset int
	closed bool
	mu     sync.Mutex
}

func (r *slowReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.offset]
	r.offset++
	return 1, nil
}

func (r *slowReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// TestProbeFirstToken_ScannerErrorRecoveryWithDataInBuffer verifies that
// probeFirstToken succeeds when data arrives slowly and a short timeout
// fires. The scanner may find the data line via the normal path (most likely)
// or via the recovery path (race-dependent).
func TestProbeFirstToken_ScannerErrorRecoveryWithDataInBuffer(t *testing.T) {
	h := &Handler{}

	body := &slowReader{
		data: "data: hello world\n\ndata: second\n",
	}

	startTime := time.Now()
	probeBuf, ttft, err := h.probeFirstToken(
		context.Background(),
		body,
		100*time.Millisecond, // generous timeout — slowReader delivers 1 byte/ms
		startTime,
	)

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected non-nil probeBuf on success")
	}
	if ttft <= 0 {
		t.Errorf("expected positive ttft, got %f", ttft)
	}

	bufStr := probeBuf.String()
	if !strings.Contains(bufStr, "data:") {
		t.Errorf("expected data: line in buffer, got %q", bufStr)
	}
}

// TestProbeFirstToken_ScannerErrorRecovery_PipeRace uses io.Pipe to test
// probeFirstToken with a reader that errors after delivering data. In practice,
// the bufio.Scanner processes the complete line before the pipe close fires,
// so this exercises the normal scan path (not the recovery path at line 1921).
// The recovery path is defense-in-depth for a race that's practically impossible
// to trigger deterministically. Kept as a robustness test.
func TestProbeFirstToken_ScannerErrorRecovery_PipeRace(t *testing.T) {
	h := &Handler{}

	pr, pw := io.Pipe()

	// Writer goroutine: send a complete data line, give the TeeReader time
	// to capture it, then close the pipe with an error.
	go func() {
		pw.Write([]byte("data: hello world\n"))
		// Small sleep to let the TeeReader capture the bytes into buf
		// but NOT enough for the scanner to fully process the line.
		time.Sleep(100 * time.Microsecond)
		pw.CloseWithError(errors.New("body closed by timeout goroutine"))
	}()

	startTime := time.Now()
	probeBuf, ttft, err := h.probeFirstToken(
		context.Background(),
		pr,            // io.PipeReader implements io.ReadCloser
		5*time.Second, // generous timeout — error comes from pipe, not timeout
		startTime,
	)

	// Accept either:
	// 1. Success: scanner found the data line before pipe closed (normal path)
	// 2. Success via recovery: scanner errored, buffer had the data (recovery path)
	if err != nil {
		t.Fatalf("expected success (either path), got error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected non-nil probeBuf")
	}
	if ttft <= 0 {
		t.Errorf("expected positive ttft, got %f", ttft)
	}

	bufStr := probeBuf.String()
	if !strings.Contains(bufStr, "data: hello world") {
		t.Errorf("expected data line in buffer, got %q", bufStr)
	}
}

func TestStallWatchdog_ProgressiveTimeout(t *testing.T) {
	// Verify that after 50 chunks the stall timeout is extended 3×.
	// Send 51 chunks rapidly, then pause for longer than the base timeout
	// but shorter than the extended timeout (base * 3). The stream should
	// NOT be marked as stalled.
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Base stall timeout: 100ms. Extended (3×): 300ms.
	// We'll send 51 chunks, then pause for 200ms (> base, < extended).
	// If progressive timeout works, the watchdog won't fire.
	// If it doesn't work, the watchdog fires at 100ms and kills the stream.
	baseStall := 100 * time.Millisecond
	pauseDuration := 200 * time.Millisecond // between base (100ms) and extended (300ms)

	pr, pw := io.Pipe()
	go func() {
		// Send 51 chunks rapidly to cross the 50-chunk threshold
		for i := 0; i < 51; i++ {
			pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
			time.Sleep(time.Millisecond) // tiny delay
		}
		// Now pause — this is longer than baseStall but shorter than extendedStall
		time.Sleep(pauseDuration)
		// Send final chunk + DONE
		pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"y\"}}]}\n\n"))
		pw.Write([]byte("data: [DONE]\n\n"))
		pw.Close()
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "test-model",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(50 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: baseStall,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)

	// The stream should complete successfully — the 200ms pause was within
	// the extended timeout (300ms) even though it exceeded the base (100ms).
	if logData.state != "completed" {
		t.Errorf("expected state=completed (progressive timeout should allow 200ms pause after 50 chunks), got %q, error=%s", logData.state, logData.errorMessage)
	}

	// Verify we got content from after the pause (the "y" chunk)
	body := w.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("expected [DONE] sentinel in response")
	}
	if !strings.Contains(body, "y") {
		t.Error("expected content 'y' after pause — stream was likely killed by watchdog")
	}
}

func TestStallWatchdog_ProgressiveTimeout_Boundary50(t *testing.T) {
	// Verify that the progressive timeout only kicks in AFTER 50 chunks.
	// Send exactly 50 chunks, then block. The stall watchdog should fire
	// because chunkCount == 50 does not satisfy chunkCount > 50.
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Base stall timeout: 50ms.
	// Send 50 chunks, then block on a channel. The watchdog should fire
	// at ~50ms because the threshold is >50, not >=50.
	// If the goroutine wakes from the 200ms timeout, it means the
	// watchdog did NOT fire (bug).
	baseStall := 50 * time.Millisecond
	closeCh := make(chan struct{})

	pr, pw := io.Pipe()
	go func() {
		// Send exactly 50 chunks — NOT enough to trigger progressive timeout
		for i := 0; i < 50; i++ {
			pw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
		}
		// Block until either: watchdog closes the body (closeCh) or a
		// safety timeout fires (meaning the watchdog failed to fire).
		select {
		case <-closeCh:
			// Body was closed by watchdog, pipe write will fail
		case <-time.After(200 * time.Millisecond):
			// Watchdog did NOT fire — write [DONE] so the stream completes
			// and the test can observe the wrong behavior.
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
		id:             uuid.New().String(),
		modelID:        "test-model",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	startTime := time.Now()
	opts := streamOptions{
		responseHeaderMs:   10.0,
		streamStallTimeout: baseStall,
		vkHash:             "test-hash",
		attempt:            1,
		cancelOrigin:       "failover_timeout",
	}

	h.handleStreamingResponse(w, req, logData, resp, startTime, opts)
	close(closeCh)

	// The stream should be killed by the watchdog — 50 chunks is NOT enough
	// for progressive timeout (needs > 50).
	if logData.state != "failed" {
		t.Errorf("expected state=failed (watchdog should fire at base timeout for 50 chunks), got %q", logData.state)
	}
	if logData.errorMessage == "" {
		t.Error("expected non-empty error message after stall")
	}
	// Duration should be well under 500ms since the watchdog fires at ~50ms.
	// Use generous margin for CI scheduling jitter (goroutine wake-up, pipe I/O).
	if logData.durationMs > 500 {
		t.Errorf("expected duration < 500ms (stall fired early), got %.1fms", logData.durationMs)
	}
}

// ---------------------------------------------------------------------------
// probeFirstToken additional edge case tests
// ---------------------------------------------------------------------------

func TestProbeFirstToken_DataNoSpaceAfterColon(t *testing.T) {
	h := &Handler{}
	// Some providers send "data:" without a space after the colon
	body := makeSSEBody(t, "data:{\"choices\":[]}\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
		return
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
}

func TestProbeFirstToken_DataWithSpaces(t *testing.T) {
	h := &Handler{}
	// Standard "data: " with space
	body := makeSSEBody(t, "data:   {\"choices\":[]}\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
		return
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
}

func TestProbeFirstToken_DoneWithDataAfter(t *testing.T) {
	h := &Handler{}
	// [DONE] first, then data (shouldn't happen normally but tests the short-circuit)
	body := makeSSEBody(t, "data: [DONE]\n\ndata: {\"choices\":[]}\n\n")
	startTime := time.Now()

	_, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// [DONE] before any real token means trueTtftMs should be 0
	if trueTtftMs != 0 {
		t.Errorf("expected trueTtftMs == 0 for [DONE] first, got %f", trueTtftMs)
	}
}

func TestProbeFirstToken_UnknownLineFormat(t *testing.T) {
	h := &Handler{}
	// Unknown line format (not data:, not a comment, not empty) — should be skipped
	body := makeSSEBody(t, "some-random-text\ndata: {\"choices\":[]}\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
		return
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
	// The unknown line should still be captured in the buffer
	got := probeBuf.String()
	if !strings.Contains(got, "some-random-text") {
		t.Errorf("expected unknown line captured in probeBuf, got: %q", got)
	}
}

func TestProbeFirstToken_MultipleDataLines(t *testing.T) {
	h := &Handler{}
	// Multiple data lines — should return on the first real one
	body := makeSSEBody(t, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
	startTime := time.Now()

	probeBuf, trueTtftMs, err := h.probeFirstToken(context.Background(), body, 5*time.Second, startTime)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probeBuf == nil {
		t.Fatal("expected probeBuf to be non-nil")
		return
	}
	if trueTtftMs <= 0 {
		t.Errorf("expected trueTtftMs > 0, got %f", trueTtftMs)
	}
	// Should contain the first data line and stop there
	got := probeBuf.String()
	if !strings.Contains(got, "hello") {
		t.Errorf("expected first data line in buffer, got: %q", got)
	}
}
