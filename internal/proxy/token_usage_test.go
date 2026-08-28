package proxy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// TestTokenUsage_RecordedOnClientDisconnect verifies that token usage is
// recorded against the virtual key quota even when the client disconnects
// mid-stream. The upstream provider already billed for these tokens, so
// not counting them would cause quota drift.
//
// This is an integration test — requires the test PostgreSQL instance
// on port 5433 (make test-db-up).
func TestTokenUsage_RecordedOnClientDisconnect(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	pool := testDB.Pool()

	// Create a real virtual key in the DB.
	vkRepo := virtualkey.NewRepository(pool)
	ctx := context.Background()
	keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test-disconnect-key")))
	vk, err := vkRepo.Create(ctx, "disconnect-test-key", keyHash, "disco...key", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() {
		_ = vkRepo.Delete(ctx, vk.ID)
	}()

	// disconnected is closed once the test has cancelled the client context;
	// the upstream holds the stream open until then.
	disconnected := make(chan struct{})

	// Build an upstream that streams tokens, sends usage, then waits so the
	// client disconnects mid-stream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream must support flushing")
		}

		// Send a content chunk.
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Send a usage chunk (stream_options.include_usage).
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
		flusher.Flush()

		// Hold the stream open until the test has disconnected, so the
		// disconnect always lands mid-stream rather than after [DONE].
		<-disconnected

		// Send [DONE] — may or may not be received by the client.
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

	// Create a cancelable context to simulate client disconnect.
	cancelCtx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(cancelCtx)

	// The usage chunk is the second data frame the proxy forwards, and the
	// observers run BEFORE the emit — so a write carrying "usage" proves the
	// token counts are already in streamState. Waiting on that instead of on a
	// sleep is what makes the disconnect land at a known point: an 80ms guess
	// was enough locally and not on a loaded runner under -race, where the
	// stream had not reached the usage chunk yet and the test read tokens=0.
	usageSeen := make(chan struct{})
	inner := &notifyOnMatchWriter{inner: httptest.NewRecorder(), match: "usage", seen: usageSeen}
	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       true,
		virtualKeyName:  "disconnect-test-key",
		virtualKeyID:    vk.ID.String(),
		failoverAttempt: 0,
		state:           "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	// Start streaming in a goroutine.
	done := make(chan struct{})
	go func() {
		h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: keyHash, attempt: 1, cancelOrigin: "failover_timeout"})
		close(done)
	}()

	// Disconnect once the usage chunk has actually been forwarded.
	select {
	case <-usageSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("the usage chunk was never forwarded")
	}
	cancel()
	close(disconnected)

	// Wait for the handler to finish.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleStreamingResponse did not finish in time")
	}

	// Verify the handler detected the disconnect.
	if logData.state != "failed" {
		t.Errorf("expected state=failed, got %q", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "client disconnected") {
		t.Errorf("expected error message to mention disconnect, got %q", logData.errorMessage)
	}

	// The critical assertion: tokens_used should have been incremented
	// despite the disconnect. Usage was: prompt_tokens=10, completion_tokens=5.
	// recordTokenUsage adds prompt+completion+reasoning tokens.
	refreshed, err := vkRepo.FindByKeyHash(ctx, keyHash)
	if err != nil {
		t.Fatalf("failed to find VK after stream: %v", err)
	}
	if refreshed.TokensUsed == 0 {
		t.Error("tokens_used should be > 0 — token usage must be recorded even on client disconnect")
	}
	// prompt_tokens=10 + completion_tokens=5 + reasoning_tokens=0 = 15
	if refreshed.TokensUsed != 15 {
		t.Errorf("expected tokens_used=15 (10 prompt + 5 completion), got %d", refreshed.TokensUsed)
	}
}

// TestTokenUsage_RecordedOnNonStreamingSuccess verifies that token usage
// is recorded for non-streaming responses, as a baseline for the
// disconnect test above.
func TestTokenUsage_RecordedOnNonStreamingSuccess(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	pool := testDB.Pool()

	vkRepo := virtualkey.NewRepository(pool)
	ctx := context.Background()
	keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test-nonstream-key")))
	vk, err := vkRepo.Create(ctx, "nonstream-test-key", keyHash, "nonst...key", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}
	defer func() {
		_ = vkRepo.Delete(ctx, vk.ID)
	}()

	// Call recordTokenUsage directly to verify it works end-to-end.
	h.recordTokenUsage(keyHash, &requestLogData{virtualKeyName: "nonstream-test-key"}, 50, 25, 0)

	refreshed, err := vkRepo.FindByKeyHash(ctx, keyHash)
	if err != nil {
		t.Fatalf("failed to find VK after recordTokenUsage: %v", err)
	}
	if refreshed.TokensUsed != 75 {
		t.Errorf("expected tokens_used=75 (50 prompt + 25 completion), got %d", refreshed.TokensUsed)
	}
}

// notifyOnMatchWriter closes seen the first time a written chunk contains match.
// It exists so a streaming test can act at a point in the stream rather than at
// a point in time.
type notifyOnMatchWriter struct {
	inner http.ResponseWriter
	match string
	seen  chan struct{}
	fired bool
}

func (w *notifyOnMatchWriter) Header() http.Header { return w.inner.Header() }

func (w *notifyOnMatchWriter) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	if !w.fired && strings.Contains(string(p), w.match) {
		w.fired = true
		close(w.seen)
	}
	return n, err
}

func (w *notifyOnMatchWriter) WriteHeader(statusCode int) { w.inner.WriteHeader(statusCode) }

func (w *notifyOnMatchWriter) Flush() {}
