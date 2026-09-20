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

// TestUpsert_PriceSourcesFollowThePrices pins that a source travels with its
// price through the upsert's merge: a rescan that keeps a stored price keeps
// its source, one that replaces a price replaces the source, and a pinned
// row keeps the operator's sources for the prices it keeps.
func TestUpsert_PriceSourcesFollowThePrices(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	providerID := insertTestProvider(ctx, t, "test-upsert-price-sources")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	base := newBareModel(providerID, "sourced")
	base.InputPricePerMillion = new(1.0)
	base.OutputPricePerMillion = new(2.0)
	base.PriceSources = PriceSources{Input: PriceSourceProvider, Output: PriceSourceProvider}
	if err := repo.Upsert(ctx, base); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	get := func() *Model {
		t.Helper()
		got, err := repo.Get(ctx, base.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return got
	}
	got := get()
	if got.PriceSources != (PriceSources{Input: PriceSourceProvider, Output: PriceSourceProvider}) {
		t.Fatalf("stored sources = %+v, want provider/provider", got.PriceSources)
	}

	// A rescan carrying only a models.dev cache-hit price and no input or
	// output price: the two stored prices keep their provider source, the
	// new one arrives with its own.
	rescan := newBareModel(providerID, "sourced")
	rescan.InputPricePerMillionCacheHit = new(0.1)
	rescan.PriceSources = PriceSources{CacheHit: PriceSourceModelsDev}
	if err := repo.Upsert(ctx, rescan); err != nil {
		t.Fatalf("rescan upsert: %v", err)
	}
	got = get()
	want := PriceSources{Input: PriceSourceProvider, CacheHit: PriceSourceModelsDev, Output: PriceSourceProvider}
	if got.PriceSources != want {
		t.Errorf("after fill-only rescan sources = %+v, want %+v", got.PriceSources, want)
	}

	// A rescan that reprices the input from the catalog replaces the source
	// with the price.
	reprice := newBareModel(providerID, "sourced")
	reprice.InputPricePerMillion = new(5.0)
	reprice.PriceSources = PriceSources{Input: PriceSourceCatalog}
	if err := repo.Upsert(ctx, reprice); err != nil {
		t.Fatalf("reprice upsert: %v", err)
	}
	got = get()
	want.Input = PriceSourceCatalog
	if got.PriceSources != want || *got.InputPricePerMillion != 5.0 {
		t.Errorf("after reprice sources = %+v price=%v, want %+v and 5", got.PriceSources, *got.InputPricePerMillion, want)
	}

	// An operator edit of the output price pins the row: the edited price is
	// theirs and stays so through a rescan that would have repriced it.
	if _, err := repo.Update(ctx, base.ID, UpdateModelRequest{OutputPricePerMillion: new(9.0)}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got = get()
	want.Output = PriceSourceManual
	if got.PriceSources != want {
		t.Errorf("after manual edit sources = %+v, want %+v (only the edited price becomes manual)", got.PriceSources, want)
	}
	pinnedScan := newBareModel(providerID, "sourced")
	pinnedScan.OutputPricePerMillion = new(3.0)
	pinnedScan.PriceSources = PriceSources{Output: PriceSourceProvider}
	if err := repo.Upsert(ctx, pinnedScan); err != nil {
		t.Fatalf("pinned rescan upsert: %v", err)
	}
	got = get()
	if got.PriceSources != want || *got.OutputPricePerMillion != 9.0 {
		t.Errorf("pinned row sources = %+v price=%v, want %+v and 9", got.PriceSources, *got.OutputPricePerMillion, want)
	}

	// Unpinning drops the prices and their sources together.
	if _, err := repo.Update(ctx, base.ID, UpdateModelRequest{PriceCustomized: new(false)}); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	got = get()
	if got.PriceSources != (PriceSources{}) || got.InputPricePerMillion != nil {
		t.Errorf("after unpin sources = %+v price=%v, want none", got.PriceSources, got.InputPricePerMillion)
	}
}

func TestStampPriceSources_OnlyPricedAndUnsourced(t *testing.T) {
	m := &Model{InputPricePerMillion: new(1.0), OutputPricePerMillion: new(2.0), PriceSources: PriceSources{Output: PriceSourceCatalog}}
	m.StampPriceSources(PriceSourceProvider)
	want := PriceSources{Input: PriceSourceProvider, Output: PriceSourceCatalog}
	if m.PriceSources != want {
		t.Errorf("sources = %+v, want %+v: the unset cache-hit price gets none, the catalog output keeps its source", m.PriceSources, want)
	}

	all := &Model{InputPricePerMillion: new(1.0), InputPricePerMillionCacheHit: new(0.1), OutputPricePerMillion: new(2.0)}
	all.StampPriceSources(PriceSourceModelsDev)
	if want := (PriceSources{Input: PriceSourceModelsDev, CacheHit: PriceSourceModelsDev, Output: PriceSourceModelsDev}); all.PriceSources != want {
		t.Errorf("sources = %+v, want every priced field stamped %+v", all.PriceSources, want)
	}
}

// Editing the cache-hit price alone marks that price manual and nothing else.
func TestUpdate_CacheHitEditStampsOnlyItself(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	providerID := insertTestProvider(ctx, t, "test-update-cache-hit-source")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	base := newBareModel(providerID, "cache-edit")
	base.InputPricePerMillion = new(1.0)
	base.PriceSources = PriceSources{Input: PriceSourceProvider}
	if err := repo.Upsert(ctx, base); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Update(ctx, base.ID, UpdateModelRequest{InputPricePerMillionCacheHit: new(0.2)})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if want := (PriceSources{Input: PriceSourceProvider, CacheHit: PriceSourceManual}); got.PriceSources != want || !got.PriceCustomized {
		t.Errorf("sources = %+v pinned=%v, want %+v and pinned", got.PriceSources, got.PriceCustomized, want)
	}
}

// An operator's edit to context_length or max_output_tokens survives the next
// scan on a provider that marks those limits live (OpenRouter, Ollama, ...),
// which used to let the provider's value win with no pin. The pin follows the
// edit like price_customized; an explicit unpin nulls the limits so the next
// scan refills them from source.
func TestUpsert_LimitsPinBlocksLiveMeta(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-limits-pin")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	base := newBareModel(providerID, "pinned-limits")
	base.ContextLength = new(200000)
	base.MaxOutputTokens = new(8192)
	if err := repo.Upsert(ctx, base); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	if _, err := repo.Update(ctx, base.ID, UpdateModelRequest{ContextLength: new(32000)}); err != nil {
		t.Fatalf("limit edit: %v", err)
	}

	// A live-marked scan (the provider's own listing) must not touch the pin.
	rescan := newBareModel(providerID, "pinned-limits")
	rescan.ContextLength = new(200000)
	rescan.MaxOutputTokens = new(16384)
	rescan.MarkLiveMetaFromCurrent()
	if err := repo.Upsert(ctx, rescan); err != nil {
		t.Fatalf("rescan upsert: %v", err)
	}
	got, err := repo.GetByProviderAndModelID(ctx, providerID, "pinned-limits")
	if err != nil {
		t.Fatalf("get after rescan: %v", err)
	}
	if !got.LimitsCustomized {
		t.Fatal("limits_customized = false after a limit edit, want true")
	}
	if got.ContextLength == nil || *got.ContextLength != 32000 {
		t.Errorf("context_length = %v after a live rescan, want the operator's 32000", got.ContextLength)
	}
	if got.MaxOutputTokens == nil || *got.MaxOutputTokens != 8192 {
		t.Errorf("max_output_tokens = %v after a live rescan, want the pinned 8192", got.MaxOutputTokens)
	}

	// Unpin: the limits null out and the next scan refills them from source.
	if _, err := repo.Update(ctx, base.ID, UpdateModelRequest{LimitsCustomized: new(false)}); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "pinned-limits")
	if err != nil {
		t.Fatalf("get after unpin: %v", err)
	}
	if got.LimitsCustomized || got.ContextLength != nil || got.MaxOutputTokens != nil {
		t.Fatalf("after unpin: customized=%v context=%v output=%v, want cleared", got.LimitsCustomized, got.ContextLength, got.MaxOutputTokens)
	}
	rescan2 := newBareModel(providerID, "pinned-limits")
	rescan2.ContextLength = new(200000)
	rescan2.MarkLiveMetaFromCurrent()
	if err := repo.Upsert(ctx, rescan2); err != nil {
		t.Fatalf("post-unpin rescan upsert: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "pinned-limits")
	if err != nil {
		t.Fatalf("get after post-unpin rescan: %v", err)
	}
	if got.ContextLength == nil || *got.ContextLength != 200000 {
		t.Errorf("context_length = %v after unpin + rescan, want the source's 200000", got.ContextLength)
	}
}
