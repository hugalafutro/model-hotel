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
// TestRecordMissingModels
// ---------------------------------------------------------------------------

func TestRecordMissingModels_EmptyList(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	// Empty list should return (nil, nil, nil) without executing any query: an
	// empty listing is far more likely a broken scan than a real full removal.
	disabled, pending, err := repo.RecordMissingModels(ctx, uuid.New(), "test-provider", nil)
	if err != nil {
		t.Fatalf("RecordMissingModels nil list: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 0 {
		t.Errorf("expected no refs, got disabled=%v pending=%v", disabled, pending)
	}

	disabled, pending, err = repo.RecordMissingModels(ctx, uuid.New(), "test-provider", []string{})
	if err != nil {
		t.Fatalf("RecordMissingModels empty list: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 0 {
		t.Errorf("expected no refs, got disabled=%v pending=%v", disabled, pending)
	}
}

func TestRecordMissingModels_FirstMissIsPendingOnly(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-pending")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "model-a")
	insertTestModel(ctx, t, providerID, "model-b")
	insertTestModel(ctx, t, providerID, "model-c")
	insertTestModel(ctx, t, providerID, "model-d")

	// First scan missing model-a and model-c: nothing may be disabled yet.
	existing := []string{"model-b", "model-d"}
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
	if err != nil {
		t.Fatalf("RecordMissingModels: %v", err)
	}
	if len(disabled) != 0 {
		t.Fatalf("expected 0 disabled refs on first miss, got %v", disabled)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending refs, got %v", pending)
	}
	pendingIDs := map[string]bool{}
	for _, ref := range pending {
		if ref.ID == uuid.Nil {
			t.Errorf("expected non-nil UUID for %s", ref.ModelID)
		}
		pendingIDs[ref.ModelID] = true
	}
	if !pendingIDs["model-a"] || !pendingIDs["model-c"] {
		t.Errorf("expected model-a and model-c pending, got %v", pendingIDs)
	}

	// All 4 models must still be enabled after a single miss.
	if enabled := countEnabledModels(ctx, t, providerID); enabled != 4 {
		t.Errorf("expected 4 enabled models after first miss, got %d", enabled)
	}
}

func TestRecordMissingModels_SecondConsecutiveMissDisables(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-disables")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "model-a")
	insertTestModel(ctx, t, providerID, "model-b")

	existing := []string{"model-b"}
	if _, _, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing); err != nil {
		t.Fatalf("RecordMissingModels first scan: %v", err)
	}
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
	if err != nil {
		t.Fatalf("RecordMissingModels second scan: %v", err)
	}
	if len(disabled) != 1 || disabled[0].ModelID != "model-a" {
		t.Fatalf("expected model-a disabled on second consecutive miss, got %v", disabled)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending refs, got %v", pending)
	}
	if enabled := countEnabledModels(ctx, t, providerID); enabled != 1 {
		t.Errorf("expected 1 enabled model, got %d", enabled)
	}

	// The disabled row's streak must be reset so a later reappearance does not
	// sit one flaky scan away from another disable.
	var streak int
	if err := testPool.QueryRow(ctx, `SELECT missing_scans FROM models WHERE id = $1`, disabled[0].ID).Scan(&streak); err != nil {
		t.Fatalf("query streak: %v", err)
	}
	if streak != 0 {
		t.Errorf("expected missing_scans reset to 0 after disable, got %d", streak)
	}
}

func TestRecordMissingModels_SightingResetsStreak(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-reset")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "flappy-model")
	insertTestModel(ctx, t, providerID, "stable-model")

	// Scan 1: flappy-model missing (streak 1).
	if _, _, err := repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"stable-model"}); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	// Scan 2: flappy-model listed again — streak must reset.
	if _, _, err := repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"stable-model", "flappy-model"}); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	// Scan 3: flappy-model missing again — still only pending, not disabled.
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"stable-model"})
	if err != nil {
		t.Fatalf("scan 3: %v", err)
	}
	if len(disabled) != 0 {
		t.Fatalf("expected no disable after streak reset, got %v", disabled)
	}
	if len(pending) != 1 || pending[0].ModelID != "flappy-model" {
		t.Fatalf("expected flappy-model pending, got %v", pending)
	}
	if enabled := countEnabledModels(ctx, t, providerID); enabled != 2 {
		t.Errorf("expected 2 enabled models, got %d", enabled)
	}
}

func TestRecordMissingModels_AllPresent(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-all-present")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "model-a")
	insertTestModel(ctx, t, providerID, "model-b")
	insertTestModel(ctx, t, providerID, "model-c")

	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"model-a", "model-b", "model-c"})
	if err != nil {
		t.Fatalf("RecordMissingModels: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 0 {
		t.Errorf("expected no refs, got disabled=%v pending=%v", disabled, pending)
	}
	if enabled := countEnabledModels(ctx, t, providerID); enabled != 3 {
		t.Errorf("expected 3 enabled models, got %d", enabled)
	}
}

func TestRecordMissingModels_IgnoresOtherProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerA := insertTestProvider(ctx, t, "test-record-missing-provider-a")
	providerB := insertTestProvider(ctx, t, "test-record-missing-provider-b")
	t.Cleanup(func() {
		cleanupProvider(ctx, t, providerA)
		cleanupProvider(ctx, t, providerB)
	})

	insertTestModel(ctx, t, providerA, "model-a1")
	insertTestModel(ctx, t, providerA, "model-a2")
	insertTestModel(ctx, t, providerB, "model-b1")
	insertTestModel(ctx, t, providerB, "model-b2")

	// Two consecutive misses of model-a2 on provider A disable it.
	if _, _, err := repo.RecordMissingModels(ctx, providerA, "test-provider-a", []string{"model-a1"}); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	disabled, _, err := repo.RecordMissingModels(ctx, providerA, "test-provider-a", []string{"model-a1"})
	if err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if len(disabled) != 1 || disabled[0].ModelID != "model-a2" {
		t.Errorf("expected single ref for model-a2, got %v", disabled)
	}

	// Provider B untouched — both models still enabled with zero streak.
	if enabledB := countEnabledModels(ctx, t, providerB); enabledB != 2 {
		t.Errorf("expected 2 enabled models for provider B, got %d", enabledB)
	}
}

func TestRecordMissingModels_AlreadyDisabledUnaffected(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-already-disabled")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "active-1")
	insertTestModel(ctx, t, providerID, "active-2")
	insertTestModel(ctx, t, providerID, "already-off")
	if _, err := testPool.Exec(ctx, `UPDATE models SET enabled = false WHERE provider_id = $1 AND model_id = $2`, providerID, "already-off"); err != nil {
		t.Fatalf("pre-disable failed: %v", err)
	}

	// Two scans listing both active models: already-off is never returned as
	// disabled or pending (it is not enabled, so it accrues no misses).
	existing := []string{"active-1", "active-2"}
	for i := range 2 {
		disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
		if err != nil {
			t.Fatalf("scan %d: %v", i+1, err)
		}
		if len(disabled) != 0 || len(pending) != 0 {
			t.Errorf("scan %d: expected no refs, got disabled=%v pending=%v", i+1, disabled, pending)
		}
	}
}

func TestRecordMissingModels_DisabledModelNotReturnedAgain(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-third-scan")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	insertTestModel(ctx, t, providerID, "kept-model")
	insertTestModel(ctx, t, providerID, "gone-model")

	existing := []string{"kept-model"}
	if _, _, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	disabled, _, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(disabled) != 1 || disabled[0].ModelID != "gone-model" {
		t.Fatalf("expected single ref for gone-model on second scan, got %v", disabled)
	}

	// Third scan with the same listing: gone-model is already disabled and must
	// not be returned again.
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 0 {
		t.Errorf("expected no refs on third scan, got disabled=%v pending=%v", disabled, pending)
	}
}

func TestRecordMissingModels_NonExistentProvider(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	disabled, pending, err := repo.RecordMissingModels(ctx, uuid.New(), "test-provider", []string{"some-model"})
	if err != nil {
		t.Fatalf("RecordMissingModels with non-existent provider: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 0 {
		t.Errorf("expected no refs, got disabled=%v pending=%v", disabled, pending)
	}
}

func TestRecordMissingModels_InvalidatesCache(t *testing.T) {
	// Verify InvalidateModelCache is called by checking cached entries are
	// cleared by the operation.
	InvalidateModelCache()

	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-cache")
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

	// Now run RecordMissingModels — should invalidate the cache.
	_, _, err = repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"cache-model-a"})
	if err != nil {
		t.Fatalf("RecordMissingModels: %v", err)
	}

	// The cached entry should be gone.
	_, ok = GetCachedByUUID(m.ID)
	if ok {
		t.Error("expected cache to be invalidated after RecordMissingModels")
	}
}
