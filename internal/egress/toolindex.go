package egress

// ToolCallIndexer resolves the chat-completions tool_calls index a streamed
// fragment belongs to. A fragment without an index is keyed by its call id
// (a synthetic negative index per id), and one with neither continues the
// call streamed last, since fragments of one call arrive contiguously.
type ToolCallIndexer struct {
	idxByCallID map[string]int // call id -> resolved index
	idByIndex   map[int]string // wire index -> the id that last opened under it
	aliasOf     map[int]int    // wire index -> the call it currently names
	last        int            // index of the call streamed last
}

// NewToolCallIndexer returns an indexer for one stream.
func NewToolCallIndexer() *ToolCallIndexer {
	return &ToolCallIndexer{
		idxByCallID: map[string]int{},
		idByIndex:   map[int]string{},
		aliasOf:     map[int]int{},
	}
}

// Resolve returns the index of the fragment carrying index and id. Mixed
// shapes are read for what they mean: an opener that reuses an index another
// call already holds (a provider that stamps 0 on every call) is a new call,
// and that wire index then names the new call for the id-less fragments that
// follow (latest opener wins); a continuation whose index opened nothing
// (opened reports false) while the last call was id-keyed belongs to that call.
func (x *ToolCallIndexer) Resolve(index *int, id string, opened func(wire int) bool) int {
	switch {
	case index != nil:
		wire := *index
		idx := wire
		if id != "" {
			if known, ok := x.idxByCallID[id]; ok {
				// A call already keyed: an id-bearing continuation, or an
				// opener whose id arrived before its index. Neither re-aliases
				// the wire index; only an opener may, or a continuation of
				// the first call would steal the alias from the call opened
				// after it.
				idx = known
			} else {
				if owner, taken := x.idByIndex[wire]; taken && owner != id {
					idx = -1 - len(x.idxByCallID)
				}
				x.idxByCallID[id] = idx
				x.idByIndex[wire] = id
				x.aliasOf[wire] = idx
			}
		} else if alias, ok := x.aliasOf[wire]; ok {
			idx = alias
		} else if x.last < 0 && !opened(wire) {
			idx = x.last
		}
		x.last = idx
	case id == "":
		// Neither index nor id: a continuation of the call streamed last, not
		// index 0, which an id-keyed opener never claimed.
	default:
		idx, ok := x.idxByCallID[id]
		if !ok {
			idx = -1 - len(x.idxByCallID)
			x.idxByCallID[id] = idx
		}
		x.last = idx
	}
	return x.last
}
