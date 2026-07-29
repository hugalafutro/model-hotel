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

// TestNilDiffEntriesAreInvisible pins how the two feed post-processors treat a
// journal row that carries no diff at all.
//
// The column is scanned into a pointer, so a row written by a future/older
// build, or one whose JSON failed to decode, arrives here as nil. Such a row
// has nothing to render: the modal would draw an empty card for it, and
// counting it would light the "something changed" dot with nothing behind it,
// which is exactly the ignorable indicator the claim badge exists not to be.
// Both functions must drop it, and both must keep the real entry beside it.
func TestNilDiffEntriesAreInvisible(t *testing.T) {
	entries := []DiscoveryChangeEntry{
		{ProviderName: "broken-row", Diff: nil},
		{ProviderName: "real-row", Diff: &DiscoveryDiff{Added: []ModelChange{{ModelID: "m"}}}},
	}

	kept := stripClaimedBuckets(entries, map[string]struct{}{})
	if len(kept) != 1 {
		t.Fatalf("stripClaimedBuckets kept %d entries, want 1 (the nil-diff row must not survive)", len(kept))
	}
	if kept[0].ProviderName != "real-row" {
		t.Errorf("survivor = %q, want the entry that actually has a diff", kept[0].ProviderName)
	}

	if n := countInformationalUnseen(entries); n != 1 {
		t.Errorf("countInformationalUnseen = %d, want 1: a diff-less row must not light the indicator", n)
	}
	if n := countInformationalUnseen([]DiscoveryChangeEntry{{ProviderName: "broken-row", Diff: nil}}); n != 0 {
		t.Errorf("countInformationalUnseen of nil-diff rows alone = %d, want 0", n)
	}
}
