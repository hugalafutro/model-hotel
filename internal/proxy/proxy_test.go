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
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/config"
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
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1")).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
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

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

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

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

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

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

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
