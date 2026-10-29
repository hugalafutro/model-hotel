package egress

import "testing"

func TestToolCallIndexer_Resolve(t *testing.T) {
	// opened is the caller's answer to "did this wire index open a call?". nil
	// means Resolve must not ask at all for that fragment.
	type step struct {
		index  *int
		id     string
		opened *bool
		want   int
	}
	yes, no := new(true), new(false)

	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "index with a new id opens the call at its wire index",
			steps: []step{
				{index: new(0), id: "a", want: 0},
				{index: new(1), id: "b", want: 1},
			},
		},
		{
			name: "index-only fragment follows the call its wire index opened",
			steps: []step{
				{index: new(0), id: "a", want: 0},
				{index: new(1), id: "b", want: 1},
				{index: new(0), want: 0},
				{index: new(1), want: 1},
			},
		},
		{
			name: "opener reusing a wire index another id owns gets a synthetic index and the wire index aliases it, latest opener wins",
			steps: []step{
				{index: new(0), id: "a", want: 0},
				{index: new(0), want: 0},
				{index: new(0), id: "b", want: -2},
				{index: new(0), want: -2},
				{index: new(0), id: "c", want: -3},
				{index: new(0), want: -3},
			},
		},
		{
			name: "id-bearing continuation keeps its index and does not re-alias the wire index",
			steps: []step{
				{index: new(0), id: "a", want: 0},
				{index: new(0), id: "b", want: -2},
				{index: new(0), id: "a", want: 0},
				// Wire 0 still names b, the call opened after a.
				{index: new(0), want: -2},
			},
		},
		{
			name: "id that arrived before its index keeps its synthetic index and claims no alias",
			steps: []step{
				{id: "a", want: -1},
				{index: new(0), id: "a", want: -1},
				{index: new(0), opened: no, want: -1},
			},
		},
		{
			name: "index-only fragment whose wire index opened nothing continues the id-keyed last call",
			steps: []step{
				{id: "a", want: -1},
				{index: new(0), opened: no, want: -1},
			},
		},
		{
			name: "index-only fragment whose wire index opened a call keeps the wire index",
			steps: []step{
				{id: "a", want: -1},
				{index: new(0), opened: yes, want: 0},
				{want: 0},
			},
		},
		{
			name: "index-only fragment with no alias keeps the wire index without asking when the last call is index-keyed",
			steps: []step{
				{index: new(3), want: 3},
			},
		},
		{
			name: "id-only new id gets a synthetic negative index and becomes last",
			steps: []step{
				{id: "a", want: -1},
				{id: "b", want: -2},
				{want: -2},
			},
		},
		{
			name: "id-only known id resolves to its own index and becomes last",
			steps: []step{
				{id: "a", want: -1},
				{id: "b", want: -2},
				{id: "a", want: -1},
				{want: -1},
			},
		},
		{
			name: "fragment with neither index nor id continues the last call, not the unclaimed index 0",
			steps: []step{
				{want: 0},
				{id: "a", want: -1},
				{want: -1},
				{want: -1},
			},
		},
		{
			name: "synthetic indexes count every keyed id, including index-keyed openers",
			steps: []step{
				{index: new(0), id: "a", want: 0},
				{id: "b", want: -2},
				{want: -2},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := NewToolCallIndexer()
			for i, s := range tc.steps {
				opened := func(wire int) bool {
					if s.opened == nil {
						t.Fatalf("step %d: opened(%d) called, want no call", i, wire)
					}
					if s.index == nil || wire != *s.index {
						t.Fatalf("step %d: opened(%d) called with the wrong wire index", i, wire)
					}
					return *s.opened
				}
				if got := x.Resolve(s.index, s.id, opened); got != s.want {
					t.Fatalf("step %d (index=%v id=%q): got %d, want %d", i, deref(s.index), s.id, got, s.want)
				}
			}
		})
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
