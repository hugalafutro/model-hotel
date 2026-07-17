package frontdesk

// This file computes the fleet-wide state machine: one server-side judgment of
// "how is the fleet doing" (ok / degraded / faulty) with machine-readable
// reason codes, so every client (Front Desk web, Bellhop) translates the same
// verdict instead of re-deriving its own from raw member rows. Severity is by
// blast radius, never by comparing version strings (dev builds are
// unorderable): some tokened members sync-held means those members drift
// (degraded); ALL of them held means the primary itself is the odd one out and
// nothing can converge (faulty). Distinct file from fleetstatus.go, which is
// the sync wizard's per-member convergence probe.

// FleetState is the server-computed fleet condition, spelled exactly as it
// travels on the wire (fleet_state on GET /api/fleet/autosync).
type FleetState string

// The three fleet conditions, ordered by severity.
const (
	FleetOK       FleetState = "ok"
	FleetDegraded FleetState = "degraded"
	FleetFaulty   FleetState = "faulty"
)

// Reason codes carried in fleet_state_reasons and the fleet.state_changed
// event. Wire constants: clients key translations off them, so never rename.
const (
	reasonMemberDown        = "member_down"
	reasonAllMembersDown    = "all_members_down"
	reasonSyncHeld          = "sync_held"
	reasonAllSyncHeld       = "all_sync_held"
	reasonAutosyncStale     = "autosync_stale"
	reasonAutosyncStaleLong = "autosync_stale_long"
	reasonTraefikStale      = "traefik_stale"
)

// fleetFaultyReasons is the subset of reason codes that escalate the state to
// faulty; every other reason yields degraded.
var fleetFaultyReasons = map[string]bool{
	reasonAllMembersDown:    true,
	reasonAllSyncHeld:       true,
	reasonAutosyncStaleLong: true,
	reasonTraefikStale:      true,
}

// memberFleetFacts is the per-member slice of the state machine's input:
// health as confirmed by the poller (Known false means never probed, which
// counts as neither up nor down), and whether the member is a sync-hold
// candidate (tokened, non-primary) currently held for version skew.
type memberFleetFacts struct {
	Known    bool
	Healthy  bool
	Syncable bool
	Held     bool
}

// fleetStateInput bundles everything computeFleetState judges: member facts,
// the autosync staleness tier (autoSyncStaleTier), and whether Traefik has
// stopped fetching the dynamic config (Poller.ConfigPollStale).
type fleetStateInput struct {
	Members      []memberFleetFacts
	AutoSyncTier int
	TraefikStale bool
}

// computeFleetState reduces the input to a state plus the active reason codes,
// in a fixed order (health, sync holds, autosync staleness, Traefik) so equal
// inputs always serialize identically. Pure: all live reads happen in the
// Server wrapper (fleetStateNow), keeping this exhaustively table-testable.
func computeFleetState(in fleetStateInput) (FleetState, []string) {
	var down, held, syncable int
	for _, m := range in.Members {
		if m.Known && !m.Healthy {
			down++
		}
		if m.Syncable {
			syncable++
			if m.Held {
				held++
			}
		}
	}

	var reasons []string
	switch {
	case len(in.Members) > 0 && down == len(in.Members):
		reasons = append(reasons, reasonAllMembersDown)
	case down > 0:
		reasons = append(reasons, reasonMemberDown)
	}
	switch {
	// With a single candidate there is no way to tell which side of the skew is
	// the odd one out, so it stays a per-member degradation.
	case syncable >= 2 && held == syncable:
		reasons = append(reasons, reasonAllSyncHeld)
	case held > 0:
		reasons = append(reasons, reasonSyncHeld)
	}
	switch in.AutoSyncTier {
	case 2:
		reasons = append(reasons, reasonAutosyncStaleLong)
	case 1:
		reasons = append(reasons, reasonAutosyncStale)
	}
	if in.TraefikStale {
		reasons = append(reasons, reasonTraefikStale)
	}

	state := FleetOK
	if len(reasons) > 0 {
		state = FleetDegraded
	}
	for _, r := range reasons {
		if fleetFaultyReasons[r] {
			state = FleetFaulty
			break
		}
	}
	return state, reasons
}
