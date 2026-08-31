package frontdesk

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestComputeFleetState(t *testing.T) {
	up := memberFleetFacts{Known: true, Healthy: true}
	down := memberFleetFacts{Known: true, Healthy: false}
	// heldOf is a hold judged against the build the primary is running now, the
	// ordinary case. staleHeldOf is one carried over from a previous primary
	// build, which a rolling rebuild always produces and which must not be part
	// of the evidence for an all_sync_held escalation.
	heldOf := func(f memberFleetFacts) memberFleetFacts {
		f.Syncable, f.Held, f.HeldCurrent = true, true, true
		return f
	}
	staleHeldOf := func(f memberFleetFacts) memberFleetFacts {
		f.Syncable, f.Held, f.HeldCurrent = true, true, false
		return f
	}
	incompleteOf := func(f memberFleetFacts) memberFleetFacts { f.Syncable = true; f.Incomplete = true; return f }
	syncable := func(f memberFleetFacts) memberFleetFacts { f.Syncable = true; return f }
	drainedOf := func(f memberFleetFacts) memberFleetFacts { f.Drained = true; return f }

	cases := []struct {
		name        string
		in          fleetStateInput
		wantState   FleetState
		wantReasons []string
	}{
		{"empty fleet is ok", fleetStateInput{}, FleetOK, nil},
		{"all up is ok", fleetStateInput{Members: []memberFleetFacts{up, up}}, FleetOK, nil},
		{"unknown members are neither up nor down",
			fleetStateInput{Members: []memberFleetFacts{{}, {}}}, FleetOK, nil},
		{"one down degrades",
			fleetStateInput{Members: []memberFleetFacts{up, down}},
			FleetDegraded, []string{"member_down"}},
		{"all down is faulty",
			fleetStateInput{Members: []memberFleetFacts{down, down}},
			FleetFaulty, []string{"all_members_down"}},
		{"unknown member blocks all-down escalation",
			fleetStateInput{Members: []memberFleetFacts{down, {}}},
			FleetDegraded, []string{"member_down"}},
		{"some held degrades",
			fleetStateInput{Members: []memberFleetFacts{up, heldOf(up), syncable(up)}},
			FleetDegraded, []string{"sync_held"}},
		{"all held (2+) is faulty: primary is the odd one out",
			fleetStateInput{Members: []memberFleetFacts{up, heldOf(up), heldOf(up)}},
			FleetFaulty, []string{"all_sync_held"}},
		{"a stale hold degrades but cannot escalate: it was judged against a primary build nothing runs now",
			fleetStateInput{Members: []memberFleetFacts{up, staleHeldOf(up), heldOf(up)}},
			FleetDegraded, []string{"sync_held"}},
		{"every candidate stale is still only a degradation",
			fleetStateInput{Members: []memberFleetFacts{up, staleHeldOf(up), staleHeldOf(up)}},
			FleetDegraded, []string{"sync_held"}},
		{"the rolling-rebuild window: one member re-checked, the rest flagged against the old primary",
			fleetStateInput{Members: []memberFleetFacts{up, staleHeldOf(up), heldOf(up), heldOf(up)}},
			FleetDegraded, []string{"sync_held"}},
		{"single held candidate cannot prove the primary is odd",
			fleetStateInput{Members: []memberFleetFacts{up, heldOf(up)}},
			FleetDegraded, []string{"sync_held"}},
		{"one drained while two-plus stay active degrades",
			fleetStateInput{Members: []memberFleetFacts{up, up, drainedOf(up)}},
			FleetDegraded, []string{"member_drained"}},
		{"drained down to a single active is faulty",
			fleetStateInput{Members: []memberFleetFacts{up, drainedOf(up)}},
			FleetFaulty, []string{"drained_to_single"}},
		{"all drained is faulty (defensive; the drain guard makes this unreachable)",
			fleetStateInput{Members: []memberFleetFacts{drainedOf(up), drainedOf(up)}},
			FleetFaulty, []string{"drained_to_single"}},
		{"a naturally single active member (no drains) does not trigger a drain reason",
			fleetStateInput{Members: []memberFleetFacts{up}}, FleetOK, nil},
		{"drain and health reasons compose in fixed order",
			fleetStateInput{Members: []memberFleetFacts{up, down, drainedOf(up)}},
			FleetDegraded, []string{"member_down", "member_drained"}},
		{"a held and an incomplete member report both reasons in fixed order",
			fleetStateInput{
				Members:      []memberFleetFacts{up, heldOf(up), incompleteOf(up)},
				AutoSyncTier: 1,
			},
			FleetDegraded, []string{"sync_held", "sync_incomplete", "autosync_stale"}},
		{"stale tier 1 degrades", fleetStateInput{AutoSyncTier: 1},
			FleetDegraded, []string{"autosync_stale"}},
		{"stale tier 2 is faulty", fleetStateInput{AutoSyncTier: 2},
			FleetFaulty, []string{"autosync_stale_long"}},
		{"traefik stale is faulty", fleetStateInput{TraefikStale: true},
			FleetFaulty, []string{"traefik_config_stale"}},
		{"reasons accumulate and worst severity wins",
			fleetStateInput{
				Members:      []memberFleetFacts{up, down, heldOf(up), syncable(up)},
				AutoSyncTier: 1, TraefikStale: true,
			},
			FleetFaulty, []string{"member_down", "sync_held", "autosync_stale", "traefik_config_stale"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, reasons := computeFleetState(tc.in)
			if state != tc.wantState {
				t.Errorf("state = %s, want %s", state, tc.wantState)
			}
			if !reflect.DeepEqual(reasons, tc.wantReasons) {
				t.Errorf("reasons = %v, want %v", reasons, tc.wantReasons)
			}
		})
	}
}

// TestFleetState_IncompleteMemberDegrades pins the severity of a member that
// committed a config without building every custom failover group: amber, so the
// operator sees the fleet is not whole.
func TestFleetState_IncompleteMemberDegrades(t *testing.T) {
	state, reasons := computeFleetState(fleetStateInput{Members: []memberFleetFacts{
		{Known: true, Healthy: true, Syncable: true},
		{Known: true, Healthy: true, Syncable: true, Incomplete: true},
	}})

	if state != FleetDegraded {
		t.Fatalf("state = %q, want degraded", state)
	}
	if !slices.Contains(reasons, reasonSyncIncomplete) {
		t.Fatalf("reasons = %v, want to contain %q", reasons, reasonSyncIncomplete)
	}
}

// TestFleetState_AllIncompleteStillDegradedNotFaulty guards the ceiling: unlike
// sync_held, an incomplete member has no all_* escalation, because it still
// serves every model outside the missing groups.
func TestFleetState_AllIncompleteStillDegradedNotFaulty(t *testing.T) {
	state, _ := computeFleetState(fleetStateInput{Members: []memberFleetFacts{
		{Known: true, Healthy: true, Syncable: true, Incomplete: true},
		{Known: true, Healthy: true, Syncable: true, Incomplete: true},
	}})

	if state != FleetDegraded {
		t.Fatalf("state = %q, want degraded: incomplete members still serve traffic, but the fleet is not whole", state)
	}
}

// setHealth marks a member healthy or down in the poller's in-memory status map,
// exactly as PollHealthOnce would, so checkFleetState reads a confirmed verdict.
func setHealth(s *Server, id string, healthy bool) {
	s.poller.mu.Lock()
	s.poller.statuses[id] = MemberStatus{Health: HealthStatus{Known: true, Healthy: healthy}}
	s.poller.mu.Unlock()
}

// fleetStateEvents returns every persisted fleet.state_changed row, newest first.
func fleetStateEvents(ctx context.Context, t *testing.T, s *Server) []Event {
	t.Helper()
	evs, _, err := s.store.ListEvents(ctx, EventFilter{Type: "fleet.state_changed"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return evs
}

// reasonsContain reports whether the round-tripped metadata reasons (a JSON
// array decodes to []any) include the given code.
func reasonsContain(meta map[string]any, code string) bool {
	raw, ok := meta["reasons"].([]any)
	if !ok {
		return false
	}
	for _, r := range raw {
		if s, _ := r.(string); s == code {
			return true
		}
	}
	return false
}

func TestCheckFleetStateEmitsOnTransitions(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, true)
	warmFleetInputs(ctx, srv)

	// Baseline ok: with no persisted transition the seed is ok, so no
	// transition, no event.
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 0 {
		t.Fatalf("baseline emitted %d fleet.state_changed events, want 0", len(evs))
	}

	// One member confirmed down -> degraded: exactly one event.
	setHealth(srv, m1.ID, false)
	srv.checkFleetState(ctx)
	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 1 {
		t.Fatalf("after member down: %d events, want 1", len(evs))
	}
	if evs[0].Severity != "warning" {
		t.Errorf("severity = %q, want warning", evs[0].Severity)
	}
	if got := evs[0].Metadata["to"]; got != "degraded" {
		t.Errorf(`metadata["to"] = %v, want "degraded"`, got)
	}
	if !reasonsContain(evs[0].Metadata, reasonMemberDown) {
		t.Errorf("reasons %v missing %q", evs[0].Metadata["reasons"], reasonMemberDown)
	}

	// A repeat check while nothing changed must not re-emit (edge-triggered).
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 1 {
		t.Fatalf("unchanged re-check emitted again: %d events, want 1", len(evs))
	}

	// Recovery -> ok: a second event whose newest row reads ok/success.
	setHealth(srv, m1.ID, true)
	srv.checkFleetState(ctx)
	evs = fleetStateEvents(ctx, t, srv)
	if len(evs) != 2 {
		t.Fatalf("after recovery: %d events, want 2", len(evs))
	}
	if got := evs[0].Metadata["to"]; got != "ok" {
		t.Errorf(`recovery metadata["to"] = %v, want "ok"`, got)
	}
	if evs[0].Severity != "success" {
		t.Errorf("recovery severity = %q, want success", evs[0].Severity)
	}
}

// TestCheckFleetStateEmitsFaultyTransition covers the to=faulty branch of
// fleetStateEvent (severity "error"), which the ok/degraded/ok path above never
// exercises: with every member confirmed down the state is faulty via
// all_members_down.
func TestCheckFleetStateEmitsFaultyTransition(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, true)

	// Baseline ok: no transition, no event.
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 0 {
		t.Fatalf("baseline emitted %d events, want 0", len(evs))
	}

	// Every member confirmed down -> faulty via all_members_down: one event,
	// severity error.
	setHealth(srv, m1.ID, false)
	setHealth(srv, m2.ID, false)
	srv.checkFleetState(ctx)
	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 1 {
		t.Fatalf("after all-down: %d events, want 1", len(evs))
	}
	if evs[0].Severity != "error" {
		t.Errorf("severity = %q, want error", evs[0].Severity)
	}
	if got := evs[0].Metadata["to"]; got != "faulty" {
		t.Errorf(`metadata["to"] = %v, want "faulty"`, got)
	}
	if !reasonsContain(evs[0].Metadata, reasonAllMembersDown) {
		t.Errorf("reasons %v missing %q", evs[0].Metadata["reasons"], reasonAllMembersDown)
	}
}

// TestCheckFleetStateEmitsDrainTransitions drives the drain reasons through the
// emit path end to end: member_drained -> degraded (warning) and, once drained
// down to a single active member, drained_to_single -> faulty (error). This is
// what makes a forgotten drain actually fire fleet.state_changed.
func TestCheckFleetStateEmitsDrainTransitions(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	m3, err := store.CreateMember(ctx, "hotel-3", "https://h3.example", "tok3")
	if err != nil {
		t.Fatalf("create m3: %v", err)
	}
	for _, id := range []string{m1.ID, m2.ID, m3.ID} {
		setHealth(srv, id, true)
	}

	// Baseline ok: no transition, no event.
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 0 {
		t.Fatalf("baseline emitted %d events, want 0", len(evs))
	}

	// Drain one of three -> degraded via member_drained (two still active).
	if err := store.SetMemberState(ctx, m3.ID, StateDrained); err != nil {
		t.Fatalf("drain m3: %v", err)
	}
	srv.checkFleetState(ctx)
	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 1 {
		t.Fatalf("after first drain: %d events, want 1", len(evs))
	}
	if evs[0].Severity != "warning" || evs[0].Metadata["to"] != "degraded" {
		t.Errorf("first drain: severity=%q to=%v, want warning/degraded", evs[0].Severity, evs[0].Metadata["to"])
	}
	if !reasonsContain(evs[0].Metadata, reasonMemberDrained) {
		t.Errorf("reasons %v missing %q", evs[0].Metadata["reasons"], reasonMemberDrained)
	}

	// Drain a second -> only one active member left -> faulty via
	// drained_to_single (the last active member itself cannot be drained).
	if err := store.SetMemberState(ctx, m2.ID, StateDrained); err != nil {
		t.Fatalf("drain m2: %v", err)
	}
	srv.checkFleetState(ctx)
	evs = fleetStateEvents(ctx, t, srv)
	if len(evs) != 2 {
		t.Fatalf("after second drain: %d events, want 2", len(evs))
	}
	// fleetStateEvents is newest first.
	if evs[0].Severity != "error" || evs[0].Metadata["to"] != "faulty" {
		t.Errorf("second drain: severity=%q to=%v, want error/faulty", evs[0].Severity, evs[0].Metadata["to"])
	}
	if !reasonsContain(evs[0].Metadata, reasonDrainedToSingle) {
		t.Errorf("reasons %v missing %q", evs[0].Metadata["reasons"], reasonDrainedToSingle)
	}
}

// simulateFleetStateRestart resets every piece of in-memory fleet-state input
// exactly as a process restart does: the edge detector's previous state, the
// poller's health verdicts and fail counters, the version-skew hold set and
// the incomplete-apply set all start empty in a fresh process. Only the store
// (members, events, autosync config) survives.
func simulateFleetStateRestart(s *Server) {
	s.fleetStateMu.Lock()
	s.fleetStatePrev = ""
	s.fleetStateMu.Unlock()
	s.poller.mu.Lock()
	s.poller.statuses = make(map[string]MemberStatus)
	s.poller.healthFailures = make(map[string]int)
	s.poller.lastConfigPollAt = time.Time{}
	s.poller.mu.Unlock()
	s.syncHeldMu.Lock()
	s.syncHeld = make(map[string]string)
	s.syncHeldMu.Unlock()
	s.syncIncompleteMu.Lock()
	s.syncIncomplete = make(map[string]incompleteState)
	s.syncIncompleteMu.Unlock()
	s.autoSyncEvaluated.Store(false)
	s.startedAt = time.Now()
}

// warmFleetInputs gives every non-health cold-start input a first real
// observation, the way a running process acquires them within its first
// ticks: one auto-sync evaluation (rebuilding the hold set) and one Traefik
// config poll. Health verdicts stay whatever setHealth installed.
func warmFleetInputs(ctx context.Context, s *Server) {
	s.autoSyncOnce(ctx, "")
	s.poller.RecordConfigPoll()
}

// TestCheckFleetStateSeedsPrevAcrossRestart_EmitsMissedRecovery pins the seed:
// when the fleet recovers while no tick observes it (the process is down) and
// Front Desk then restarts, the recovery is still emitted, seeded from the
// newest persisted fleet.state_changed event rather than assuming the process
// started with a healthy fleet. It must only be emitted once the inputs are
// warm: the cold first tick computes ok out of ignorance, not evidence.
func TestCheckFleetStateSeedsPrevAcrossRestart_EmitsMissedRecovery(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, true)
	srv.checkFleetState(ctx)

	// One member confirmed down -> the ok-to-degraded transition is persisted.
	setHealth(srv, m1.ID, false)
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 1 {
		t.Fatalf("after member down: %d events, want 1", len(evs))
	}

	// The member recovers while Front Desk is down, then the process restarts.
	// The cold tick must sit on the seeded degraded state: nothing has been
	// probed yet, so the computed ok is not evidence of recovery.
	setHealth(srv, m1.ID, true)
	simulateFleetStateRestart(srv)
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 1 {
		t.Fatalf("cold tick after restart: %d events, want 1 (no recovery before the inputs are warm)", len(evs))
	}

	// Once every input has a real observation, the recovery is emitted.
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, true)
	warmFleetInputs(ctx, srv)
	srv.checkFleetState(ctx)

	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 2 {
		t.Fatalf("warm tick after restart: %d events, want 2 (recovery must not be swallowed)", len(evs))
	}
	if got := evs[0].Metadata["to"]; got != "ok" {
		t.Errorf(`recovery metadata["to"] = %v, want "ok"`, got)
	}
	if got := evs[0].Metadata["from"]; got != "degraded" {
		t.Errorf(`recovery metadata["from"] = %v, want "degraded"`, got)
	}
	if evs[0].Severity != "success" {
		t.Errorf("recovery severity = %q, want success", evs[0].Severity)
	}
}

// TestCheckFleetStateSeedsPrevAcrossRestart_NoDuplicateDegradation is the
// mirror case: restarting while the fleet is still degraded must emit nothing
// at all — no false degraded-to-ok while the poller has not confirmed anything
// (fleet.state_changed alerts by default, so that would page an operator with
// an all-clear mid-incident), and no repeat of the ok-to-degraded the previous
// process already reported once the down verdict is confirmed again.
func TestCheckFleetStateSeedsPrevAcrossRestart_NoDuplicateDegradation(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, false)
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 1 {
		t.Fatalf("after member down: %d events, want 1", len(evs))
	}

	// Restart with the member still down. The cold tick computes ok because the
	// fresh poller has confirmed nothing: that must not surface as a recovery.
	simulateFleetStateRestart(srv)
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 1 {
		t.Fatalf("cold tick after restart emitted: %d events, want 1", len(evs))
	}

	// The poller re-confirms the member down and the other inputs warm up: the
	// state matches the seed, so there is still nothing to say.
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, false)
	warmFleetInputs(ctx, srv)
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 1 {
		t.Fatalf("warm tick after restart re-emitted: %d events, want 1", len(evs))
	}
}

// TestCheckFleetStateColdDegradationStillEmits pins the gate's asymmetry: a
// degradation is always backed by a confirmed observation (a down verdict only
// exists once the poller crossed its fail threshold), so it must be emitted
// even while other inputs are still cold. Only improvements wait for warmth.
func TestCheckFleetStateColdDegradationStillEmits(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	if _, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2"); err != nil {
		t.Fatalf("create m2: %v", err)
	}

	// m1 is confirmed down while m2 has never been probed, auto-sync has not
	// evaluated and Traefik has not polled: as cold as a first tick gets.
	setHealth(srv, m1.ID, false)
	srv.checkFleetState(ctx)

	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 1 {
		t.Fatalf("cold degradation: %d events, want 1 (degradations must not wait for warm-up)", len(evs))
	}
	if got := evs[0].Metadata["to"]; got != "degraded" {
		t.Errorf(`metadata["to"] = %v, want "degraded"`, got)
	}
}

// TestCheckFleetStateSeedsPrevAcrossRestart_UnknownStateFallsBackToOK guards
// the seed against a fleet.state_changed row whose metadata carries no valid
// state (a corrupted or future-format row): it falls back to ok, so the first
// transition after restart reads from=ok rather than echoing the garbage.
// This pins the fallback value only (the unseeded detector also assumed ok),
// so it holds on any implementation that ignores the bogus row.
func TestCheckFleetStateSeedsPrevAcrossRestart_UnknownStateFallsBackToOK(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	setHealth(srv, m1.ID, false)
	setHealth(srv, m2.ID, true)
	srv.emit(ctx, Event{
		Type: "fleet.state_changed", Severity: "success", Source: "frontdesk",
		Message:  "bogus row",
		Metadata: map[string]any{"to": "banana"},
	})

	srv.checkFleetState(ctx)
	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 2 {
		t.Fatalf("after first tick: %d events, want 2 (bogus row plus the new transition)", len(evs))
	}
	if got := evs[0].Metadata["from"]; got != "ok" {
		t.Errorf(`metadata["from"] = %v, want "ok"`, got)
	}
	if got := evs[0].Metadata["to"]; got != "degraded" {
		t.Errorf(`metadata["to"] = %v, want "degraded"`, got)
	}
}

// TestCheckFleetStateWarmupBounded pins the gate's escape hatch: when Traefik
// never fetches the config (so that input can never arm), the warm-up must
// still open once a full staleness window has passed since process start.
// Suppressing a recovery is a grace period, never a deadlock.
func TestCheckFleetStateWarmupBounded(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	// The previous process left the fleet reported degraded.
	srv.emit(ctx, fleetStateEvent(FleetDegraded, FleetDegraded, []string{reasonMemberDown}))

	// This process: health confirmed, auto-sync evaluated, but not a single
	// Traefik poll — and a start time a full staleness window in the past.
	setHealth(srv, m1.ID, true)
	srv.autoSyncOnce(ctx, "")
	srv.startedAt = time.Now().Add(-time.Hour)
	srv.checkFleetState(ctx)

	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 2 {
		t.Fatalf("tick past the grace window: %d events, want 2 (recovery must not be held forever)", len(evs))
	}
	if got := evs[0].Metadata["to"]; got != "ok" {
		t.Errorf(`metadata["to"] = %v, want "ok"`, got)
	}
}

// TestLastEmittedFleetState_UsesNewestRow pins the seed's load-bearing
// ordering assumption: with several persisted transitions, the newest row is
// the one that seeds. Driven through real ticks so the rows are exactly what
// production persists; the fleet comes to rest at faulty, which also
// exercises that arm of the seed's state switch.
func TestLastEmittedFleetState_UsesNewestRow(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	warmFleetInputs(ctx, srv)

	// ok -> degraded, then degraded -> faulty: two persisted transitions.
	setHealth(srv, m1.ID, false)
	setHealth(srv, m2.ID, true)
	srv.checkFleetState(ctx)
	setHealth(srv, m2.ID, false)
	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 2 {
		t.Fatalf("setup persisted %d events, want 2", len(evs))
	}

	if got := srv.lastEmittedFleetState(ctx); got != FleetFaulty {
		t.Fatalf("lastEmittedFleetState = %q, want faulty (the newest row)", got)
	}
}

// TestCheckFleetStateSeedsOncePerProcess pins that the seed is a first-tick
// affair: once the detector holds a state, a fleet.state_changed row landing
// out of band must not re-seed it and fake a transition.
func TestCheckFleetStateSeedsOncePerProcess(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	setHealth(srv, m1.ID, true)
	warmFleetInputs(ctx, srv)
	srv.checkFleetState(ctx)

	// A bogus faulty row lands after the detector is seeded. If a later tick
	// re-read the store it would see prev=faulty, cur=ok and emit a recovery.
	srv.emit(ctx, fleetStateEvent(FleetOK, FleetFaulty, []string{reasonAllMembersDown}))
	srv.checkFleetState(ctx)

	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 1 {
		t.Fatalf("tick after out-of-band row: %d events, want 1 (the injected row only)", len(evs))
	}
}

// TestCheckFleetStateStoreErrorLeavesDetectorUntouched pins the error path: a
// tick that cannot read the store computes nothing, emits nothing and leaves
// the edge detector unseeded, so the next healthy tick starts from scratch.
func TestCheckFleetStateStoreErrorLeavesDetectorUntouched(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	srv.checkFleetState(context.Background())

	srv.fleetStateMu.Lock()
	prev := srv.fleetStatePrev
	srv.fleetStateMu.Unlock()
	if prev != "" {
		t.Fatalf("fleetStatePrev = %q after a failed tick, want unseeded", prev)
	}
}

// TestLastEmittedFleetState_StoreErrorFallsBackToOK pins the fail-safe: when
// the events table cannot be read back the seed reports ok, the same posture a
// first boot has, rather than blocking the tick or inventing a state.
func TestLastEmittedFleetState_StoreErrorFallsBackToOK(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if got := srv.lastEmittedFleetState(context.Background()); got != FleetOK {
		t.Fatalf("lastEmittedFleetState on a closed store = %q, want ok", got)
	}
}

// TestCheckFleetStateEmitsIncompleteTransition drives the diverged set through
// the live path: the auto-sync loop's own bookkeeping (markMemberIncomplete)
// feeds fleetStateFrom, so the badge turns amber for as long as the member does
// not hold the primary's config, and clears when it converges.
func TestCheckFleetStateEmitsIncompleteTransition(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}
	setHealth(srv, m1.ID, true)
	setHealth(srv, m2.ID, true)
	warmFleetInputs(ctx, srv)

	srv.checkFleetState(ctx)
	if evs := fleetStateEvents(ctx, t, srv); len(evs) != 0 {
		t.Fatalf("baseline emitted %d events, want 0", len(evs))
	}

	srv.recordSyncAttempt(m1.ID, []string{"fast"}, nil, nil)
	srv.markMemberIncomplete(ctx, m1)
	srv.checkFleetState(ctx)
	evs := fleetStateEvents(ctx, t, srv)
	if len(evs) != 1 {
		t.Fatalf("after incomplete apply: %d events, want 1", len(evs))
	}
	if evs[0].Severity != "warning" || evs[0].Metadata["to"] != "degraded" {
		t.Errorf("incomplete: severity=%q to=%v, want warning/degraded", evs[0].Severity, evs[0].Metadata["to"])
	}
	if !reasonsContain(evs[0].Metadata, reasonSyncIncomplete) {
		t.Errorf("reasons %v missing %q", evs[0].Metadata["reasons"], reasonSyncIncomplete)
	}

	srv.clearMemberIncomplete(ctx, m1)
	srv.checkFleetState(ctx)
	evs = fleetStateEvents(ctx, t, srv)
	if len(evs) != 2 {
		t.Fatalf("after recovery: %d events, want 2", len(evs))
	}
	if evs[0].Metadata["to"] != "ok" {
		t.Errorf(`recovery metadata["to"] = %v, want "ok"`, evs[0].Metadata["to"])
	}
}
