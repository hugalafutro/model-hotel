package model

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

// ---------------------------------------------------------------------------
// TestUpsert
// ---------------------------------------------------------------------------

func TestUpsert_InsertNewModel(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-insert")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	model := &Model{
		ID:               modelID,
		ProviderID:       providerID,
		ModelID:          "test-model-new",
		Name:             "Test Model New",
		Enabled:          true,
		DisplayName:      "Test Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}

	err := repo.Upsert(ctx, model)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify the ID matches what was passed in (Upsert doesn't generate new IDs)
	if model.ID != modelID {
		t.Errorf("Model ID should match the ID passed to Upsert: got %v, want %v", model.ID, modelID)
	}

	// Verify in database
	var name string
	err = testPool.QueryRow(ctx, `SELECT name FROM models WHERE id = $1`, model.ID).Scan(&name)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if name != "Test Model New" {
		t.Errorf("expected name 'Test Model New', got %q", name)
	}
}

func TestUpsert_UpdateExistingModel(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-update")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert initial model using basic columns only
	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "test-model-update", "Original Name")
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// Update the model with same provider_id and model_id
	model := &Model{
		ProviderID:       providerID,
		ModelID:          "test-model-update",
		Name:             "Updated Name",
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}

	err = repo.Upsert(ctx, model)
	if err != nil {
		t.Fatalf("update upsert failed: %v", err)
	}

	// Verify ID is same (not recreated)
	if model.ID != modelID {
		t.Errorf("expected same ID after update, got %v, want %v", model.ID, modelID)
	}
}

func TestUpsert_OverwriteExisting(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-overwrite")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert first version
	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "overwrite-model", "First Version")
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Insert second version with same model_id (overwrites)
	model2 := &Model{
		ProviderID:       providerID,
		ModelID:          "overwrite-model",
		Name:             "Second Version",
		Enabled:          false,
		DisplayName:      "Overwritten",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err = repo.Upsert(ctx, model2)
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Verify second version overwrote first
	var name string
	err = testPool.QueryRow(ctx, `SELECT name FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "overwrite-model").Scan(&name)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if name != "Second Version" {
		t.Errorf("expected 'Second Version', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// TestList
// ---------------------------------------------------------------------------

func TestList_EmptyDatabase(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	// No cleanup needed - list should return empty with no providers/models
	models, err := repo.List(ctx, nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestList_OneProvider(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-list-one-provider")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create multiple models for this provider
	models := []*Model{
		{ProviderID: providerID, ModelID: "model-1", Name: "Model 1", Enabled: true},
		{ProviderID: providerID, ModelID: "model-2", Name: "Model 2", Enabled: true},
		{ProviderID: providerID, ModelID: "model-3", Name: "Model 3", Enabled: true},
	}

	for _, m := range models {
		_, err := testPool.Exec(ctx, `
			INSERT INTO models (provider_id, model_id, name, enabled, created_at)
			VALUES ($1, $2, $3, $4, now())
		`, providerID, m.ModelID, m.Name, m.Enabled)
		if err != nil {
			t.Fatalf("insert model %s failed: %v", m.ModelID, err)
		}
	}

	// List without filter should return all
	allModels, err := repo.List(ctx, nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(allModels) < 3 {
		t.Errorf("expected at least 3 models, got %d", len(allModels))
	}
}

func TestList_ByProviderID(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerA := insertTestProvider(ctx, t, "test-list-by-provider-a")
	providerB := insertTestProvider(ctx, t, "test-list-by-provider-b")
	t.Cleanup(func() {
		cleanupProvider(ctx, t, providerA)
		cleanupProvider(ctx, t, providerB)
	})

	// Create models for both providers
	modelIDA := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelIDA, providerA, "provider-a-model", "Provider A Model")
	if err != nil {
		t.Fatalf("insert model A failed: %v", err)
	}

	modelIDA2 := uuid.New()
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelIDA2, providerA, "provider-a-model-2", "Provider A Model 2")
	if err != nil {
		t.Fatalf("insert model A2 failed: %v", err)
	}

	modelIDB := uuid.New()
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelIDB, providerB, "provider-b-model", "Provider B Model")
	if err != nil {
		t.Fatalf("insert model B failed: %v", err)
	}

	// List for provider A only
	modelsA, err := repo.List(ctx, &providerA)
	if err != nil {
		t.Fatalf("List for provider A failed: %v", err)
	}
	if len(modelsA) != 2 {
		t.Errorf("expected 2 models for provider A, got %d", len(modelsA))
	}
	for _, m := range modelsA {
		if m.ProviderID != providerA {
			t.Errorf("model %s has wrong provider_id", m.ModelID)
		}
	}

	// List for provider B only
	modelsB, err := repo.List(ctx, &providerB)
	if err != nil {
		t.Fatalf("List for provider B failed: %v", err)
	}
	if len(modelsB) != 1 {
		t.Errorf("expected 1 model for provider B, got %d", len(modelsB))
	}
}

// ---------------------------------------------------------------------------
// TestListEnabled
// ---------------------------------------------------------------------------

func TestListEnabled_EmptyDatabase(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	models, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestListEnabled_OnlyEnabledModels(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-list-enabled-only")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create models with different enabled states
	enabledID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, enabledID, providerID, "enabled-model", "Enabled Model")
	if err != nil {
		t.Fatalf("insert enabled model failed: %v", err)
	}

	disabledID := uuid.New()
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, false, now())
	`, disabledID, providerID, "disabled-model", "Disabled Model")
	if err != nil {
		t.Fatalf("insert disabled model failed: %v", err)
	}

	models, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled failed: %v", err)
	}
	if len(models) != 1 {
		t.Errorf("expected 1 enabled model, got %d: %v", len(models), models)
	}
	if models[0].ModelID != "enabled-model" {
		t.Errorf("expected 'enabled-model', got %q", models[0].ModelID)
	}
}

// ---------------------------------------------------------------------------
// TestGet
// ---------------------------------------------------------------------------

func TestGet_NotFound(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	id := uuid.New()
	model, err := repo.Get(ctx, id)
	if err == nil {
		t.Fatal("expected error for non-existent model")
	}
	if model != nil {
		t.Errorf("expected nil model, got %v", model.ID)
	}
}

func TestGet_Found(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-get-found")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Use insertTestModel which uses only basic fields (no last_seen_at in original schema)
	modelID := insertTestModel(ctx, t, providerID, "get-found-model")

	got, err := repo.Get(ctx, modelID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != modelID {
		t.Errorf("ID mismatch: got %v, want %v", got.ID, modelID)
	}
	if got.ModelID != "get-found-model" {
		t.Errorf("ModelID mismatch: %q", got.ModelID)
	}
}

func TestGet_CacheHit(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-get-cache-hit")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := insertTestModel(ctx, t, providerID, "cache-hit-model")

	_, _ = repo.Get(ctx, modelID)

	got, ok := GetCachedByUUID(modelID)
	if !ok {
		t.Fatal("model should be in cache after Get")
	}
	if got.ID != modelID {
		t.Errorf("cached ID mismatch: %v", got.ID)
	}
}

// ---------------------------------------------------------------------------
// TestGetByIDs
// ---------------------------------------------------------------------------

func TestGetByIDs_EmptyIDs(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	result, err := repo.GetByIDs(ctx, []uuid.UUID{})
	if err != nil {
		t.Fatalf("GetByIDs with empty slice failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestGetByIDs_NotFound(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	id1 := uuid.New()
	id2 := uuid.New()

	result, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for non-existent IDs, got %d", len(result))
	}
}

func TestGetByIDs_Found(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getbyids-found")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	id1 := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, id1, providerID, "byids-1", "ByID 1")
	if err != nil {
		t.Fatalf("insert model 1 failed: %v", err)
	}

	id2 := uuid.New()
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, id2, providerID, "byids-2", "ByID 2")
	if err != nil {
		t.Fatalf("insert model 2 failed: %v", err)
	}

	result, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
	if _, ok := result[id1]; !ok {
		t.Error("id1 should be in result")
	}
	if _, ok := result[id2]; !ok {
		t.Error("id2 should be in result")
	}
}

func TestGetByIDs_CacheHit(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getbyids-cache")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := insertTestModel(ctx, t, providerID, "cache-model")

	_, _ = repo.GetByIDs(ctx, []uuid.UUID{modelID})

	_, ok := GetCachedByUUID(modelID)
	if !ok {
		t.Error("model should be in cache after GetByIDs")
	}
}

// ---------------------------------------------------------------------------
// TestGetByModelID
// ---------------------------------------------------------------------------

func TestGetByModelID_NotFound(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	models, err := repo.GetByModelID(ctx, "non-existent-model")
	if err != nil {
		t.Fatalf("GetByModelID with non-existent model failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestGetByModelID_Found(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerA := insertTestProvider(ctx, t, "test-getbymodelid-a")
	providerB := insertTestProvider(ctx, t, "test-getbymodelid-b")
	t.Cleanup(func() {
		cleanupProvider(ctx, t, providerA)
		cleanupProvider(ctx, t, providerB)
	})

	modelID := "shared-model-id"

	idA := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, idA, providerA, modelID, "From Provider A")
	if err != nil {
		t.Fatalf("insert model A failed: %v", err)
	}

	idB := uuid.New()
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, idB, providerB, modelID, "From Provider B")
	if err != nil {
		t.Fatalf("insert model B failed: %v", err)
	}

	models, err := repo.GetByModelID(ctx, modelID)
	if err != nil {
		t.Fatalf("GetByModelID failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models with same model_id, got %d", len(models))
	}

	providers := make(map[uuid.UUID]bool)
	for _, m := range models {
		providers[m.ProviderID] = true
	}
	if len(providers) != 2 {
		t.Errorf("expected 2 different providers, got %d", len(providers))
	}
}

func TestGetByModelID_OnlyEnabled(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getbymodelid-enabled")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := "enabled-test"
	idEnabled := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, idEnabled, providerID, modelID, "Enabled")
	if err != nil {
		t.Fatalf("insert enabled model failed: %v", err)
	}

	idDisabled := uuid.New()
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, false, now())
	`, idDisabled, providerID, "disabled-test", "Disabled")
	if err != nil {
		t.Fatalf("insert disabled model failed: %v", err)
	}

	models, err := repo.GetByModelID(ctx, modelID)
	if err != nil {
		t.Fatalf("GetByModelID failed: %v", err)
	}
	if len(models) != 1 {
		t.Errorf("expected 1 enabled model, got %d", len(models))
	}
	if models[0].ModelID != modelID {
		t.Errorf("expected %q, got %q", modelID, models[0].ModelID)
	}
}

func TestGetByModelID_CacheHit(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getbymodelid-cache")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := "cache-test-model"
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, uuid.New(), providerID, modelID, "Cache Test")
	if err != nil {
		t.Fatalf("insert model failed: %v", err)
	}

	models1, err := repo.GetByModelID(ctx, modelID)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	models2, err := repo.GetByModelID(ctx, modelID)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if len(models1) != len(models2) {
		t.Errorf("cache returned different count: %d vs %d", len(models1), len(models2))
	}
}

// ---------------------------------------------------------------------------
// TestGetByProviderAndModelID

// ---------------------------------------------------------------------------
// TestUpdate
// ---------------------------------------------------------------------------

func TestUpdate_AllFields(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-update-all")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert initial model
	modelID := insertTestModel(ctx, t, providerID, "update-all-model")

	// Update all fields
	updated, err := repo.Update(ctx, modelID, UpdateModelRequest{
		DisplayName:           strPtr("Updated Display Name"),
		ContextLength:         intPtr(8192),
		MaxOutputTokens:       intPtr(1024),
		InputPricePerMillion:  float64Ptr(0.5),
		OutputPricePerMillion: float64Ptr(1.5),
		Enabled:               boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify all fields were updated
	if updated.DisplayName != "Updated Display Name" {
		t.Errorf("DisplayName: expected 'Updated Display Name', got %q", updated.DisplayName)
	}
	if updated.ContextLength == nil || *updated.ContextLength != 8192 {
		t.Errorf("ContextLength: expected 8192, got %v", updated.ContextLength)
	}
	if updated.MaxOutputTokens == nil || *updated.MaxOutputTokens != 1024 {
		t.Errorf("MaxOutputTokens: expected 1024, got %v", updated.MaxOutputTokens)
	}
	if updated.InputPricePerMillion == nil || *updated.InputPricePerMillion != 0.5 {
		t.Errorf("InputPricePerMillion: expected 0.5, got %v", updated.InputPricePerMillion)
	}
	if updated.OutputPricePerMillion == nil || *updated.OutputPricePerMillion != 1.5 {
		t.Errorf("OutputPricePerMillion: expected 1.5, got %v", updated.OutputPricePerMillion)
	}
	if !updated.Enabled {
		t.Error("Enabled: expected true, got false")
	}
}

func TestUpdate_NoFields(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-update-none")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert initial model
	modelID := insertTestModel(ctx, t, providerID, "update-none-model")

	// Get original model
	original, err := repo.Get(ctx, modelID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Update with empty request (should return same model)
	updated, err := repo.Update(ctx, modelID, UpdateModelRequest{})
	if err != nil {
		t.Fatalf("Update with no fields failed: %v", err)
	}

	// Verify returned model matches original
	if updated.ID != original.ID {
		t.Errorf("ID mismatch: expected %v, got %v", original.ID, updated.ID)
	}
	if updated.ModelID != original.ModelID {
		t.Errorf("ModelID mismatch: expected %q, got %q", original.ModelID, updated.ModelID)
	}
}

func TestUpdate_SingleField_DisplayName(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-update-single")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert initial model
	modelID := insertTestModel(ctx, t, providerID, "update-single-model")

	// Update only DisplayName
	updated, err := repo.Update(ctx, modelID, UpdateModelRequest{
		DisplayName: strPtr("Only Display Name Updated"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify only DisplayName changed
	if updated.DisplayName != "Only Display Name Updated" {
		t.Errorf("DisplayName: expected 'Only Display Name Updated', got %q", updated.DisplayName)
	}
	if updated.ModelID != "update-single-model" {
		t.Errorf("ModelID should not change: expected 'update-single-model', got %q", updated.ModelID)
	}
}

func TestUpdate_EnabledFalse(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-update-disabled")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert initial model with enabled=true
	modelID := insertTestModel(ctx, t, providerID, "update-disabled-model")

	// Update to set Enabled to false
	updated, err := repo.Update(ctx, modelID, UpdateModelRequest{
		Enabled: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify Enabled is false and disabled_manually is true
	if updated.Enabled {
		t.Error("Enabled should be false")
	}

	// Check disabled_manually in database
	var disabledManually bool
	err = testPool.QueryRow(ctx, `SELECT disabled_manually FROM models WHERE id = $1`, modelID).Scan(&disabledManually)
	if err != nil {
		t.Fatalf("failed to query disabled_manually: %v", err)
	}
	if !disabledManually {
		t.Error("disabled_manually should be true when Enabled is set to false")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	// Try to update non-existent model
	nonExistentID := uuid.New()
	updated, err := repo.Update(ctx, nonExistentID, UpdateModelRequest{
		DisplayName: strPtr("Should Not Be Set"),
	})

	// Should return error from Get
	if err == nil {
		t.Fatal("expected error for non-existent model")
	}
	if updated != nil {
		t.Errorf("expected nil model, got %v", updated)
	}
}

// ---------------------------------------------------------------------------

func TestGetByProviderAndModelID_NotFound(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	providerID := uuid.New()

	model, err := repo.GetByProviderAndModelID(ctx, providerID, "non-existent")
	if err == nil {
		t.Fatal("expected error for non-existent model")
	}
	if model != nil {
		t.Errorf("expected nil, got %v", model.ID)
	}
}

func TestGetByProviderAndModelID_Found(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getby-provider-and-model")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := "specific-model"
	modelIDVal := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelIDVal, providerID, modelID, "Specific Model")
	if err != nil {
		t.Fatalf("insert model failed: %v", err)
	}

	model, err := repo.GetByProviderAndModelID(ctx, providerID, modelID)
	if err != nil {
		t.Fatalf("GetByProviderAndModelID failed: %v", err)
	}

	if model.ModelID != modelID {
		t.Errorf("ModelID mismatch: %q", model.ModelID)
	}
	if model.Name != "Specific Model" {
		t.Errorf("Name mismatch: %q", model.Name)
	}
}

func TestGetByProviderAndModelID_CacheHit(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getby-cached")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := "cached-composite"
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, uuid.New(), providerID, modelID, "Cached")
	if err != nil {
		t.Fatalf("insert model failed: %v", err)
	}

	_, _ = repo.GetByProviderAndModelID(ctx, providerID, modelID)

	found, ok := GetCachedByCompositeKey(providerID, modelID)
	if !ok {
		t.Fatal("composite cache should have entry")
	}
	if found.ModelID != modelID {
		t.Errorf("cached ModelID mismatch: %q", found.ModelID)
	}
}

// ---------------------------------------------------------------------------
// TestSetEnabled
// ---------------------------------------------------------------------------

func TestSetEnabled_Enable(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-enable")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, false, now())
	`, modelID, providerID, "disable-enable", "Disable Enable Test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	updated, err := repo.SetEnabled(ctx, modelID, true)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	if !updated.Enabled {
		t.Error("model should be enabled")
	}
}

func TestSetEnabled_Disable(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-disable")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "enable-disable", "Enable Disable Test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	updated, err := repo.SetEnabled(ctx, modelID, false)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	if updated.Enabled {
		t.Error("model should be disabled")
	}
}

// ---------------------------------------------------------------------------
// TestDeleteByID
// ---------------------------------------------------------------------------

func TestDeleteByID_Success(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-delete-success")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, created_at)
		VALUES ($1, $2, $3, $4, now())
	`, modelID, providerID, "delete-me", "Delete Me")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	var count int
	err = testPool.QueryRow(ctx, `SELECT count(*) FROM models WHERE id = $1`, modelID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("model should exist before delete")
	}

	err = repo.DeleteByID(ctx, modelID)
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	err = testPool.QueryRow(ctx, `SELECT count(*) FROM models WHERE id = $1`, modelID).Scan(&count)
	if err != nil || count != 0 {
		t.Errorf("model should not exist after delete")
	}
}

func TestDeleteByID_CacheInvalidated(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-delete-cache")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "cache-delete-test", "Cache Delete Test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	_, _ = repo.Get(ctx, modelID)

	_, ok := GetCachedByUUID(modelID)
	if !ok {
		t.Fatal("model should be in cache")
	}

	err = repo.DeleteByID(ctx, modelID)
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	_, ok = GetCachedByUUID(modelID)
	if ok {
		t.Error("cache should be invalidated after delete")
	}
}

// ---------------------------------------------------------------------------
// TestProviderNameResolution
// ---------------------------------------------------------------------------

func TestGetIncludesProviderName(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-provider-name-resolution")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "provider-name-test", "Provider Name Test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	model, err := repo.Get(ctx, modelID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if model.ProviderName == "" {
		t.Error("ProviderName should be populated from JOIN")
	}
}

func TestList_WithProviderFilter(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-list-filter")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	_ = insertTestModel(ctx, t, providerID, "filtered-model-a")
	_ = insertTestModel(ctx, t, providerID, "filtered-model-b")

	models, err := repo.List(ctx, &providerID)
	if err != nil {
		t.Fatalf("List with filter failed: %v", err)
	}
	if len(models) < 2 {
		t.Errorf("expected at least 2 models for provider, got %d", len(models))
	}
}

func TestGetByIDs(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-getbyids")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	id1 := insertTestModel(ctx, t, providerID, "getbyids-model-a")
	id2 := insertTestModel(ctx, t, providerID, "getbyids-model-b")

	models, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}

	// Empty list should return empty
	empty, err := repo.GetByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("GetByIDs with nil failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 models for nil input, got %d", len(empty))
	}
}

// ---------------------------------------------------------------------------
// TestDeleteByID edge cases
// ---------------------------------------------------------------------------

func TestRepository_DeleteByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	// Delete non-existent model - should not error (idempotent)
	nonExistentID := uuid.New()
	err := repo.DeleteByID(ctx, nonExistentID)
	if err != nil {
		t.Errorf("DeleteByID on non-existent model should not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestSetEnabled edge cases
// ---------------------------------------------------------------------------

func TestRepository_SetEnabled_DisableThenVerify(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-verify")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create a model
	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "setenabled-verify", "SetEnabled Verify")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Disable it
	updated, err := repo.SetEnabled(ctx, modelID, false)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	// Verify enabled=false
	if updated.Enabled {
		t.Error("model should be disabled after SetEnabled(false)")
	}

	// Verify in database
	var enabled bool
	err = testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabled)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if enabled {
		t.Error("database should show enabled=false")
	}
}

// ---------------------------------------------------------------------------
// TestUpsert edge cases
// ---------------------------------------------------------------------------

func TestRepository_Upsert_NewModel(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-new")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	model := &Model{
		ID:               modelID,
		ProviderID:       providerID,
		ModelID:          "upsert-new-model",
		Name:             "New Upsert Model",
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}

	err := repo.Upsert(ctx, model)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify the model was created
	var name string
	err = testPool.QueryRow(ctx, `SELECT name FROM models WHERE id = $1`, modelID).Scan(&name)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if name != "New Upsert Model" {
		t.Errorf("expected name 'New Upsert Model', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// TestGetByIDs edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetByIDs_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	// Get by non-existent IDs - should return empty result
	id1 := uuid.New()
	id2 := uuid.New()

	result, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for non-existent IDs, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// TestGetByModelID edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetByModelID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	// Get by non-existent model ID - should return nil/empty
	models, err := repo.GetByModelID(ctx, "non-existent-model-id")
	if err != nil {
		t.Fatalf("GetByModelID failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models for non-existent model ID, got %d", len(models))
	}
}

// ---------------------------------------------------------------------------
// TestDisableMissingModels edge cases
// ---------------------------------------------------------------------------

func TestRepository_DisableMissingModels_WithProviderAndModel(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-disable-missing-crud")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create two models
	modelID1 := uuid.New()
	modelID2 := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID1, providerID, "keep-this-model", "Keep This Model")
	if err != nil {
		t.Fatalf("insert model1 failed: %v", err)
	}
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID2, providerID, "remove-this-model", "Remove This Model")
	if err != nil {
		t.Fatalf("insert model2 failed: %v", err)
	}

	// Call DisableMissingModels with only modelID1 in the list - should disable modelID2
	count, err := repo.DisableMissingModels(ctx, providerID, []string{"keep-this-model"})
	if err != nil {
		t.Fatalf("DisableMissingModels failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row affected, got %d", count)
	}

	// Verify modelID1 is still enabled
	var enabled1 bool
	err = testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID1).Scan(&enabled1)
	if err != nil {
		t.Fatalf("failed to query model1: %v", err)
	}
	if !enabled1 {
		t.Error("model1 should still be enabled")
	}

	// Verify modelID2 is now disabled
	var enabled2 bool
	err = testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID2).Scan(&enabled2)
	if err != nil {
		t.Fatalf("failed to query model2: %v", err)
	}
	if enabled2 {
		t.Error("model2 should be disabled after DisableMissingModels")
	}
}

// ---------------------------------------------------------------------------
// Cancelled context error path tests
// ---------------------------------------------------------------------------

func TestUpsert_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	providerID := insertTestProvider(context.Background(), t, "test-upsert-cancel")
	t.Cleanup(func() { cleanupProvider(context.Background(), t, providerID) })

	m := &Model{
		ID:         uuid.New(),
		ProviderID: providerID,
		ModelID:    "test-model-upsert-cancel",
		Name:       "Test Model Upsert Cancel",
		Enabled:    true,
	}
	err := repo.Upsert(ctx, m)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestList_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.List(ctx, nil)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestListEnabled_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.ListEnabled(ctx)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestGetByIDs_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	InvalidateModelCache()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use a random UUID that won't be in cache, forcing a DB query
	_, err := repo.GetByIDs(ctx, []uuid.UUID{uuid.New()})
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestGetByIDs_CacheHitOnly(t *testing.T) {
	repo := NewRepository(testPool)
	ctx := context.Background()

	providerID := insertTestProvider(ctx, t, "test-getbyids-cache")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert a model so it gets cached
	m := &Model{
		ID:               uuid.New(),
		ProviderID:       providerID,
		ModelID:          "test-model-getbyids-cache",
		Name:             "Test Model GetByIDs Cache",
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
		OwnedBy:          "",
	}
	err := repo.Upsert(ctx, m)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Now GetByIDs with the same ID should hit cache and return early (line 211-213)
	result, err := repo.GetByIDs(ctx, []uuid.UUID{m.ID})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 model, got %d", len(result))
	}
	if result[m.ID] == nil {
		t.Error("expected model in result")
	}
}

func TestGetByModelID_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	InvalidateModelCache()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use a model ID that won't be in cache, forcing a DB query
	_, err := repo.GetByModelID(ctx, "nonexistent-model-id")
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestGetByProviderAndModelID_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	InvalidateModelCache()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use IDs that won't be in cache, forcing a DB query
	_, err := repo.GetByProviderAndModelID(ctx, uuid.New(), "nonexistent-model-id")
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestDisableMissingModels_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.DisableMissingModels(ctx, uuid.New(), []string{"some-model"})
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestSetEnabled_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.SetEnabled(ctx, uuid.New(), false)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestDeleteByID_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.DeleteByID(ctx, uuid.New())
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestUpdate_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	displayName := "updated"
	_, err := repo.Update(ctx, uuid.New(), UpdateModelRequest{
		DisplayName: &displayName,
	})
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
