package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime/metrics"
	"testing"
	"time"
	"unsafe"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// value constructs a metrics.Value with the given kind and scalar.
// The fields of metrics.Value are unexported, so we use unsafe to build
// test values matching the internal layout: {kind ValueKind, scalar uint64}.
//
//nolint:gosec // test-only: unsafe for reflective struct access
func value(kind metrics.ValueKind, scalar uint64) metrics.Value {
	return *(*metrics.Value)(unsafe.Pointer(&struct {
		kind   metrics.ValueKind
		scalar uint64
	}{kind, scalar}))
}

// ---------------------------------------------------------------------------

func TestGetInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    metrics.Sample
		want int64
	}{
		{
			name: "kind_uint64_42",
			s:    metrics.Sample{Value: value(metrics.KindUint64, 42)},
			want: 42,
		},
		{
			name: "kind_uint64_zero",
			s:    metrics.Sample{Value: value(metrics.KindUint64, 0)},
			want: 0,
		},
		{
			name: "kind_float64_3_7",
			s:    metrics.Sample{Value: value(metrics.KindFloat64, math.Float64bits(3.7))},
			want: 3,
		},
		{
			name: "kind_float64_99_9",
			s:    metrics.Sample{Value: value(metrics.KindFloat64, math.Float64bits(99.9))},
			want: 99,
		},
		{
			name: "kind_bad_default",
			s:    metrics.Sample{Value: value(metrics.KindBad, 0)},
			want: 0,
		},
		{
			name: "kind_uint64_large",
			s:    metrics.Sample{Value: value(metrics.KindUint64, 1<<32)},
			want: int64(1 << 32),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getInt64(tt.s)
			if got != tt.want {
				t.Errorf("getInt64(%+v) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

func TestGetUint64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    metrics.Sample
		want uint64
	}{
		{
			name: "kind_uint64_42",
			s:    metrics.Sample{Value: value(metrics.KindUint64, 42)},
			want: 42,
		},
		{
			name: "kind_uint64_zero",
			s:    metrics.Sample{Value: value(metrics.KindUint64, 0)},
			want: 0,
		},
		{
			name: "kind_uint64_large",
			s:    metrics.Sample{Value: value(metrics.KindUint64, 1<<63)},
			want: 1 << 63,
		},
		{
			name: "kind_float64_wrong_kind",
			s:    metrics.Sample{Value: value(metrics.KindFloat64, math.Float64bits(42.5))},
			want: 0,
		},
		{
			name: "kind_bad_wrong_kind",
			s:    metrics.Sample{Value: value(metrics.KindBad, 0)},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getUint64(tt.s)
			if got != tt.want {
				t.Errorf("getUint64(%+v) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

// TestSetDockerStatsCollector tests the SetDockerStatsCollector setter.
func TestSetDockerStatsCollector(t *testing.T) {
	t.Parallel()

	h := &SystemHandler{}
	called := false
	fn := func(filter util.ContainerFilter) util.AggregatedDockerStats {
		called = true
		return util.AggregatedDockerStats{}
	}

	h.SetDockerStatsCollector(fn)

	// Verify it was set by calling it
	_ = h.dockerStatsCollector(util.ContainerFilter{})
	if !called {
		t.Error("expected dockerStatsCollector to be set and callable")
	}
}

// TestGetSystem_Handler tests the GetSystem handler integration.
func TestGetSystem_Handler(t *testing.T) {
	t.Parallel()

	_, r := newTestHandlerWithRouter(t)

	// Make a GET request to /system/
	req := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// Decode and verify response structure
	var response SystemStats
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response has expected structure (app and db fields)
	// App stats should have non-zero uptime (server has been running)
	if response.App.UptimeSeconds < 0 {
		t.Error("expected non-negative uptime_seconds")
	}

	// DB stats should be present (may be zero if DB queries fail, but struct should exist)
	// Cache hit ratio is a percentage, should be between 0 and 100
	if response.DB.CacheHitRatio < 0 || response.DB.CacheHitRatio > 100 {
		t.Errorf("expected cache_hit_ratio between 0 and 100, got %f", response.DB.CacheHitRatio)
	}
}

// TestGetSystem_CacheHit tests that calling GetSystem twice within the cache TTL
// returns cached data on the second call.
func TestGetSystem_CacheHit(t *testing.T) {
	// Do NOT use t.Parallel() — this test resets and populates the
	// package-level system cache, which is shared mutable state.

	resetSystemCache()
	_, r := newTestHandlerWithRouter(t)

	// First request - cache miss, collects fresh data
	req1 := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected status 200 OK, got %d: %s", w1.Code, w1.Body.String())
	}

	var response1 SystemStats
	if err := json.NewDecoder(w1.Body).Decode(&response1); err != nil {
		t.Fatalf("first request: failed to decode response: %v", err)
	}

	// Second request immediately - should hit cache
	req2 := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected status 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response2 SystemStats
	if err := json.NewDecoder(w2.Body).Decode(&response2); err != nil {
		t.Fatalf("second request: failed to decode response: %v", err)
	}

	// Cache hit returns the cached struct verbatim, so uptime must match
	if response1.App.UptimeSeconds != response2.App.UptimeSeconds {
		t.Errorf("cache hit should return same data: uptime1=%d, uptime2=%d",
			response1.App.UptimeSeconds, response2.App.UptimeSeconds)
	}
}

// TestGetSystem_InvalidSince tests that an invalid 'since' query parameter
// causes collect() to return an error, resulting in 500 Internal Server Error.
func TestGetSystem_InvalidSince(t *testing.T) {
	t.Parallel()

	_, r := newTestHandlerWithRouter(t)

	// Make request with invalid since parameter
	req := httptest.NewRequest(http.MethodGet, "/system/?since=not-a-date", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 Internal Server Error, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetSystem_DockerStatsCollector tests that the Docker stats collector
// is called and its result appears in the response.
func TestGetSystem_DockerStatsCollector(t *testing.T) {
	// Do NOT use t.Parallel() — this test mutates the handler's
	// systemHandler.dockerStatsCollector, which is shared state.

	resetSystemCache()
	h, r := newTestHandlerWithRouter(t)

	// Override the Docker stats collector with a mock that returns non-empty stats
	collectorCalled := false
	mockStats := util.AggregatedDockerStats{
		Available:      true,
		CPUPercent:     42.5,
		MemoryUsage:    123456789,
		MemoryLimit:    987654321,
		ContainerCount: 3,
	}
	h.SetDockerStatsCollector(func(filter util.ContainerFilter) util.AggregatedDockerStats {
		collectorCalled = true
		return mockStats
	})

	req := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// Decode and verify response
	var response SystemStats
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify the collector was called
	if !collectorCalled {
		t.Error("expected Docker stats collector to be called")
	}

	// Verify the mock data appears in the response
	if response.Docker.Available != mockStats.Available {
		t.Errorf("expected Docker.Available=%v, got %v", mockStats.Available, response.Docker.Available)
	}
	if response.Docker.CPUPercent != mockStats.CPUPercent {
		t.Errorf("expected Docker.CPUPercent=%f, got %f", mockStats.CPUPercent, response.Docker.CPUPercent)
	}
	if response.Docker.MemoryUsage != mockStats.MemoryUsage {
		t.Errorf("expected Docker.MemoryUsage=%d, got %d", mockStats.MemoryUsage, response.Docker.MemoryUsage)
	}
	if response.Docker.ContainerCount != mockStats.ContainerCount {
		t.Errorf("expected Docker.ContainerCount=%d, got %d", mockStats.ContainerCount, response.Docker.ContainerCount)
	}
}

func TestGetSystem_CachedJSONEncodeError(t *testing.T) {
	// Do NOT use t.Parallel() — mutates package-level cache.
	resetSystemCache()
	_, r := newTestHandlerWithRouter(t)

	// First call to populate cache
	req := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Second call with broken writer — should hit cache and fail to encode
	w := &brokenResponseWriter{}
	req2 := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(w, req2)

	if w.code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.code)
	}
}

func TestGetSystem_FreshJSONEncodeError(t *testing.T) {
	// Do NOT use t.Parallel() — mutates package-level cache.
	resetSystemCache()
	_, r := newTestHandlerWithRouter(t)

	// Call with broken writer — cache miss, collect succeeds, encode fails
	w := &brokenResponseWriter{}
	req := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(w, req)

	if w.code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.code)
	}
}

func TestGetSystem_ValidSince(t *testing.T) {
	// Do NOT use t.Parallel() — mutates package-level cache.
	resetSystemCache()
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/system/?since=2024-01-01T00:00:00Z", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response SystemStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
}

func TestGetSystem_CancelledContext(t *testing.T) {
	// Do NOT use t.Parallel() — mutates package-level cache.
	// A caller that cancels mid-flight must NOT abort the collect. GetSystem runs
	// the collect on a context detached from the request, so a poller that gives
	// up early (e.g. Front Desk's 4s probe timing out on a freshly restarted, slow
	// instance) still lets the collect finish and warm the 3s cache. Without this
	// the instance stays stuck cold on every poll. So an immediately-canceled
	// request still returns 200, and — the key property — it warms the cache.
	resetSystemCache()
	_, r := newTestHandlerWithRouter(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody).
		WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (detached collect ignores request cancellation), got %d: %s",
			rec.Code, rec.Body.String())
	}

	// The detached collect must have populated the cache despite the cancellation.
	cachedSystemMu.Lock()
	warmed := cachedSystem != nil
	cachedSystemMu.Unlock()
	if !warmed {
		t.Error("canceled request should still warm the cache via the detached collect")
	}

	// Follow-up healthy request is served (from the warmed cache) as 200.
	req2 := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("follow-up request: expected 200, got %d", rec2.Code)
	}
}

func TestRequestsSince_CachesWithinTTL(t *testing.T) {
	// Do NOT use t.Parallel() — mutates the package-level requestsToday cache and
	// inserts into the shared request_logs table.
	resetSystemCache()
	h, _ := newTestHandlerWithRouter(t)
	sh := h.systemHandler
	ctx := context.Background()

	const key = "cache-test-window"
	since := time.Now().Add(-time.Hour)

	baseline := sh.requestsSince(ctx, since, key)

	// Insert a row inside the window AFTER the value was cached. Within the TTL the
	// cached baseline must come back: the COUNT is not re-run on every collect,
	// which is what keeps a stale-stats seq-scan off the hot status path.
	if _, err := sh.pool.Exec(ctx,
		`INSERT INTO request_logs (created_at) VALUES ($1)`, time.Now()); err != nil {
		t.Fatalf("insert request_log: %v", err)
	}
	if got := sh.requestsSince(ctx, since, key); got != baseline {
		t.Errorf("within TTL expected cached %d, got %d (COUNT re-ran)", baseline, got)
	}

	// After a cache reset the fresh COUNT sees the inserted row.
	resetSystemCache()
	if got := sh.requestsSince(ctx, since, key); got != baseline+1 {
		t.Errorf("after reset expected fresh %d, got %d", baseline+1, got)
	}
}

func TestCollect_CancelledContext(t *testing.T) {
	// collect() zeroes DB stats gracefully under a cancelled context, but the
	// fleet-status read propagates the cancellation as an error so the caller
	// (GetSystem) can refuse to cache a half-read payload.
	h, _ := newTestHandlerWithRouter(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats, err := h.systemHandler.collect(ctx, "")
	if err == nil {
		t.Fatal("expected an error from collect with cancelled context (fleet read), got nil")
	}

	// The partially-collected DB stats are still zeroed (graceful degradation).
	if stats.DB.SizeMB != 0 {
		t.Errorf("expected DB.SizeMB=0 (cancelled context), got %f", stats.DB.SizeMB)
	}
	if stats.DB.Connections != 0 {
		t.Errorf("expected DB.Connections=0 (cancelled context), got %d", stats.DB.Connections)
	}
	if stats.DB.CacheHitRatio != 0 {
		t.Errorf("expected DB.CacheHitRatio=0 (cancelled context), got %f", stats.DB.CacheHitRatio)
	}
	if stats.DB.DeadTuples != 0 {
		t.Errorf("expected DB.DeadTuples=0 (cancelled context), got %d", stats.DB.DeadTuples)
	}
	if stats.DB.LockWaits != 0 {
		t.Errorf("expected DB.LockWaits=0 (cancelled context), got %d", stats.DB.LockWaits)
	}
}

func TestCollect_TxPerSecNegative(t *testing.T) {
	// Do NOT use t.Parallel() — mutates package-level prevTxCount/prevTxTime.
	// Set prevTxCount to a very high value so that (totalTx - prevTxCount) < 0
	prevTxMu.Lock()
	prevTxCount = 1<<62 - 1 // Very high value
	prevTxTime = time.Now().Add(-1 * time.Second)
	prevTxMu.Unlock()
	defer func() {
		prevTxMu.Lock()
		prevTxCount = 0
		prevTxTime = time.Time{}
		prevTxMu.Unlock()
	}()

	h, _ := newTestHandlerWithRouter(t)

	stats, err := h.systemHandler.collect(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// txPerSec should be 0 (reset from negative)
	if stats.DB.TxPerSec < 0 {
		t.Errorf("expected txPerSec >= 0, got %f", stats.DB.TxPerSec)
	}
}

// TestGetSystem_FleetReadErrorNotCached is the regression guard for the
// cache-poisoning incident: a settings read failure while computing fleet
// status must make GetSystem respond 500 and must NOT store the half-read
// payload in the 3s response cache. A follow-up request with healthy settings
// must then get fresh, correct data rather than a cached bad payload.
func TestGetSystem_ExposesInstanceID(t *testing.T) {
	// Do NOT use t.Parallel(): mutates the package-level system cache.
	resetSystemCache()
	h, _ := newTestHandlerWithRouter(t)
	ctx := context.Background()

	// In production migration 056 seeds instance_id once; the test harness clears
	// the settings table, so seed it here to exercise the collect() wiring that
	// surfaces it on /api/system for Front Desk's same-instance detection.
	if err := h.settingsRepo.Set(ctx, "instance_id", "iid-abc-123"); err != nil {
		t.Fatalf("seed instance_id: %v", err)
	}

	stats, err := h.systemHandler.collect(ctx, "")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if stats.InstanceID != "iid-abc-123" {
		t.Fatalf("instance_id = %q, want %q", stats.InstanceID, "iid-abc-123")
	}
}

func TestGetSystem_FleetReadErrorNotCached(t *testing.T) {
	// Do NOT use t.Parallel(): mutates the package-level system cache.
	resetSystemCache()
	h, _ := newTestHandlerWithRouter(t)

	seen := time.Now().UTC().Format(time.RFC3339)
	fake := &fakeFleetSettings{
		values: map[string]string{
			keyFleetManagedSeenAt: seen,
			keyFleetIsPrimary:     "true",
		},
		getErr: map[string]error{keyFleetIsPrimary: errors.New("db read failed")},
	}
	h.systemHandler.settings = fake

	// First request: the fleet read errors -> 500, nothing cached.
	req1 := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	w1 := httptest.NewRecorder()
	h.systemHandler.GetSystem(w1, req1)
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("errored fleet read: expected 500, got %d: %s", w1.Code, w1.Body.String())
	}

	// Heal the settings; the next request must return fresh 200 data, proving the
	// bad payload was never cached.
	fake.getErr = nil
	req2 := httptest.NewRequest(http.MethodGet, "/system/", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	h.systemHandler.GetSystem(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("healed fleet read: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp SystemStats
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("healed request: failed to decode: %v", err)
	}
	if resp.Fleet == nil || resp.Fleet.State != "primary" {
		t.Fatalf("healed request: fleet = %+v, want state \"primary\"", resp.Fleet)
	}
}
