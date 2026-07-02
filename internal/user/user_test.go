package user

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("user")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("user")

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

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash not in PHC format: %q", hash)
	}

	ok, err := VerifyPassword("hunter2", hash)
	if err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(wrong): %v", err)
	}
	if ok {
		t.Fatal("wrong password accepted")
	}
}

func TestHashPassword_UniqueSalts(t *testing.T) {
	h1, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("two hashes of the same password are identical; salt not random")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",  // wrong variant
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$aGFzaA", // wrong version
		"$argon2id$v=19$m=0,t=0,p=0$c2FsdA$aGFzaA",     // zero params
		"$argon2id$v=19$notparams$c2FsdA$aGFzaA",       // unparsable param section
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",    // bad salt b64
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$!!!",    // bad key b64
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$",       // empty key
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$" +
			base64.RawStdEncoding.EncodeToString(make([]byte, 513)), // key over 512 bytes
	} {
		if _, err := VerifyPassword("x", bad); !errors.Is(err, ErrHashFormat) {
			t.Errorf("VerifyPassword(%q) err = %v, want ErrHashFormat", bad, err)
		}
	}
}

func TestValidateGrants(t *testing.T) {
	if err := ValidateGrants(nil); err != nil {
		t.Errorf("nil grants: %v", err)
	}
	if err := ValidateGrants([]string{"chat", "usage"}); err != nil {
		t.Errorf("valid grants: %v", err)
	}
	if err := ValidateGrants([]string{"nonsense"}); err == nil {
		t.Error("unknown grant accepted")
	}
	if err := ValidateGrants([]string{"chat", "chat"}); err == nil {
		t.Error("duplicate grant accepted")
	}
}

func TestHasGrant(t *testing.T) {
	grants := []string{"chat", "logs"}
	if !HasGrant(grants, GrantChat) {
		t.Error("expected chat grant")
	}
	if HasGrant(grants, GrantVirtualKeys) {
		t.Error("unexpected virtual_keys grant")
	}
	if HasGrant(nil, GrantChat) {
		t.Error("nil grants granted chat")
	}
}

func TestNormalizeEmail(t *testing.T) {
	if NormalizeEmail(nil) != nil {
		t.Error("nil should stay nil")
	}
	empty := "   "
	if NormalizeEmail(&empty) != nil {
		t.Error("blank should map to nil")
	}
	mixed := "  Alice@Example.COM "
	if got := NormalizeEmail(&mixed); got == nil || *got != "alice@example.com" {
		t.Errorf("got %v, want alice@example.com", got)
	}
}

// ---------------------------------------------------------------------------
// Repository integration tests
// ---------------------------------------------------------------------------

func mustCreate(t *testing.T, repo *Repository, username string, email *string, role Role, grants []string) *User {
	t.Helper()
	hash, err := HashPassword("test-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u, err := repo.Create(context.Background(), username, "Display "+username, email, hash, role, grants)
	if err != nil {
		t.Fatalf("Create(%s): %v", username, err)
	}
	t.Cleanup(func() { _ = repo.Delete(context.Background(), u.ID) })
	return u
}

func TestRepository_CreateGetList(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	email := "Bob@Example.com"
	u := mustCreate(t, repo, "bob-"+uuid.NewString(), &email, RoleUser, []string{"chat"})

	got, err := repo.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Username != u.Username || got.Role != RoleUser || !got.Enabled {
		t.Errorf("unexpected row: %+v", got)
	}
	if got.Email == nil || *got.Email != "bob@example.com" {
		t.Errorf("email not normalized: %v", got.Email)
	}
	if len(got.Grants) != 1 || got.Grants[0] != "chat" {
		t.Errorf("grants = %v", got.Grants)
	}

	byName, err := repo.GetByUsername(context.Background(), u.Username)
	if err != nil || byName.ID != u.ID {
		t.Fatalf("GetByUsername: %v", err)
	}
	byEmail, err := repo.GetByEmail(context.Background(), "BOB@example.COM")
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("GetByEmail: %v", err)
	}

	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, row := range list {
		if row.ID == u.ID {
			found = true
		}
	}
	if !found {
		t.Error("created user missing from List")
	}
}

func TestRepository_UpdateAndPassword(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	u := mustCreate(t, repo, "carol-"+uuid.NewString(), nil, RoleUser, nil)

	updated, err := repo.Update(context.Background(), u.ID, u.Username, "Carol", nil, RoleAdmin, []string{"logs", "usage"}, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Role != RoleAdmin || updated.Enabled || len(updated.Grants) != 2 {
		t.Errorf("update not applied: %+v", updated)
	}
	if !updated.UpdatedAt.After(u.UpdatedAt) {
		t.Error("updated_at not bumped")
	}

	newHash, err := HashPassword("rotated")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPassword(context.Background(), u.ID, newHash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	got, err := repo.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("rotated", got.PasswordHash); !ok {
		t.Error("rotated password does not verify")
	}

	if err := repo.TouchLastLogin(context.Background(), u.ID); err != nil {
		t.Fatalf("TouchLastLogin: %v", err)
	}
	got, err = repo.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastLoginAt == nil {
		t.Error("last_login_at not set")
	}
}

func TestRepository_NotFoundAndDelete(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	missing := uuid.New()

	if _, err := repo.Get(context.Background(), missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByUsername(context.Background(), "nobody-"+uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByUsername(missing) = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByEmail(context.Background(), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByEmail(empty) = %v, want ErrNotFound", err)
	}
	if err := repo.SetPassword(context.Background(), missing, "$argon2id$x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPassword(missing) = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(context.Background(), missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}

	u := mustCreate(t, repo, "dave-"+uuid.NewString(), nil, RoleUser, nil)
	if err := repo.Delete(context.Background(), u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(context.Background(), u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(deleted) = %v, want ErrNotFound", err)
	}
}

func TestRepository_HasEnabled(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	if _, err := testDB.Pool().Exec(context.Background(), `DELETE FROM users`); err != nil {
		t.Fatalf("clear users: %v", err)
	}

	if got, err := repo.HasEnabled(context.Background()); err != nil || got {
		t.Errorf("HasEnabled(empty) = %v, %v; want false", got, err)
	}

	u := mustCreate(t, repo, "gina-"+uuid.NewString(), nil, RoleUser, nil)
	if got, err := repo.HasEnabled(context.Background()); err != nil || !got {
		t.Errorf("HasEnabled(one enabled) = %v, %v; want true", got, err)
	}

	if _, err := repo.Update(context.Background(), u.ID, u.Username, "", nil, RoleUser, nil, false); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.HasEnabled(context.Background()); err != nil || got {
		t.Errorf("HasEnabled(all disabled) = %v, %v; want false", got, err)
	}
}

func TestRepository_DuplicateUsername(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	name := "erin-" + uuid.NewString()
	mustCreate(t, repo, name, nil, RoleUser, nil)

	hash, err := HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(context.Background(), name, "", nil, hash, RoleUser, nil); err == nil {
		t.Error("duplicate username accepted")
	}
}

// TestRepository_CancelledContext drives every repository method's query-error
// branch by handing it an already-cancelled context, so a DB failure surfaces
// as an error rather than a nil-deref or a silent success.
func TestRepository_CancelledContext(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	id := uuid.New()
	if _, err := repo.List(ctx); err == nil {
		t.Error("List(cancelled) err = nil, want error")
	}
	if _, err := repo.Get(ctx, id); err == nil {
		t.Error("Get(cancelled) err = nil, want error")
	}
	if _, err := repo.GetByUsername(ctx, "x"); err == nil {
		t.Error("GetByUsername(cancelled) err = nil, want error")
	}
	if _, err := repo.GetByEmail(ctx, "x@example.com"); err == nil {
		t.Error("GetByEmail(cancelled) err = nil, want error")
	}
	if _, err := repo.Create(ctx, "x", "d", nil, "h", RoleUser, nil); err == nil {
		t.Error("Create(cancelled) err = nil, want error")
	}
	if _, err := repo.Update(ctx, id, "x", "d", nil, RoleUser, nil, true); err == nil {
		t.Error("Update(cancelled) err = nil, want error")
	}
	if err := repo.SetPassword(ctx, id, "h"); err == nil {
		t.Error("SetPassword(cancelled) err = nil, want error")
	}
	if err := repo.Delete(ctx, id); err == nil {
		t.Error("Delete(cancelled) err = nil, want error")
	}
	if err := repo.TouchLastLogin(ctx, id); err == nil {
		t.Error("TouchLastLogin(cancelled) err = nil, want error")
	}
	if _, err := repo.HasEnabled(ctx); err == nil {
		t.Error("HasEnabled(cancelled) err = nil, want error")
	}
}
