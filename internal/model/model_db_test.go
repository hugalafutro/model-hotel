package model

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

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
		os.Exit(1)
	}
	testPool = testDB.Pool()
	defer testDB.Close()

	os.Exit(m.Run())
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

// readPin returns the model's manually_enabled_at stamp, which no struct field
// carries, so tests read it straight from the row.
func readPin(ctx context.Context, t *testing.T, id uuid.UUID) *time.Time {
	t.Helper()

	var pinnedAt *time.Time
	if err := testPool.QueryRow(ctx, `SELECT manually_enabled_at FROM models WHERE id = $1`, id).Scan(&pinnedAt); err != nil {
		t.Fatalf("read manually_enabled_at: %v", err)
	}
	return pinnedAt
}

// pinModel stamps the operator's manual-enable pin directly, seeding the state
// a hand enable leaves behind.
func pinModel(ctx context.Context, t *testing.T, id uuid.UUID) {
	t.Helper()

	if _, err := testPool.Exec(ctx, `UPDATE models SET manually_enabled_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
}

// readMissingScans returns the model's consecutive-miss streak.
func readMissingScans(ctx context.Context, t *testing.T, id uuid.UUID) int {
	t.Helper()

	var streak int
	if err := testPool.QueryRow(ctx, `SELECT missing_scans FROM models WHERE id = $1`, id).Scan(&streak); err != nil {
		t.Fatalf("read missing_scans: %v", err)
	}
	return streak
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

// TestRecordMissingModels_PinnedModelNeverDisabled covers the case the pin
// exists for: the operator enabled a model the listing keeps omitting. Misses
// still accrue, but the row is never disabled and never surfaces as a claim.
func TestRecordMissingModels_PinnedModelNeverDisabled(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-pinned")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	pinnedID := insertTestModel(ctx, t, providerID, "pinned-model")
	insertTestModel(ctx, t, providerID, "stable-model")
	pinModel(ctx, t, pinnedID)

	existing := []string{"stable-model"}
	// Two scans, one past MissingScanThreshold, with pinned-model absent.
	for scan, wantStreak := range []int{1, 2} {
		disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
		if err != nil {
			t.Fatalf("scan %d: %v", scan+1, err)
		}
		if len(disabled) != 0 || len(pending) != 0 {
			t.Errorf("scan %d: pinned model must appear in neither slice, got disabled=%v pending=%v", scan+1, disabled, pending)
		}
		if got := readMissingScans(ctx, t, pinnedID); got != wantStreak {
			t.Errorf("scan %d: missing_scans = %d, want %d: a pinned model keeps counting misses", scan+1, got, wantStreak)
		}
	}

	if enabled := countEnabledModels(ctx, t, providerID); enabled != 2 {
		t.Errorf("expected both models still enabled, got %d", enabled)
	}
	if readPin(ctx, t, pinnedID) == nil {
		t.Error("manually_enabled_at = nil, want set: a miss does not clear the pin")
	}
}

// TestRecordMissingModels_UnpinnedRowStillDisables is the control for the pin
// exemption: a pinned and an unpinned model missing in the SAME statement must
// get different verdicts, so the exemption is decided per row rather than
// switching auto-disable off wholesale.
func TestRecordMissingModels_UnpinnedRowStillDisables(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-record-missing-unpinned-control")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	pinnedID := insertTestModel(ctx, t, providerID, "pinned-model")
	unpinnedID := insertTestModel(ctx, t, providerID, "unpinned-model")
	insertTestModel(ctx, t, providerID, "stable-model")
	pinModel(ctx, t, pinnedID)

	existing := []string{"stable-model"}
	if _, _, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", existing)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(disabled) != 1 || disabled[0].ModelID != "unpinned-model" {
		t.Fatalf("expected only unpinned-model disabled on the second miss, got %v", disabled)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending refs, got %v", pending)
	}
	if streak := readMissingScans(ctx, t, unpinnedID); streak != 0 {
		t.Errorf("unpinned missing_scans = %d, want 0 after disable", streak)
	}
	if enabled := countEnabledModels(ctx, t, providerID); enabled != 2 {
		t.Errorf("expected 2 enabled models (pinned + stable), got %d", enabled)
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

// TestSetEnabled_EnableStampsPin verifies the operator path arms and disarms
// the pin: enabling by hand records that the operator vouched for the model,
// disabling withdraws it.
func TestSetEnabled_EnableStampsPin(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-pin")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	modelID := insertTestModel(ctx, t, providerID, "hand-enabled")

	if _, err := repo.SetEnabled(ctx, modelID, true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	if readPin(ctx, t, modelID) == nil {
		t.Error("manually_enabled_at = nil after SetEnabled(true), want a stamp")
	}

	if _, err := repo.SetEnabled(ctx, modelID, false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	if pinnedAt := readPin(ctx, t, modelID); pinnedAt != nil {
		t.Errorf("manually_enabled_at = %v after SetEnabled(false), want nil", *pinnedAt)
	}
}

// TestUpdate_EnableStampsPin verifies the partial-update path matches
// SetEnabled, and that an update which does not touch enabled leaves an
// existing pin alone — renaming a model is not a statement about the listing.
func TestUpdate_EnableStampsPin(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-update-pin")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	modelID := insertTestModel(ctx, t, providerID, "hand-updated")

	enabled := true
	if _, err := repo.Update(ctx, modelID, UpdateModelRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("Update(enabled=true): %v", err)
	}
	pinnedAt := readPin(ctx, t, modelID)
	if pinnedAt == nil {
		t.Fatal("manually_enabled_at = nil after Update(enabled=true), want a stamp")
	}

	displayName := "Hand Updated"
	if _, err := repo.Update(ctx, modelID, UpdateModelRequest{DisplayName: &displayName}); err != nil {
		t.Fatalf("Update(display_name): %v", err)
	}
	if got := readPin(ctx, t, modelID); got == nil || !got.Equal(*pinnedAt) {
		t.Errorf("manually_enabled_at = %v after an update that does not touch enabled, want %v unchanged", got, *pinnedAt)
	}

	disabled := false
	if _, err := repo.Update(ctx, modelID, UpdateModelRequest{Enabled: &disabled}); err != nil {
		t.Fatalf("Update(enabled=false): %v", err)
	}
	if got := readPin(ctx, t, modelID); got != nil {
		t.Errorf("manually_enabled_at = %v after Update(enabled=false), want nil", *got)
	}
}

// TestUpsertClearsDiscoveryDismissal drives the sequence the dismissal column
// exists for: an operator dismisses a gone model, the provider lists it again,
// and it later vanishes again. The second disappearance must read as a fresh
// claim, so the sighting in between has to clear the dismissal stamp.
func TestUpsertClearsDiscoveryDismissal(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-dismiss-clear")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	insertTestModel(ctx, t, providerID, "vanishing-model")

	// Operator dismissed it while it was gone.
	if _, err := testPool.Exec(ctx,
		`UPDATE models SET enabled = false, discovery_dismissed_at = now()
		  WHERE provider_id = $1 AND model_id = $2`, providerID, "vanishing-model"); err != nil {
		t.Fatalf("seed dismissal: %v", err)
	}

	// The provider lists it again.
	m := newBareModel(providerID, "vanishing-model")
	m.Enabled = true
	if err := repo.Upsert(ctx, m); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var dismissedAt *time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT discovery_dismissed_at FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "vanishing-model").Scan(&dismissedAt); err != nil {
		t.Fatalf("read dismissal: %v", err)
	}
	if dismissedAt != nil {
		t.Errorf("discovery_dismissed_at = %v, want nil: a sighting must clear the stamp so a later disappearance is a fresh claim", *dismissedAt)
	}
}

// ---------------------------------------------------------------------------
// TestPinnedModelIDs
// ---------------------------------------------------------------------------

// TestPinnedModelIDs verifies the set returned scopes to one provider and
// includes only rows that actually carry the manual-enable pin.
func TestPinnedModelIDs(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-pinned-ids")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	otherProviderID := insertTestProvider(ctx, t, "test-pinned-ids-other")
	t.Cleanup(func() { cleanupProvider(ctx, t, otherProviderID) })

	pinnedID := insertTestModel(ctx, t, providerID, "pinned-model")
	insertTestModel(ctx, t, providerID, "unpinned-model")
	otherPinnedID := insertTestModel(ctx, t, otherProviderID, "other-provider-pinned")
	pinModel(ctx, t, pinnedID)
	pinModel(ctx, t, otherPinnedID)

	pinned, err := repo.PinnedModelIDs(ctx, providerID)
	if err != nil {
		t.Fatalf("PinnedModelIDs: %v", err)
	}
	// Keyed by model_id (the provider-facing string), not the row UUID: that is
	// what the caller compares against a listed model's model_id.
	if len(pinned) != 1 || !pinned["pinned-model"] {
		t.Fatalf("PinnedModelIDs = %v, want exactly {pinned-model: true}", pinned)
	}

	// A provider with no pinned rows must still get a non-nil, empty map: the
	// caller does a bare map lookup with no nil guard.
	unpinnedProviderID := insertTestProvider(ctx, t, "test-pinned-ids-none")
	t.Cleanup(func() { cleanupProvider(ctx, t, unpinnedProviderID) })
	insertTestModel(ctx, t, unpinnedProviderID, "never-pinned")

	none, err := repo.PinnedModelIDs(ctx, unpinnedProviderID)
	if err != nil {
		t.Fatalf("PinnedModelIDs (no pins): %v", err)
	}
	if none == nil {
		t.Fatal("PinnedModelIDs = nil, want a non-nil empty map")
	}
	if len(none) != 0 {
		t.Errorf("PinnedModelIDs (no pins) = %v, want empty", none)
	}
}

// TestPinnedModelIDs_CancelledContext mirrors
// TestRecordMissingModels_CancelledContext: a cancelled context must surface
// as an error rather than a silently empty result.
func TestPinnedModelIDs_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.PinnedModelIDs(ctx, uuid.New())
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
