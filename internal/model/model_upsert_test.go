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

	// A non-live rescan value is fill-only: it must NOT overwrite the stored
	// value, so a catalog/models.dev value can't flip a provider value across
	// restarts (the source-oscillation fix).
	nonLive := newBareModel(providerID, "preserve-me")
	nonLive.InputPricePerMillion = new(0.5) // LiveMeta zero value => fill-only
	if err := repo.Upsert(ctx, nonLive); err != nil {
		t.Fatalf("non-live update upsert: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "preserve-me")
	if err != nil {
		t.Fatalf("get after non-live update: %v", err)
	}
	assertFloatPtr(t, "input_price (non-live kept)", got.InputPricePerMillion, 1.4)

	// A live rescan value overwrites: a genuine provider-reported change
	// propagates to the stored (and served) metadata.
	live := newBareModel(providerID, "preserve-me")
	live.InputPricePerMillion = new(0.5)
	live.LiveMeta.InputPrice = true
	if err := repo.Upsert(ctx, live); err != nil {
		t.Fatalf("live update upsert: %v", err)
	}
	got, err = repo.GetByProviderAndModelID(ctx, providerID, "preserve-me")
	if err != nil {
		t.Fatalf("get after live update: %v", err)
	}
	assertFloatPtr(t, "input_price (live overwrote)", got.InputPricePerMillion, 0.5)
	// Untouched fields are still preserved.
	assertIntPtr(t, "context_length", got.ContextLength, 200000)
	assertFloatPtr(t, "output_price", got.OutputPricePerMillion, 4.4)
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
