package api

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// TestClampClaimAlertDays pins the one thing that can silently kill this alert:
// a threshold at or above ClaimWindow. A gone model stops counting once it is
// older than the window, so such a threshold could never be crossed by a
// COUNTED claim and the operator would have configured a warning that can
// never arrive. Everything is expressed against the constants, so shortening
// ClaimWindow moves the ceiling instead of breaking the test.
func TestClampClaimAlertDays(t *testing.T) {
	cases := []struct {
		name        string
		in          int
		wantDays    int
		wantClamped bool
	}{
		{"unset falls back to the default", 0, DefaultClaimAlertDays, false},
		{"negative falls back to the default", -5, DefaultClaimAlertDays, false},
		{"a value inside the window is honoured", 3, 3, false},
		{"the ceiling itself is honoured", MaxClaimAlertDays, MaxClaimAlertDays, false},
		{"exactly the claim window is clamped", ClaimWindowDays, MaxClaimAlertDays, true},
		{"beyond the claim window is clamped", ClaimWindowDays * 4, MaxClaimAlertDays, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotDays, gotClamped := clampClaimAlertDays(tc.in)
			if gotDays != tc.wantDays || gotClamped != tc.wantClamped {
				t.Fatalf("clampClaimAlertDays(%d) = (%d, %t), want (%d, %t)",
					tc.in, gotDays, gotClamped, tc.wantDays, tc.wantClamped)
			}
		})
	}

	// The property the cases above exist to protect: whatever comes out must be
	// strictly less than the window, in the same duration units the evaluation
	// compares in. A ceiling equal to the window would make the firing window
	// measure-zero rather than merely narrow.
	for _, in := range []int{0, 1, MaxClaimAlertDays, ClaimWindowDays, 10 * ClaimWindowDays} {
		days, _ := clampClaimAlertDays(in)
		if time.Duration(days)*24*time.Hour >= ClaimWindow {
			t.Fatalf("clampClaimAlertDays(%d) = %d days, which is not below ClaimWindow (%d days)",
				in, days, ClaimWindowDays)
		}
	}
}

// captureClaimAlerts subscribes to the events bus and returns a drain function
// that reports how many outstanding-claims events arrived, plus the last one.
func captureClaimAlerts(t *testing.T) func() (int, events.Event) {
	t.Helper()
	ch := events.DefaultBus.Subscribe()
	t.Cleanup(func() { events.DefaultBus.Unsubscribe(ch) })
	return func() (int, events.Event) {
		// The bus delivers asynchronously; give a published event time to land
		// before concluding that none did.
		deadline := time.After(2 * time.Second)
		n := 0
		var last events.Event
		for {
			select {
			case ev := <-ch:
				if ev.Type == EventTypeClaimsOutstanding {
					n++
					last = ev
				}
			case <-deadline:
				return n, last
			}
		}
	}
}

// seedGoneModel inserts a discovery-disabled model last seen at the given
// instant. The timestamp is supplied by the caller rather than computed from
// the database's now(): the evaluation ages claims against the `now` it is
// handed, and a database clock even microseconds ahead of the Go one turns an
// exact "12 days" into 11 whole days after integer division.
func seedGoneModel(t *testing.T, providerID uuid.UUID, modelID string, lastSeenAt time.Time) {
	t.Helper()
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, enabled, disabled_manually, missing_scans, last_seen_at)
		 VALUES ($1, $2, $3, false, false, 0, $4)`,
		uuid.New(), providerID, modelID, lastSeenAt); err != nil {
		t.Fatalf("seed gone model %s: %v", modelID, err)
	}
}

// TestEvaluateClaimAgeAlert_FiresOnceOnCrossing is the behavioural core: the
// alert must fire when the OLDEST counted claim crosses the threshold, must not
// fire again on the next discovery scan while it stays crossed, and must re-arm
// once nothing is crossed any more.
func TestEvaluateClaimAgeAlert_FiresOnceOnCrossing(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()

	prov := seedClaimProvider(t, pool, "NanoGPT", true)
	quiet := seedClaimProvider(t, pool, "Alpha", true)
	// Threshold is the default 7 days. The 12-day claim is the one that decides;
	// the 1-day claim proves the evaluation looks at the OLDEST, not the newest.
	seedGoneModel(t, prov, "long-gone", now.Add(-12*24*time.Hour))
	seedGoneModel(t, prov, "just-gone", now.Add(-24*time.Hour))
	seedGoneModel(t, quiet, "also-gone", now.Add(-2*24*time.Hour))

	drain := captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
		t.Fatalf("first evaluation: %v", err)
	}
	n, ev := drain()
	if n != 1 {
		t.Fatalf("crossing the threshold published %d alerts, want exactly 1", n)
	}

	// Payload: routing metadata only, and enough of it to act on.
	if got := ev.Metadata["claim_count"]; got != 3 {
		t.Errorf("claim_count = %v, want 3", got)
	}
	if got := ev.Metadata["oldest_age_days"]; got != 12 {
		t.Errorf("oldest_age_days = %v, want 12 (the oldest claim, not the newest)", got)
	}
	if got := ev.Metadata["threshold_days"]; got != DefaultClaimAlertDays {
		t.Errorf("threshold_days = %v, want %d", got, DefaultClaimAlertDays)
	}
	worst, ok := ev.Metadata["worst_providers"].([]map[string]any)
	if !ok || len(worst) != 2 {
		t.Fatalf("worst_providers = %#v, want 2 entries", ev.Metadata["worst_providers"])
	}
	if worst[0]["provider"] != "NanoGPT" || worst[0]["gone"] != 2 {
		t.Errorf("worst_providers[0] = %#v, want the provider with the most claims first", worst[0])
	}
	// No model identifiers anywhere in the payload: the alert names counts and
	// providers, never the individual rows.
	for _, k := range []string{"model_id", "models", "provider", "provider_id"} {
		if _, present := ev.Metadata[k]; present {
			t.Errorf("payload must not carry %q: it is either per-model detail or a dispatcher debounce key", k)
		}
	}

	// The latch is what stops a re-fire on every subsequent scan.
	if v := h.settingsRepo.GetWithDefault(ctx, settingKeyClaimAlertFired, ""); v == "" {
		t.Fatal("crossing the threshold must persist the edge latch")
	}

	drain2 := captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now.Add(time.Hour)); err != nil {
		t.Fatalf("second evaluation: %v", err)
	}
	if n, _ := drain2(); n != 0 {
		t.Fatalf("a still-crossed threshold published %d alerts on the next scan, want 0", n)
	}

	// Resolve every claim: the models come back, so nothing is counted and the
	// latch must be released, ready to fire again on the next genuine crossing.
	if _, err := pool.Exec(ctx, `UPDATE models SET enabled = true, last_seen_at = now()`); err != nil {
		t.Fatalf("resolve claims: %v", err)
	}
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("third evaluation: %v", err)
	}
	if v := h.settingsRepo.GetWithDefault(ctx, settingKeyClaimAlertFired, ""); v != "" {
		t.Fatalf("the latch must be released once nothing is crossed, still %q", v)
	}
}

// TestEvaluateClaimAgeAlert_QuietBelowThreshold anchors the test above: without
// it, an evaluation that fired unconditionally on any claim at all would pass
// every assertion there. It also pins the clamp end-to-end, by configuring a
// threshold the settings validator would reject and checking the effective
// value is the ceiling rather than the configured one.
func TestEvaluateClaimAgeAlert_QuietBelowThreshold(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()

	prov := seedClaimProvider(t, pool, "NanoGPT", true)
	// Older than the shipped default of 7 days, so this only stays quiet
	// because the configured threshold is honoured.
	seedGoneModel(t, prov, "recently-gone", now.Add(-10*24*time.Hour))

	if err := h.settingsRepo.Set(ctx, SettingKeyClaimAlertDays, strconv.Itoa(MaxClaimAlertDays)); err != nil {
		t.Fatalf("set threshold: %v", err)
	}
	drain := captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if n, _ := drain(); n != 0 {
		t.Fatalf("a claim younger than the threshold published %d alerts, want 0", n)
	}

	// A threshold at the claim window is clamped to the ceiling, which is still
	// above this 10-day claim, so nothing fires. If the clamp instead fell back
	// to the 7-day default, this claim WOULD cross and the alert would fire.
	if err := h.settingsRepo.Set(ctx, SettingKeyClaimAlertDays, strconv.Itoa(ClaimWindowDays*3)); err != nil {
		t.Fatalf("set out-of-range threshold: %v", err)
	}
	drain = captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
		t.Fatalf("evaluate with an out-of-range threshold: %v", err)
	}
	if n, _ := drain(); n != 0 {
		t.Fatalf("an out-of-range threshold must clamp to the ceiling, not to the default; published %d alerts", n)
	}
}

// TestEvaluateClaimAgeAlert_CountsDisabledFailoverGroups pins that a group
// discovery disabled ages the alert too. A disabled group is a counted claim in
// the badge (it means hotel/<model> routing is dead), so an instance whose only
// discrepancy is a group must still get the nudge.
func TestEvaluateClaimAgeAlert_CountsDisabledFailoverGroups(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()

	if _, err := pool.Exec(ctx,
		`INSERT INTO model_failover_groups (id, display_model, priority_order, group_enabled, auto_disabled_at)
		 VALUES ($1, 'claim-alert-group', '[]'::jsonb, false, $2)`,
		uuid.New(), now.Add(-20*24*time.Hour)); err != nil {
		t.Fatalf("seed group: %v", err)
	}

	drain := captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	n, ev := drain()
	if n != 1 {
		t.Fatalf("a long-disabled failover group published %d alerts, want 1", n)
	}
	if got := ev.Metadata["group_claims"]; got != 1 {
		t.Errorf("group_claims = %v, want 1", got)
	}
	if got := ev.Metadata["oldest_age_days"]; got != 20 {
		t.Errorf("oldest_age_days = %v, want 20 (aged from auto_disabled_at)", got)
	}
}
