package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime/metrics"
	"testing"
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
	fn := func(project string) util.AggregatedDockerStats {
		called = true
		return util.AggregatedDockerStats{}
	}

	h.SetDockerStatsCollector(fn)

	// Verify it was set by calling it
	_ = h.dockerStatsCollector("test")
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
	t.Parallel()

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
	t.Parallel()

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
	h.SetDockerStatsCollector(func(project string) util.AggregatedDockerStats {
		collectorCalled = true
		return mockStats
	})

	// Make request to /system/
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
