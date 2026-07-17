package frontdesk

import (
	"context"
	"reflect"
	"testing"
)

func TestComputeFleetState(t *testing.T) {
	up := memberFleetFacts{Known: true, Healthy: true}
	down := memberFleetFacts{Known: true, Healthy: false}
	heldOf := func(f memberFleetFacts) memberFleetFacts { f.Syncable = true; f.Held = true; return f }
	syncable := func(f memberFleetFacts) memberFleetFacts { f.Syncable = true; return f }

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
		{"single held candidate cannot prove the primary is odd",
			fleetStateInput{Members: []memberFleetFacts{up, heldOf(up)}},
			FleetDegraded, []string{"sync_held"}},
		{"stale tier 1 degrades", fleetStateInput{AutoSyncTier: 1},
			FleetDegraded, []string{"autosync_stale"}},
		{"stale tier 2 is faulty", fleetStateInput{AutoSyncTier: 2},
			FleetFaulty, []string{"autosync_stale_long"}},
		{"traefik stale is faulty", fleetStateInput{TraefikStale: true},
			FleetFaulty, []string{"traefik_stale"}},
		{"reasons accumulate and worst severity wins",
			fleetStateInput{
				Members:      []memberFleetFacts{up, down, heldOf(up), syncable(up)},
				AutoSyncTier: 1, TraefikStale: true,
			},
			FleetFaulty, []string{"member_down", "sync_held", "autosync_stale", "traefik_stale"}},
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

	// Baseline ok: the empty-prev is treated as ok, so no transition, no event.
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
