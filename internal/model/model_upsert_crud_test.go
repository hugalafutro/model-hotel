package model

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Upsert: what it preserves (a customised display name, a pinned price) and
// what a re-discovery is allowed to overwrite.

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
