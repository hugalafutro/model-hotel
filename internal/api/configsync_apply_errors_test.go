package api

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// beginApplyTx opens a transaction on the shared test pool for the direct
// apply-function tests and rolls it back at the end of the test.
func beginApplyTx(t *testing.T) pgx.Tx {
	t.Helper()
	tx, err := apiTestDB.Pool().Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

// A statement that fails inside the apply must abort the apply with that
// error: a config sync that swallowed a failed write would leave the member
// half-applied while reporting success. The cancelled context is the fault
// every one of these functions meets first.
func TestConfigSyncApply_FailedStatementAbortsTheApply(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("apiTestDB is nil; the api test main must set it up")
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	hash, err := user.HashPassword(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h := &ConfigSyncHandler{db: apiTestDB}
	refs := []ExportModelRef{{ProviderName: "p", ModelID: "m"}}

	t.Run("upsertProviders", func(t *testing.T) {
		err := upsertProviders(cctx, beginApplyTx(t), []ExportProvider{{Name: "p", BaseURL: "https://example.com/v1", ProviderType: "openai"}}, nil)
		if err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("applyUsers", func(t *testing.T) {
		err := applyUsers(cctx, beginApplyTx(t), []ExportUser{{Username: "sync-user", PasswordHash: hash, Role: "user", Enabled: true}}, map[string]string{})
		if err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("usernameToID", func(t *testing.T) {
		if _, err := usernameToID(cctx, beginApplyTx(t)); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("upsertVirtualKeys", func(t *testing.T) {
		err := upsertVirtualKeys(cctx, beginApplyTx(t), []ExportVK{{Name: "k", KeyHash: "hash-k", KeyPreview: "mh-k"}}, map[string]string{}, map[string]string{})
		if err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("upsertFailoverGroups", func(t *testing.T) {
		_, err := upsertFailoverGroups(cctx, beginApplyTx(t), []ExportFailoverGroup{{DisplayModel: "g", GroupEnabled: true}})
		if err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("writeUnappliedModelRefs", func(t *testing.T) {
		if err := writeUnappliedModelRefs(cctx, beginApplyTx(t), keyFleetUnappliedModelDisables, refs); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	// The writer decides nothing on its own: a statement that failed inside it
	// leaves the transaction aborted, and the read-back that follows must
	// surface that even when the writer itself reported success.
	t.Run("applyModelIntent/poisoned transaction", func(t *testing.T) {
		poison := func(ctx context.Context, tx pgx.Tx, _ string, _, _ []string) error {
			_, _ = tx.Exec(ctx, `SELECT 1/0`)
			return nil
		}
		if _, err := h.applyModelIntent(context.Background(), refs, keyFleetUnappliedModelDisables, poison); err == nil {
			t.Fatal("expected the aborted transaction to fail the apply")
		}
	})
	t.Run("applyDisabledModels", func(t *testing.T) {
		if _, err := h.applyDisabledModels(cctx, refs); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("applyEnabledModels", func(t *testing.T) {
		if _, err := h.applyEnabledModels(cctx, refs); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("applyFailoverGroups", func(t *testing.T) {
		if _, err := h.applyFailoverGroups(cctx, []ExportFailoverGroup{}); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
}

// users.grants is TEXT[] NOT NULL, so a synced user whose envelope carried no
// grants at all must land with an empty array rather than a NULL that the
// insert would reject.
func TestApplyUsers_MissingGrantsLandAsEmptyArray(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("apiTestDB is nil; the api test main must set it up")
	}
	ctx := context.Background()
	tx := beginApplyTx(t)
	hash, err := user.HashPassword(ctx, "correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	users := []ExportUser{{Username: "grantless", PasswordHash: hash, Role: "user", Enabled: true, Grants: nil}}
	if err := applyUsers(ctx, tx, users, map[string]string{}); err != nil {
		t.Fatalf("applyUsers: %v", err)
	}
	var grants []string
	if err := tx.QueryRow(ctx, `SELECT grants FROM users WHERE username = 'grantless'`).Scan(&grants); err != nil {
		t.Fatalf("read grants: %v", err)
	}
	if grants == nil || len(grants) != 0 {
		t.Fatalf("grants = %#v, want an empty non-NULL array", grants)
	}
}

// A role the users table does not know is rejected by its CHECK constraint;
// the apply must surface that rather than store the account with a role the
// dashboard cannot reason about.
func TestApplyUsers_UnknownRoleIsRejected(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("apiTestDB is nil; the api test main must set it up")
	}
	ctx := context.Background()
	hash, err := user.HashPassword(ctx, "correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	users := []ExportUser{{Username: "odd-role", PasswordHash: hash, Role: "superuser", Enabled: true}}
	if err := applyUsers(ctx, beginApplyTx(t), users, map[string]string{}); err == nil {
		t.Fatal("expected the unknown role to be rejected")
	}
}
