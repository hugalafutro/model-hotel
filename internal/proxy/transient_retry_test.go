package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

func TestIsRetryableUpstreamError(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		requestWritten bool
		want           bool
	}{
		{"nil error", nil, false, false},
		{"context canceled", &url.Error{Op: "Post", URL: "http://x", Err: context.Canceled}, false, false},
		{"deadline exceeded", &url.Error{Op: "Post", URL: "http://x", Err: context.DeadlineExceeded}, true, false},
		{"timeout pre-write", &net.DNSError{Err: "timeout", IsTimeout: true}, false, false},
		{"timeout post-write", &net.DNSError{Err: "timeout", IsTimeout: true}, true, false},
		{"pre-write any transport error", errors.New("tls: handshake failure"), false, true},
		{"pre-write connection refused", &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, false, true},
		{"post-write connection reset", &url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)}}, true, true},
		{"post-write broken pipe", &net.OpError{Op: "write", Err: os.NewSyscallError("write", syscall.EPIPE)}, true, true},
		{"post-write EOF", &url.Error{Op: "Post", URL: "http://x", Err: io.EOF}, true, true},
		{"post-write unexpected EOF", io.ErrUnexpectedEOF, true, true},
		{"post-write server closed idle connection", errors.New("http: server closed idle connection"), true, true},
		{"post-write arbitrary error", errors.New("boom"), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableUpstreamError(tc.err, tc.requestWritten); got != tc.want {
				t.Errorf("isRetryableUpstreamError(%v, %v) = %v, want %v", tc.err, tc.requestWritten, got, tc.want)
			}
		})
	}
}

// newResettingUpstream returns an httptest server that hard-resets (TCP RST)
// the first `failures` connections, then delegates to onSuccess. The returned
// counter tracks how many requests the upstream received in total.
func newResettingUpstream(t *testing.T, failures int, onSuccess http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(calls.Add(1)) <= failures {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("upstream does not support hijacking")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack failed: %v", err)
				return
			}
			// SO_LINGER 0 makes Close send RST instead of FIN, so the
			// proxy sees "connection reset by peer".
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = conn.Close()
			return
		}
		onSuccess(w, r)
	}))
	return upstream, &calls
}

// doTransientChatRequest sends a chat completion through the proxy with the
// env's virtual key attached, returning the recorder.
func doTransientChatRequest(env *testProxyEnv, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req)
	return w
}

// breakerConsecutiveFails returns the circuit breaker's consecutive failure
// count for the given provider, or 0 when the breaker has no entry for it.
func breakerConsecutiveFails(h *Handler, providerID uuid.UUID) int {
	for _, s := range h.circuitBreaker.Status() {
		if s.ProviderID == providerID.String() {
			return s.ConsecutiveFails
		}
	}
	return 0
}

func TestChatCompletions_TransientResetRetriesSameProvider(t *testing.T) {
	// Two RSTs then success: a single-provider model must survive transient
	// connection resets via same-provider retries (no failover candidates).
	upstream, calls := newResettingUpstream(t, maxTransientRetries, func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   reqBody["model"],
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "recovered"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
		})
	})
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	w := doTransientChatRequest(env, body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after transient retries, got %d: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != int32(maxTransientRetries+1) {
		t.Errorf("expected %d upstream calls (1 + %d retries), got %d", maxTransientRetries+1, maxTransientRetries, got)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Errorf("expected at least one choice, got %v", resp["choices"])
	}
	// A blip that self-heals on retry must not count against the breaker.
	if fails := breakerConsecutiveFails(env.Handler, env.ProviderID); fails != 0 {
		t.Errorf("expected 0 breaker failures after self-healed retry, got %d", fails)
	}
}

func TestChatCompletions_TransientResetRetriesExhausted(t *testing.T) {
	// Every connection resets: after 1 + maxTransientRetries tries the single
	// candidate is exhausted and the request fails with 502 — and the breaker
	// records exactly one failure for the whole candidate attempt.
	upstream, calls := newResettingUpstream(t, int(^uint(0)>>1), func(w http.ResponseWriter, r *http.Request) {
		t.Error("success handler should never be reached")
	})
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	w := doTransientChatRequest(env, body)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 after retries exhausted, got %d: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != int32(maxTransientRetries+1) {
		t.Errorf("expected exactly %d upstream calls, got %d", maxTransientRetries+1, got)
	}
	if fails := breakerConsecutiveFails(env.Handler, env.ProviderID); fails != 1 {
		t.Errorf("expected exactly 1 breaker failure per candidate attempt, got %d", fails)
	}
}

func TestDoUpstream_GetBodyErrorStopsRetry(t *testing.T) {
	// When the body cannot be replayed, the retry loop must stop and surface
	// the original transport error instead of retrying with an empty body.
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstream, calls := newResettingUpstream(t, int(^uint(0)>>1), func(w http.ResponseWriter, r *http.Request) {
		t.Error("success handler should never be reached")
	})
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL, bytes.NewReader([]byte(`{"model":"m"}`)))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("body replay failed") }

	var dialMs float64
	st := &requestState{logData: &requestLogData{modelID: "test-model", providerName: "test-provider"}}
	cand := modelCandidate{
		model:    &model.Model{ModelID: "test-model"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}
	resp, ok := h.doUpstream(context.Background(), req, st, cand, 0, &dialMs)

	if ok || resp != nil {
		t.Errorf("expected (nil, false), got (%v, %v)", resp, ok)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 upstream call (no retry without replayable body), got %d", got)
	}
	if !strings.Contains(st.lastErr, "provider error") {
		t.Errorf("expected lastErr to contain the transport error, got %q", st.lastErr)
	}
}

func TestChatCompletions_TransientRetryClientDisconnectDuringBackoff(t *testing.T) {
	// A client disconnect during the retry backoff must stop retrying without
	// penalizing the circuit breaker (the provider is not at fault).
	resetHit := make(chan struct{}, 1)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
		select {
		case resetHit <- struct{}{}:
		default:
		}
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)

	// Cancel the client context shortly after the first reset lands, i.e.
	// while the proxy sits in the inter-try backoff (>=100ms).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-resetHit
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	vctx := context.WithValue(ctx, virtualKeyNameKey, "test-key")
	vctx = context.WithValue(vctx, virtualKeyIDKey, uuid.New().String())
	vctx = context.WithValue(vctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(vctx)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 after client disconnect, got %d: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 upstream call (no retry after disconnect), got %d", got)
	}
	if fails := breakerConsecutiveFails(env.Handler, env.ProviderID); fails != 0 {
		t.Errorf("expected 0 breaker failures on client disconnect, got %d", fails)
	}
}

func TestChatCompletions_TransientResetRetryStreaming(t *testing.T) {
	// The retry happens before any response bytes, so streaming requests are
	// retried the same way: two RSTs, then a normal SSE stream.
	upstream, calls := newResettingUpstream(t, maxTransientRetries, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"recovered\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": true}`
	w := doTransientChatRequest(env, body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after transient retries, got %d: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != int32(maxTransientRetries+1) {
		t.Errorf("expected %d upstream calls (1 + %d retries), got %d", maxTransientRetries+1, maxTransientRetries, got)
	}
	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "data: {") {
		t.Error("expected SSE data format")
	}
	if !strings.Contains(responseBody, "data: [DONE]") {
		t.Error("expected [DONE] sentinel")
	}
}
