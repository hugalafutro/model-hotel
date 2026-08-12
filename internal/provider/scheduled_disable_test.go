package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// OptionalDate
// ---------------------------------------------------------------------------

func TestOptionalDate_UnmarshalJSON(t *testing.T) {
	type body struct {
		Sched OptionalDate `json:"scheduled_disable_on"`
	}

	t.Run("absent field leaves Set false", func(t *testing.T) {
		var b body
		if err := json.Unmarshal([]byte(`{}`), &b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if b.Sched.Set {
			t.Error("Set should be false when the field is absent")
		}
	})

	t.Run("explicit null sets Set with nil Value", func(t *testing.T) {
		var b body
		if err := json.Unmarshal([]byte(`{"scheduled_disable_on":null}`), &b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !b.Sched.Set {
			t.Error("Set should be true for an explicit null")
		}
		if b.Sched.Value != nil {
			t.Errorf("Value = %v, want nil", *b.Sched.Value)
		}
	})

	t.Run("string value sets Set and Value", func(t *testing.T) {
		var b body
		if err := json.Unmarshal([]byte(`{"scheduled_disable_on":"2027-03-01"}`), &b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !b.Sched.Set || b.Sched.Value == nil || *b.Sched.Value != "2027-03-01" {
			t.Errorf("got Set=%v Value=%v, want Set=true Value=2027-03-01", b.Sched.Set, b.Sched.Value)
		}
	})

	t.Run("non-string value errors", func(t *testing.T) {
		var b body
		if err := json.Unmarshal([]byte(`{"scheduled_disable_on":42}`), &b); err == nil {
			t.Error("expected error for a non-string value")
		}
	})
}

// ---------------------------------------------------------------------------
// Repository.Update scheduled_disable_on CASE arms
// ---------------------------------------------------------------------------

func createScheduledTestProvider(t *testing.T, repo *Repository) *Provider {
	t.Helper()
	p, err := repo.Create(context.Background(), CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://sched.example.com", APIKey: "sk-sched",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return p
}

func TestRepository_Update_ScheduledDisableLifecycle(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	p := createScheduledTestProvider(t, repo)

	future := time.Now().AddDate(0, 0, 7).Format("2006-01-02")

	// Set a schedule on an enabled provider.
	updated, err := repo.Update(ctx, p.ID, UpdateProviderRequest{
		ScheduledDisableOn: OptionalDate{Set: true, Value: &future},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update(set): %v", err)
	}
	if updated.ScheduledDisableOn == nil || updated.ScheduledDisableOn.Format("2006-01-02") != future {
		t.Fatalf("ScheduledDisableOn = %v, want %s", updated.ScheduledDisableOn, future)
	}

	// An update that does not mention the field keeps the stored value.
	name := "kept-" + uuid.New().String()[:8]
	updated, err = repo.Update(ctx, p.ID, UpdateProviderRequest{Name: &name}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update(absent): %v", err)
	}
	if updated.ScheduledDisableOn == nil || updated.ScheduledDisableOn.Format("2006-01-02") != future {
		t.Errorf("absent field changed the schedule: %v", updated.ScheduledDisableOn)
	}

	// An explicit null clears it.
	updated, err = repo.Update(ctx, p.ID, UpdateProviderRequest{
		ScheduledDisableOn: OptionalDate{Set: true, Value: nil},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update(null): %v", err)
	}
	if updated.ScheduledDisableOn != nil {
		t.Errorf("explicit null left schedule %v", updated.ScheduledDisableOn)
	}
}

func TestRepository_Update_DisablingForcesScheduleNull(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	p := createScheduledTestProvider(t, repo)

	future := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	disabled := false

	// Disabling wins even when the same request tries to set a schedule: the
	// resulting enabled state is false, so the CASE's first arm forces NULL.
	updated, err := repo.Update(ctx, p.ID, UpdateProviderRequest{
		Enabled:            &disabled,
		ScheduledDisableOn: OptionalDate{Set: true, Value: &future},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update(disable+set): %v", err)
	}
	if updated.Enabled {
		t.Fatal("provider should be disabled")
	}
	if updated.ScheduledDisableOn != nil {
		t.Errorf("disable should force the schedule NULL, got %v", updated.ScheduledDisableOn)
	}

	// Setting a schedule on an already-disabled provider (enabled untouched)
	// also lands on the first arm: the RESULTING state is still disabled.
	updated, err = repo.Update(ctx, p.ID, UpdateProviderRequest{
		ScheduledDisableOn: OptionalDate{Set: true, Value: &future},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update(set-on-disabled): %v", err)
	}
	if updated.ScheduledDisableOn != nil {
		t.Errorf("schedule on a disabled provider should stay NULL, got %v", updated.ScheduledDisableOn)
	}
}

// ---------------------------------------------------------------------------
// Repository.DisableDueScheduled
// ---------------------------------------------------------------------------

func setScheduleSQL(t *testing.T, id uuid.UUID, enabled bool, date string) {
	t.Helper()
	// Written directly because Update deliberately refuses to persist a
	// schedule on a disabled provider and never accepts a past date.
	_, err := testDB.Pool().Exec(context.Background(),
		`UPDATE providers SET enabled = $1, scheduled_disable_on = $2::date WHERE id = $3`,
		enabled, date, id)
	if err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
}

func TestRepository_DisableDueScheduled(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	future := time.Now().AddDate(0, 0, 7).Format("2006-01-02")

	due := createScheduledTestProvider(t, repo)
	notYet := createScheduledTestProvider(t, repo)
	alreadyOff := createScheduledTestProvider(t, repo)
	setScheduleSQL(t, due.ID, true, today)
	setScheduleSQL(t, notYet.ID, true, future)
	setScheduleSQL(t, alreadyOff.ID, false, yesterday)

	disabled, err := repo.DisableDueScheduled(ctx)
	if err != nil {
		t.Fatalf("DisableDueScheduled: %v", err)
	}

	// Other tests share the table but never schedule a disable, so the due
	// provider is found by ID rather than asserting on the slice length.
	var swept *Provider
	for _, p := range disabled {
		if p.ID == due.ID {
			swept = p
		}
		if p.ID == notYet.ID || p.ID == alreadyOff.ID {
			t.Errorf("swept provider %s that was not due", p.Name)
		}
	}
	if swept == nil {
		t.Fatal("due provider missing from the sweep result")
	}
	if swept.Enabled {
		t.Error("swept provider should be returned disabled")
	}
	if swept.ScheduledDisableOn != nil {
		t.Errorf("sweep should clear the schedule, got %v", swept.ScheduledDisableOn)
	}

	// Persisted state: due is off with no schedule; the future one is
	// untouched; the already-disabled one keeps its stale schedule (the
	// sweep only ever touches enabled rows).
	got, err := repo.Get(ctx, due.ID)
	if err != nil {
		t.Fatalf("Get(due): %v", err)
	}
	if got.Enabled || got.ScheduledDisableOn != nil {
		t.Errorf("due provider persisted as enabled=%v sched=%v", got.Enabled, got.ScheduledDisableOn)
	}
	got, err = repo.Get(ctx, notYet.ID)
	if err != nil {
		t.Fatalf("Get(notYet): %v", err)
	}
	if !got.Enabled || got.ScheduledDisableOn == nil {
		t.Errorf("future-scheduled provider changed: enabled=%v sched=%v", got.Enabled, got.ScheduledDisableOn)
	}
	got, err = repo.Get(ctx, alreadyOff.ID)
	if err != nil {
		t.Fatalf("Get(alreadyOff): %v", err)
	}
	if got.Enabled || got.ScheduledDisableOn == nil {
		t.Errorf("already-disabled provider changed: enabled=%v sched=%v", got.Enabled, got.ScheduledDisableOn)
	}

	// A second sweep finds nothing new for these providers.
	again, err := repo.DisableDueScheduled(ctx)
	if err != nil {
		t.Fatalf("DisableDueScheduled(second): %v", err)
	}
	for _, p := range again {
		if p.ID == due.ID {
			t.Error("second sweep re-disabled the same provider")
		}
	}
}

// ---------------------------------------------------------------------------
// ToResponse scheduled_disable_on formatting
// ---------------------------------------------------------------------------

func TestToResponse_ScheduledDisableOn(t *testing.T) {
	day := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	p := &Provider{ID: uuid.New(), Name: "x", BaseURL: "https://x", ScheduledDisableOn: &day}
	resp := ToResponse(p)
	if resp.ScheduledDisableOn == nil || *resp.ScheduledDisableOn != "2027-03-01" {
		t.Errorf("ScheduledDisableOn = %v, want 2027-03-01", resp.ScheduledDisableOn)
	}

	p.ScheduledDisableOn = nil
	resp = ToResponse(p)
	if resp.ScheduledDisableOn != nil {
		t.Errorf("nil schedule should stay nil, got %v", *resp.ScheduledDisableOn)
	}
}
