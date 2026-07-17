package frontdesk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

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
	reasonMemberDown     = "member_down"
	reasonAllMembersDown = "all_members_down"
	// member_drained: at least one member is drained (out of the routing pool)
	// while two or more remain active. drained_to_single: draining has left just
	// one active member, so the fleet has no redundancy (the last active member
	// cannot be drained, so this is the enforced floor) and is treated as faulty.
	reasonMemberDrained     = "member_drained"
	reasonDrainedToSingle   = "drained_to_single"
	reasonSyncHeld          = "sync_held"
	reasonAllSyncHeld       = "all_sync_held"
	reasonAutosyncStale     = "autosync_stale"
	reasonAutosyncStaleLong = "autosync_stale_long"
	// traefik_config_stale is config-poll staleness specifically (Traefik stopped
	// fetching the dynamic config), not a member's data-plane serverStatus; named
	// to keep it distinct from the traefik.stale alert event and the per-member
	// traefik_status badge.
	reasonTraefikConfigStale = "traefik_config_stale"
)

// fleetFaultyReasons is the subset of reason codes that escalate the state to
// faulty; every other reason yields degraded.
var fleetFaultyReasons = map[string]bool{
	reasonAllMembersDown:     true,
	reasonDrainedToSingle:    true,
	reasonAllSyncHeld:        true,
	reasonAutosyncStaleLong:  true,
	reasonTraefikConfigStale: true,
}

// memberFleetFacts is the per-member slice of the state machine's input:
// health as confirmed by the poller (Known false means never probed, which
// counts as neither up nor down), whether the member is drained (pulled out of
// the routing pool by the operator; the zero value is the common active case),
// and whether it is a sync-hold candidate (tokened, non-primary) currently held
// for version skew.
type memberFleetFacts struct {
	Known    bool
	Healthy  bool
	Drained  bool
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
// in a fixed order (health, drain, sync holds, autosync staleness, Traefik) so
// equal inputs always serialize identically. Pure: all live reads happen in the
// Server wrapper (fleetStateNow), keeping this exhaustively table-testable.
func computeFleetState(in fleetStateInput) (FleetState, []string) {
	var down, held, syncable, drained int
	for _, m := range in.Members {
		if m.Known && !m.Healthy {
			down++
		}
		if m.Drained {
			drained++
		}
		if m.Syncable {
			syncable++
			if m.Held {
				held++
			}
		}
	}
	active := len(in.Members) - drained

	var reasons []string
	switch {
	case len(in.Members) > 0 && down == len(in.Members):
		reasons = append(reasons, reasonAllMembersDown)
	case down > 0:
		reasons = append(reasons, reasonMemberDown)
	}
	switch {
	// Draining down to a single active member (or fewer, defensively — the drain
	// guard makes zero-active unreachable) leaves no routing redundancy: faulty.
	// Any other drain with two or more still active is a plain degradation.
	case drained >= 1 && active <= 1:
		reasons = append(reasons, reasonDrainedToSingle)
	case drained >= 1:
		reasons = append(reasons, reasonMemberDrained)
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
		reasons = append(reasons, reasonTraefikConfigStale)
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

// fleetStateInterval is how often RunFleetState re-judges the fleet. Matches
// the default poll cadence; the inputs are in-memory snapshots plus three
// cheap settings reads, so a tight tick costs little.
const fleetStateInterval = 5 * time.Second

// heldSnapshot copies the autosync version-skew hold set under its lock. The
// auto-sync loop owns the set; if auto-sync is disabled while members are held,
// the set stays frozen rather than clearing, so those members keep contributing
// sync_held / all_sync_held. That direction is deliberate: a stale hold degrades
// the fleet state, it never reports a false ok, and it clears when auto-sync
// resumes or the skew resolves.
func (s *Server) heldSnapshot() map[string]bool {
	s.syncHeldMu.Lock()
	defer s.syncHeldMu.Unlock()
	out := make(map[string]bool, len(s.syncHeld))
	for k, v := range s.syncHeld {
		out[k] = v
	}
	return out
}

// fleetStateNow gathers the live inputs and computes the current fleet state.
// The background loop uses this self-contained form; autoSyncStatusNow reuses
// reads it has already made by calling fleetStateFrom directly.
func (s *Server) fleetStateNow(ctx context.Context) (FleetState, []string, error) {
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		return "", nil, err
	}
	cfg, err := s.store.GetAutoSync(ctx)
	if err != nil {
		return "", nil, err
	}
	syncState, haveSync, err := s.store.GetFleetSyncState(ctx)
	if err != nil {
		return "", nil, err
	}
	state, reasons := s.fleetStateFrom(ctx, members, cfg, syncState.LastRunAt, haveSync)
	return state, reasons, nil
}

// fleetStateFrom assembles the per-member facts from already-read store data
// (member list, auto-sync config, last-sync marker) and computes the state,
// folding in the live poller snapshots (health, Traefik staleness) and the
// version-skew hold set. Callers that already hold those reads pass them in so
// the polled /api/fleet/autosync endpoint does not re-query the store.
func (s *Server) fleetStateFrom(ctx context.Context, members []*Member, cfg AutoSyncConfig, lastSync time.Time, haveSync bool) (FleetState, []string) {
	statuses := s.poller.Snapshot()
	held := s.heldSnapshot()
	facts := make([]memberFleetFacts, 0, len(members))
	for _, m := range members {
		st := statuses[m.ID]
		facts = append(facts, memberFleetFacts{
			Known:    st.Health.Known,
			Healthy:  st.Health.Healthy,
			Drained:  m.State == StateDrained,
			Syncable: m.HasToken && m.ID != cfg.PrimaryID,
			Held:     held[m.ID],
		})
	}
	return computeFleetState(fleetStateInput{
		Members:      facts,
		AutoSyncTier: autoSyncStaleTier(cfg, lastSync, haveSync, time.Now().UTC()),
		TraefikStale: s.poller.ConfigPollStale(ctx),
	})
}

// checkFleetState computes the state and emits fleet.state_changed exactly on
// transitions. Split from RunFleetState so tests drive ticks directly.
func (s *Server) checkFleetState(ctx context.Context) {
	cur, reasons, err := s.fleetStateNow(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: compute fleet state", "error", err)
		return
	}
	s.fleetStateMu.Lock()
	prev := s.fleetStatePrev
	if prev == "" {
		prev = FleetOK
	}
	changed := cur != prev
	s.fleetStatePrev = cur
	s.fleetStateMu.Unlock()
	if changed {
		s.emit(ctx, fleetStateEvent(prev, cur, reasons))
	}
}

// fleetStateEvent shapes the edge-triggered transition event. The message is
// operator-log prose; clients translate from the metadata reason codes, never
// from this string.
func fleetStateEvent(from, to FleetState, reasons []string) Event {
	// Serialize reasons as [] rather than null on a recovery (to=ok carries no
	// active reasons), so a client reading the event metadata never has to
	// special-case a null array.
	if reasons == nil {
		reasons = []string{}
	}
	sev := "success"
	switch to {
	case FleetDegraded:
		sev = "warning"
	case FleetFaulty:
		sev = "error"
	}
	msg := fmt.Sprintf("Fleet state changed: %s to %s", from, to)
	if len(reasons) > 0 {
		msg += " (" + strings.Join(reasons, ", ") + ")"
	}
	return Event{
		Type: "fleet.state_changed", Severity: sev, Source: "frontdesk",
		Message:  msg,
		Metadata: map[string]any{"from": string(from), "to": string(to), "reasons": reasons},
	}
}

// RunFleetState re-judges the fleet on a fixed tick until ctx is cancelled.
// Started once at startup, alongside RunAutoSync.
func (s *Server) RunFleetState(ctx context.Context) {
	ticker := time.NewTicker(fleetStateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkFleetState(ctx)
		}
	}
}
