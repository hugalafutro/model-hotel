package proxy

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
		cfg:               &config.Config{MasterKey: "test-master-key-for-proxy-tests"},
		settingsRepo:      settingsRepo,
		failoverRepo:      failoverRepo,
		modelRepo:         modelRepo,
		providerRepo:      providerRepo,
		virtualKeyRepo:    &virtualKeyRepoAdapter{repo: virtualKeyRepo},
		rateLimiter:       limiter,
		tpmLimiter:        ratelimit.NewTPMLimiter(settingsRepo),
		ipLimiter:         ipLimiter,
		dbPool:            pool,
		circuitBreaker:    failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: newCanonicalTransport(),
		safeDialer:        NewSafeDialer(nil, nil),
	}
}

// newCanonicalTransport returns the standard upstream transport used by proxy
// integration tests: loopback plus known provider hosts allowed, 120s timeouts.
func newCanonicalTransport() *http.Transport {
	return &http.Transport{
		DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
		ResponseHeaderTimeout: 120 * time.Second,
		IdleConnTimeout:       120 * time.Second,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   20,
	}
}

// newCanonicalHandler assembles a fully-wired proxy Handler from the standard
// set of test repositories and rate limiters. It is the shared replacement for
// the ~20-line &Handler{...} literal that integration tests would otherwise
// repeat verbatim. The upstream transport's idle connections are closed via
// t.Cleanup, and a fresh circuit breaker is created from settingsRepo.
func newCanonicalHandler(t *testing.T, masterKey string, pool *pgxpool.Pool,
	settingsRepo *settings.Repository, failoverRepo *failover.Repository,
	modelRepo ModelRepository, providerRepo *provider.Repository,
	virtualKeyRepo *virtualkey.Repository,
	limiter *ratelimit.Limiter, ipLimiter *ratelimit.IPLimiter,
) *Handler {
	t.Helper()
	h := &Handler{
		cfg:               &config.Config{MasterKey: masterKey},
		settingsRepo:      settingsRepo,
		failoverRepo:      failoverRepo,
		modelRepo:         modelRepo,
		providerRepo:      providerRepo,
		virtualKeyRepo:    WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:       limiter,
		tpmLimiter:        ratelimit.NewTPMLimiter(settingsRepo),
		ipLimiter:         ipLimiter,
		circuitBreaker:    failover.NewCircuitBreaker(settingsRepo),
		dbPool:            pool,
		upstreamTransport: newCanonicalTransport(),
		safeDialer:        NewSafeDialer(nil, nil),
	}
	t.Cleanup(func() { h.upstreamTransport.CloseIdleConnections() })
	return h
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

	h.failRequest(logData, 502, KindProviderError, "test error", 1, startTime, 1.5, timings, resolveCacheHits{}, 0.5)

	if logData.statusCode != 502 {
		t.Errorf("expected statusCode=502, got %d", logData.statusCode)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("expected errorKind=%q, got %q", KindProviderError, logData.errorKind)
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

// TestCacheHits_RoundTrip verifies that cache hit data written to request_logs
// can be read back from the database as valid JSON, confirming the full
// proxy → DB → API path works for the cache_hits JSONB column.
func TestCacheHits_RoundTrip(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	timings := resolveTimings{}
	failoverHit := true
	modelHit := false
	cacheHits := resolveCacheHits{
		Failover: &failoverHit,
		Model:    &modelHit,
	}

	logData := &requestLogData{
		modelID:         "roundtrip-model",
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		failoverAttempt: 0,
		state:           "pending",
	}

	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	h.failRequest(logData, 502, KindProviderError, "cache hits round-trip test", 0, time.Now(), 0, timings, cacheHits, 0)

	// Read the cache_hits column back from DB.
	var rawJSON []byte
	err := h.dbPool.QueryRow(context.Background(),
		`SELECT cache_hits FROM request_logs WHERE model_id = $1`, "roundtrip-model",
	).Scan(&rawJSON)
	if err != nil {
		t.Fatalf("failed to read cache_hits: %v", err)
	}

	// Empty JSON means the column is null (should not happen after update).
	if len(rawJSON) == 0 {
		t.Fatal("cache_hits column is null — updateRequestLog did not write it")
	}

	// Parse the JSON to verify the values round-tripped correctly.
	var parsed map[string]interface{}
	if err := json.Unmarshal(rawJSON, &parsed); err != nil {
		t.Fatalf("cache_hits is not valid JSON: %v (raw: %s)", err, string(rawJSON))
	}
	if parsed["failover"] != true {
		t.Errorf("expected failover=true, got %v", parsed["failover"])
	}
	if parsed["model"] != false {
		t.Errorf("expected model=false, got %v", parsed["model"])
	}
	// Provider, Key, Settings are nil (omitempty) so should not appear.
	if _, exists := parsed["provider"]; exists {
		t.Errorf("expected provider to be omitted (nil), but found: %v", parsed["provider"])
	}
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
