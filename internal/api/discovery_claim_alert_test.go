package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
		// Bus.Publish delivers into the subscriber's buffered channel
		// synchronously, so an event published by the call under test is
		// already queued by the time it returns. The wait is slack against a
		// slow scheduler, not against asynchronous delivery, which is why it can
		// be short: a "published nothing" assertion pays it in full.
		deadline := time.After(500 * time.Millisecond)
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

	// The message is the entire operator-facing artifact of this feature, so it
	// is asserted whole rather than by substring. The fixture is exactly the
	// case a looser wording gets wrong: only ONE of these three claims is over
	// the 7-day threshold, so the total must not be attached to the threshold.
	wantMsg := "3 model discrepancies outstanding. The oldest has been unaddressed for 12 days " +
		"(threshold: 7 days). Worst: NanoGPT (2), Alpha (1)."
	if ev.Message != wantMsg {
		t.Errorf("message =\n  %q\nwant\n  %q", ev.Message, wantMsg)
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

// TestEvaluateClaimAgeAlert_RefiresWhenTheBacklogGrows pins that the latch
// cannot mute the alert permanently.
//
// A disabled failover group never ages out (listGroupClaims has no window
// filter), so an operator who accepts that one hotel/<model> is dead holds the
// crossed condition true forever. With a boolean latch that state would swallow
// every future alert, including a brand-new backlog. The latch therefore
// carries the count it fired at, rises only by firing, and falls silently when
// the situation improves.
//
// The second round trip is the half that a plain high-water mark fails: after
// firing at 4 and being fixed back down to 1, an identical new backlog reaches
// 4 again and is not "greater than" 4.
func TestEvaluateClaimAgeAlert_RefiresWhenTheBacklogGrows(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()
	prov := seedClaimProvider(t, pool, "NanoGPT", true)

	// The permanent claim: a group disabled well past ClaimWindow, which no
	// amount of waiting will retire.
	if _, err := pool.Exec(ctx,
		`INSERT INTO model_failover_groups (id, display_model, priority_order, group_enabled, auto_disabled_at)
		 VALUES ($1, 'permanently-dead', '[]'::jsonb, false, $2)`,
		uuid.New(), now.Add(-3*ClaimWindow)); err != nil {
		t.Fatalf("seed permanent group claim: %v", err)
	}

	step := func(label string, wantAlerts int) events.Event {
		t.Helper()
		drain := captureClaimAlerts(t)
		if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		n, ev := drain()
		if n != wantAlerts {
			t.Fatalf("%s: published %d alerts, want %d", label, n, wantAlerts)
		}
		return ev
	}

	step("first crossing", 1)
	step("unchanged situation", 0)

	// A real backlog arrives on top of the accepted group.
	for _, id := range []string{"gone-a", "gone-b", "gone-c"} {
		seedGoneModel(t, prov, id, now.Add(-10*24*time.Hour))
	}
	ev := step("backlog arrives", 1)
	if got := ev.Metadata["claim_count"]; got != 4 {
		t.Errorf("claim_count = %v, want 4 (3 models plus the group)", got)
	}
	step("backlog unchanged", 0)

	// The operator fixes the models. The group remains, so nothing re-arms the
	// latch, but the level they were told about must come down with the count.
	if _, err := pool.Exec(ctx, `UPDATE models SET enabled = true, last_seen_at = $1`, now); err != nil {
		t.Fatalf("resolve backlog: %v", err)
	}
	step("backlog resolved, group remains", 0)

	// An identically sized second backlog. A high-water latch would still read
	// 4 here and stay silent forever.
	for _, id := range []string{"gone-d", "gone-e", "gone-f"} {
		seedGoneModel(t, prov, id, now.Add(-10*24*time.Hour))
	}
	step("second backlog arrives", 1)
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

// TestParseClaimAlertLatch pins the two-value contract, and in particular the
// direction an unreadable value degrades in.
//
// The second return is "is a latch set at all", and it is what decides between
// firing and staying quiet. For a value that does not unmarshal, reporting
// "not latched" would re-fire on every scan forever, and reporting a latch with
// a huge count would mute the alert forever. Reporting "latched at zero" makes
// the next crossed evaluation fire exactly once and rewrite the value in the
// current shape: one duplicate notification, then self-healed.
func TestParseClaimAlertLatch(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantLatch claimAlertLatch
		wantSet   bool
	}{
		{"unset is not latched", "", claimAlertLatch{}, false},
		{"blank is not latched", "   \n", claimAlertLatch{}, false},
		{"a well-formed latch round-trips", `{"fired_at":"2026-08-01T00:00:00Z","count":4}`,
			claimAlertLatch{FiredAt: "2026-08-01T00:00:00Z", Count: 4}, true},
		{"unreadable JSON latches at zero", `{"count":`, claimAlertLatch{}, true},
		{"a non-object latches at zero", `"7"`, claimAlertLatch{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLatch, gotSet := parseClaimAlertLatch(tc.raw)
			if gotSet != tc.wantSet {
				t.Fatalf("parseClaimAlertLatch(%q) latched = %t, want %t", tc.raw, gotSet, tc.wantSet)
			}
			if gotLatch != tc.wantLatch {
				t.Fatalf("parseClaimAlertLatch(%q) = %+v, want %+v", tc.raw, gotLatch, tc.wantLatch)
			}
		})
	}

	// The property the "latches at zero" rows exist for: a corrupt value must
	// never behave like a latch that can mute a crossing. Count 0 is below any
	// live total, so `total() > latch.Count` still fires.
	l, latched := parseClaimAlertLatch(`{"count":`)
	if !latched || l.Count != 0 {
		t.Fatalf("corrupt latch = (%+v, %t), want a set latch at count 0", l, latched)
	}
}

// TestEvaluateClaimAgeAlert_NamesAtMostThreeWorstProviders pins the payload cap.
//
// The alert is delivered as a push notification, so the provider list exists to
// aim the operator at the worst offender, not to reproduce the modal. An
// instance with real history carries claims on many providers at once; naming
// all of them makes the notification unreadable and, on some transports,
// truncated at an arbitrary point. Providers with no counted claim must be
// absent entirely rather than padding the list with zeroes.
func TestEvaluateClaimAgeAlert_NamesAtMostThreeWorstProviders(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()

	// Four providers with 4, 3, 2 and 1 counted claims, all well past the
	// default 7-day threshold, plus one whose models are merely suspect and
	// therefore counted nowhere.
	for _, p := range []struct {
		name string
		gone int
	}{{"Delta", 4}, {"Charlie", 3}, {"Bravo", 2}, {"Alfa", 1}} {
		id := seedClaimProvider(t, pool, p.name, true)
		for i := range p.gone {
			seedGoneModel(t, id, fmt.Sprintf("%s-gone-%d", p.name, i), now.Add(-10*24*time.Hour))
		}
	}
	suspectOnly := seedClaimProvider(t, pool, "Echo", true)
	seedClaimModel(t, pool, suspectOnly, "echo-wobbling", true, false, 1, nil)

	drain := captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	n, ev := drain()
	if n != 1 {
		t.Fatalf("published %d alerts, want 1", n)
	}
	// Sanity on the fixture: every gone model is counted, and the suspect one is
	// not. Without this the assertions below could pass on a truncated summary.
	if got := ev.Metadata["claim_count"]; got != 10 {
		t.Fatalf("claim_count = %v, want 10 (4+3+2+1 gone models, the suspect one excluded)", got)
	}

	worst, ok := ev.Metadata["worst_providers"].([]map[string]any)
	if !ok {
		t.Fatalf("worst_providers = %#v, want []map[string]any", ev.Metadata["worst_providers"])
	}
	if len(worst) != claimAlertWorstProviders {
		t.Fatalf("worst_providers named %d providers, want at most %d", len(worst), claimAlertWorstProviders)
	}
	wantNames := []string{"Delta", "Charlie", "Bravo"}
	wantCounts := []int{4, 3, 2}
	for i := range worst {
		if worst[i]["provider"] != wantNames[i] || worst[i]["gone"] != wantCounts[i] {
			t.Errorf("worst_providers[%d] = %#v, want %s (%d)", i, worst[i], wantNames[i], wantCounts[i])
		}
	}
	// Explicit: the provider that was dropped by the cap and the one with no
	// counted claims are both absent, not merely late in the list.
	for _, entry := range worst {
		if entry["provider"] == "Alfa" || entry["provider"] == "Echo" {
			t.Errorf("worst_providers must not contain %v", entry["provider"])
		}
	}
}

// TestEvaluateClaimAgeAlert_StaysSilentWhenTheDatabaseFails pins that a
// half-derived picture never becomes an alert.
//
// summarizeCountedClaims runs three queries and the total it produces is what
// the operator is told. If a failure were swallowed the evaluation would
// compare a zero or partial total against the threshold: it would either fire
// with a wrong count or, worse, look "not crossed" and delete the latch,
// re-arming the alert to fire again about a backlog nobody fixed. The error
// goes back to the discovery run, which logs it as housekeeping.
func TestEvaluateClaimAgeAlert_StaysSilentWhenTheDatabaseFails(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()

	prov := seedClaimProvider(t, pool, "db-down-prov", true)
	seedGoneModel(t, prov, "long-gone", now.Add(-12*24*time.Hour))

	drain := captureClaimAlerts(t)
	err := EvaluateClaimAgeAlert(ctx, closedAPIPool(t).Pool(), h.settingsRepo, now)
	if err == nil {
		t.Fatal("a failed claim derivation must be reported, got nil")
	}
	if n, _ := drain(); n != 0 {
		t.Errorf("published %d alerts on a failed derivation, want 0", n)
	}
	if v := h.settingsRepo.GetWithDefault(ctx, settingKeyClaimAlertFired, ""); v != "" {
		t.Errorf("a failed derivation must not touch the latch, got %q", v)
	}

	// Anchor: the same fixture and the same store DO fire once the pool works,
	// so the silence above is the failure path and not an unfireable fixture.
	drain = captureClaimAlerts(t)
	if err := EvaluateClaimAgeAlert(ctx, pool, h.settingsRepo, now); err != nil {
		t.Fatalf("evaluate against a healthy pool: %v", err)
	}
	if n, _ := drain(); n != 1 {
		t.Fatalf("healthy-pool evaluation published %d alerts, want 1", n)
	}
}

// TestEvaluateClaimAgeAlert_LatchPersistenceFailuresAreReported pins that every
// latch write is load-bearing enough to fail the evaluation, and that the
// publish-before-latch ordering survives a write failure.
//
// The latch is the entire edge-trigger mechanism. A silently dropped write
// means the alert either re-fires on every scan or never lowers its bar again,
// and the caller has no way to notice. Each of the three writes therefore
// reports its own failure, wrapped so the discovery log says which one.
//
// The firing case additionally pins the ordering the implementation comment
// commits to: the event is published BEFORE the latch is stored, so a failed
// latch costs a duplicate notification later rather than a lost warning now.
func TestEvaluateClaimAgeAlert_LatchPersistenceFailuresAreReported(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	now := time.Now()

	prov := seedClaimProvider(t, pool, "latch-fail-prov", true)
	// Two counted claims, both well past the default threshold.
	seedGoneModel(t, prov, "gone-one", now.Add(-12*24*time.Hour))
	seedGoneModel(t, prov, "gone-two", now.Add(-11*24*time.Hour))

	writeErr := errors.New("settings write refused")

	t.Run("a failed firing latch still notifies", func(t *testing.T) {
		store := &mockSettingsStore{
			getWithDefaultFn: func(context.Context, string, string) string { return "" },
			setFn:            func(context.Context, string, string) error { return writeErr },
		}
		drain := captureClaimAlerts(t)
		err := EvaluateClaimAgeAlert(ctx, pool, store, now)
		if !errors.Is(err, writeErr) {
			t.Fatalf("error = %v, want it to wrap the store failure", err)
		}
		if !strings.Contains(err.Error(), "latch outstanding-claims alert") {
			t.Errorf("error = %q, want it to name the failed write", err)
		}
		if n, _ := drain(); n != 1 {
			t.Errorf("published %d alerts, want 1: the event must be out before the latch is written", n)
		}
	})

	t.Run("a failed lowering of the latch is reported", func(t *testing.T) {
		// Latched at 5, live total is 2: still crossed, so nothing new fires,
		// but the level the operator was told about has to come down.
		store := &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, def string) string {
				if key == settingKeyClaimAlertFired {
					return `{"fired_at":"2026-08-01T00:00:00Z","count":5}`
				}
				return def
			},
			setFn: func(context.Context, string, string) error { return writeErr },
		}
		drain := captureClaimAlerts(t)
		err := EvaluateClaimAgeAlert(ctx, pool, store, now)
		if !errors.Is(err, writeErr) {
			t.Fatalf("error = %v, want it to wrap the store failure", err)
		}
		if !strings.Contains(err.Error(), "lower outstanding-claims alert latch") {
			t.Errorf("error = %q, want it to name the lowering write", err)
		}
		if n, _ := drain(); n != 0 {
			t.Errorf("published %d alerts, want 0: an improving situation is lowered silently", n)
		}
	})

	t.Run("a failed re-arm is reported", func(t *testing.T) {
		// Nothing counted at all (an empty pool state is simulated by resolving
		// every claim), so the latch must be deleted to re-arm the alert.
		if _, err := pool.Exec(ctx, `UPDATE models SET enabled = true, last_seen_at = $1`, now); err != nil {
			t.Fatalf("resolve claims: %v", err)
		}
		store := &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, def string) string {
				if key == settingKeyClaimAlertFired {
					return `{"fired_at":"2026-08-01T00:00:00Z","count":2}`
				}
				return def
			},
			deleteKeyFn: func(context.Context, string) error { return writeErr },
		}
		drain := captureClaimAlerts(t)
		err := EvaluateClaimAgeAlert(ctx, pool, store, now)
		if !errors.Is(err, writeErr) {
			t.Fatalf("error = %v, want it to wrap the store failure", err)
		}
		if !strings.Contains(err.Error(), "re-arm outstanding-claims alert") {
			t.Errorf("error = %q, want it to name the re-arm delete", err)
		}
		if n, _ := drain(); n != 0 {
			t.Errorf("published %d alerts while re-arming, want 0", n)
		}
	})
}
