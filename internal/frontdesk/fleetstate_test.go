package frontdesk

import (
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
