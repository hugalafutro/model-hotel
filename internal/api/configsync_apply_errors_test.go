package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/logtest"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
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
		// No import sequence left behind by another test: with one, the section
		// would yield at the fence and never run the writer this case is about.
		cleanConfigTables(t)
		ran := false
		poison := func(ctx context.Context, tx pgx.Tx, _ string, _, _ []string) error {
			ran = true
			_, _ = tx.Exec(ctx, `SELECT 1/0`)
			return nil
		}
		_, err := h.applyModelIntent(context.Background(), refs, 0, keyFleetUnappliedModelDisables, poison)
		if err == nil || !ran || errors.Is(err, errImportSuperseded) {
			t.Fatalf("err = %v ran = %v, want the aborted transaction to fail the apply after the writer ran", err, ran)
		}
	})
	t.Run("applyDisabledModels", func(t *testing.T) {
		if _, err := h.applyDisabledModels(cctx, refs, 0); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("applyEnabledModels", func(t *testing.T) {
		if _, err := h.applyEnabledModels(cctx, refs, 0); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("applyFailoverGroups", func(t *testing.T) {
		if _, err := h.applyFailoverGroups(cctx, []ExportFailoverGroup{}, true, 0); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("guardAgainstVirtualKeyWipe", func(t *testing.T) {
		if _, err := guardAgainstVirtualKeyWipe(cctx, beginApplyTx(t), nil); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("guardKeysSurvived", func(t *testing.T) {
		if err := guardKeysSurvived(cctx, beginApplyTx(t), true); err == nil {
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

// A post-commit section that cannot take the fence lock fails on its own
// lock_timeout, and the refresh reports each failure rather than dropping it:
// the disable and pin failures are joined (one must not erase the other) and
// the group build's is kept. The lock is held from another connection, so
// every section waits out its lock_timeout.
func TestPostImportRefresh_ReportsEverySectionFailure(t *testing.T) {
	cleanConfigTables(t)
	h := NewConfigSyncHandler(apiTestDB, settings.NewRepository(apiTestDB.Pool()), configSyncMasterKey, "v-test", nil, nil)

	holder, err := apiTestDB.Pool().Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire holder conn: %v", err)
	}
	defer holder.Release()
	if _, err := holder.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, fleetSourceGenLock); err != nil {
		t.Fatalf("hold advisory lock: %v", err)
	}
	defer func() { _, _ = holder.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, fleetSourceGenLock) }()

	out := h.postImportRefresh(context.Background(), ConfigEnvelope{Config: ConfigPayload{
		DisabledModels: []ExportModelRef{},
		EnabledModels:  []ExportModelRef{},
		FailoverGroups: []ExportFailoverGroup{},
	}}, 0, nil, nil)

	joined, ok := out.ModelStateErr.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) != 2 {
		t.Errorf("ModelStateErr = %v, want the disable and the pin failures joined", out.ModelStateErr)
	}
	if out.GroupApplyErr == nil {
		t.Error("GroupApplyErr = nil, want the group build's lock failure")
	}
	if !out.incomplete() {
		t.Error("an import whose post-commit sections all failed must report incomplete")
	}
}

// The refresh rewrites a key mask an older dump carried in the legacy
// two-dots-two shape, and says how many it rewrote.
func TestPostImportRefresh_RewritesLegacyKeyMasks(t *testing.T) {
	cleanConfigTables(t)
	const key = "sk-legacy-mask-key-0123456789"
	id := seedProvider(t, "openai", key, configSyncMasterKey)
	if _, err := apiTestDB.Pool().Exec(context.Background(), `UPDATE providers SET masked_key = 'sk...89' WHERE id = $1`, id); err != nil {
		t.Fatalf("set legacy mask: %v", err)
	}
	h := NewConfigSyncHandler(apiTestDB, settings.NewRepository(apiTestDB.Pool()), configSyncMasterKey, "v-test", nil, nil)
	capt := logtest.Capture(t)

	h.postImportRefresh(context.Background(), ConfigEnvelope{}, 0, nil, nil)

	var masked string
	if err := apiTestDB.Pool().QueryRow(context.Background(), `SELECT masked_key FROM providers WHERE id = $1`, id).Scan(&masked); err != nil {
		t.Fatalf("read masked_key: %v", err)
	}
	if masked != provider.MaskAPIKey(key) {
		t.Errorf("masked_key = %q, want the current mask %q", masked, provider.MaskAPIKey(key))
	}
	if got := fmt.Sprint(capt.Attr("configsync: provider key masks rewritten", "count")); got != "1" {
		t.Errorf("rewritten count logged = %s, want 1", got)
	}
}
