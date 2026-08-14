package model

import "testing"

// TestMarkLiveMetaFromCurrent verifies that only the context-limit fields that
// are actually populated (non-nil) get flagged as live-sourced. Discoverers rely
// on this to distinguish provider-reported values from later catalog/models.dev
// fills, so a field that is nil must never be marked live.
func TestMarkLiveMetaFromCurrent(t *testing.T) {
	t.Run("all fields set flags every meta bit", func(t *testing.T) {
		m := &Model{
			ContextLength:   new(8192),
			MaxOutputTokens: new(4096),
		}
		m.MarkLiveMetaFromCurrent()

		want := LiveMetaFields{
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
		// Live payload carried a context length but no max-output cap. Only the
		// present field may be flagged live.
		m := &Model{
			ContextLength: new(32768),
		}
		m.MarkLiveMetaFromCurrent()

		if !m.LiveMeta.ContextLength {
			t.Error("ContextLength should be flagged live (field was set)")
		}
		if m.LiveMeta.MaxOutputTokens {
			t.Error("MaxOutputTokens must stay false (field was nil)")
		}
	})

	t.Run("recomputes from current state, clearing stale flags", func(t *testing.T) {
		// A field that was live earlier but is now nil must be un-flagged: the
		// method is a full recompute, not an additive merge.
		m := &Model{ContextLength: new(8192)}
		m.MarkLiveMetaFromCurrent()
		if !m.LiveMeta.ContextLength {
			t.Fatal("precondition: ContextLength should be live")
		}

		m.ContextLength = nil
		m.MarkLiveMetaFromCurrent()
		if m.LiveMeta.ContextLength {
			t.Error("ContextLength must be cleared once the field is nil again")
		}
	})
}
