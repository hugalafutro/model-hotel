package model

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Retirement bookkeeping: recording a missing model, the confirmed
// auto-retire and its revert, and what a manual enable/disable does to them.

func TestSetEnabled_Enable(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-enable")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, false, now())
	`, modelID, providerID, "disable-enable", "Disable Enable Test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	updated, err := repo.SetEnabled(ctx, modelID, true)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	if !updated.Enabled {
		t.Error("model should be enabled")
	}
}

func TestSetEnabled_Disable(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-disable")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "enable-disable", "Enable Disable Test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	updated, err := repo.SetEnabled(ctx, modelID, false)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	if updated.Enabled {
		t.Error("model should be disabled")
	}
}

// ---------------------------------------------------------------------------
// TestAutoRetireIfConfirmed
// ---------------------------------------------------------------------------

// TestAutoRetireIfConfirmed_AbandonedWriteIsNeverVisible is the reason this
// method exists, and it needs a real database because the property under test is
// cross-session visibility, which no mock can demonstrate.
//
// The proxy disables a model it believes the provider has retired, and the model
// can answer a request — disproving that — while the write is in flight. Writing
// and then undoing would leave the disabled row readable by everyone in between,
// and a concurrent custom-group revalidation that samples it auto-disables the
// group for having too few routable members. Re-enabling the model does not
// bring the group back, so the intermediate state has to not exist rather than
// be corrected afterwards.
func TestAutoRetireIfConfirmed_AbandonedWriteIsNeverVisible(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-confirm")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "confirm-abandon", "Confirm Abandon Test"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	readEnabled := func(t *testing.T) bool {
		t.Helper()
		// Bounded: this runs while a transaction holds the row, so a pool that
		// cannot hand out a second connection must fail the test rather than
		// hang the suite.
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var enabled bool
		if err := testPool.QueryRow(rctx, `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabled); err != nil {
			t.Fatalf("read from a separate session failed: %v", err)
		}
		return enabled
	}

	var sawDuringWrite bool
	committed, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool {
		// The row is written and locked at this point. Another session must
		// still see the old value.
		sawDuringWrite = readEnabled(t)
		return false
	})
	if err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}

	if committed {
		t.Error("an unconfirmed write must not report itself committed")
	}
	if !sawDuringWrite {
		t.Error("a staged disable leaked to another session before it was committed")
	}
	if !readEnabled(t) {
		t.Error("an abandoned write must leave the model enabled")
	}

	// The control: the same call commits when confirm holds, so the staging is
	// not swallowing legitimate writes.
	committed, err = repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true })
	if err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}
	if !committed {
		t.Error("a confirmed write must commit")
	}
	if readEnabled(t) {
		t.Error("a confirmed disable must be visible afterwards")
	}
}

// TestAutoRetireIfConfirmed_SurvivesReSighting pins the three states apart, and
// the whole reason auto_retired_at exists.
//
// enabled plus disabled_manually can express two kinds of disable, and there are
// three. An operator's must never be undone automatically. Discovery's SHOULD be
// undone by a re-sighting, because the model had vanished from the listing and
// its return is new evidence. A traffic retirement is neither: the model never
// left the listing — the provider was refusing it while still advertising it —
// so a sighting proves nothing, and reviving on one puts the model back into
// routing to fail, re-alert and churn failover groups on every scan.
//
// The discovery half is asserted alongside it, because "does not revive" is only
// correct if the mechanism it shares with discovery still revives what it should.
func TestAutoRetireIfConfirmed_SurvivesReSighting(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-autoretire-resighting")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	retiredID := insertTestModel(ctx, t, providerID, "traffic-retired-model")
	vanishedID := insertTestModel(ctx, t, providerID, "went-missing-model")

	readState := func(t *testing.T, id uuid.UUID) (enabled, manual bool, retired *time.Time) {
		t.Helper()
		if err := testPool.QueryRow(ctx,
			`SELECT enabled, disabled_manually, auto_retired_at FROM models WHERE id = $1`,
			id).Scan(&enabled, &manual, &retired); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		return enabled, manual, retired
	}

	// The proxy retires one model from traffic; discovery disables the other for
	// disappearing, which is what an unstamped automatic disable looks like.
	if committed, err := repo.AutoRetireIfConfirmed(ctx, retiredID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	} else if !committed {
		t.Fatal("the retirement should have committed")
	}
	if _, err := testPool.Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, vanishedID); err != nil {
		t.Fatalf("seed discovery disable: %v", err)
	}

	enabled, manual, retired := readState(t, retiredID)
	if enabled {
		t.Error("the retired model should be disabled")
	}
	if manual {
		t.Error("an automatic retirement must not be recorded as an operator's choice")
	}
	if retired == nil {
		t.Fatal("the retirement must be stamped, or nothing can tell it from discovery's")
	}

	// The provider lists both models again.
	for _, id := range []string{"traffic-retired-model", "went-missing-model"} {
		if err := repo.Upsert(ctx, newBareModel(providerID, id)); err != nil {
			t.Fatalf("Upsert %q failed: %v", id, err)
		}
	}

	if enabled, _, _ := readState(t, retiredID); enabled {
		t.Error("a re-sighting must not revive a model the provider refuses; it never left the listing")
	}
	if enabled, _, _ := readState(t, vanishedID); !enabled {
		t.Error("a model that came back after vanishing must be re-enabled, as it was before")
	}

	// An operator enabling by hand clears the retirement, which is how they tell
	// the gateway to trust the listing again.
	if _, err := repo.SetEnabled(ctx, retiredID, true); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	enabled, _, retired = readState(t, retiredID)
	if !enabled {
		t.Error("the operator's enable should stand")
	}
	if retired != nil {
		t.Error("an operator's enable must clear the retirement, not leave a stale stamp")
	}
}

// TestAutoRetireIfConfirmed_StandsDownOnceTheRowMovedOn covers the other half of
// the same window as RevertAutoRetire's condition.
//
// The retirement is decided on the request path and executed on a detached
// goroutine, so the row can change in between. Writing by id alone would
// overwrite an operator's own decision with a conclusion drawn from traffic that
// predates it — and the operator has no way to tell that happened, because the
// gateway's alert says exactly what it would have said anyway.
func TestAutoRetireIfConfirmed_StandsDownOnceTheRowMovedOn(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-autoretire-standdown")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	assertNotRetired := func(t *testing.T, id uuid.UUID) {
		t.Helper()
		var retired *time.Time
		if err := testPool.QueryRow(ctx,
			`SELECT auto_retired_at FROM models WHERE id = $1`, id).Scan(&retired); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if retired != nil {
			t.Error("a retirement that stood down must not stamp the row")
		}
	}

	t.Run("operator disabled it by hand first", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "operator-disabled")
		if _, err := repo.SetEnabled(ctx, id, false); err != nil {
			t.Fatalf("operator disable failed: %v", err)
		}

		confirmed := false
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool {
			confirmed = true
			return true
		})
		if err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		if committed {
			t.Error("a retirement must not overwrite an operator's disable")
		}
		if confirmed {
			t.Error("confirm must not run once the row has already moved on")
		}
		assertNotRetired(t, id)

		var manual bool
		if err := testPool.QueryRow(ctx,
			`SELECT disabled_manually FROM models WHERE id = $1`, id).Scan(&manual); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if !manual {
			t.Error("the operator's choice must survive intact")
		}
	})

	t.Run("already retired", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "already-retired")
		if _, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true }); err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true })
		if err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		if committed {
			t.Error("a model already retired must not be retired a second time")
		}
	})

	t.Run("deleted since the decision", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "deleted-model")
		if err := repo.DeleteByID(ctx, id); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true })
		if err != nil {
			t.Fatalf("a missing row is an outcome, not an error: %v", err)
		}
		if committed {
			t.Error("a deleted model cannot be retired")
		}
	})

	t.Run("an untouched model still retires", func(t *testing.T) {
		id := insertTestModel(ctx, t, providerID, "untouched-model")
		committed, err := repo.AutoRetireIfConfirmed(ctx, id, func() bool { return true })
		if err != nil {
			t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
		}
		if !committed {
			t.Fatal("the condition must not swallow a legitimate retirement")
		}
	})
}

// TestRevertAutoRetire_DoesNotOverwriteAnOperatorDisable covers the window
// between a retirement committing and the gateway undoing it because the model
// answered.
//
// The undo runs after the disable has committed, so anything can have happened
// in between — and the case that matters is an operator disabling the model by
// hand right then. An unconditional re-enable would silently put their disabled
// model back into routing, replacing a deliberate decision with a stale
// automatic one.
func TestRevertAutoRetire_DoesNotOverwriteAnOperatorDisable(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-revert-autoretire")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := insertTestModel(ctx, t, providerID, "contested-model")

	if _, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}

	// The control first: with the row untouched, the undo restores the model.
	reverted, err := repo.RevertAutoRetire(ctx, modelID)
	if err != nil {
		t.Fatalf("RevertAutoRetire failed: %v", err)
	}
	if !reverted {
		t.Fatal("an untouched retirement must be revertible")
	}

	// Retire again, then have an operator disable it by hand before the undo.
	if _, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}
	if _, err := repo.SetEnabled(ctx, modelID, false); err != nil {
		t.Fatalf("operator disable failed: %v", err)
	}

	reverted, err = repo.RevertAutoRetire(ctx, modelID)
	if err != nil {
		t.Fatalf("RevertAutoRetire failed: %v", err)
	}
	if reverted {
		t.Error("the undo must stand down once someone else owns the row's state")
	}

	var enabled, manual bool
	if err := testPool.QueryRow(ctx,
		`SELECT enabled, disabled_manually FROM models WHERE id = $1`, modelID).Scan(&enabled, &manual); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if enabled {
		t.Error("an operator's disabled model must not be returned to routing")
	}
	if !manual {
		t.Error("the operator's choice must survive intact")
	}
}

// TestUpsert_RetiredModelKeepsItsDismissal covers the interaction that decides
// whether a traffic-retired model can be silenced at all.
//
// A sighting normally clears an operator's dismissal, so a model that goes,
// comes back and goes again raises a fresh claim instead of staying suppressed
// by a stale stamp. A retired model breaks that assumption: it never left the
// listing, so it is sighted on EVERY scan. Clearing the stamp for it would mean
// the operator dismisses the claim, the next scan brings it straight back, and
// there is no way to stop it.
func TestUpsert_RetiredModelKeepsItsDismissal(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-retired-dismissal")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	retiredID := insertTestModel(ctx, t, providerID, "retired-model")
	vanishedID := insertTestModel(ctx, t, providerID, "vanished-model")

	if _, err := repo.AutoRetireIfConfirmed(ctx, retiredID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}
	// The other model is discovery-disabled, which is an automatic disable with
	// no retirement stamp.
	if _, err := testPool.Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, vanishedID); err != nil {
		t.Fatalf("seed discovery disable: %v", err)
	}

	// The operator dismisses both claims.
	if _, err := testPool.Exec(ctx,
		`UPDATE models SET discovery_dismissed_at = now() WHERE id = ANY($1)`,
		[]uuid.UUID{retiredID, vanishedID}); err != nil {
		t.Fatalf("seed dismissal: %v", err)
	}

	// The provider lists both again.
	for _, id := range []string{"retired-model", "vanished-model"} {
		if err := repo.Upsert(ctx, newBareModel(providerID, id)); err != nil {
			t.Fatalf("Upsert %q failed: %v", id, err)
		}
	}

	dismissed := func(t *testing.T, id uuid.UUID) bool {
		t.Helper()
		var at *time.Time
		if err := testPool.QueryRow(ctx,
			`SELECT discovery_dismissed_at FROM models WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatalf("read failed: %v", err)
		}
		return at != nil
	}

	if !dismissed(t, retiredID) {
		t.Error("a sighting must not un-dismiss a retired model: it is sighted on every scan, so the claim could never be silenced")
	}
	// The control: the existing behaviour for everything else is unchanged.
	if dismissed(t, vanishedID) {
		t.Error("a model that came back after vanishing must have its dismissal cleared, as before")
	}
}

// TestSetEnabled_ClearsADismissalSoAReRetirementIsVisible covers the way a
// claim could be lost permanently.
//
// Upsert keeps a retired model's dismissal, because it is sighted on every scan
// and would otherwise be impossible to silence. That makes clearing it the
// operator's enable's job, and the timing is the whole point: traffic reaches a
// re-enabled model in seconds while a discovery scan is roughly an hour away, so
// a model that fails again immediately gets its retirement stamp back BEFORE any
// sighting. If the enable had not already cleared the dismissal, the
// preserve-it rule would re-arm around a stamp nothing could ever clear, and the
// model would sit disabled and absent from the claim list for good. It fails
// safe for routing and silent for the operator, which is the worse half.
func TestSetEnabled_ClearsADismissalSoAReRetirementIsVisible(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-dismissal-recovery")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := insertTestModel(ctx, t, providerID, "twice-retired")

	// Retired by traffic, then dismissed by the operator: hidden, as intended.
	if _, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true }); err != nil {
		t.Fatalf("AutoRetireIfConfirmed failed: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE models SET discovery_dismissed_at = now() WHERE id = $1`, modelID); err != nil {
		t.Fatalf("seed dismissal: %v", err)
	}

	// The operator enables it by hand.
	if _, err := repo.SetEnabled(ctx, modelID, true); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	var dismissedAt *time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT discovery_dismissed_at FROM models WHERE id = $1`, modelID).Scan(&dismissedAt); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if dismissedAt != nil {
		t.Fatal("enabling by hand must clear the dismissal in the same write, not leave it to a scan that may arrive too late")
	}

	// It fails again before any scan, so it is retired a second time.
	if _, err := repo.AutoRetireIfConfirmed(ctx, modelID, func() bool { return true }); err != nil {
		t.Fatalf("second AutoRetireIfConfirmed failed: %v", err)
	}

	// The second retirement must be visible: disabled, not dismissed, so the
	// claim query surfaces it.
	var enabled bool
	if err := testPool.QueryRow(ctx,
		`SELECT enabled, discovery_dismissed_at FROM models WHERE id = $1`,
		modelID).Scan(&enabled, &dismissedAt); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if enabled {
		t.Error("the second retirement should have disabled the model")
	}
	if dismissedAt != nil {
		t.Error("a re-retired model must raise a fresh claim, not inherit the old dismissal and vanish")
	}
}

// TestAutoRetireIfConfirmed_DeadContextReportsNotCommitted pins the failure
// direction, which matters more here than for an ordinary write.
//
// The caller acts on the returned bool: a true tells the proxy its disable
// landed, so it announces the retirement and resizes failover groups around it.
// If a write that never reached the database reported itself committed, the
// gateway would publish a model retirement that did not happen.
func TestAutoRetireIfConfirmed_DeadContextReportsNotCommitted(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-deadctx")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	modelID := uuid.New()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "confirm-deadctx", "Confirm Dead Context Test"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	dead, cancel := context.WithCancel(ctx)
	cancel()

	confirmed := false
	committed, err := repo.AutoRetireIfConfirmed(dead, modelID, func() bool {
		confirmed = true
		return true
	})
	if err == nil {
		t.Fatal("a cancelled context must surface an error")
	}
	if committed {
		t.Error("a write that never reached the database must not report itself committed")
	}
	if confirmed {
		t.Error("confirm must not run once the write has already failed")
	}

	var enabled bool
	if err := testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabled); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !enabled {
		t.Error("a failed write must leave the model untouched")
	}
}

// ---------------------------------------------------------------------------
// TestDeleteByID
// ---------------------------------------------------------------------------

func TestRepository_SetEnabled_DisableThenVerify(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-setenabled-verify")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create a model
	modelID := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID, providerID, "setenabled-verify", "SetEnabled Verify")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Disable it
	updated, err := repo.SetEnabled(ctx, modelID, false)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	// Verify enabled=false
	if updated.Enabled {
		t.Error("model should be disabled after SetEnabled(false)")
	}

	// Verify in database
	var enabled bool
	err = testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabled)
	if err != nil {
		t.Fatalf("failed to query model: %v", err)
	}
	if enabled {
		t.Error("database should show enabled=false")
	}
}

// ---------------------------------------------------------------------------
// TestUpsert edge cases
// ---------------------------------------------------------------------------

func TestRepository_RecordMissingModels_WithProviderAndModel(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	providerID := insertTestProvider(ctx, t, "test-disable-missing-crud")
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })

	// Create two models
	modelID1 := uuid.New()
	modelID2 := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID1, providerID, "keep-this-model", "Keep This Model")
	if err != nil {
		t.Fatalf("insert model1 failed: %v", err)
	}
	_, err = testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, created_at)
		VALUES ($1, $2, $3, $4, true, now())
	`, modelID2, providerID, "remove-this-model", "Remove This Model")
	if err != nil {
		t.Fatalf("insert model2 failed: %v", err)
	}

	// Two consecutive scans missing modelID2 disable it: the first records a
	// pending miss, the second reaches MissingScanThreshold.
	disabled, pending, err := repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"keep-this-model"})
	if err != nil {
		t.Fatalf("RecordMissingModels first scan failed: %v", err)
	}
	if len(disabled) != 0 || len(pending) != 1 || pending[0].ModelID != "remove-this-model" {
		t.Errorf("expected pending ref for remove-this-model, got disabled=%v pending=%v", disabled, pending)
	}
	disabled, _, err = repo.RecordMissingModels(ctx, providerID, "test-provider", []string{"keep-this-model"})
	if err != nil {
		t.Fatalf("RecordMissingModels second scan failed: %v", err)
	}
	if len(disabled) != 1 || disabled[0].ModelID != "remove-this-model" || disabled[0].ID != modelID2 {
		t.Errorf("expected single disabled ref for remove-this-model (%s), got %v", modelID2, disabled)
	}

	// Verify modelID1 is still enabled
	var enabled1 bool
	err = testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID1).Scan(&enabled1)
	if err != nil {
		t.Fatalf("failed to query model1: %v", err)
	}
	if !enabled1 {
		t.Error("model1 should still be enabled")
	}

	// Verify modelID2 is now disabled
	var enabled2 bool
	err = testPool.QueryRow(ctx, `SELECT enabled FROM models WHERE id = $1`, modelID2).Scan(&enabled2)
	if err != nil {
		t.Fatalf("failed to query model2: %v", err)
	}
	if enabled2 {
		t.Error("model2 should be disabled after two consecutive missing scans")
	}
}

// ---------------------------------------------------------------------------
// Cancelled context error path tests
// ---------------------------------------------------------------------------

func TestRecordMissingModels_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := repo.RecordMissingModels(ctx, uuid.New(), "test-provider", []string{"some-model"})
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

func TestSetEnabled_CancelledContext(t *testing.T) {
	repo := NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.SetEnabled(ctx, uuid.New(), false)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
