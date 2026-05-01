package virtualkey

import (
	"context"
	"errors"
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
	var err error
	testDB, err = db.New(ctx, "postgres://llmproxy:changeme@localhost:5432/testdb?sslmode=disable", 25, 5)
	if err != nil {
		testDB = nil
	}
	code := m.Run()
	if testDB != nil {
		testDB.Close()
	}
	os.Exit(code)
}

func skipIfNoDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		t.Skip("database not available")
	}
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
	var nf *notFoundError
	if !errors.As(err, &nf) {
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	vk, err := repo.Create(ctx, "integration-create-"+suffix, "hash-create-"+suffix, "sk-...cr")
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	// Create at least one key so the list isn't empty
	_, err := repo.Create(ctx, "integration-list-"+suffix, "hash-list-"+suffix, "sk-...li")
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-get-"+suffix, "hash-get-"+suffix, "sk-...ge")
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	_, err := repo.Get(ctx, uuid.New())
	if err == nil {
		t.Error("expected error for non-existent UUID, got nil")
	}
}

func TestRepository_Delete(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-delete-"+suffix, "hash-delete-"+suffix, "sk-...de")
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
	skipIfNoDB(t)
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-addtokens-"+suffix, "hash-addtokens-"+suffix, "sk-...at")
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-touch-"+suffix, "hash-touch-"+suffix, "sk-...to")
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]

	created, err := repo.Create(ctx, "integration-findbyhash-"+suffix, "hash-findbyhash-"+suffix, "sk-...fh")
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
	skipIfNoDB(t)
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())

	_, err := repo.FindByKeyHash(ctx, "nonexistent-hash-value")
	if err == nil {
		t.Error("expected error for non-existent key hash, got nil")
	}
}
