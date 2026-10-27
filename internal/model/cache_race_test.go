package model

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/dbtest"
)

func TestGet_InvalidationDuringQueryIsNotReinstalled(t *testing.T) {
	InvalidateModelCache()
	ctx := context.Background()
	providerID := dbtest.InsertTestProvider(ctx, t, testPool, "race-"+uuid.NewString())
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	modelID := insertTestModel(ctx, t, providerID, "race-model")

	racing := NewRepository(dbtest.TracedPool(t, testDBURL, InvalidateModelCache))
	if _, err := racing.Get(ctx, modelID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := GetCachedByUUID(modelID); ok {
		t.Fatal("a fill whose SELECT overlapped an invalidation must not install")
	}
	if _, err := racing.GetByProviderAndModelID(ctx, providerID, "race-model"); err != nil {
		t.Fatalf("GetByProviderAndModelID: %v", err)
	}
	if _, ok := GetCachedByCompositeKey(providerID, "race-model"); ok {
		t.Fatal("composite fill whose SELECT overlapped an invalidation must not install")
	}

	// Control: the same read without a racing write installs.
	if _, err := NewRepository(testPool).Get(ctx, modelID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := GetCachedByUUID(modelID); !ok {
		t.Fatal("an unraced read-through must install")
	}
}
