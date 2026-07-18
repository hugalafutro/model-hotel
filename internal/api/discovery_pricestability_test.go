package api

import (
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// fc builds a price/context FieldChange for the collapse tests.
func fc(field string, oldV, newV *float64) FieldChange {
	return FieldChange{Field: field, Old: oldV, New: newV}
}

// updEntry builds one provider's recorded diff carrying a single model update.
func updEntry(provID, provName string, at time.Time, modelID string, changes ...FieldChange) DiscoveryChangeEntry {
	return DiscoveryChangeEntry{
		ProviderID:   provID,
		ProviderName: provName,
		DetectedAt:   at,
		Diff:         &DiscoveryDiff{Updated: []ModelUpdate{{ModelID: modelID, Changes: changes}}},
	}
}

func TestCollapseRoundTrips(t *testing.T) {
	t0 := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)

	t.Run("net-zero pair is removed entirely", func(t *testing.T) {
		// Newest-first, as listPendingDiscoveryChanges returns: the later run
		// ($0.182 -> $0.49) sits above the earlier run ($0.49 -> $0.182).
		entries := []DiscoveryChangeEntry{
			updEntry("p1", "OR", t1, "z-ai/glm-5.1", fc("input_price_cache", new(0.182), new(0.49))),
			updEntry("p1", "OR", t0, "z-ai/glm-5.1", fc("input_price_cache", new(0.49), new(0.182))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 0 {
			t.Fatalf("expected round-trip to collapse to 0 entries, got %d: %+v", len(got), got)
		}
	})

	t.Run("one-directional change is kept", func(t *testing.T) {
		entries := []DiscoveryChangeEntry{
			updEntry("p1", "OR", t0, "z-ai/glm-5.1", fc("input_price_cache", new(0.49), new(0.182))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 1 || len(got[0].Diff.Updated) != 1 {
			t.Fatalf("expected the single genuine change to survive, got %+v", got)
		}
	})

	t.Run("only the round-tripped field is dropped", func(t *testing.T) {
		// input_price returns to its start; output_price ends changed.
		entries := []DiscoveryChangeEntry{
			updEntry("p1", "OR", t1, "m",
				fc("input_price", new(2.0), new(1.0)),
				fc("output_price", new(8.0), new(9.0)),
			),
			updEntry("p1", "OR", t0, "m",
				fc("input_price", new(1.0), new(2.0)),
				fc("output_price", new(7.0), new(8.0)),
			),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 2 {
			t.Fatalf("expected both entries to survive (output_price still changed), got %d", len(got))
		}
		for _, e := range got {
			for _, u := range e.Diff.Updated {
				for _, c := range u.Changes {
					if c.Field == "input_price" {
						t.Fatalf("input_price round-trip should have been dropped, found %+v", c)
					}
				}
			}
		}
	})

	t.Run("same model+field under different providers is not merged", func(t *testing.T) {
		// Two distinct providers each show one genuine, opposite move. They must
		// NOT chain into a false round-trip.
		entries := []DiscoveryChangeEntry{
			updEntry("pA", "A", t0, "openai/gpt-5-mini", fc("input_price_cache", new(0.49), new(0.182))),
			updEntry("pB", "B", t0, "openai/gpt-5-mini", fc("input_price_cache", new(0.182), new(0.49))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 2 {
			t.Fatalf("changes from two providers must both survive, got %d", len(got))
		}
	})

	t.Run("three-run bounce A->B->C->A collapses", func(t *testing.T) {
		entries := []DiscoveryChangeEntry{
			updEntry("p1", "OR", t2, "m", fc("input_price", new(3.0), new(1.0))),
			updEntry("p1", "OR", t1, "m", fc("input_price", new(2.0), new(3.0))),
			updEntry("p1", "OR", t0, "m", fc("input_price", new(1.0), new(2.0))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 0 {
			t.Fatalf("expected full bounce back to start to collapse, got %d entries", len(got))
		}
	})

	t.Run("membership churn is left untouched", func(t *testing.T) {
		entries := []DiscoveryChangeEntry{
			{ProviderID: "p1", ProviderName: "OR", DetectedAt: t1, Diff: &DiscoveryDiff{Disabled: []ModelChange{{ModelID: "m", Reason: "not_listed"}}}},
			{ProviderID: "p1", ProviderName: "OR", DetectedAt: t0, Diff: &DiscoveryDiff{Added: []ModelChange{{ModelID: "m", Reason: "new_model"}}}},
		}
		got := collapseRoundTrips(entries)
		if len(got) != 2 {
			t.Fatalf("add/disable churn must not be collapsed, got %d", len(got))
		}
	})

	t.Run("provider keyed by name when id is empty", func(t *testing.T) {
		entries := []DiscoveryChangeEntry{
			updEntry("", "Deleted OR", t1, "m", fc("input_price", new(0.182), new(0.49))),
			updEntry("", "Deleted OR", t0, "m", fc("input_price", new(0.49), new(0.182))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 0 {
			t.Fatalf("round-trip on a name-keyed provider should collapse, got %d", len(got))
		}
	})

	t.Run("entry with a round-trip and a membership change keeps only the membership", func(t *testing.T) {
		entries := []DiscoveryChangeEntry{
			{
				ProviderID: "p1", ProviderName: "OR", DetectedAt: t1,
				Diff: &DiscoveryDiff{
					Updated:  []ModelUpdate{{ModelID: "m", Changes: []FieldChange{fc("input_price", new(0.182), new(0.49))}}},
					Disabled: []ModelChange{{ModelID: "x", Reason: "not_listed"}},
				},
			},
			updEntry("p1", "OR", t0, "m", fc("input_price", new(0.49), new(0.182))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 1 {
			t.Fatalf("expected 1 surviving entry (the one with the membership change), got %d", len(got))
		}
		if len(got[0].Diff.Updated) != 0 {
			t.Fatalf("round-tripped field should be gone, got Updated=%+v", got[0].Diff.Updated)
		}
		if len(got[0].Diff.Disabled) != 1 || got[0].Diff.Disabled[0].ModelID != "x" {
			t.Fatalf("membership change must survive, got Disabled=%+v", got[0].Diff.Disabled)
		}
	})

	t.Run("equal DetectedAt ties still collapse a net-zero swing", func(t *testing.T) {
		// Same timestamp on both runs: first/last resolve by slice order, and the
		// earliest "from" ($0.49) still equals the latest "to" ($0.49).
		entries := []DiscoveryChangeEntry{
			updEntry("p1", "OR", t0, "m", fc("input_price", new(0.49), new(0.182))),
			updEntry("p1", "OR", t0, "m", fc("input_price", new(0.182), new(0.49))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 0 {
			t.Fatalf("net-zero swing with tied timestamps should collapse, got %d", len(got))
		}
	})

	t.Run("same provider under id then name-only does not chain", func(t *testing.T) {
		// A provider deleted mid-review: one entry carries its ID, a later one only
		// the name. Different keys, so the two opposite moves must NOT cancel.
		entries := []DiscoveryChangeEntry{
			updEntry("", "OR", t1, "m", fc("input_price", new(0.182), new(0.49))),
			updEntry("p1", "OR", t0, "m", fc("input_price", new(0.49), new(0.182))),
		}
		got := collapseRoundTrips(entries)
		if len(got) != 2 {
			t.Fatalf("id-keyed and name-keyed entries must not chain, got %d", len(got))
		}
	})
}

func TestCollapseRoundTrips_DoesNotMutateInput(t *testing.T) {
	t0 := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	entries := []DiscoveryChangeEntry{
		updEntry("p1", "OR", t1, "m", fc("input_price", new(0.182), new(0.49))),
		updEntry("p1", "OR", t0, "m", fc("input_price", new(0.49), new(0.182))),
	}
	origDiff0 := entries[0].Diff
	origUpdatedLen := len(entries[0].Diff.Updated)
	origChangesLen := len(entries[0].Diff.Updated[0].Changes)

	_ = collapseRoundTrips(entries)

	if entries[0].Diff != origDiff0 {
		t.Fatal("collapseRoundTrips must not swap the caller's Diff pointer")
	}
	if len(entries[0].Diff.Updated) != origUpdatedLen {
		t.Fatalf("caller's Updated slice was mutated: len %d != %d", len(entries[0].Diff.Updated), origUpdatedLen)
	}
	if len(entries[0].Diff.Updated[0].Changes) != origChangesLen {
		t.Fatalf("caller's Changes slice was mutated: len %d != %d", len(entries[0].Diff.Updated[0].Changes), origChangesLen)
	}
}

func TestDampenOpenRouterPriceJitter(t *testing.T) {
	const orURL = "https://openrouter.ai/api/v1"

	liveModel := func(id string, cache *float64) *model.Model {
		m := &model.Model{ModelID: id, InputPricePerMillionCacheHit: cache}
		m.LiveMeta.InputPriceCache = cache != nil
		return m
	}

	t.Run("within tolerance demotes the field to fill-only", func(t *testing.T) {
		snap := map[string]ModelSnapshot{"m": {inputPriceCache: new(0.50)}}
		m := liveModel("m", new(0.48)) // 4% drift, under 7%
		DampenOpenRouterPriceJitter(orURL, snap, []*model.Model{m})
		if m.LiveMeta.InputPriceCache {
			t.Fatal("sub-tolerance wiggle should have cleared the live flag")
		}
	})

	t.Run("beyond tolerance stays live", func(t *testing.T) {
		snap := map[string]ModelSnapshot{"m": {inputPriceCache: new(0.49)}}
		m := liveModel("m", new(0.182)) // 63% drop, real upstream switch
		DampenOpenRouterPriceJitter(orURL, snap, []*model.Model{m})
		if !m.LiveMeta.InputPriceCache {
			t.Fatal("a large genuine price move must remain live")
		}
	})

	t.Run("non-openrouter provider is a no-op", func(t *testing.T) {
		snap := map[string]ModelSnapshot{"m": {inputPriceCache: new(0.50)}}
		m := liveModel("m", new(0.48))
		DampenOpenRouterPriceJitter("https://api.deepseek.com", snap, []*model.Model{m})
		if !m.LiveMeta.InputPriceCache {
			t.Fatal("damping must only apply to openrouter providers")
		}
	})

	t.Run("model absent from snapshot is untouched", func(t *testing.T) {
		m := liveModel("m", new(0.48))
		DampenOpenRouterPriceJitter(orURL, map[string]ModelSnapshot{}, []*model.Model{m})
		if !m.LiveMeta.InputPriceCache {
			t.Fatal("first-seen model has no prior value to compare; flag must stay")
		}
	})

	t.Run("filling a previously-unset price is kept", func(t *testing.T) {
		snap := map[string]ModelSnapshot{"m": {inputPriceCache: nil}}
		m := liveModel("m", new(0.48))
		DampenOpenRouterPriceJitter(orURL, snap, []*model.Model{m})
		if !m.LiveMeta.InputPriceCache {
			t.Fatal("filling an unset field is a genuine change, not jitter")
		}
	})

	t.Run("input and output price jitter are damped independently", func(t *testing.T) {
		// A model whose input price wiggled under tolerance but whose output price
		// genuinely jumped: only the input flag should be demoted.
		snap := map[string]ModelSnapshot{"m": {
			inputPrice:  new(2.00),
			outputPrice: new(6.00),
		}}
		m := &model.Model{
			ModelID:               "m",
			InputPricePerMillion:  new(1.96), // 2% drift -> jitter
			OutputPricePerMillion: new(3.00), // 50% drop -> real move
		}
		m.LiveMeta.InputPrice = true
		m.LiveMeta.OutputPrice = true

		DampenOpenRouterPriceJitter(orURL, snap, []*model.Model{m})

		if m.LiveMeta.InputPrice {
			t.Error("sub-tolerance input price wiggle should be demoted to fill-only")
		}
		if !m.LiveMeta.OutputPrice {
			t.Error("a real output price move must stay live")
		}
	})

	t.Run("both input and output sub-tolerance wiggles are damped", func(t *testing.T) {
		snap := map[string]ModelSnapshot{"m": {
			inputPrice:  new(2.00),
			outputPrice: new(6.00),
		}}
		m := &model.Model{
			ModelID:               "m",
			InputPricePerMillion:  new(2.02),
			OutputPricePerMillion: new(5.90),
		}
		m.LiveMeta.InputPrice = true
		m.LiveMeta.OutputPrice = true

		DampenOpenRouterPriceJitter(orURL, snap, []*model.Model{m})

		if m.LiveMeta.InputPrice || m.LiveMeta.OutputPrice {
			t.Errorf("both jittered prices should be demoted, got input=%v output=%v",
				m.LiveMeta.InputPrice, m.LiveMeta.OutputPrice)
		}
	})
}

// TestFloatPtrVal covers the price-logging sentinel: a real price dereferences,
// and a nil pointer reports -1 (no real price is negative, so it reads
// unambiguously as "unset" in the damping debug log).
func TestFloatPtrVal(t *testing.T) {
	if got := floatPtrVal(new(0.5)); got != 0.5 {
		t.Errorf("floatPtrVal(0.5) = %v, want 0.5", got)
	}
	if got := floatPtrVal(nil); got != -1 {
		t.Errorf("floatPtrVal(nil) = %v, want -1 sentinel", got)
	}
}

func TestWithinPriceTolerance(t *testing.T) {
	cases := []struct {
		name     string
		old, new *float64
		want     bool
	}{
		{"equal", new(1.0), new(1.0), true},
		{"within band", new(1.0), new(1.05), true},
		// denom is the larger value, so exactly 7% is 0.93 -> 1.0 (0.07/1.0).
		{"exactly on the 7% edge", new(0.93), new(1.0), true},
		{"just past the edge", new(0.92), new(1.0), false},
		{"beyond band", new(1.0), new(1.5), false},
		{"old nil", nil, new(1.0), false},
		{"new nil", new(1.0), nil, false},
		{"both nil", nil, nil, false},
		{"both zero", new(float64(0)), new(float64(0)), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withinPriceTolerance(c.old, c.new); got != c.want {
				t.Fatalf("withinPriceTolerance(%v, %v) = %v, want %v", c.old, c.new, got, c.want)
			}
		})
	}
}
