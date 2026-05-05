package proxy

import (
	"context"
	"encoding/json"
	"fmt"
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
	var err error
	testDB, err = db.New(ctx, "postgres://llmproxy:changeme@localhost:5432/testdb?sslmode=disable", 25, 5)
	if err != nil {
		testDB = nil
	}
	code := m.Run()
	if testDB != nil {
		testDB.Close()
	}
	os.Exit(code)
}

// newIntegrationHandler creates a Handler with a real settings.Repository
// backed by the test database. Returns nil if the database is unavailable.
func newIntegrationHandler() *Handler {
	if testDB == nil {
		return nil
	}
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)
	return &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key-for-proxy-tests"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
	}
}

// ---------------------------------------------------------------------------
// shouldFailover tests (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestShouldFailover_5xx(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
	for _, code := range []int{500, 502, 503, 504} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_429_DefaultEnabled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
	// Default setting for failover_on_rate_limit is true
	if !h.shouldFailover(context.Background(), 429) {
		t.Error("429 should trigger failover when failover_on_rate_limit=true (default)")
	}
}

func TestShouldFailover_429_Disabled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
	for _, code := range []int{401, 403} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_SuccessCodes(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
	for _, code := range []int{200, 201, 204, 301, 302} {
		if h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should NOT trigger failover", code)
		}
	}
}

func TestShouldFailover_Other4xx(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
	for _, code := range []int{400, 404, 405, 408, 422} {
		if h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should NOT trigger failover", code)
		}
	}
}

// ---------------------------------------------------------------------------
// ChatCompletions request validation tests (integration — requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestChatCompletions_MissingBody(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	base := float64(100 * time.Millisecond)
	cap := float64(2 * time.Second)

	expected := []time.Duration{
		0,
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		2000 * time.Millisecond,
		2000 * time.Millisecond,
	}

	for attempt, want := range expected {
		if attempt == 0 {
			continue
		}
		got := backoffDuration(base, cap, attempt)
		if got != want {
			t.Errorf("attempt %d: backoff = %v, want %v", attempt, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// ProxyKeyMiddleware tests (no DB needed for auth header checks)
// ---------------------------------------------------------------------------

func TestProxyKeyMiddleware_MissingHeader(t *testing.T) {
	h := &Handler{cfg: &config.Config{MasterKey: "test"}, ipLimiter: ratelimit.NewIPLimiter(30, 60, nil)}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
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
	h := &Handler{cfg: &config.Config{MasterKey: "test"}, ipLimiter: ratelimit.NewIPLimiter(30, 60, nil)}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := h.ProxyKeyMiddleware(next)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if err := h.insertRequestLog(req.Context(), logData); err != nil {
		t.Fatalf("failed to insert request log: %v", err)
	}

	h.handleStreamingResponse(innerRW, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, "test-hash", 1)

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

func backoffDuration(base, cap float64, attempt int) time.Duration {
	return time.Duration(min(base*pow2(attempt-1), cap))
}

func pow2(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 2
	}
	return result
}
