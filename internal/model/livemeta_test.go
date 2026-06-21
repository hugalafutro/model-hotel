package model

import "testing"

// TestMarkLiveMetaFromCurrent verifies that only the pricing/context fields that
// are actually populated (non-nil) get flagged as live-sourced. Discoverers rely
// on this to distinguish provider-reported values from later catalog/models.dev
// fills, so a field that is nil must never be marked live.
func TestMarkLiveMetaFromCurrent(t *testing.T) {
	t.Run("all fields set flags every meta bit", func(t *testing.T) {
		m := &Model{
			InputPricePerMillion:         float64Ptr(1),
			InputPricePerMillionCacheHit: float64Ptr(0.5),
			OutputPricePerMillion:        float64Ptr(2),
			ContextLength:                intPtr(8192),
			MaxOutputTokens:              intPtr(4096),
		}
		m.MarkLiveMetaFromCurrent()

		want := LiveMetaFields{
			InputPrice:      true,
			InputPriceCache: true,
			OutputPrice:     true,
			ContextLength:   true,
			MaxOutputTokens: true,
		}
		if m.LiveMeta != want {
			t.Errorf("LiveMeta = %+v, want %+v", m.LiveMeta, want)
		}
	})

	t.Run("no fields set leaves every meta bit false", func(t *testing.T) {
		m := &Model{}
		m.MarkLiveMetaFromCurrent()
		if m.LiveMeta != (LiveMetaFields{}) {
			t.Errorf("LiveMeta = %+v, want zero value", m.LiveMeta)
		}
	})

	t.Run("flags only the populated fields", func(t *testing.T) {
		// Live payload carried a price and context length but no cache-hit price
		// or max-output cap. Only the two present fields may be flagged live.
		m := &Model{
			InputPricePerMillion: float64Ptr(3),
			ContextLength:        intPtr(32768),
		}
		m.MarkLiveMetaFromCurrent()

		if !m.LiveMeta.InputPrice {
			t.Error("InputPrice should be flagged live (field was set)")
		}
		if !m.LiveMeta.ContextLength {
			t.Error("ContextLength should be flagged live (field was set)")
		}
		if m.LiveMeta.InputPriceCache {
			t.Error("InputPriceCache must stay false (field was nil)")
		}
		if m.LiveMeta.OutputPrice {
			t.Error("OutputPrice must stay false (field was nil)")
		}
		if m.LiveMeta.MaxOutputTokens {
			t.Error("MaxOutputTokens must stay false (field was nil)")
		}
	})

	t.Run("recomputes from current state, clearing stale flags", func(t *testing.T) {
		// A field that was live earlier but is now nil must be un-flagged: the
		// method is a full recompute, not an additive merge.
		m := &Model{InputPricePerMillion: float64Ptr(1)}
		m.MarkLiveMetaFromCurrent()
		if !m.LiveMeta.InputPrice {
			t.Fatal("precondition: InputPrice should be live")
		}

		m.InputPricePerMillion = nil
		m.MarkLiveMetaFromCurrent()
		if m.LiveMeta.InputPrice {
			t.Error("InputPrice must be cleared once the field is nil again")
		}
	})
}
