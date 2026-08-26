package frontdesk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// fleetPrimary resolves which member the fleet's primary is, for the announce,
// and returns its current name from the live roster.
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
// Nothing resolving means no member is flagged primary. The membership signal is
// still worth sending, so the caller continues without one rather than aborting; a
// read error is treated the same way.
func (p *Poller) fleetPrimary(ctx context.Context, members []*Member) (id, name string, ok bool) {
	cfg, cfgErr := p.store.GetAutoSync(ctx)
	if cfgErr != nil {
		debuglog.Warn("frontdesk: poll announce: read auto-sync config", "error", cfgErr)
	}
	designated := ""
	if cfgErr == nil {
		designated = cfg.PrimaryID
	}
	state, hasMarker, stateErr := p.store.GetFleetSyncState(ctx)
	if stateErr != nil {
		debuglog.Warn("frontdesk: poll announce: fleet sync state", "error", stateErr)
		hasMarker = false
	}
	marked := ""
	if hasMarker {
		marked = state.PrimaryID
	}

	// designated is empty unless the read succeeded, so the error case needs no arm
	// of its own here: it simply contributes no candidate.
	var candidates []string
	if designated != "" && cfg.Enabled {
		candidates = append(candidates, designated)
	}
	if marked != "" {
		candidates = append(candidates, marked)
	}
	// A designation with auto-sync off still beats naming nobody.
	if designated != "" && !cfg.Enabled {
		candidates = append(candidates, designated)
	}

	for _, want := range candidates {
		for _, m := range members {
			if m.ID == want {
				return m.ID, m.Name, true
			}
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
		token, ok, err := p.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
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

// announceToMember POSTs one heartbeat through the guarded probe client (the
// same SSRF-protected client the health poll uses), carrying the member's admin
// Bearer token. A non-204 reply is an error so the caller can log-and-continue.
func (p *Poller) announceToMember(ctx context.Context, baseURL, token string, ann memberAnnounce) error {
	body, err := json.Marshal(ann)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+memberAnnouncePath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode == http.StatusConflict {
		// The member is owned by another Front Desk. Distinguish this from a
		// generic failure so the caller can surface it once, not spam Debug.
		return errAnnounceConflict
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("announce returned %d", resp.StatusCode)
	}
	return nil
}
