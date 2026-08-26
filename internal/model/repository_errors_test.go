package model

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A read that fails must come back as an error: a false "nothing retired",
// "not confirmed" or "nothing reverted" would let the caller act on state it
// never saw. The cancelled context is the fault each method meets first.
func TestRepository_FailedReadIsAnError(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool is nil; TestMain must set it up")
	}
	repo := NewRepository(testPool)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	id := uuid.New()

	t.Run("RecordMissingModels", func(t *testing.T) {
		if _, _, err := repo.RecordMissingModels(cctx, id, "gone-provider", []string{"still-here"}); err == nil {
			t.Fatal("expected an error from the cancelled context")
		}
	})
	t.Run("AutoRetireIfConfirmed", func(t *testing.T) {
		asked := false
		retired, err := repo.AutoRetireIfConfirmed(cctx, id, func() bool { asked = true; return true })
		if err == nil || retired {
			t.Fatalf("retired=%v err=%v, want an error and no retirement", retired, err)
		}
		if asked {
			t.Fatal("the confirmer must not run when the state read failed")
		}
	})
	t.Run("RevertAutoRetire", func(t *testing.T) {
		reverted, err := repo.RevertAutoRetire(cctx, id)
		if err == nil || reverted {
			t.Fatalf("reverted=%v err=%v, want an error and no revert", reverted, err)
		}
	})
}

// The confirmer is a real request to the provider and can outlive the caller's
// context. A retirement whose commit then fails must report the error and
// leave the row untouched, not claim a retirement that never landed.
func TestAutoRetireIfConfirmed_ContextLostDuringConfirmDoesNotRetire(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool is nil; TestMain must set it up")
	}
	ctx := context.Background()
	repo := NewRepository(testPool)
	providerID := insertTestProvider(ctx, t, "test-autoretire-ctx-lost")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	id := insertTestModel(ctx, t, providerID, "confirm-outlives-ctx")

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	retired, err := repo.AutoRetireIfConfirmed(cctx, id, func() bool { cancel(); return true })
	if err == nil || retired {
		t.Fatalf("retired=%v err=%v, want an error and no retirement", retired, err)
	}
	var enabled bool
	var retiredAt *time.Time
	if err := testPool.QueryRow(ctx, `SELECT enabled, auto_retired_at FROM models WHERE id = $1`, id).Scan(&enabled, &retiredAt); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !enabled || retiredAt != nil {
		t.Fatalf("enabled=%v auto_retired_at=%v, want the row left enabled and unstamped", enabled, retiredAt)
	}
}

// The eligibility read locks the row (FOR UPDATE). While another writer holds
// it, the read blocks until the caller's deadline expires, and the retire must
// come back as that error with the confirmer never run and the row untouched.
func TestAutoRetireIfConfirmed_RowHeldByAnotherWriterTimesOut(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool is nil; TestMain must set it up")
	}
	ctx := context.Background()
	repo := NewRepository(testPool)
	providerID := insertTestProvider(ctx, t, "test-autoretire-row-held")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	id := insertTestModel(ctx, t, providerID, "row-held-elsewhere")

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT 1 FROM models WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatalf("hold row: %v", err)
	}

	tctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	asked := false
	retired, err := repo.AutoRetireIfConfirmed(tctx, id, func() bool { asked = true; return true })
	if err == nil || retired {
		t.Fatalf("retired=%v err=%v, want an error and no retirement", retired, err)
	}
	if asked {
		t.Fatal("the confirmer must not run when the state read failed")
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release row: %v", err)
	}
	var enabled bool
	var retiredAt *time.Time
	if err := testPool.QueryRow(ctx, `SELECT enabled, auto_retired_at FROM models WHERE id = $1`, id).Scan(&enabled, &retiredAt); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !enabled || retiredAt != nil {
		t.Fatalf("enabled=%v auto_retired_at=%v, want the row left enabled and unstamped", enabled, retiredAt)
	}
}
