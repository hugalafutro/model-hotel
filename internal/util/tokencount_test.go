package util

import (
	"math"
	"testing"
)

// The clamp is the shared definition two packages write int4 token columns
// through, so it is pinned here rather than only through its callers.
func TestClampTokenCount(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int
		want int
	}{
		{"zero", 0, 0},
		{"one", 1, 1},
		{"an ordinary count", 12_345, 12_345},
		{"the largest catalogued context window", 1_050_000, 1_050_000},
		{"the ceiling itself", MaxSaneTokenCount, MaxSaneTokenCount},
		{"one past the ceiling", MaxSaneTokenCount + 1, MaxSaneTokenCount},
		{"minus one", -1, 0},
		{"a negative count", -500, 0},
		{"int32 max", math.MaxInt32, MaxSaneTokenCount},
		{"int64 max", math.MaxInt64, MaxSaneTokenCount},
		{"int64 min", math.MinInt64, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampTokenCount(tc.in); got != tc.want {
				t.Errorf("ClampTokenCount(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// largestCatalogueContextWindow is the biggest context length in the provider
// catalogs, which bounds any figure a provider can honestly report for a
// request it actually served.
const largestCatalogueContextWindow = 1_050_000

// The ceiling has to stay clear of the int4 columns a clamped figure lands in,
// with room for a per-request sum of members, while staying comfortably above
// anything an honest response can report.
func TestMaxSaneTokenCount_FitsTheColumnsAndRealTraffic(t *testing.T) {
	if MaxSaneTokenCount*4 > math.MaxInt32 {
		t.Errorf("four clamped members sum to %d, past the int4 column limit %d", MaxSaneTokenCount*4, math.MaxInt32)
	}
	if MaxSaneTokenCount < 4*largestCatalogueContextWindow {
		t.Errorf("ceiling %d leaves less than 4x headroom over the largest context window %d", MaxSaneTokenCount, largestCatalogueContextWindow)
	}
	// The cost of the ceiling is how long one clamped charge holds a default
	// 60k-TPM bucket at 429. Keep it inside a working day.
	if hours := float64(MaxSaneTokenCount) / (60_000.0 / 60.0) / 3600.0; hours > 8 {
		t.Errorf("one clamped charge holds a 60k TPM bucket for %.1fh, want <= 8h", hours)
	}
}
