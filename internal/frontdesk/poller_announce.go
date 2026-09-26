package frontdesk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// memberAnnounce is the heartbeat body Front Desk POSTs to each member's
// /api/fleet/announce. It carries only routing metadata: whether the member is
// the fleet primary, and the primary's display name for the member's tooltip.
type memberAnnounce struct {
	IsPrimary   bool   `json:"is_primary"`
	PrimaryName string `json:"primary_name,omitempty"`
	// FrontdeskID is this Front Desk's persistent identity. A member records the
	// first ID-bearing announcer as the owner of its fleet role and rejects
	// announces from any other Front Desk while that owner is live. Empty on a
	// legacy Front Desk build (the member then accepts unconditionally).
	FrontdeskID string `json:"frontdesk_id,omitempty"`
	// ActiveMembers is the fleet-wide count of StateActive members — the fair-share
	// rate-limit divisor. Every active member is a Traefik round-robin backend, so
	// each enforces 1/ActiveMembers of every configured limit. Omitted (0) by a
	// legacy Front Desk build, which the member reads as divisor 1 (no division).
	ActiveMembers int `json:"active_members,omitempty"`
}

// errAnnounceConflict is returned by announceToMember when a member rejects the
// announce with 409: another Front Desk currently owns that member's fleet role.
var errAnnounceConflict = errors.New("frontdesk: announce rejected (managed by another Front Desk)")

// activeMemberCount counts members in StateActive — the exact set
// BuildTraefikConfig puts behind the round-robin /v1 pool (traefik.go:96). It is
// the fleet fair-share divisor each active member applies to its rate limits.
// The divisor tracks the announced StateActive set, which is the same set
// BuildTraefikConfig routes to — but Traefik's live pool can diverge from it
// transiently: it may eject a StateActive backend its own health check finds
// unhealthy, or pick up a state change on a different schedule than the ~5s
// announce loop. That skew is bounded and self-correcting on the next
// announce, and its dominant direction is safe: when Traefik routes to fewer
// backends than N, each survivor divides by a too-large N and the fleet
// under-serves (never exceeds the global cap). The opposite (brief over-cap)
// only occurs on an Active->Drained transition where Traefik still routes to
// the old member for a beat after N dropped — the accepted membership-change
// blip, not a sustained violation.
func activeMemberCount(members []*Member) int {
	n := 0
	for _, m := range members {
		if m.State == StateActive {
			n++
		}
	}
	return n
}

// effectivePrimaryID is the single answer to "which member is the fleet
// primary", shared by the announce, the quota proxy and distribution, the fleet
// state machine and the auto-sync status payload the Members page reads. members
// is the live roster, cfg the auto-sync row and marker the fleet sync-state
// record's primary id (empty when there is none). It returns "" when nothing
// resolves.
//
// Two sources can name a primary, and which one is authoritative depends on
// whether auto-sync is running. While it is, the operator's designation is the
// live answer: it is what the loop pushes from, and it takes effect the moment
// they repoint, where the last-sync marker still names whichever member drove the
// previous run. With auto-sync off the designation is dormant configuration, it
// survives being switched off (clearing it needs a confirmed token), and the last
// wizard run is then the more recent operator act, so the marker wins.
//
// A candidate that is not in the roster is skipped rather than returned: a
// designation left pointing at a removed member would otherwise beat a marker that
// still names a real one, and every member would be told there is no primary.
//
// A one-member roster is the primary by itself, whatever the two sources say.
// The only designation that can exist there is a legacy one from before the
// fleet-size floor (the wizard and SetAutoSyncGuarded refuse a new one below
// two members), and it can only name this same member. Without the rule the sole
// member of a fresh fleet would be announced as a non-primary member: its own
// state machine reads a fresh heartbeat plus is_primary=false as "managed
// member" and refuses every synced-entity edit, pointing the operator at a
// primary that cannot be designated. It is the only instance in the fleet, so it
// is the config source of truth by definition. When a second member joins, the
// add records the lone member as the marker (recordLonePrimary), so this answer
// carries over to the two-member roster until the operator designates one.
//
// Nothing resolving on a larger roster means no member is flagged primary.
func effectivePrimaryID(members []*Member, cfg AutoSyncConfig, marker string) string {
	if len(members) == 1 {
		return members[0].ID
	}
	var candidates []string
	if cfg.PrimaryID != "" && cfg.Enabled {
		candidates = append(candidates, cfg.PrimaryID)
	}
	if marker != "" {
		candidates = append(candidates, marker)
	}
	// A designation with auto-sync off still beats naming nobody.
	if cfg.PrimaryID != "" && !cfg.Enabled {
		candidates = append(candidates, cfg.PrimaryID)
	}
	for _, want := range candidates {
		for _, m := range members {
			if m.ID == want {
				return m.ID
			}
		}
	}
	return ""
}

// fleetPrimary resolves the fleet primary for the announce (effectivePrimaryID)
// and returns its current name from the live roster. A source that cannot be
// read contributes no candidate rather than aborting: the membership signal is
// still worth sending, so the caller continues without a primary if nothing
// resolves.
func (p *Poller) fleetPrimary(ctx context.Context, members []*Member) (id, name string, ok bool) {
	cfg, cfgErr := p.store.GetAutoSync(ctx)
	if cfgErr != nil {
		debuglog.Warn("frontdesk: poll announce: read auto-sync config", "error", cfgErr)
		cfg = AutoSyncConfig{}
	}
	// PrimaryID is empty when the read fails or no record exists.
	state, _, stateErr := p.store.GetFleetSyncState(ctx)
	if stateErr != nil {
		debuglog.Warn("frontdesk: poll announce: fleet sync state", "error", stateErr)
	}
	want := effectivePrimaryID(members, cfg, state.PrimaryID)
	for _, m := range members {
		if m.ID == want {
			return m.ID, m.Name, true
		}
	}
	return "", "", false
}

// PollAnnounceOnce tells every reachable, tokened member that Front Desk is in
// contact, and which member is the fleet primary. It is the producing half of
// HA Phase 6: a member uses these announces to light up the HA line on its own
// dashboard and to self-clear it when they stop. Best-effort, exactly like the
// health poll: a member that is down, has no stored token, or runs an older
// build without the endpoint is silently skipped, never retried or surfaced.
func (p *Poller) PollAnnounceOnce(ctx context.Context) {
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: poll announce: list members", "error", err)
		return
	}
	// Resolved against the live roster, so a renamed primary announces its current
	// name and a designation pointing at a removed member never wins.
	primaryID, primaryName, hasPrimary := p.fleetPrimary(ctx, members)
	activeCount := activeMemberCount(members)
	for _, m := range members {
		token, ok := p.store.MemberTokenOf(ctx, m)
		if !ok {
			continue // no stored token: the announce endpoint needs admin auth
		}
		ann := memberAnnounce{
			IsPrimary:     hasPrimary && m.ID == primaryID,
			PrimaryName:   primaryName,
			FrontdeskID:   p.frontdeskID,
			ActiveMembers: activeCount,
		}
		if err := p.announceToMember(ctx, m.URL, token, ann); err != nil {
			if errors.Is(err, errAnnounceConflict) {
				// Another Front Desk owns this member. Warn once per member per
				// process run (announces fire every ~5s; without the latch this
				// would be a log line every poll). The latch is never reset: the
				// conflict is a persistent misconfiguration the operator resolves.
				p.mu.Lock()
				already := p.conflictNotified[m.ID]
				if !already {
					p.conflictNotified[m.ID] = true
				}
				p.mu.Unlock()
				if !already {
					debuglog.Warn("frontdesk: member is managed by another Front Desk; announce rejected", "member", m.ID)
				}
				continue
			}
			debuglog.Debug("frontdesk: announce to member failed", "member", m.ID, "error", err)
		}
	}
}

// announceToMember POSTs one heartbeat through the guarded announce client
// (the health poll's SSRF-protected client, with the longer announce timeout),
// carrying the member's admin Bearer token. A non-204 reply is an error so the
// caller can log-and-continue.
func (p *Poller) announceToMember(ctx context.Context, baseURL, token string, ann memberAnnounce) error {
	body, err := json.Marshal(ann)
	if err != nil {
		return err
	}
	status, _, err := callMemberWith(ctx, p.announceClient, http.MethodPost, baseURL, memberAnnouncePath, token, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		// The member is owned by another Front Desk. Distinguish this from a
		// generic failure so the caller can surface it once, not spam Debug.
		return errAnnounceConflict
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("announce returned %d", status)
	}
	return nil
}
