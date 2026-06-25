package totp

import (
	"context"
	"testing"
)

// TestPostgresStoreErrorsOnCanceledContext covers the DB-error branches of every
// PostgresStore method. A canceled context makes each query/transaction fail
// before touching a row, without disturbing the shared test pool.
func TestPostgresStoreErrorsOnCanceledContext(t *testing.T) {
	store := NewPostgresStore(testDB.Pool())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.UpsertEnrollment(ctx, []byte("c"), []byte("n"), []byte("s")); err == nil {
		t.Error("UpsertEnrollment: want error")
	}
	if _, _, err := store.LoadSecret(ctx); err == nil {
		t.Error("LoadSecret: want error")
	}
	if _, err := store.RecordUsedStep(ctx, 1); err == nil {
		t.Error("RecordUsedStep: want error")
	}
	if _, err := store.Enable(ctx); err == nil {
		t.Error("Enable: want error")
	}
	if err := store.Disable(ctx); err == nil {
		t.Error("Disable: want error")
	}
	if _, err := store.IsEnabled(ctx); err == nil {
		t.Error("IsEnabled: want error")
	}
	if _, _, err := store.EnabledAt(ctx); err == nil {
		t.Error("EnabledAt: want error")
	}
	if _, _, err := store.RecoveryCounts(ctx); err == nil {
		t.Error("RecoveryCounts: want error")
	}
	if _, _, err := store.LastUsedStep(ctx); err == nil {
		t.Error("LastUsedStep: want error")
	}
	if err := store.ReplaceRecoveryCodes(ctx, []string{"h"}); err == nil {
		t.Error("ReplaceRecoveryCodes: want error")
	}
	if _, err := store.ConsumeRecoveryCode(ctx, "h"); err == nil {
		t.Error("ConsumeRecoveryCode: want error")
	}
}
