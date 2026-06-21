package api

import (
	"testing"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// TestDiffIsEmpty covers the gate that decides whether a discovery run records a
// row: a nil or all-empty diff is empty, while any single populated field (a
// model change or a failover-group change) makes it non-empty.
func TestDiffIsEmpty(t *testing.T) {
	if !diffIsEmpty(nil) {
		t.Error("nil diff should be empty")
	}
	if !diffIsEmpty(&DiscoveryDiff{}) {
		t.Error("zero-value diff should be empty")
	}

	cases := map[string]*DiscoveryDiff{
		"added":             {Added: []ModelChange{{ModelID: "m"}}},
		"reenabled":         {Reenabled: []ModelChange{{ModelID: "m"}}},
		"disabled":          {Disabled: []ModelChange{{ModelID: "m"}}},
		"updated":           {Updated: []ModelUpdate{{ModelID: "m"}}},
		"failover deleted":  {FailoverDeletedGroups: []failover.DeletedGroupInfo{{}}},
		"failover updated":  {FailoverUpdatedGroups: []failover.UpdatedGroupInfo{{}}},
		"failover disabled": {FailoverDisabledGroups: []failover.DisabledGroupInfo{{}}},
	}
	for name, d := range cases {
		if diffIsEmpty(d) {
			t.Errorf("%s diff should NOT be empty", name)
		}
	}
}

// TestCountAffected sums every diff bucket into the badge number. A nil diff is
// 0; a diff touching one entity in each bucket is the bucket count.
func TestCountAffected(t *testing.T) {
	if got := countAffected(nil); got != 0 {
		t.Errorf("countAffected(nil) = %d, want 0", got)
	}
	if got := countAffected(&DiscoveryDiff{}); got != 0 {
		t.Errorf("countAffected(empty) = %d, want 0", got)
	}

	d := &DiscoveryDiff{
		Added:                  []ModelChange{{ModelID: "a"}},
		Reenabled:              []ModelChange{{ModelID: "b"}},
		Disabled:               []ModelChange{{ModelID: "c"}},
		Updated:                []ModelUpdate{{ModelID: "d"}, {ModelID: "e"}},
		FailoverDeletedGroups:  []failover.DeletedGroupInfo{{}},
		FailoverUpdatedGroups:  []failover.UpdatedGroupInfo{{}},
		FailoverDisabledGroups: []failover.DisabledGroupInfo{{}},
	}
	// Three single-entry buckets, two updated models, three single-entry failover
	// buckets sum to eight affected entities.
	if got := countAffected(d); got != 8 {
		t.Errorf("countAffected = %d, want 8", got)
	}
}

// TestFloatPtrEq covers the pointer-aware, float32-precision price equality used
// to fold discovery round-trips: both-nil is equal, exactly-one-nil is not, and
// equal-vs-different values compare at float32 precision.
func TestFloatPtrEq(t *testing.T) {
	f := func(v float64) *float64 { return &v }

	if !floatPtrEq(nil, nil) {
		t.Error("both nil should be equal (field unset on both ends)")
	}
	if floatPtrEq(f(1), nil) {
		t.Error("one nil should not equal a set value (a fill is a real change)")
	}
	if floatPtrEq(nil, f(1)) {
		t.Error("one nil should not equal a set value (a clear is a real change)")
	}
	if !floatPtrEq(f(0.182), f(0.182)) {
		t.Error("equal values should compare equal")
	}
	if floatPtrEq(f(0.182), f(0.49)) {
		t.Error("different values should compare unequal")
	}
	// Values that differ only below float32 precision are treated as equal, since
	// the price columns are REAL and the original diff recorded at float32.
	if !floatPtrEq(f(0.1), f(0.1+1e-9)) {
		t.Error("sub-float32 differences should be treated as equal")
	}
}
