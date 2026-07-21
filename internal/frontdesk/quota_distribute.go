package frontdesk

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// memberQuotaSnapshotsPath is the fleet quota endpoint on each member. It is
// mounted on the same fleet-authed router as config export/import (see
// internal/api QuotaFleetHandler.Register), so it lives under /api/config.
const memberQuotaSnapshotsPath = "/api/config/quota-snapshots"

// quotaDistributeInterval is how often Front Desk redistributes the primary's
// quota snapshots to the fleet. It is kept well under the member's quota-poll
// interval (default 5 min) so a member Front Desk feeds always has a fresh fleet
// snapshot and stays suppressed rather than falling back to its own upstream
// poll. Redundant passes are cheap: the member applies with skip-if-newer, so an
// unchanged snapshot is a no-op write. It is a var so tests can drive a tick.
var quotaDistributeInterval = 60 * time.Second

// RunQuotaDistribute runs the quota-distribution pass on a fixed tick until ctx
// is cancelled. It is started once at startup, alongside RunAutoSync, and
// mirrors that loop: a Front-Desk-owned ticker driving a best-effort,
// error-free pass over the fleet.
func (s *Server) RunQuotaDistribute(ctx context.Context) {
	ticker := time.NewTicker(quotaDistributeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.DistributeQuotaOnce(ctx)
		}
	}
}

// DistributeQuotaOnce fetches the designated primary's quota snapshots and posts
// them to every other member, so members do not each poll the same upstream
// account. It mirrors the FD-orchestrated config-sync path (autoSyncOnce): the
// primary is the read source, Front Desk only relays the snapshots (they carry
// no key material), and members apply them with skip-if-newer so an older fleet
// write never clobbers a fresher local one.
//
// Best-effort and error-free: the primary or any member being unreachable is
// logged at debug and skipped, and those members fall back to their own
// self-poll. It is a no-op when no primary is designated (standalone or a fleet
// that has not been set up yet), which is what the receiving member's suppression
// relies on: a node Front Desk is not feeding has no recent fleet snapshot and
// keeps self-polling.
func (s *Server) DistributeQuotaOnce(ctx context.Context) {
	cfg, err := s.store.GetAutoSync(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: quota distribute: read auto-sync config", "error", err)
		return
	}
	if cfg.PrimaryID == "" {
		return // no primary designated: nothing to distribute from
	}

	primary, primaryToken, err := s.memberTokenOrErr(ctx, cfg.PrimaryID)
	if err != nil {
		// The designated primary was removed or lost its token; retry next tick.
		debuglog.Debug("frontdesk: quota distribute: primary unavailable", "error", err)
		return
	}
	status, body, err := s.callMember(ctx, http.MethodGet, primary.URL, memberQuotaSnapshotsPath, primaryToken, nil)
	if err != nil || status != http.StatusOK {
		debuglog.Debug("frontdesk: quota distribute: fetch primary snapshots",
			"member", primary.Name, "status", status, "error", err)
		return
	}

	members, err := s.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: quota distribute: list members", "error", err)
		return
	}
	for _, m := range members {
		if m.ID == cfg.PrimaryID {
			continue // the primary is the source, not a destination
		}
		_, token, err := s.memberTokenOrErr(ctx, m.ID)
		if err != nil {
			debuglog.Debug("frontdesk: quota distribute: member token", "member", m.Name, "error", err)
			continue
		}
		if status, _, err := s.callMember(ctx, http.MethodPost, m.URL, memberQuotaSnapshotsPath, token, bytes.NewReader(body)); err != nil || status != http.StatusOK {
			debuglog.Debug("frontdesk: quota distribute: push to member",
				"member", m.Name, "status", status, "error", err)
		}
	}
}
