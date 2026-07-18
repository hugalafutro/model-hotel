package virtualkey

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// ---------------------------------------------------------------------------
// Integration test infrastructure (follows proxy_test.go pattern)
// ---------------------------------------------------------------------------

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("virtualkey")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("virtualkey")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// ---------------------------------------------------------------------------
// Pure unit tests — no DB required
// ---------------------------------------------------------------------------

func TestErrNotFound_Error(t *testing.T) {
	err := ErrNotFound
	if err.Error() != "virtual key not found" {
		t.Errorf("ErrNotFound.Error() = %q, want %q", err.Error(), "virtual key not found")
	}
}

func TestErrNotFound_IsSentinel(t *testing.T) {
	// ErrNotFound is a singleton; errors.Is must match the same pointer.
	if !errors.Is(ErrNotFound, ErrNotFound) {
		t.Error("errors.Is(ErrNotFound, ErrNotFound) should be true")
	}

	// A non-notFoundError must not match ErrNotFound.
	other := errors.New("some other error")
	if errors.Is(other, ErrNotFound) {
		t.Error("errors.Is for unrelated error should be false")
	}
}

func TestErrNotFound_Type(t *testing.T) {
	var err error = ErrNotFound
	if _, ok := errors.AsType[*notFoundError](err); !ok {
		t.Error("errors.As(ErrNotFound, *notFoundError) should be true")
	}
}

func TestNotFoundErrorMessage(t *testing.T) {
	e := &notFoundError{}
	if e.Error() != "virtual key not found" {
		t.Errorf("notFoundError.Error() = %q, want %q", e.Error(), "virtual key not found")
	}
}

func TestVirtualKeyResponse_FieldMapping(t *testing.T) {
	now := time.Now()
	lastUsedStr := now.Add(-1 * time.Hour).Format(time.RFC3339)
	vk := &VirtualKey{
		ID:         uuid.New(),
		Name:       "test-key",
		KeyHash:    "deadbeef",
		KeyPreview: "sk-...ab",
		TokensUsed: 42,
		LastUsedAt: &now,
		CreatedAt:  now,
	}

	resp := VirtualKeyResponse{
		ID:         vk.ID.String(),
		Name:       vk.Name,
		Key:        "sk-raw-key",
		KeyPreview: vk.KeyPreview,
		TokensUsed: vk.TokensUsed,
		LastUsedAt: &lastUsedStr,
		CreatedAt:  vk.CreatedAt.Format(time.RFC3339),
	}

	if resp.ID != vk.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, vk.ID.String())
	}
	if resp.Name != "test-key" {
		t.Errorf("Name = %q, want %q", resp.Name, "test-key")
	}
	if resp.Key != "sk-raw-key" {
		t.Errorf("Key = %q, want %q", resp.Key, "sk-raw-key")
	}
	if resp.KeyPreview != "sk-...ab" {
		t.Errorf("KeyPreview = %q, want %q", resp.KeyPreview, "sk-...ab")
	}
	if resp.TokensUsed != 42 {
		t.Errorf("TokensUsed = %d, want %d", resp.TokensUsed, 42)
	}
	if resp.LastUsedAt == nil {
		t.Error("LastUsedAt should not be nil")
	}
	if resp.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", resp.CreatedAt, now.Format(time.RFC3339))
	}
}

func TestVirtualKeyResponse_NilLastUsedAt(t *testing.T) {
	resp := VirtualKeyResponse{
		ID:         uuid.New().String(),
		Name:       "unused-key",
		LastUsedAt: nil,
	}
	if resp.LastUsedAt != nil {
		t.Error("LastUsedAt should be nil")
	}
}

func TestVirtualKeyResponse_EmptyKey(t *testing.T) {
	resp := VirtualKeyResponse{
		ID:  uuid.New().String(),
		Key: "",
	}
	if resp.Key != "" {
		t.Errorf("Key = %q, want empty", resp.Key)
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Repository CRUD
// ---------------------------------------------------------------------------

func TestRepository_Create(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	vk, err := repo.Create(ctx, "integration-create-"+suffix, "hash-create-"+suffix, "sk-...cr", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if vk.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if vk.Name != "integration-create-"+suffix {
		t.Errorf("Name = %q, want %q", vk.Name, "integration-create-"+suffix)
	}
	if vk.KeyHash != "hash-create-"+suffix {
		t.Errorf("KeyHash = %q, want %q", vk.KeyHash, "hash-create-"+suffix)
	}
	if vk.KeyPreview != "sk-...cr" {
		t.Errorf("KeyPreview = %q, want %q", vk.KeyPreview, "sk-...cr")
	}
	if vk.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestRepository_List(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create at least one key so the list isn't empty
	_, err := repo.Create(ctx, "integration-list-"+suffix, "hash-list-"+suffix, "sk-...li", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	keys, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(keys) == 0 {
		t.Error("expected at least one virtual key")
	}
}

func TestRepository_Get(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-get-"+suffix, "hash-get-"+suffix, "sk-...ge", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	vk, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if vk.ID != created.ID {
		t.Errorf("ID = %q, want %q", vk.ID, created.ID)
	}
	if vk.Name != "integration-get-"+suffix {
		t.Errorf("Name = %q, want %q", vk.Name, "integration-get-"+suffix)
	}
}

func TestRepository_Get_NotFound(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	_, err := repo.Get(ctx, uuid.New())
	if err == nil {
		t.Error("expected error for non-existent UUID, got nil")
	}
}

func TestRepository_Delete(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-delete-"+suffix, "hash-delete-"+suffix, "sk-...de", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	err = repo.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// Verify it's gone
	_, err = repo.Get(ctx, created.ID)
	if err == nil {
		t.Error("expected error after deleting key, got nil")
	}
}

func TestRepository_Delete_NotFound(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	err := repo.Delete(ctx, uuid.New())
	if err == nil {
		t.Error("expected ErrNotFound when deleting non-existent key, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRepository_AddTokens(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-addtokens-"+suffix, "hash-addtokens-"+suffix, "sk-...at", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	err = repo.AddTokens(ctx, created.KeyHash, 100)
	if err != nil {
		t.Fatalf("AddTokens() failed: %v", err)
	}

	updated, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after AddTokens failed: %v", err)
	}
	if updated.TokensUsed < 100 {
		t.Errorf("TokensUsed = %d, want >= 100", updated.TokensUsed)
	}
	if updated.LastUsedAt == nil {
		t.Error("LastUsedAt should be set after AddTokens")
	}
}

func TestRepository_TouchLastUsed(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-touch-"+suffix, "hash-touch-"+suffix, "sk-...to", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	err = repo.TouchLastUsed(ctx, created.KeyHash)
	if err != nil {
		t.Fatalf("TouchLastUsed() failed: %v", err)
	}

	updated, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after TouchLastUsed failed: %v", err)
	}
	if updated.LastUsedAt == nil {
		t.Error("LastUsedAt should be set after TouchLastUsed")
	}
}

func TestRepository_FindByKeyHash(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-findbyhash-"+suffix, "hash-findbyhash-"+suffix, "sk-...fh", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	vk, err := repo.FindByKeyHash(ctx, created.KeyHash)
	if err != nil {
		t.Fatalf("FindByKeyHash() failed: %v", err)
	}
	if vk.ID != created.ID {
		t.Errorf("ID = %q, want %q", vk.ID, created.ID)
	}
	if vk.KeyHash != created.KeyHash {
		t.Errorf("KeyHash = %q, want %q", vk.KeyHash, created.KeyHash)
	}
}

func TestRepository_FindByKeyHash_NotFound(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	// A miss must be ErrNotFound (not the raw pgx "no rows in result set"), so the
	// proxy maps it to a clean 401 instead of leaking it as a 500.
	_, err := repo.FindByKeyHash(ctx, "nonexistent-hash-value")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByKeyHash(missing) err = %v, want ErrNotFound", err)
	}
}

func TestRepository_Update(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create a key to update
	created, err := repo.Create(ctx, "integration-update-"+suffix, "hash-update-"+suffix, "sk-...up", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	// Update name only
	updated, err := repo.Update(ctx, created.ID, "renamed-"+suffix, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update() name-only failed: %v", err)
	}
	if updated.Name != "renamed-"+suffix {
		t.Errorf("Name = %q, want %q", updated.Name, "renamed-"+suffix)
	}
	if updated.RateLimitRPS != nil {
		t.Errorf("RateLimitRPS = %v, want nil", updated.RateLimitRPS)
	}
	if updated.RateLimitBurst != nil {
		t.Errorf("RateLimitBurst = %v, want nil", updated.RateLimitBurst)
	}

	// Update name + rate limits
	rps := 10.5
	burst := 20
	updated2, err := repo.Update(ctx, created.ID, "limited-"+suffix, &rps, &burst, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update() with limits failed: %v", err)
	}
	if updated2.Name != "limited-"+suffix {
		t.Errorf("Name = %q, want %q", updated2.Name, "limited-"+suffix)
	}
	if updated2.RateLimitRPS == nil || *updated2.RateLimitRPS != 10.5 {
		t.Errorf("RateLimitRPS = %v, want 10.5", updated2.RateLimitRPS)
	}
	if updated2.RateLimitBurst == nil || *updated2.RateLimitBurst != 20 {
		t.Errorf("RateLimitBurst = %v, want 20", updated2.RateLimitBurst)
	}
}

// TestRepository_RateLimitTPM round-trips the nullable rate_limit_tpm column
// through Create, Update (set) and Update (clear).
func TestRepository_RateLimitTPM(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create with a TPM cap set.
	tpm := 50000
	created, err := repo.Create(ctx, "integration-tpm-"+suffix, "hash-tpm-"+suffix, "sk-...tp", nil, nil, &tpm, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() with tpm failed: %v", err)
	}
	if created.RateLimitTPM == nil || *created.RateLimitTPM != 50000 {
		t.Fatalf("created RateLimitTPM = %v, want 50000", created.RateLimitTPM)
	}

	// Read back via FindByKeyHash to confirm it persisted and scans.
	got, err := repo.FindByKeyHash(ctx, "hash-tpm-"+suffix)
	if err != nil {
		t.Fatalf("FindByKeyHash() failed: %v", err)
	}
	if got.RateLimitTPM == nil || *got.RateLimitTPM != 50000 {
		t.Errorf("fetched RateLimitTPM = %v, want 50000", got.RateLimitTPM)
	}

	// Update the cap to a new value.
	newTPM := 12000
	updated, err := repo.Update(ctx, created.ID, created.Name, nil, nil, &newTPM, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update() set tpm failed: %v", err)
	}
	if updated.RateLimitTPM == nil || *updated.RateLimitTPM != 12000 {
		t.Errorf("updated RateLimitTPM = %v, want 12000", updated.RateLimitTPM)
	}

	// Clear the cap (nil persists as NULL).
	cleared, err := repo.Update(ctx, created.ID, created.Name, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update() clear tpm failed: %v", err)
	}
	if cleared.RateLimitTPM != nil {
		t.Errorf("cleared RateLimitTPM = %v, want nil", cleared.RateLimitTPM)
	}
}

// ---------------------------------------------------------------------------
// TestRepository_Create with allowed_providers
// ---------------------------------------------------------------------------

func TestRepository_CreateWithAllowedProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]
	providers := &[]string{"provider-1", "provider-2"}

	vk, err := repo.Create(ctx, "test-allowed-"+suffix, "hash-allowed-"+suffix, "sk-...ap", nil, nil, nil, providers, nil, nil)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if vk.AllowedProviders == nil {
		t.Fatal("AllowedProviders should not be nil")
	}
	if len(*vk.AllowedProviders) != 2 {
		t.Fatalf("AllowedProviders length = %d, want 2", len(*vk.AllowedProviders))
	}
	if (*vk.AllowedProviders)[0] != "provider-1" || (*vk.AllowedProviders)[1] != "provider-2" {
		t.Fatalf("AllowedProviders = %v, want [provider-1, provider-2]", *vk.AllowedProviders)
	}
}

func TestRepository_CreateWithNilAllowedProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	vk, err := repo.Create(ctx, "test-nil-"+suffix, "hash-nil-"+suffix, "sk-...np", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if vk.AllowedProviders != nil {
		t.Fatalf("AllowedProviders should be nil, got %v", *vk.AllowedProviders)
	}
}

func TestRepository_UpdateWithAllowedProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create a key without allowed_providers
	created, err := repo.Create(ctx, "test-update-"+suffix, "hash-update-"+suffix, "sk-...up", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	// Update to set allowed_providers
	providers := &[]string{"provider-3"}
	updated, err := repo.Update(ctx, created.ID, "updated-"+suffix, nil, nil, nil, providers, nil, nil)
	if err != nil {
		t.Fatalf("Update() failed: %v", err)
	}
	if updated.AllowedProviders == nil {
		t.Fatal("AllowedProviders should not be nil after update")
	}
	if len(*updated.AllowedProviders) != 1 {
		t.Fatalf("AllowedProviders length = %d, want 1", len(*updated.AllowedProviders))
	}
	if (*updated.AllowedProviders)[0] != "provider-3" {
		t.Fatalf("AllowedProviders = %v, want [provider-3]", *updated.AllowedProviders)
	}
}

func TestRepository_UpdateToClearAllowedProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create a key with allowed_providers
	providers := &[]string{"provider-to-clear"}
	created, err := repo.Create(ctx, "test-clear-"+suffix, "hash-clear-"+suffix, "sk-...cl", nil, nil, nil, providers, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	// Update to clear allowed_providers (set to nil)
	updated, err := repo.Update(ctx, created.ID, "cleared-"+suffix, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update() failed: %v", err)
	}
	if updated.AllowedProviders != nil {
		t.Fatalf("AllowedProviders should be nil after clearing, got %v", *updated.AllowedProviders)
	}
}

func TestRepository_ListIncludesAllowedProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]
	providers := &[]string{"provider-list-1", "provider-list-2"}

	// Create a key with allowed_providers
	created, err := repo.Create(ctx, "test-list-"+suffix, "hash-list-"+suffix, "sk-...li", nil, nil, nil, providers, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	// List all keys
	keys, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	// Find the created key in the list
	var found *VirtualKey
	for _, k := range keys {
		if k.ID == created.ID {
			found = k
			break
		}
	}
	if found == nil {
		t.Fatalf("created key not found in list")
		return
	}
	if found.AllowedProviders == nil {
		t.Fatal("AllowedProviders should not be nil in list results")
	}
	if len(*found.AllowedProviders) != 2 {
		t.Fatalf("AllowedProviders length = %d, want 2", len(*found.AllowedProviders))
	}
}

func TestRepository_Update_NotFound(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	_, err := repo.Update(ctx, uuid.New(), "nonexistent", nil, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error for non-existent UUID, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestRepository_TouchLastUsed edge cases
// ---------------------------------------------------------------------------

func TestRepository_TouchLastUsed_NotFound(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	// Touch non-existent key - should not error
	err := repo.TouchLastUsed(ctx, "non-existent-key-hash")
	if err != nil {
		t.Errorf("TouchLastUsed on non-existent key should not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestRepository_Create edge cases
// ---------------------------------------------------------------------------

func TestRepository_Create_Duplicate(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create a key with a specific hash
	_, err := repo.Create(ctx, "duplicate-key-"+suffix, "hash-duplicate-"+suffix, "sk-...du", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}

	// Try to create another key with the same key_hash - should error (unique constraint)
	_, err = repo.Create(ctx, "duplicate-key-2-"+suffix, "hash-duplicate-"+suffix, "sk-...d2", nil, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("Create with duplicate key_hash should error")
	}
}

// ---------------------------------------------------------------------------
// TestRepository_Delete edge cases
// ---------------------------------------------------------------------------

func TestRepository_Delete_NotFound_NoError(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	// Delete non-existent key - returns ErrNotFound (RowsAffected == 0)
	nonExistentID := uuid.New()
	err := repo.Delete(ctx, nonExistentID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// DB error-path tests (canceled context forces errors)
// ---------------------------------------------------------------------------

func TestRepository_List_DBError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately to force DB error
	repo := NewRepository(testDB.Pool())

	_, err := repo.List(ctx)
	if err == nil {
		t.Error("expected error with canceled context, got nil")
	}
}

func TestRepository_Delete_DBError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewRepository(testDB.Pool())

	err := repo.Delete(ctx, uuid.New())
	if err == nil {
		t.Error("expected error with canceled context, got nil")
	}
}

func TestRepository_TouchLastUsed_DBError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewRepository(testDB.Pool())

	err := repo.TouchLastUsed(ctx, "any-hash")
	if err == nil {
		t.Error("expected error with canceled context, got nil")
	}
}

func TestRepository_Update_DBError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewRepository(testDB.Pool())

	_, err := repo.Update(ctx, uuid.New(), "name", nil, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error with canceled context, got nil")
	}
}

func TestRepository_Get_DBError(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewRepository(testDB.Pool())

	_, err := repo.Get(ctx, uuid.New())
	if err == nil {
		t.Error("expected error with canceled context in Get, got nil")
	}
}

func TestRepository_List_Empty(t *testing.T) {

	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	// Clean up any existing keys first
	_, err := testDB.Pool().Exec(ctx, "DELETE FROM virtual_keys")
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// List when no keys exist - should return empty slice
	keys, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

// ---------------------------------------------------------------------------
// Ownership (phase 2 of multi-user)
// ---------------------------------------------------------------------------

// seedOwner inserts a users row directly (this package must not import
// internal/user) and returns its id. Cleaned up with the test.
func seedOwner(t *testing.T, enabled bool, rps *float64, burst, tpm *int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := testDB.Pool().QueryRow(ctx,
		`INSERT INTO users (username, password_hash, enabled, rate_limit_rps, rate_limit_burst, rate_limit_tpm)
		 VALUES ($1, 'x', $2, $3, $4, $5) RETURNING id`,
		"vk-owner-"+uuid.NewString(), enabled, rps, burst, tpm).Scan(&id)
	if err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func TestRepository_Ownership(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	rps := 2.5
	burst := 4
	tpm := 6000
	owner := seedOwner(t, true, &rps, &burst, &tpm)

	created, err := repo.Create(ctx,
		"owned-"+suffix, "hash-owned-"+suffix, "sk-...ow",
		nil, nil, nil, nil, nil, &owner)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	defer func() { _ = repo.Delete(ctx, created.ID) }()
	if created.OwnerUserID == nil || *created.OwnerUserID != owner {
		t.Fatalf("Create() OwnerUserID = %v, want %v", created.OwnerUserID, owner)
	}

	// FindByKeyHash joins the owner and carries enabled + limits.
	got, err := repo.FindByKeyHash(ctx, "hash-owned-"+suffix)
	if err != nil {
		t.Fatalf("FindByKeyHash() failed: %v", err)
	}
	if got.Owner == nil {
		t.Fatal("FindByKeyHash() Owner = nil, want populated")
	}
	if !got.Owner.Enabled {
		t.Error("Owner.Enabled = false, want true")
	}
	if got.Owner.RateLimitRPS == nil || *got.Owner.RateLimitRPS != rps {
		t.Errorf("Owner.RateLimitRPS = %v, want %v", got.Owner.RateLimitRPS, rps)
	}
	if got.Owner.RateLimitBurst == nil || *got.Owner.RateLimitBurst != burst {
		t.Errorf("Owner.RateLimitBurst = %v, want %v", got.Owner.RateLimitBurst, burst)
	}
	if got.Owner.RateLimitTPM == nil || *got.Owner.RateLimitTPM != tpm {
		t.Errorf("Owner.RateLimitTPM = %v, want %v", got.Owner.RateLimitTPM, tpm)
	}

	// ListByOwner returns only this user's keys.
	unowned, err := repo.Create(ctx,
		"unowned-"+suffix, "hash-unowned-"+suffix, "sk-...un",
		nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create(unowned) failed: %v", err)
	}
	defer func() { _ = repo.Delete(ctx, unowned.ID) }()
	if unowned.OwnerUserID != nil {
		t.Errorf("unowned OwnerUserID = %v, want nil", unowned.OwnerUserID)
	}
	mine, err := repo.ListByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("ListByOwner() failed: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != created.ID {
		t.Errorf("ListByOwner() = %d keys, want exactly the owned key", len(mine))
	}

	// Update can clear the owner (nil = unassign).
	cleared, err := repo.Update(ctx, created.ID, created.Name, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update(clear owner) failed: %v", err)
	}
	if cleared.OwnerUserID != nil {
		t.Errorf("Update(clear) OwnerUserID = %v, want nil", cleared.OwnerUserID)
	}

	// And set it back.
	reassigned, err := repo.Update(ctx, created.ID, created.Name, nil, nil, nil, nil, nil, &owner)
	if err != nil {
		t.Fatalf("Update(set owner) failed: %v", err)
	}
	if reassigned.OwnerUserID == nil || *reassigned.OwnerUserID != owner {
		t.Errorf("Update(set) OwnerUserID = %v, want %v", reassigned.OwnerUserID, owner)
	}
}

func TestRepository_FindByKeyHash_DisabledOwner(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	owner := seedOwner(t, false, nil, nil, nil)
	created, err := repo.Create(ctx,
		"disabled-owner-"+suffix, "hash-dis-"+suffix, "sk-...di",
		nil, nil, nil, nil, nil, &owner)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	defer func() { _ = repo.Delete(ctx, created.ID) }()

	got, err := repo.FindByKeyHash(ctx, "hash-dis-"+suffix)
	if err != nil {
		t.Fatalf("FindByKeyHash() failed: %v", err)
	}
	if got.Owner == nil || got.Owner.Enabled {
		t.Errorf("Owner = %+v, want populated with Enabled=false", got.Owner)
	}
	if got.Owner != nil && got.Owner.RateLimitRPS != nil {
		t.Errorf("Owner.RateLimitRPS = %v, want nil (no cap)", got.Owner.RateLimitRPS)
	}
}

func TestRepository_DeleteOwner_OrphansKey(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	owner := seedOwner(t, true, nil, nil, nil)
	created, err := repo.Create(ctx,
		"orphan-"+suffix, "hash-orphan-"+suffix, "sk-...or",
		nil, nil, nil, nil, nil, &owner)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	defer func() { _ = repo.Delete(ctx, created.ID) }()

	if _, err := testDB.Pool().Exec(ctx, `DELETE FROM users WHERE id = $1`, owner); err != nil {
		t.Fatalf("delete owner: %v", err)
	}

	got, err := repo.FindByKeyHash(ctx, "hash-orphan-"+suffix)
	if err != nil {
		t.Fatalf("FindByKeyHash() after owner delete failed: %v", err)
	}
	if got.OwnerUserID != nil || got.Owner != nil {
		t.Errorf("key not orphaned: OwnerUserID=%v Owner=%+v", got.OwnerUserID, got.Owner)
	}
}
