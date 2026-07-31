package model

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
	var name, displayName string
	var displayNameCustomized bool
	err = testPool.QueryRow(ctx, `SELECT name, display_name, display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "overwrite-model").Scan(&name, &displayName, &displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if name != "Second Version" {
		t.Errorf("expected 'Second Version', got %q", name)
	}
	// display_name was NULL initially (not customized), so upsert should set it to EXCLUDED value
	if displayName != "Overwritten" {
		t.Errorf("display_name: expected 'Overwritten', got %q", displayName)
	}
	// display_name_customized should be false (fresh row, never customized)
	if displayNameCustomized {
		t.Error("display_name_customized should be false after overwrite (never customized)")
	}
}

func TestUpsert_PreservesCustomDisplayName(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-preserve-dn")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert model with name = display_name (provider default)
	model := &Model{
		ProviderID:       providerID,
		ModelID:          "preserve-dn-model",
		Name:             "very-long-provider-model-name-v2",
		DisplayName:      "very-long-provider-model-name-v2",
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err := repo.Upsert(ctx, model)
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// User customizes display_name via Update
	_, err = repo.Update(ctx, model.ID, UpdateModelRequest{
		DisplayName: new("short-name"),
	})
	if err != nil {
		t.Fatalf("update display_name failed: %v", err)
	}

	// Verify display_name_customized = true after Update
	var displayNameCustomized bool
	err = testPool.QueryRow(ctx,
		`SELECT display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "preserve-dn-model").Scan(&displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query display_name_customized: %v", err)
	}
	if !displayNameCustomized {
		t.Error("display_name_customized should be true after Update")
	}

	// Simulate re-discovery: upsert with same name but default display_name
	rediscovered := &Model{
		ProviderID:       providerID,
		ModelID:          "preserve-dn-model",
		Name:             "very-long-provider-model-name-v2",
		DisplayName:      "very-long-provider-model-name-v2", // provider's original
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err = repo.Upsert(ctx, rediscovered)
	if err != nil {
		t.Fatalf("re-discovery upsert failed: %v", err)
	}

	// Custom display_name should be preserved (differs from name)
	// display_name_customized should remain true
	var displayName string
	err = testPool.QueryRow(ctx,
		`SELECT display_name, display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "preserve-dn-model").Scan(&displayName, &displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query display_name: %v", err)
	}
	if displayName != "short-name" {
		t.Errorf("custom display_name should be preserved, got %q", displayName)
	}
	if !displayNameCustomized {
		t.Error("display_name_customized should remain true after re-discovery Upsert")
	}
}

func TestUpsert_UpdatesDisplayNameWhenNotCustom(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-update-dn")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Insert model with name = display_name (not customized)
	model := &Model{
		ProviderID:       providerID,
		ModelID:          "update-dn-model",
		Name:             "original-name",
		DisplayName:      "original-name", // same as name = not customized
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err := repo.Upsert(ctx, model)
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// Verify display_name_customized = false after initial upsert
	var displayNameCustomized bool
	err = testPool.QueryRow(ctx,
		`SELECT display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "update-dn-model").Scan(&displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query display_name_customized: %v", err)
	}
	if displayNameCustomized {
		t.Error("display_name_customized should be false after initial upsert")
	}

	// Re-discover with a new name (provider renamed the model)
	rediscovered := &Model{
		ProviderID:       providerID,
		ModelID:          "update-dn-model",
		Name:             "renamed-model",
		DisplayName:      "renamed-model", // matches new name
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err = repo.Upsert(ctx, rediscovered)
	if err != nil {
		t.Fatalf("re-discovery upsert failed: %v", err)
	}

	// display_name should follow name since it wasn't customized
	// display_name_customized should remain false
	var name, displayName string
	err = testPool.QueryRow(ctx,
		`SELECT name, display_name, display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "update-dn-model").Scan(&name, &displayName, &displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if name != "renamed-model" {
		t.Errorf("name: expected 'renamed-model', got %q", name)
	}
	if displayName != "renamed-model" {
		t.Errorf("display_name should follow name when not customized, got %q", displayName)
	}
	if displayNameCustomized {
		t.Error("display_name_customized should remain false after re-discovery Upsert")
	}
}

func TestUpsert_CatalogProviderDoesNotSetCustomizedFlag(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-upsert-catalog")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Simulate catalog provider (like OpenAI) that sets DisplayName != Name on first upsert
	model := &Model{
		ProviderID:       providerID,
		ModelID:          "catalog-model",
		Name:             "gpt-4-turbo-preview",
		DisplayName:      "GPT-4 Turbo Preview", // different from name, but still catalog default
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err := repo.Upsert(ctx, model)
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// Verify display_name_customized = false even though display_name != name
	var displayNameCustomized bool
	err = testPool.QueryRow(ctx,
		`SELECT display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "catalog-model").Scan(&displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query display_name_customized: %v", err)
	}
	if displayNameCustomized {
		t.Error("display_name_customized should be false for catalog provider default")
	}

	// Verify display_name was set to the catalog value
	var name, displayName string
	err = testPool.QueryRow(ctx,
		`SELECT name, display_name FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "catalog-model").Scan(&name, &displayName)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if displayName != "GPT-4 Turbo Preview" {
		t.Errorf("display_name: expected 'GPT-4 Turbo Preview', got %q", displayName)
	}

	// Simulate re-discovery with a new catalog name (provider updated their catalog)
	rediscovered := &Model{
		ProviderID:       providerID,
		ModelID:          "catalog-model",
		Name:             "gpt-4-turbo",
		DisplayName:      "GPT-4 Turbo", // new catalog name
		Enabled:          true,
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "",
		InputModalities:  "[]",
		OutputModalities: "[]",
	}
	err = repo.Upsert(ctx, rediscovered)
	if err != nil {
		t.Fatalf("re-discovery upsert failed: %v", err)
	}

	// display_name should be updated because display_name_customized was false
	err = testPool.QueryRow(ctx,
		`SELECT name, display_name, display_name_customized FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "catalog-model").Scan(&name, &displayName, &displayNameCustomized)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if name != "gpt-4-turbo" {
		t.Errorf("name: expected 'gpt-4-turbo', got %q", name)
	}
	if displayName != "GPT-4 Turbo" {
		t.Errorf("display_name should be updated from catalog, got %q", displayName)
	}
	if displayNameCustomized {
		t.Error("display_name_customized should remain false")
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
		return
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
		DisplayName:           new("Updated Display Name"),
		ContextLength:         new(8192),
		MaxOutputTokens:       new(1024),
		InputPricePerMillion:  new(0.5),
		OutputPricePerMillion: new(1.5),
		Enabled:               new(true),
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
		DisplayName: new("Only Display Name Updated"),
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
		Enabled: new(false),
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
		DisplayName: new("Should Not Be Set"),
	})

	// Should return error from Get
	if err == nil {
		t.Fatal("expected error for non-existent model")
		return
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
		return
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
// TestAutoRetireIfConfirmed
// ---------------------------------------------------------------------------

// TestAutoRetireIfConfirmed_AbandonedWriteIsNeverVisible is the reason this
// method exists, and it needs a real database because the property under test is
// cross-session visibility, which no mock can demonstrate.
//
// The proxy disables a model it believes the provider has retired, and the model
// can answer a request — disproving that — while the write is in flight. Writing
// and then undoing would leave the disabled row readable by everyone in between,
// and a concurrent custom-group revalidation that samples it auto-disables the
// group for having too few routable members. Re-enabling the model does not
// bring the group back, so the intermediate state has to not exist rather than
// be corrected afterwards.
func TestAutoRetireIfConfirmed_AbandonedWriteIsNeverVisible(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-confirm")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "confirm-abandon", "Confirm Abandon Test"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	readEnabled := func(t *testing.T) bool {
		t.Helper()
		// Bounded: this runs while a transaction holds the row, so a pool that
		// cannot hand out a second connection must fail the test rather than
		// hang the suite.
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var enabled bool
		if err := testPool.QueryRow(rctx, `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabled); err != nil {
			t.Fatalf("read from a separate session failed: %v", err)
		}
		return enabled
	}

	var sawDuringWrite bool
	committed, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool {
		// The row is written and locked at this point. Another session must
		// still see the old value.
		sawDuringWrite = readEnabled(t)
		return false
	})
	if err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}

	if committed {
		t.Error("an unconfirmed write must not report itself committed")
	}
	if !sawDuringWrite {
		t.Error("a staged disable leaked to another session before it was committed")
	}
	if !readEnabled(t) {
		t.Error("an abandoned write must leave the model enabled")
	}

	// The control: the same call commits when confirm holds, so the staging is
	// not swallowing legitimate writes.
	committed, err = repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true })
	if err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}
	if !committed {
		t.Error("a confirmed write must commit")
	}
	if readEnabled(t) {
		t.Error("a confirmed disable must be visible afterwards")
	}
}

// TestAutoRetireIfConfirmed_SurvivesReSighting pins the three states apart, and
// the whole reason auto_retired_at exists.
//
// enabled plus disabled_manually can express two kinds of disable, and there are
// three. An operator's must never be undone automatically. Discovery's SHOULD be
// undone by a re-sighting, because the model had vanished from the listing and
// its return is new evidence. A traffic retirement is neither: the model never
// left the listing — the provider was refusing it while still advertising it —
// so a sighting proves nothing, and reviving on one puts the model back into
// routing to fail, re-alert and churn failover groups on every scan.
//
// The discovery half is asserted alongside it, because "does not revive" is only
// correct if the mechanism it shares with discovery still revives what it should.
func TestAutoRetireIfConfirmed_SurvivesReSighting(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-autoretire-resighting")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	retiredID := insertTestModel(ctx, t, providerID, "traffic-retired-model")
	vanishedID := insertTestModel(ctx, t, providerID, "went-missing-model")

	readState := func(t *testing.T, id uuid.UUID) (enabled, manual bool, retired *time.Time) {
		t.Helper()
		if err := testPool.QueryRow(ctx,
			`SELECT enabled, disabled_manually, auto_retired_at FROM models WHERE id = $1`,
			id).Scan(&enabled, &manual, &retired); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		return enabled, manual, retired
	}

	// The proxy retires one model from traffic; discovery disables the other for
	// disappearing, which is what an unstamped automatic disable looks like.
	if committed, err := repo.AutoRetireIfConfirmed(ctx, retiredID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	} else if !committed {
		t.Fatal("the retirement should have committed")
	}
	if _, err := testPool.Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, vanishedID); err != nil {
		t.Fatalf("seed discovery disable: %v", err)
	}

	enabled, manual, retired := readState(t, retiredID)
	if enabled {
		t.Error("the retired model should be disabled")
	}
	if manual {
		t.Error("an automatic retirement must not be recorded as an operator's choice")
	}
	if retired == nil {
		t.Fatal("the retirement must be stamped, or nothing can tell it from discovery's")
	}

	// The provider lists both models again.
	for _, id := range []string{"traffic-retired-model", "went-missing-model"} {
		if err := repo.Upsert(ctx, newBareModel(providerID, id)); err != nil {
			t.Fatalf("Upsert %q failed: %v", id, err)
		}
	}

	if enabled, _, _ := readState(t, retiredID); enabled {
		t.Error("a re-sighting must not revive a model the provider refuses; it never left the listing")
	}
	if enabled, _, _ := readState(t, vanishedID); !enabled {
		t.Error("a model that came back after vanishing must be re-enabled, as it was before")
	}

	// An operator enabling by hand clears the retirement, which is how they tell
	// the gateway to trust the listing again.
	if _, err := repo.SetEnabled(ctx, retiredID, true); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	enabled, _, retired = readState(t, retiredID)
	if !enabled {
		t.Error("the operator's enable should stand")
	}
	if retired != nil {
		t.Error("an operator's enable must clear the retirement, not leave a stale stamp")
	}
}

// TestAutoRetireIfConfirmed_StandsDownOnceTheRowMovedOn covers the other half of
// the same window as RevertAutoRetire's condition.
//
// The retirement is decided on the request path and executed on a detached
// goroutine, so the row can change in between. Writing by id alone would
// overwrite an operator's own decision with a conclusion drawn from traffic that
// predates it — and the operator has no way to tell that happened, because the
// gateway's alert says exactly what it would have said anyway.
func TestAutoRetireIfConfirmed_StandsDownOnceTheRowMovedOn(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-autoretire-standdown")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	assertNotRetired := func(t *testing.T, id uuid.UUID) {
		t.Helper()
		var retired *time.Time
		if err := testPool.QueryRow(ctx,
			`SELECT auto_retired_at FROM models WHERE id = $1`, id).Scan(&retired); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if retired != nil {
			t.Error("a retirement that stood down must not stamp the row")
		}
	}

	t.Run("operator disabled it by hand first", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "operator-disabled")
		if _, err := repo.SetEnabled(ctx, id, false); err != nil {
			t.Fatalf("operator disable failed: %v", err)
		}

		confirmed := false
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool {
			confirmed = true
			return true
		})
		if err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		if committed {
			t.Error("a retirement must not overwrite an operator's disable")
		}
		if confirmed {
			t.Error("confirm must not run once the row has already moved on")
		}
		assertNotRetired(t, id)

		var manual bool
		if err := testPool.QueryRow(ctx,
			`SELECT disabled_manually FROM models WHERE id = $1`, id).Scan(&manual); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if !manual {
			t.Error("the operator's choice must survive intact")
		}
	})

	t.Run("already retired", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "already-retired")
		if _, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true }); err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true })
		if err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		if committed {
			t.Error("a model already retired must not be retired a second time")
		}
	})

	t.Run("deleted since the decision", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "deleted-model")
		if err := repo.DeleteByID(ctx, id); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true })
		if err != nil {
			t.Fatalf("a missing row is an outcome, not an error: %v", err)
		}
		if committed {
			t.Error("a deleted model cannot be retired")
		}
	})

	t.Run("an untouched model still retires", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "untouched-model")
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true })
		if err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		if !committed {
			t.Fatal("the condition must not swallow a legitimate retirement")
		}
	})
}

// TestRevertAutoRetire_DoesNotOverwriteAnOperatorDisable covers the window
// between a retirement committing and the gateway undoing it because the model
// answered.
//
// The undo runs after the disable has committed, so anything can have happened
// in between — and the case that matters is an operator disabling the model by
// hand right then. An unconditional re-enable would silently put their disabled
// model back into routing, replacing a deliberate decision with a stale
// automatic one.
func TestRevertAutoRetire_DoesNotOverwriteAnOperatorDisable(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-revert-autoretire")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := insertTestModel(ctx, t, providerID, "contested-model")

	if _, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}

	// The control first: with the row untouched, the undo restores the model.
	reverted, err := repo.RevertAutoRetire(ctx, modelID)
	if err != nil {
		t.Fatalf("RevertAutoRetire failed: %v", err)
	}
	if !reverted {
		t.Fatal("an untouched retirement must be revertible")
	}

	// Retire again, then have an operator disable it by hand before the undo.
	if _, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}
	if _, err := repo.SetEnabled(ctx, modelID, false); err != nil {
		t.Fatalf("operator disable failed: %v", err)
	}

	reverted, err = repo.RevertAutoRetire(ctx, modelID)
	if err != nil {
		t.Fatalf("RevertAutoRetire failed: %v", err)
	}
	if reverted {
		t.Error("the undo must stand down once someone else owns the row's state")
	}

	var enabled, manual bool
	if err := testPool.QueryRow(ctx,
		`SELECT enabled, disabled_manually FROM models WHERE id = $1`, modelID).Scan(&enabled, &manual); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if enabled {
		t.Error("an operator's disabled model must not be returned to routing")
	}
	if !manual {
		t.Error("the operator's choice must survive intact")
	}
}

// TestAutoRetireIfConfirmed_DeadContextReportsNotCommitted pins the failure
// direction, which matters more here than for an ordinary write.
//
// The caller acts on the returned bool: a true tells the proxy its disable
// landed, so it announces the retirement and resizes failover groups around it.
// If a write that never reached the database reported itself committed, the
// gateway would publish a model retirement that did not happen.
func TestAutoRetireIfConfirmed_DeadContextReportsNotCommitted(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-deadctx")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "confirm-deadctx", "Confirm Dead Context Test"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	dead, cancel := context.WithCancel(ctx)
	cancel()

	confirmed := false
	committed, err := repo.AutoRetireIfConfirmed(dead, modelID, func() bool {
		confirmed = true
		return true
	})
	if err == nil {
		t.Fatal("a cancelled context must surface an error")
	}
	if committed {
		t.Error("a write that never reached the database must not report itself committed")
	}
	if confirmed {
		t.Error("confirm must not run once the write has already failed")
	}

	var enabled bool
	if err := testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabled); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !enabled {
		t.Error("a failed write must leave the model untouched")
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

func TestDeleteByIDs_Success(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-delete-ids-success")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		_, err := testPool.Exec(ctx, `
			INSERT INTO models (id, provider_id, model_id, name, created_at)
			VALUES ($1, $2, $3, $4, now())
		`, id, providerID, "delete-me-"+id.String()[:8], "Delete Me")
		if err != nil {
			t.Fatalf("insert %d failed: %v", i, err)
		}
	}

	// Delete the first two; the third must survive.
	deleted, err := repo.DeleteByIDs(ctx, ids[:2])
	if err != nil {
		t.Fatalf("DeleteByIDs failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	var remaining int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM models WHERE id = ANY($1)`, ids).Scan(&remaining); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if remaining != 1 {
		t.Errorf("expected 1 remaining model, got %d", remaining)
	}
}

func TestDeleteByIDs_Empty(t *testing.T) {
	repo := NewRepository(testPool)
	deleted, err := repo.DeleteByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("DeleteByIDs(nil) should not error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted for empty input, got %d", deleted)
	}
}

func TestDeleteByIDs_Idempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-delete-ids-idempotent")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	realID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, created_at)
		VALUES ($1, $2, $3, $4, now())
	`, realID, providerID, "real-model", "Real Model")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// One real ID plus one that does not exist: only the real one counts.
	deleted, err := repo.DeleteByIDs(ctx, []uuid.UUID{realID, uuid.New()})
	if err != nil {
		t.Fatalf("DeleteByIDs failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
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
// TestRecordMissingModels edge cases
// ---------------------------------------------------------------------------

func TestRepository_RecordMissingModels_WithProviderAndModel(t *testing.T) {
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

	// Two consecutive scans missing modelID2 disable it: the first records a
	// pending miss, the second reaches MissingScanThreshold.
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"keep-this-model"})
	if err != nil {
		t.Fatalf("RecordMissingModels first scan failed: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 1 || pending[0].ModelID != "remove-this-model" {
		t.Errorf("expected pending ref for remove-this-model, got disabled=%v pending=%v", disabled, pending)
	}
	disabled, _, err = repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"keep-this-model"})
	if err != nil {
		t.Fatalf("RecordMissingModels second scan failed: %v", err)
	}
	if len(disabled) != 1 || disabled[0].ModelID != "remove-this-model" || disabled[0].ID != modelID2 {
		t.Errorf("expected single disabled ref for remove-this-model (%s), got %v", modelID2, disabled)
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
		t.Error("model2 should be disabled after two consecutive missing scans")
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

func TestRecordMissingModels_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := repo.RecordMissingModels(ctx, uuid.New(), "test-provider", []string{"some-model"})
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

// ---------------------------------------------------------------------------
// TestGetByIDs uncached/mixed paths
// ---------------------------------------------------------------------------

// TestGetByIDs_AfterCacheInvalidation verifies that GetByIDs fetches from the
// database after the cache is invalidated, and that WarmModelCache is called
// on the results (subsequent lookups hit the refreshed cache).
func TestGetByIDs_AfterCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	InvalidateModelCache()

	providerID := insertTestProvider(ctx, t, "test-getbyids-after-invalidate")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	id1 := insertTestModel(ctx, t, providerID, "post-invalidate-a")
	id2 := insertTestModel(ctx, t, providerID, "post-invalidate-b")

	// First call populates cache via WarmModelCache
	result, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result))
	}
	if result[id1].ModelID != "post-invalidate-a" {
		t.Errorf("model 1 ModelID: got %q, want %q", result[id1].ModelID, "post-invalidate-a")
	}
	if result[id2].ModelID != "post-invalidate-b" {
		t.Errorf("model 2 ModelID: got %q, want %q", result[id2].ModelID, "post-invalidate-b")
	}

	// Both should be cached now
	if !IsCachedByUUID(id1) {
		t.Error("id1 should be cached after GetByIDs")
	}
	if !IsCachedByUUID(id2) {
		t.Error("id2 should be cached after GetByIDs")
	}

	// Invalidate cache and fetch again — should go to DB
	InvalidateModelCache()

	if IsCachedByUUID(id1) {
		t.Error("id1 should not be cached after invalidation")
	}

	result2, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs after invalidation failed: %v", err)
	}
	if len(result2) != 2 {
		t.Fatalf("expected 2 models after invalidation, got %d", len(result2))
	}

	// WarmModelCache should have been called — both back in cache
	if !IsCachedByUUID(id1) {
		t.Error("id1 should be cached after second GetByIDs (WarmModelCache)")
	}
	if !IsCachedByUUID(id2) {
		t.Error("id2 should be cached after second GetByIDs (WarmModelCache)")
	}
}

// TestGetByIDs_MixedCacheAndDB verifies the uncached path where some requested
// IDs are already in cache and others require a database fetch.
func TestGetByIDs_MixedCacheAndDB(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	InvalidateModelCache()

	providerID := insertTestProvider(ctx, t, "test-getbyids-mixed")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	id1 := insertTestModel(ctx, t, providerID, "mixed-cached")
	id2 := insertTestModel(ctx, t, providerID, "mixed-uncached")
	id3 := insertTestModel(ctx, t, providerID, "mixed-also-uncached")

	// Fetch id1 alone to put it in the UUID cache
	_, err := repo.Get(ctx, id1)
	if err != nil {
		t.Fatalf("Get id1 failed: %v", err)
	}
	if !IsCachedByUUID(id1) {
		t.Fatal("id1 should be cached after Get")
	}

	// id2 and id3 should NOT be cached yet
	if IsCachedByUUID(id2) {
		t.Error("id2 should not be cached yet")
	}
	if IsCachedByUUID(id3) {
		t.Error("id3 should not be cached yet")
	}

	// Now fetch all three — id1 from cache, id2+id3 from DB
	result, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2, id3})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 models, got %d", len(result))
	}
	if result[id1].ModelID != "mixed-cached" {
		t.Errorf("cached model: ModelID got %q, want %q", result[id1].ModelID, "mixed-cached")
	}
	if result[id2].ModelID != "mixed-uncached" {
		t.Errorf("uncached model: ModelID got %q, want %q", result[id2].ModelID, "mixed-uncached")
	}
	if result[id3].ModelID != "mixed-also-uncached" {
		t.Errorf("uncached model: ModelID got %q, want %q", result[id3].ModelID, "mixed-also-uncached")
	}

	// WarmModelCache should have cached id2 and id3
	if !IsCachedByUUID(id2) {
		t.Error("id2 should be cached after GetByIDs mixed fetch")
	}
	if !IsCachedByUUID(id3) {
		t.Error("id3 should be cached after GetByIDs mixed fetch")
	}
}

// TestGetByIDs_PartiallyNonExistent verifies that when some IDs exist and
// others don't, the existing ones are returned and the non-existent ones
// are simply absent from the result map.
func TestGetByIDs_PartiallyNonExistent(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	InvalidateModelCache()

	providerID := insertTestProvider(ctx, t, "test-getbyids-partial")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	id1 := insertTestModel(ctx, t, providerID, "partial-exists")
	id2 := uuid.New() // does not exist

	result, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 model (existing), got %d", len(result))
	}
	if result[id1].ModelID != "partial-exists" {
		t.Errorf("ModelID: got %q, want %q", result[id1].ModelID, "partial-exists")
	}
	if _, ok := result[id2]; ok {
		t.Error("non-existent id2 should not be in result map")
	}
}
