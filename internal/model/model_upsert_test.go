package model

import (
	"context"
	"math"
	"testing"

	"github.com/google/uuid"
)

// newBareModel builds a Model with valid-JSON defaults for the JSON columns
// (capabilities/params/modalities), which Upsert writes verbatim.
func newBareModel(providerID uuid.UUID, modelID string) *Model {
	return &Model{
		ProviderID:       providerID,
		ModelID:          modelID,
		Name:             modelID,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
}

// TestUpsert_PreservesMetadataOnNullRescan verifies that a rescan which omits
// pricing/context (e.g. a flaky live probe) does not blank the stored values,
// while a rescan with new non-nil values still overwrites them.
func TestUpsert_PreservesMetadataOnNullRescan(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-preserve")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	base := newBareModel(providerID, "preserve-me")
	base.ContextLength = new(200000)
	base.MaxOutputTokens = new(131072)
	base.InputPricePerMillion = new(1.4)
	base.InputPricePerMillionCacheHit = new(0.26)
	base.OutputPricePerMillion = new(4.4)
	if err := repo.Upsert(ctx, base); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Rescan that fetched no pricing/context at all (all nil).
	if err := repo.Upsert(ctx, newBareModel(providerID, "preserve-me")); err != nil {
		t.Fatalf("nil rescan upsert: %v", err)
	}

	got, err := repo.GetByProviderAndModelID(ctx, providerID, "preserve-me")
	if err != nil {
		t.Fatalf("get after nil rescan: %v", err)
	}
	assertIntPtr(t, "context_length", got.ContextLength, 200000)
	assertIntPtr(t, "max_output_tokens", got.MaxOutputTokens, 131072)
	assertFloatPtr(t, "input_price", got.InputPricePerMillion, 1.4)
	assertFloatPtr(t, "input_price_cache", got.InputPricePerMillionCacheHit, 0.26)
	assertFloatPtr(t, "output_price", got.OutputPricePerMillion, 4.4)

	// A non-live CONTEXT value stays fill-only: it must NOT overwrite the
	// stored value, so a catalog/models.dev value can't flip a provider value
	// across restarts (the source-oscillation fix). A non-live PRICE, however,
	// follows the source on an unpinned row: catalog and models.dev corrections
	// (and vendor price changes) propagate instead of freezing forever.
	nonLive := newBareModel(providerID, "preserve-me")
	nonLive.ContextLength = new(999)        // LiveMeta zero value => fill-only, kept
	nonLive.InputPricePerMillion = new(0.5) // unpinned price => follows source
	if err := repo.Upsert(ctx, nonLive); err != nil {
		t.Fatalf("non-live update upsert: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "preserve-me")
	if err != nil {
		t.Fatalf("get after non-live update: %v", err)
	}
	assertIntPtr(t, "context_length (non-live kept)", got.ContextLength, 200000)
	assertFloatPtr(t, "input_price (followed source)", got.InputPricePerMillion, 0.5)

	// A live CONTEXT value overwrites: a genuine provider-reported change
	// propagates to the stored (and served) metadata.
	live := newBareModel(providerID, "preserve-me")
	live.ContextLength = new(400000)
	live.LiveMeta.ContextLength = true
	if err := repo.Upsert(ctx, live); err != nil {
		t.Fatalf("live update upsert: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "preserve-me")
	if err != nil {
		t.Fatalf("get after live update: %v", err)
	}
	assertIntPtr(t, "context_length (live overwrote)", got.ContextLength, 400000)
	// Untouched fields are still preserved.
	assertFloatPtr(t, "input_price", got.InputPricePerMillion, 0.5)
	assertFloatPtr(t, "output_price", got.OutputPricePerMillion, 4.4)
}

// TestUpsert_PricePinBlocksAllSources verifies price_customized: a pinned
// row's prices survive both a models.dev-style non-live rescan and a live
// provider-reported price, while an unpinned gap on the pinned row still
// fills. Unpinning (via Update with price_customized=false) hands the prices
// back to the source on the next upsert.
func TestUpsert_PricePinBlocksAllSources(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-price-pin")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	base := newBareModel(providerID, "pinned-price")
	base.InputPricePerMillion = new(9.9)
	if err := repo.Upsert(ctx, base); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Operator edits the price: the edit pins the row.
	if _, err := repo.Update(ctx, base.ID, UpdateModelRequest{InputPricePerMillion: new(2.5)}); err != nil {
		t.Fatalf("price edit: %v", err)
	}

	rescan := newBareModel(providerID, "pinned-price")
	rescan.InputPricePerMillion = new(1.4)  // scan price (any source) must not touch a pin
	rescan.OutputPricePerMillion = new(4.4) // fills the pinned row's gap
	if err := repo.Upsert(ctx, rescan); err != nil {
		t.Fatalf("rescan upsert: %v", err)
	}
	got, err := repo.GetByProviderAndModelID(ctx, providerID, "pinned-price")
	if err != nil {
		t.Fatalf("get after rescan: %v", err)
	}
	if !got.PriceCustomized {
		t.Fatal("price_customized = false after a price edit, want true")
	}
	assertFloatPtr(t, "input_price (pin kept)", got.InputPricePerMillion, 2.5)
	assertFloatPtr(t, "output_price (gap filled on pinned row)", got.OutputPricePerMillion, 4.4)

	// Unpin: prices null out, and the next scan re-derives them from source.
	if _, err := repo.Update(ctx, base.ID, UpdateModelRequest{PriceCustomized: new(false)}); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "pinned-price")
	if err != nil {
		t.Fatalf("get after unpin: %v", err)
	}
	if got.PriceCustomized {
		t.Fatal("price_customized = true after unpin, want false")
	}
	if got.InputPricePerMillion != nil || got.OutputPricePerMillion != nil || got.InputPricePerMillionCacheHit != nil {
		t.Fatalf("prices not nulled on unpin: in=%v cache=%v out=%v",
			got.InputPricePerMillion, got.InputPricePerMillionCacheHit, got.OutputPricePerMillion)
	}
	// Fresh object: Upsert scans the RETURNING row back into its argument, so
	// the first rescan's model now carries the pinned 2.5, not the source 1.4.
	rescan2 := newBareModel(providerID, "pinned-price")
	rescan2.InputPricePerMillion = new(1.4)
	if err := repo.Upsert(ctx, rescan2); err != nil {
		t.Fatalf("post-unpin rescan upsert: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "pinned-price")
	if err != nil {
		t.Fatalf("get after post-unpin rescan: %v", err)
	}
	assertFloatPtr(t, "input_price (re-derived after unpin)", got.InputPricePerMillion, 1.4)
}

// TestUpsert_SightingClearsPin covers the end of the disagreement the pin
// records: once the listing names the model again there is nothing left to
// overrule, so the row goes back to automatic management. A model the proxy
// retired from traffic still stays disabled through that sighting, because the
// pin never spoke to the enabled CASE.
func TestUpsert_SightingClearsPin(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-pin-clear")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	pinnedID := insertTestModel(ctx, t, providerID, "pinned-model")
	pinModel(ctx, t, pinnedID)

	if err := repo.Upsert(ctx, newBareModel(providerID, "pinned-model")); err != nil {
		t.Fatalf("upsert pinned model: %v", err)
	}
	if pinnedAt := readPin(ctx, t, pinnedID); pinnedAt != nil {
		t.Errorf("manually_enabled_at = %v after a sighting, want nil", *pinnedAt)
	}

	// A pinned model the proxy retired from traffic: the sighting clears the
	// pin but must not revive the model.
	retiredID := insertTestModel(ctx, t, providerID, "retired-model")
	if _, err := testPool.Exec(ctx,
		`UPDATE models SET enabled = false, auto_retired_at = now(), manually_enabled_at = now() WHERE id = $1`,
		retiredID); err != nil {
		t.Fatalf("seed retired pinned model: %v", err)
	}

	retired := newBareModel(providerID, "retired-model")
	retired.Enabled = true
	if err := repo.Upsert(ctx, retired); err != nil {
		t.Fatalf("upsert retired model: %v", err)
	}
	if retired.Enabled {
		t.Error("enabled = true after a sighting of a traffic-retired model, want false")
	}
	if pinnedAt := readPin(ctx, t, retiredID); pinnedAt != nil {
		t.Errorf("manually_enabled_at = %v after a sighting of a retired model, want nil", *pinnedAt)
	}
}

func assertIntPtr(t *testing.T, field string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: got nil, want %d", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s: got %d, want %d", field, *got, want)
	}
}

func assertFloatPtr(t *testing.T, field string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: got nil, want %g", field, want)
		return
	}
	// Price columns are stored at float32 precision, so compare with tolerance.
	if math.Abs(*got-want) > 1e-4 {
		t.Errorf("%s: got %g, want %g", field, *got, want)
	}
}
