package model

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/db"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL, setupErr := db.SetupTestDB("model")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("model")

	testDB, err := db.New(ctx, dbURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	testPool = testDB.Pool()
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// insertTestProvider inserts a provider row and returns its ID.
func insertTestProvider(ctx context.Context, t *testing.T, name string) uuid.UUID {
	t.Helper()

	// Need the same columns that the app would write.
	// encrypted_key, key_nonce, key_salt are nullable after migration 026.
	id := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, true, now(), now())
	`, id, name, "https://test.example.com")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}

// insertTestModel inserts a model row for a given provider.
func insertTestModel(ctx context.Context, t *testing.T, providerID uuid.UUID, modelID string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, true, now(), now())
	`, id, providerID, modelID, modelID)
	if err != nil {
		t.Fatalf("insert model %q: %v", modelID, err)
	}
	return id
}

// countEnabledModels returns the number of enabled models for a provider.
func countEnabledModels(ctx context.Context, t *testing.T, providerID uuid.UUID) int {
	t.Helper()

	var count int
	err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM models WHERE provider_id = $1 AND enabled = true`,
		providerID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count enabled models: %v", err)
	}
	return count
}

// cleanupProvider deletes models and provider for a test provider ID.
func cleanupProvider(ctx context.Context, t *testing.T, providerID uuid.UUID) {
	t.Helper()

	_, _ = testPool.Exec(ctx, `DELETE FROM models WHERE provider_id = $1`, providerID)
	_, _ = testPool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)
}

// ---------------------------------------------------------------------------
// TestDisableMissingModels
// ---------------------------------------------------------------------------

func TestDisableMissingModels_EmptyList(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	// Empty list should return (0, nil) without executing any query.
	count, err := repo.DisableMissingModels(ctx, uuid.New(), nil)
	if err != nil {
		t.Fatalf("DisableMissingModels with nil list: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows affected, got %d", count)
	}

	count, err = repo.DisableMissingModels(ctx, uuid.New(), []string{})
	if err != nil {
		t.Fatalf("DisableMissingModels with empty list: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows affected, got %d", count)
	}
}

func TestDisableMissingModels_DisablesMissing(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-disable-missing-disables")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create 4 models for this provider.
	insertTestModel(ctx, t, providerID, "model-a")
	insertTestModel(ctx, t, providerID, "model-b")
	insertTestModel(ctx, t, providerID, "model-c")
	insertTestModel(ctx, t, providerID, "model-d")

	// Mark "model-b" and "model-d" as still existing. "model-a" and "model-c" are missing.
	existing := []string{"model-b", "model-d"}

	count, err := repo.DisableMissingModels(ctx, providerID, existing)
	if err != nil {
		t.Fatalf("DisableMissingModels: %v", err)
	}

	// 2 models (model-a, model-c) should be disabled.
	if count != 2 {
		t.Errorf("expected 2 rows affected, got %d", count)
	}

	// Verify: only model-b and model-d remain enabled.
	enabled := countEnabledModels(ctx, t, providerID)
	if enabled != 2 {
		t.Errorf("expected 2 enabled models, got %d", enabled)
	}
}

func TestDisableMissingModels_AllPresent(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-disable-missing-all-present")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create 3 models.
	insertTestModel(ctx, t, providerID, "alpha")
	insertTestModel(ctx, t, providerID, "beta")
	insertTestModel(ctx, t, providerID, "gamma")

	// All models are "still existing".
	existing := []string{"alpha", "beta", "gamma"}

	count, err := repo.DisableMissingModels(ctx, providerID, existing)
	if err != nil {
		t.Fatalf("DisableMissingModels: %v", err)
	}

	// 0 models should be disabled.
	if count != 0 {
		t.Errorf("expected 0 rows affected, got %d", count)
	}

	// All models should still be enabled.
	enabled := countEnabledModels(ctx, t, providerID)
	if enabled != 3 {
		t.Errorf("expected 3 enabled models, got %d", enabled)
	}
}

func TestDisableMissingModels_IgnoresOtherProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerA := insertTestProvider(ctx, t, "test-disable-missing-provider-a")
	providerB := insertTestProvider(ctx, t, "test-disable-missing-provider-b")
	t.Cleanup(func() {
		cleanupProvider(ctx, t, providerA)
		cleanupProvider(ctx, t, providerB)
	})

	// Provider A: model-a1, model-a2
	insertTestModel(ctx, t, providerA, "model-a1")
	insertTestModel(ctx, t, providerA, "model-a2")

	// Provider B: model-b1, model-b2
	insertTestModel(ctx, t, providerB, "model-b1")
	insertTestModel(ctx, t, providerB, "model-b2")

	// Disable missing on provider A, saying only model-a1 still exists.
	count, err := repo.DisableMissingModels(ctx, providerA, []string{"model-a1"})
	if err != nil {
		t.Fatalf("DisableMissingModels: %v", err)
	}

	// Only model-a2 should be disabled (1 row).
	if count != 1 {
		t.Errorf("expected 1 row affected, got %d", count)
	}

	// Provider B should be untouched — both models still enabled.
	enabledB := countEnabledModels(ctx, t, providerB)
	if enabledB != 2 {
		t.Errorf("expected 2 enabled models for provider B, got %d", enabledB)
	}
}

func TestDisableMissingModels_AlreadyDisabledUnaffected(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-disable-missing-already-disabled")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create 3 models, then manually disable one.
	insertTestModel(ctx, t, providerID, "active-1")
	insertTestModel(ctx, t, providerID, "active-2")
	insertTestModel(ctx, t, providerID, "already-off")

	_, err := testPool.Exec(ctx,
		`UPDATE models SET enabled = false WHERE provider_id = $1 AND model_id = $2`,
		providerID, "already-off",
	)
	if err != nil {
		t.Fatalf("pre-disable model: %v", err)
	}

	// Only "active-1" and "active-2" are in the existing list.
	// "already-off" is not in the list, but it's already disabled — should still
	// count as a matched row (the WHERE clause matches, but SET enabled = false
	// on an already-false row is still a no-op update). pgx RowsAffected
	// returns the number of rows matched, not actually changed.
	existing := []string{"active-1", "active-2"}

	count, err := repo.DisableMissingModels(ctx, providerID, existing)
	if err != nil {
		t.Fatalf("DisableMissingModels: %v", err)
	}

	// "already-off" matched the WHERE clause (provider_id matches AND model_id
	// is not in the list). pgx reports it as affected even though value didn't change.
	if count != 1 {
		t.Errorf("expected 1 row affected (already-off matched), got %d", count)
	}
}

func TestDisableMissingModels_NonExistentProvider(t *testing.T) {
	ctx := context.Background()

	repo := NewRepository(testPool)

	// A provider UUID that does not exist should result in 0 rows affected.
	count, err := repo.DisableMissingModels(ctx, uuid.New(), []string{"some-model"})
	if err != nil {
		t.Fatalf("DisableMissingModels with non-existent provider: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows affected, got %d", count)
	}
}

func TestDisableMissingModels_InvalidatesCache(t *testing.T) {
	// Verify that InvalidateModelCache is called by checking that cached entries
	// are cleared after the operation.
	InvalidateModelCache()

	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-disable-missing-cache")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "cache-model-a")
	insertTestModel(ctx, t, providerID, "cache-model-b")

	// First, populate the cache by fetching a model.
	m, err := repo.GetByProviderAndModelID(ctx, providerID, "cache-model-a")
	if err != nil {
		t.Fatalf("GetByProviderAndModelID: %v", err)
	}

	// Confirm it's cached.
	_, ok := GetCachedByUUID(m.ID)
	if !ok {
		t.Fatal("expected model to be cached after GetByProviderAndModelID")
	}

	// Now run DisableMissingModels — should invalidate the cache.
	_, err = repo.DisableMissingModels(ctx, providerID, []string{"cache-model-a"})
	if err != nil {
		t.Fatalf("DisableMissingModels: %v", err)
	}

	// The cached entry should be gone.
	_, ok = GetCachedByUUID(m.ID)
	if ok {
		t.Error("expected cache to be invalidated after DisableMissingModels")
	}
}
