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
	t.Run("aliveModelIDs", func(t *testing.T) {
		if _, err := repo.aliveModelIDs(cctx, []uuid.UUID{id}); err == nil {
			t.Fatal("expected an error from the cancelled context")
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
