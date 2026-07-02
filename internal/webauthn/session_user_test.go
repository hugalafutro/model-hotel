package webauthn

import (
	"context"
	"testing"
)

func TestSessionManagerTokenUser(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	adminTok, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken(admin): %v", err)
	}
	userHandle := "2f9c3a1e-0000-4000-8000-000000000001"
	userTok, err := mgr.CreateAuthToken(ctx, []byte(userHandle), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken(user): %v", err)
	}

	if got, ok := mgr.TokenUser(ctx, adminTok); !ok || string(got) != "admin" {
		t.Errorf("TokenUser(admin token) = %q, %v", got, ok)
	}
	if got, ok := mgr.TokenUser(ctx, userTok); !ok || string(got) != userHandle {
		t.Errorf("TokenUser(user token) = %q, %v", got, ok)
	}
	if _, ok := mgr.TokenUser(ctx, "bogus"); ok {
		t.Error("TokenUser accepted a bogus token")
	}
	if _, ok := mgr.TokenUser(ctx, ""); ok {
		t.Error("TokenUser accepted an empty token")
	}
}

func TestDeleteSessionsByUserID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	handle := "3f9c3a1e-0000-4000-8000-000000000002"
	tok1, err := mgr.CreateAuthToken(ctx, []byte(handle), nil)
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := mgr.CreateAuthToken(ctx, []byte(handle), nil)
	if err != nil {
		t.Fatal(err)
	}
	otherTok, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatal(err)
	}

	n, err := repo.DeleteSessionsByUserID(ctx, []byte(handle))
	if err != nil {
		t.Fatalf("DeleteSessionsByUserID: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted %d sessions, want 2", n)
	}
	if mgr.Validate(ctx, tok1) || mgr.Validate(ctx, tok2) {
		t.Error("revoked session still validates")
	}
	if !mgr.Validate(ctx, otherTok) {
		t.Error("unrelated session was revoked")
	}

	// Idempotent: second call deletes nothing, no error.
	if n, err := repo.DeleteSessionsByUserID(ctx, []byte(handle)); err != nil || n != 0 {
		t.Errorf("second delete: n=%d err=%v", n, err)
	}
}
