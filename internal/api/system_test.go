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
