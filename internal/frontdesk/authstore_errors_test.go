package frontdesk

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// TestAuthStoresErrorWhenDBClosed verifies the TOTP and WebAuthn stores surface
// an error (instead of panicking) when the database is unavailable, covering the
// DB-error branches the CRUD round-trip tests don't reach.
func TestAuthStoresErrorWhenDBClosed(t *testing.T) {
	s := newTestStore(t)
	ts := NewTOTPStore(s)
	ws := NewWebAuthnStore(s)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	// TOTP store.
	if err := ts.UpsertEnrollment(ctx, []byte("c"), []byte("n"), []byte("s")); err == nil {
		t.Error("UpsertEnrollment: want error")
	}
	if _, _, err := ts.LoadSecret(ctx); err == nil {
		t.Error("LoadSecret: want error")
	}
	if _, err := ts.RecordUsedStep(ctx, 1); err == nil {
		t.Error("RecordUsedStep: want error")
	}
	if _, err := ts.Enable(ctx); err == nil {
		t.Error("Enable: want error")
	}
	if err := ts.Disable(ctx); err == nil {
		t.Error("Disable: want error")
	}
	if _, err := ts.IsEnabled(ctx); err == nil {
		t.Error("IsEnabled: want error")
	}
	if _, _, err := ts.EnabledAt(ctx); err == nil {
		t.Error("EnabledAt: want error")
	}
	if _, _, err := ts.RecoveryCounts(ctx); err == nil {
		t.Error("RecoveryCounts: want error")
	}
	if _, _, err := ts.LastUsedStep(ctx); err == nil {
		t.Error("LastUsedStep: want error")
	}
	if err := ts.ReplaceRecoveryCodes(ctx, []string{"h"}); err == nil {
		t.Error("ReplaceRecoveryCodes: want error")
	}
	if _, err := ts.ConsumeRecoveryCode(ctx, "h"); err == nil {
		t.Error("ConsumeRecoveryCode: want error")
	}

	// WebAuthn store.
	id := []byte("cred-id")
	if err := ws.StoreCredential(ctx, &webauthn.CredentialRecord{ID: id}); err == nil {
		t.Error("StoreCredential: want error")
	}
	if _, err := ws.ListCredentials(ctx); err == nil {
		t.Error("ListCredentials: want error")
	}
	if _, err := ws.GetCredentialByID(ctx, id); err == nil {
		t.Error("GetCredentialByID: want error")
	}
	if err := ws.DeleteCredential(ctx, id); err == nil {
		t.Error("DeleteCredential: want error")
	}
	if err := ws.RenameCredential(ctx, id, "name"); err == nil {
		t.Error("RenameCredential: want error")
	}
	if err := ws.UpdateSignCount(ctx, id, 5); err == nil {
		t.Error("UpdateSignCount: want error")
	}
	sid := uuid.New()
	if err := ws.CreateSession(ctx, &webauthn.SessionRecord{ID: sid}); err == nil {
		t.Error("CreateSession: want error")
	}
	if _, err := ws.GetSession(ctx, sid); err == nil {
		t.Error("GetSession: want error")
	}
	if _, err := ws.GetSessionByTokenHash(ctx, "hash"); err == nil {
		t.Error("GetSessionByTokenHash: want error")
	}
	if err := ws.DeleteSession(ctx, sid); err == nil {
		t.Error("DeleteSession: want error")
	}
	if _, err := ws.CleanupExpiredSessions(ctx); err == nil {
		t.Error("CleanupExpiredSessions: want error")
	}
}
