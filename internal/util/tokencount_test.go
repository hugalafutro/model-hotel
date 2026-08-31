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
		{"a large but real count", 2_000_000, 2_000_000},
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

// The ceiling has to stay clear of both column widths a clamped figure can
// land in, with room for a per-request sum of several members.
func TestMaxSaneTokenCount_FitsTheColumns(t *testing.T) {
	if MaxSaneTokenCount*4 > math.MaxInt32 {
		t.Errorf("four clamped members sum to %d, past the int4 column limit %d", MaxSaneTokenCount*4, math.MaxInt32)
	}
	if MaxSaneTokenCount < 2_000_000 {
		t.Errorf("ceiling %d is below the largest context windows in the catalog", MaxSaneTokenCount)
	}
}
