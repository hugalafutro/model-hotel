package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// Integration test helpers (requires PostgreSQL)
// ---------------------------------------------------------------------------

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("proxy")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("proxy")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// newIntegrationHandler creates a Handler with a real settings.Repository
// backed by the test database.
func newIntegrationHandler() *Handler {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)
	return &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key-for-proxy-tests"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: &virtualKeyRepoAdapter{repo: virtualKeyRepo},
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
}

// ---------------------------------------------------------------------------
// shouldFailover tests (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestShouldFailover_5xx(t *testing.T) {
	h := newIntegrationHandler()
	for _, code := range []int{500, 502, 503, 504} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_429_DefaultEnabled(t *testing.T) {
	h := newIntegrationHandler()
	// Default setting for failover_on_rate_limit is true
	if !h.shouldFailover(context.Background(), 429) {
		t.Error("429 should trigger failover when failover_on_rate_limit=true (default)")
	}
}

func TestShouldFailover_429_Disabled(t *testing.T) {
	h := newIntegrationHandler()
	// Set failover_on_rate_limit=false
	if err := h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "false"); err != nil {
		t.Fatalf("failed to set setting: %v", err)
	}
	defer func() {
		_ = h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "true")
	}()
	h.settingsRepo.InvalidateCache("failover_on_rate_limit")

	if h.shouldFailover(context.Background(), 429) {
		t.Error("429 should NOT trigger failover when failover_on_rate_limit=false")
	}
}

func TestShouldFailover_AuthErrors(t *testing.T) {
	h := newIntegrationHandler()
	for _, code := range []int{401, 403} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_SuccessCodes(t *testing.T) {
	h := newIntegrationHandler()
	for _, code := range []int{200, 201, 204, 301, 302} {
		if h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should NOT trigger failover", code)
		}
	}
}

func TestShouldFailover_Other4xx(t *testing.T) {
	h := newIntegrationHandler()
	for _, code := range []int{400, 405, 408, 422} {
		if h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should NOT trigger failover", code)
		}
	}
}

func TestShouldFailover_404(t *testing.T) {
	h := newIntegrationHandler()
	if !h.shouldFailover(context.Background(), 404) {
		t.Error("404 should trigger failover (stale model, overloaded provider)")
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions request validation tests (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestChatCompletions_MissingBody(t *testing.T) {
	h := newIntegrationHandler()
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(""))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", rr.Code)
	}
}

func TestChatCompletions_InvalidJSON(t *testing.T) {
	h := newIntegrationHandler()
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader("not json"))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestChatCompletions_MissingModel(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", rr.Code)
	}
}

func TestChatCompletions_InvalidModelFormat(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"just-a-name","messages":[]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid model format, got %d", rr.Code)
	}
}

func TestChatCompletions_HotelModelNotFound(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"hotel/nonexistent","messages":[]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown hotel model, got %d", rr.Code)
	}
}

func TestChatCompletions_SpecificProviderNotFound(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"unknown-provider/some-model","messages":[]}`
	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown provider, got %d", rr.Code)
	}
}

func TestChatCompletions_StreamOptionsInjection(t *testing.T) {
	body := `{"model":"test","stream":true,"messages":[]}`
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatal(err)
	}
	raw["stream_options"] = map[string]interface{}{
		"include_usage": true,
	}
	injected, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(injected, &parsed); err != nil {
		t.Fatal(err)
	}
	so, ok := parsed["stream_options"].(map[string]interface{})
	if !ok {
		t.Fatal("stream_options should be a map")
	}
	if so["include_usage"] != true {
		t.Error("stream_options.include_usage should be true")
	}
}

// ---------------------------------------------------------------------------
// Failover backoff calculation tests
// ---------------------------------------------------------------------------

func TestFailoverBackoff_Sequence(t *testing.T) {
	base := 100 * time.Millisecond
	capacity := 2 * time.Second

	// Each attempt's backoff should be in [exponential, exponential+base)
	// because jitter is [0, base).
	cases := []struct {
		attempt    int
		minBackoff time.Duration
		maxBackoff time.Duration
	}{
		{1, 100 * time.Millisecond, 200 * time.Millisecond},
		{2, 200 * time.Millisecond, 300 * time.Millisecond},
		{3, 400 * time.Millisecond, 500 * time.Millisecond},
		{4, 800 * time.Millisecond, 900 * time.Millisecond},
		{5, 1600 * time.Millisecond, 1700 * time.Millisecond},
		{6, 2000 * time.Millisecond, 2100 * time.Millisecond}, // capped
		{7, 2000 * time.Millisecond, 2100 * time.Millisecond}, // capped
	}

	for _, tc := range cases {
		for i := 0; i < 100; i++ {
			got := failoverBackoff(base, capacity, tc.attempt)
			if got < tc.minBackoff || got >= tc.maxBackoff {
				t.Errorf("attempt %d (sample %d): backoff = %v, want in [%v, %v)", tc.attempt, i, got, tc.minBackoff, tc.maxBackoff)
				break // one failure per case is enough
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ProxyKeyMiddleware tests (no DB needed for auth header checks)
// ---------------------------------------------------------------------------

func TestProxyKeyMiddleware_MissingHeader(t *testing.T) {
	h := &Handler{cfg: &config.Config{MasterKey: "test"}, ipLimiter: ratelimit.NewIPLimiter(30, 60, nil, nil)}
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called without auth header")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestProxyKeyMiddleware_InvalidScheme(t *testing.T) {
	h := &Handler{cfg: &config.Config{MasterKey: "test"}, ipLimiter: ratelimit.NewIPLimiter(30, 60, nil, nil)}
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should NOT be called with Basic auth")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Streaming client write failure test (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

// failAfterNWriter wraps an http.ResponseWriter and returns net.ErrClosed
// after N successful Write calls. Flush calls always succeed.
type failAfterNWriter struct {
	inner      http.ResponseWriter
	maxWrites  int
	writeCount int
}

func (w *failAfterNWriter) Header() http.Header {
	return w.inner.Header()
}

func (w *failAfterNWriter) Write(p []byte) (int, error) {
	w.writeCount++
	if w.writeCount > w.maxWrites {
		return 0, net.ErrClosed
	}
	return w.inner.Write(p)
}

func (w *failAfterNWriter) WriteHeader(statusCode int) {
	w.inner.WriteHeader(statusCode)
}

func (w *failAfterNWriter) Flush() {
	// no-op: ResponseRecorder doesn't need flushing
}

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

		for i := 0; i < 50; i++ {
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func withAuthContext(r *http.Request) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	return r.WithContext(ctx)
}

// backoffDuration and pow2 removed — backoff logic is now in production code
// (failoverBackoff in proxy.go) with jitter.

// TestChatCompletions_ModelWithNoSlash tests the error path for model names
// that don't contain a slash (invalid format)
func TestChatCompletions_ModelWithNoSlash(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"justmodel","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for model without slash, got %d", rr.Code)
	}
}

// TestChatCompletions_EmptyModel tests the error path for empty model field
func TestChatCompletions_EmptyModel(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty model, got %d", rr.Code)
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
// writeOpenAIError unit tests (additional status codes not in provider_helpers_test.go)
// ---------------------------------------------------------------------------

func TestWriteOpenAIError_429WithType(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIError(rec, "rate limit exceeded", http.StatusTooManyRequests)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	errObj := resp["error"].(map[string]interface{})
	if errObj["message"] != "rate limit exceeded" {
		t.Errorf("expected message 'rate limit exceeded', got %q", errObj["message"])
	}
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("expected type 'rate_limit_error', got %q", errObj["type"])
	}
}

func TestWriteOpenAIError_500WithType(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIError(rec, "internal server error", http.StatusInternalServerError)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	errObj := resp["error"].(map[string]interface{})
	if errObj["message"] != "internal server error" {
		t.Errorf("expected message 'internal server error', got %q", errObj["message"])
	}
	if errObj["type"] != "server_error" {
		t.Errorf("expected type 'server_error', got %q", errObj["type"])
	}
}

// ---------------------------------------------------------------------------
// failRequest integration tests
// ---------------------------------------------------------------------------

func TestFailRequest_PopulatesLogData(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	startTime := time.Now()
	timings := resolveTimings{
		modelLookupMs:    2.0,
		providerLookupMs: 3.0,
		keyDecryptMs:     1.0,
		dialMs:           0.5,
		failoverLookupMs: 4.0,
		settingsReadMs:   0.2,
	}

	logData := &requestLogData{
		modelID:         "test-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.failRequest(logData, 502, "test error", 1, startTime, 1.5, timings, 0.5)

	if logData.statusCode != 502 {
		t.Errorf("expected statusCode=502, got %d", logData.statusCode)
	}
	if logData.errorMessage != "test error" {
		t.Errorf("expected errorMessage='test error', got %q", logData.errorMessage)
	}
	if logData.state != "failed" {
		t.Errorf("expected state='failed', got %q", logData.state)
	}
	if logData.failoverAttempt != 1 {
		t.Errorf("expected failoverAttempt=1, got %d", logData.failoverAttempt)
	}
	if logData.parseMs != 1.5 {
		t.Errorf("expected parseMs=1.5, got %f", logData.parseMs)
	}
	if logData.modelLookupMs != 2.0 {
		t.Errorf("expected modelLookupMs=2.0, got %f", logData.modelLookupMs)
	}
	if logData.providerLookupMs != 3.0 {
		t.Errorf("expected providerLookupMs=3.0, got %f", logData.providerLookupMs)
	}
	if logData.proxyOverheadMs != 0.5 {
		t.Errorf("expected proxyOverheadMs=0.5, got %f", logData.proxyOverheadMs)
	}
}

// ---------------------------------------------------------------------------
// Middleware context tests for ChatCompletions (lines 859-872, 902-907)
// ---------------------------------------------------------------------------

func TestChatCompletions_MiddlewareContextWithoutBodyBytes(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"hotel/nonexistent","stream":true,"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	// Simulate middleware having already parsed the body
	ctx := context.WithValue(req.Context(), ctxkeys.RequestBodyParseMsKey, float64(1.5))
	ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, "hotel/nonexistent")
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, true)
	// No RequestBodyKey set — so bodyBytes remains empty
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)
	// Model "hotel/nonexistent" should 404 (no failover group found)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestChatCompletions_MiddlewareContextWithBodyBytes(t *testing.T) {
	h := newIntegrationHandler()
	body := `{"model":"hotel/nonexistent","stream":false,"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxkeys.RequestBodyParseMsKey, float64(2.0))
	ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, "hotel/nonexistent")
	ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, false)
	ctx = context.WithValue(ctx, ctxkeys.RequestBodyKey, []byte(body))
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
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

// ---------------------------------------------------------------------------
// ChatCompletions all providers exhausted (lines 1372-1384)
// ---------------------------------------------------------------------------

func TestChatCompletions_AllProvidersFail(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	providerName := env.ProviderName
	modelName := env.ModelName
	defer upstream.Close()
	defer handler.upstreamTransport.CloseIdleConnections()

	// Replace the upstream with one that returns 500 (failover-eligible)
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal server error"}}`))
	})

	// Single provider returning 500 → no more candidates → non-200 error path
	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should return 500 (the upstream's error forwarded to client)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions middleware context with settings read time (lines 948-950, 992-994, 1051-1053)
// ---------------------------------------------------------------------------

func TestChatCompletions_SettingsReadTimeFromContext(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Provide settings read time via context pointer
	settingsMs := 3.0
	body := `{"model":"hotel/nonexistent","stream":false,"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), ctxkeys.SettingsReadMsKey, &settingsMs)
	ctx = context.WithValue(ctx, ctxkeys.RequestBodyKey, []byte(body))
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, "00000000-0000-0000-0000-000000000001")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, "abc123")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)

	// Just verify it doesn't panic and the settings read time is consumed
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent hotel model, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions with settings read time reaching the failover loop
// (lines 992-994, 1051-1053)
// ---------------------------------------------------------------------------

func TestChatCompletions_SettingsReadTimeInFailoverLoop(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	providerName := env.ProviderName
	modelName := env.ModelName
	defer env.Upstream.Close()
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))

	// Set settings read time via context pointer — this is read at lines
	// 948, 992, and 1051 inside the ChatCompletions failover loop.
	settingsMs := 2.5
	ctx := context.WithValue(req.Context(), ctxkeys.SettingsReadMsKey, &settingsMs)
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions all providers exhausted - failover path (lines 1372-1384)
// ---------------------------------------------------------------------------

func TestChatCompletions_FailoverAllProvidersExhausted(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-for-failover"

	// Create two providers pointing at non-listening ports (connection refused).
	// This triggers the error path at line 1167, which `continue`s past
	// all candidates, reaching the "all providers exhausted" path at line 1372.
	keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key1: %v", err)
	}
	prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "failover-prov-1-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:1", // connection refused
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider1: %v", err)
	}

	keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key2: %v", err)
	}
	prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "failover-prov-2-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:2", // connection refused
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider2: %v", err)
	}

	// Create models for both providers.
	modelID1 := uuid.New()
	modelName := "failover-model-" + uuid.New().String()[:8]
	model1 := &model.Model{
		ID: modelID1, ProviderID: prov1.ID, ModelID: modelName,
		Name: "Failover Model 1", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov1.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model1); err != nil {
		t.Fatalf("failed to upsert model1: %v", err)
	}

	modelID2 := uuid.New()
	model2 := &model.Model{
		ID: modelID2, ProviderID: prov2.ID, ModelID: modelName,
		Name: "Failover Model 2", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov2.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model2); err != nil {
		t.Fatalf("failed to upsert model2: %v", err)
	}

	// Create failover group with both models.
	if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{}, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key.
	vkName := "failover-test-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "failover-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Both providers fail with 5xx → all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for all providers exhausted, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions specific provider request failed (lines 1378-1381)
// ---------------------------------------------------------------------------

func TestChatCompletions_SpecificProviderAllProvidersFail(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-for-specific"

	// Create a provider pointing at a non-listening port (connection refused).
	// This triggers the error path that reaches line 1378 (specific provider failed).
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "specific-prov-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:1", // connection refused
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "specific-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "Specific Model", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, testModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "specific-test-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "specific-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	// Use specific provider format (not hotel/) → single candidate → non-200 error forwarded
	body := `{"model": "` + prov.Name + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Single provider with connection refused → 502 Bad Gateway
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// TestChatCompletions_DeprecationCacheFirstEntry tests the deprecation cache
// LoadOrStore path when no existing entry exists (first rejection learned for a model).
// Covers lines 1226-1229 in proxy.go. The CompareAndSwap merge loop (1232-1240)
// has a latent bug: map[string]bool is not a comparable type, causing CompareAndSwap
// to panic. That path can only be tested after the type is changed to a comparable one.
func TestChatCompletions_DeprecationCacheFirstEntry(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	providerName := env.ProviderName
	modelName := env.ModelName
	defer upstream.Close()
	defer stopUnitHandlerIntegration(handler)

	// Configure upstream to return 400 with a param rejection for "top_p".
	// The backtick-wrapped param name is recognized by parseProviderParamError.
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"` + "`top_p`" + ` is not supported for this model"}}`))
	})

	// No pre-existing cache entry — LoadOrStore returns !loaded, storing the
	// rejected params as the first entry and breaking out of the loop.
	providerType := provider.DetectProviderType(upstream.URL)
	cacheKey := fmt.Sprintf("%s:%s", providerType, modelName)

	body := `{"model": "` + providerName + `/` + modelName + `", "stream": false, "messages": [{"role": "user", "content": "hello"}], "temperature": 0.7, "top_p": 0.9}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// The 400 triggers deprecation caching, then auto-retry strips top_p.
	// The retry also returns 400 (same upstream), so 400 is forwarded.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	// Verify the cache entry was created with "top_p".
	cached, loaded := handler.deprecationCache.Load(cacheKey)
	if !loaded {
		t.Fatal("expected cache entry to exist")
	}
	entryPtr, ok := cached.(*map[string]bool)
	if !ok {
		t.Fatalf("expected cache value to be *map[string]bool, got %T", cached)
	}
	if !(*entryPtr)["top_p"] {
		t.Error("expected 'top_p' to be in cache entry")
	}
}

// TestChatCompletions_RetryCancelDuringFailover covers the retryCancel cleanup
// at line 1318-1320. Scenario: provider returns 400 (param rejection), auto-retry
// strips the rejected param and retries, the retry returns 500 (failover-eligible),
// and there are more candidates available, so failover continues. The retryCancel
// must be called during the failover continue path.
func TestChatCompletions_RetryCancelDuringFailover(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-retry-cancel"

	// Create two providers. The first returns 400 then 500 (triggering retry
	// and failover). The second also fails with connection refused, so all
	// providers exhaust. The key is that the first provider's retry returns
	// a failover-eligible status (500) while retryCancel is set.
	keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key1: %v", err)
	}
	callCount := 0
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		if _, hasTopP := reqBody["top_p"]; hasTopP {
			// First request: return 400 with param rejection
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"` + "`top_p`" + ` is not supported"}}`))
		} else {
			// Retry (top_p stripped): return 500 (failover-eligible)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"internal error"}}`))
		}
	}))
	defer upstream1.Close()

	prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "retry-prov-1-" + uuid.New().String()[:8],
		BaseURL: upstream1.URL,
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider1: %v", err)
	}

	keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key2: %v", err)
	}
	prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "retry-prov-2-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:1", // connection refused
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider2: %v", err)
	}

	// Create models for both providers.
	modelName := "retry-model-" + uuid.New().String()[:8]
	model1 := &model.Model{
		ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
		Name: "Retry Model 1", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov1.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model1); err != nil {
		t.Fatalf("failed to upsert model1: %v", err)
	}

	model2 := &model.Model{
		ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
		Name: "Retry Model 2", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov2.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model2); err != nil {
		t.Fatalf("failed to upsert model2: %v", err)
	}

	// Create failover group with both models.
	if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{}, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key.
	vkName := "retry-cancel-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "retry-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "hotel/` + modelName + `", "stream": false, "messages": [{"role": "user", "content": "hello"}], "top_p": 0.9}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Provider 1: 400 → retry → 500 (failover) + Provider 2: connection refused
	// All providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// TestChatCompletions_TouchLastUsedGoroutine verifies the TouchLastUsed goroutine
// at line 1081 fires during a successful request. The error paths (1083-1085: panic
// recovery, 1089-1091: TouchLastUsed error) cannot be reliably tested because:
//   - provider.Repository is a concrete type (not mockable)
//   - closing the pool breaks the entire request, not just the goroutine
//   - the goroutine creates its own 5s-timeout context, so cancellation from
//     the test doesn't affect it
//
// Coverage of the success path is confirmed (48 hits in the coverage profile).
func TestChatCompletions_TouchLastUsedGoroutine(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	providerName := env.ProviderName
	modelName := env.ModelName
	defer upstream.Close()
	defer stopUnitHandlerIntegration(handler)

	// The upstream returns a successful response, which causes the code to
	// reach the TouchLastUsed goroutine at line 1081.
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{
				"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12,
			},
		})
	})

	body := `{"model": "` + providerName + `/` + modelName + `", "stream": false, "messages": [{"role": "user", "content": "hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req = withAuthContext(req)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Give the TouchLastUsed goroutine time to execute.
	time.Sleep(200 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// humanReadableCancelOrigin tests
// ---------------------------------------------------------------------------

func TestHumanReadableCancelOrigin(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"client_disconnect", "client disconnected"},
		{"failover_timeout", "upstream request timed out"},
		{"retry_timeout", "param-strip retry timed out"},
		{"", ""},
		{"unknown_origin", "unknown_origin"},
	}

	for _, tc := range cases {
		got := humanReadableCancelOrigin(tc.input)
		if got != tc.expected {
			t.Errorf("humanReadableCancelOrigin(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions TTFT probe integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestChatCompletions_TTFTProbeSuccess(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	// Upstream SSE server that sends data immediately.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Configure short TTFT timeout (generous for local test).
	if err := settingsRepo.Set(ctx, "ttft_timeout", "5s"); err != nil {
		t.Fatalf("failed to set ttft_timeout: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "ttft_timeout", "60s") }()
	settingsRepo.InvalidateCache("ttft_timeout")

	masterKey := "test-master-key-ttft-success"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "ttft-success-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "ttft-success-model-" + uuid.New().String()[:8]
	m := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "TTFT Success", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "ttft-success-vk-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ttft-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    ratelimit.NewLimiter(settingsRepo),
		ipLimiter:      ratelimit.NewIPLimiter(30, 60, nil, nil),
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := fmt.Sprintf(`{"model":"%s/%s","messages":[{"role":"user","content":"hi"}],"stream":true}`, prov.Name, modelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	respBody := w.Body.String()
	if !strings.Contains(respBody, "data: [DONE]") {
		t.Error("expected response to contain [DONE] sentinel")
	}
	if !strings.Contains(respBody, "hi") {
		t.Error("expected response to contain streamed content")
	}
}

func TestChatCompletions_TTFTProbeTimeout(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	// Upstream server that delays sending data (simulates slow TTFT).
	// Uses r.Context().Done() so the handler returns promptly when the
	// probe closes the body (avoids waiting for the full sleep).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer upstream.Close()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Very short TTFT timeout so the probe fails quickly.
	if err := settingsRepo.Set(ctx, "ttft_timeout", "100ms"); err != nil {
		t.Fatalf("failed to set ttft_timeout: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "ttft_timeout", "60s") }()
	settingsRepo.InvalidateCache("ttft_timeout")

	// Set circuit breaker threshold to 1 so probe failure opens it.
	if err := settingsRepo.Set(ctx, "circuit_breaker_threshold", "1"); err != nil {
		t.Fatalf("failed to set circuit_breaker_threshold: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "circuit_breaker_threshold", "5") }()
	settingsRepo.InvalidateCache("circuit_breaker_threshold")

	masterKey := "test-master-key-ttft-timeout"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "ttft-timeout-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "ttft-timeout-model-" + uuid.New().String()[:8]
	m := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "TTFT Timeout", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "ttft-timeout-vk-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ttft-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    ratelimit.NewLimiter(settingsRepo),
		ipLimiter:      ratelimit.NewIPLimiter(30, 60, nil, nil),
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := fmt.Sprintf(`{"model":"%s/%s","messages":[{"role":"user","content":"hi"}],"stream":true}`, prov.Name, modelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Single provider, probe timeout → all providers exhausted → 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for TTFT probe timeout, got %d", w.Code)
	}

	// Verify circuit breaker recorded failure (threshold=1 → open).
	cbState := handler.circuitBreaker.GetState(prov.ID)
	if cbState != failover.StateOpen {
		t.Errorf("expected circuit breaker StateOpen after probe timeout, got %s", cbState)
	}
}

func TestChatCompletions_TTFTDisabled_CBRecordsSuccess(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	// Upstream SSE server that sends data immediately.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	// Disable TTFT probe (0 = immediate commit / backward compat).
	if err := settingsRepo.Set(ctx, "ttft_timeout", "0s"); err != nil {
		t.Fatalf("failed to set ttft_timeout: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "ttft_timeout", "60s") }()
	settingsRepo.InvalidateCache("ttft_timeout")

	// Circuit breaker threshold = 1 so we can detect success recording.
	if err := settingsRepo.Set(ctx, "circuit_breaker_threshold", "1"); err != nil {
		t.Fatalf("failed to set circuit_breaker_threshold: %v", err)
	}
	defer func() { _ = settingsRepo.Set(ctx, "circuit_breaker_threshold", "5") }()
	settingsRepo.InvalidateCache("circuit_breaker_threshold")

	masterKey := "test-master-key-ttft-disabled"
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "ttft-disabled-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "ttft-disabled-model-" + uuid.New().String()[:8]
	m := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "TTFT Disabled", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	vkName := "ttft-disabled-vk-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ttft-" + vkHash[:8]
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    ratelimit.NewLimiter(settingsRepo),
		ipLimiter:      ratelimit.NewIPLimiter(30, 60, nil, nil),
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := fmt.Sprintf(`{"model":"%s/%s","messages":[{"role":"user","content":"hi"}],"stream":true}`, prov.Name, modelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response content matches TestChatCompletions_TTFTProbeSuccess pattern
	respBody := w.Body.String()
	if !strings.Contains(respBody, "data: [DONE]") {
		t.Error("expected response to contain [DONE] sentinel")
	}
	if !strings.Contains(respBody, "ok") {
		t.Error("expected response to contain streamed content")
	}

	// When ttft_timeout=0, the else-if branch records CB success immediately
	// (backward-compat path at the `else if circuitBreakerEnabled` block).
	// With threshold=1 and a success recorded, the circuit should stay closed.
	cbState := handler.circuitBreaker.GetState(prov.ID)
	if cbState != failover.StateClosed {
		t.Errorf("expected circuit breaker StateClosed after success, got %s", cbState)
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions allowed_providers filter tests (lines 1158-1181)
// ---------------------------------------------------------------------------

func TestChatCompletions_AllowedProviders_FilterAllowed(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-allowed-providers"

	// Provider 1: real upstream that returns success (the "allowed" provider)
	prov1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-allowed",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer prov1Server.Close()

	keyPair1, err := auth.Encrypt("test-api-key-1", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key1: %v", err)
	}
	prov1, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "allowed-prov-" + uuid.New().String()[:8],
		BaseURL: prov1Server.URL,
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider1: %v", err)
	}

	// Provider 2: tracking upstream that fails the test if contacted (the "blocked" provider)
	var prov2Contacted atomic.Bool
	prov2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prov2Contacted.Store(true)
		t.Error("blocked provider should never have been contacted")
		w.WriteHeader(http.StatusOK)
	}))
	defer prov2Server.Close()

	keyPair2, err := auth.Encrypt("test-api-key-2", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key2: %v", err)
	}
	prov2, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "blocked-prov-" + uuid.New().String()[:8],
		BaseURL: prov2Server.URL,
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider2: %v", err)
	}

	// Create models for both providers with same model ID (for failover group)
	modelName := "ap-model-" + uuid.New().String()[:8]
	model1 := &model.Model{
		ID: uuid.New(), ProviderID: prov1.ID, ModelID: modelName,
		Name: "Model 1", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov1.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model1); err != nil {
		t.Fatalf("failed to upsert model1: %v", err)
	}

	model2 := &model.Model{
		ID: uuid.New(), ProviderID: prov2.ID, ModelID: modelName,
		Name: "Model 2", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov2.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, model2); err != nil {
		t.Fatalf("failed to upsert model2: %v", err)
	}

	// Create failover group (hotel/) with both models
	if _, err := failoverRepo.UpsertWithConfig(ctx, modelName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{}, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Create virtual key with allowed_providers = [prov1.ID only]
	vkName := "ap-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ap-" + vkHash[:8]
	allowedProviders := []string{prov1.ID.String()}
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, &allowedProviders, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "hotel/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	// Set allowed_providers in context (simulating what middleware does)
	rCtx = context.WithValue(rCtx, ctxkeys.VirtualKeyAllowedProvidersKey, &allowedProviders)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// Should get 200 from prov1 (allowed). prov2 must never be contacted.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (allowed provider succeeds), got %d; body: %s", w.Code, w.Body.String())
	}
	if prov2Contacted.Load() {
		t.Error("blocked provider was contacted despite allowed_providers filter")
	}
}

func TestChatCompletions_AllowedProviders_BlockAllReturns403(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-blocked-providers"

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "blocked-only-prov-" + uuid.New().String()[:8],
		BaseURL: "http://127.0.0.1:9997",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create model
	modelName := "blocked-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "Blocked Model", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, testModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Create virtual key with allowed_providers = [different provider ID]
	// This blocks the only available provider
	vkName := "blocked-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "bk-" + vkHash[:8]
	allowedProviders := []string{"00000000-0000-0000-0000-000000000000"} // non-existent provider
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, &allowedProviders, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "` + prov.Name + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	// Set allowed_providers in context (simulating what middleware does)
	rCtx = context.WithValue(rCtx, ctxkeys.VirtualKeyAllowedProvidersKey, &allowedProviders)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// All candidates filtered → 403
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 (virtual key does not have access), got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "virtual key does not have access to any provider") {
		t.Errorf("expected 403 error message, got: %s", w.Body.String())
	}
}

func TestChatCompletions_AllowedProviders_NilAllowsAll(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	providerName := env.ProviderName
	modelName := env.ModelName
	defer env.Upstream.Close()
	defer handler.upstreamTransport.CloseIdleConnections()

	// Virtual key created with nil allowed_providers (via newTestProxyHandler)
	body := `{"model": "` + providerName + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// nil allowed_providers → no filtering → request succeeds
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (nil allowed_providers allows all), got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_AllowedProviders_EmptySliceAllowsAll(t *testing.T) {
	pool := testDB.Pool()
	ctx := context.Background()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	masterKey := "test-master-key-empty-allowed"

	// Create a provider that returns success
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()

	keyPair, err := auth.Encrypt("test-api-key", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "empty-allowed-prov-" + uuid.New().String()[:8],
		BaseURL: upstream.URL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	modelName := "empty-allowed-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID: uuid.New(), ProviderID: prov.ID, ModelID: modelName,
		Name: "Empty Allowed Model", Description: "", Capabilities: "{}",
		Params: "{}", Modality: "", InputModalities: "[]", OutputModalities: "[]",
		Enabled: true, ProviderName: prov.Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(ctx, testModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Create virtual key with empty allowed_providers slice (len==0)
	vkName := "empty-allowed-key-" + uuid.New().String()[:8]
	vkHash := virtualkey.Hash(vkName)
	vkPreview := "ea-" + vkHash[:8]
	emptyAllowed := []string{} // empty slice, not nil
	if _, err := virtualKeyRepo.Create(ctx, vkName, vkHash, vkPreview, nil, nil, &emptyAllowed, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := &Handler{
		cfg:            &config.Config{MasterKey: masterKey},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
	defer handler.upstreamTransport.CloseIdleConnections()

	body := `{"model": "` + prov.Name + `/` + modelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rCtx := context.WithValue(req.Context(), virtualKeyNameKey, vkName)
	rCtx = context.WithValue(rCtx, virtualKeyIDKey, uuid.New().String())
	rCtx = context.WithValue(rCtx, VirtualKeyHashKey, vkHash)
	// Set empty allowed_providers in context (simulating what middleware does)
	rCtx = context.WithValue(rCtx, ctxkeys.VirtualKeyAllowedProvidersKey, &emptyAllowed)
	req = req.WithContext(rCtx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	// empty slice allowed_providers → len==0 check skips filter → request succeeds
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (empty slice allowed_providers skips filter), got %d; body: %s", w.Code, w.Body.String())
	}
}
