package frontdesk

import (
	"context"
	"errors"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// primaryUnreachableIdleThreshold is how many passes in a row must fail to read
// a primary that has a token before the enabled auto-sync counts as idle. A
// single failed read is a network blip the next pass retries; this many in a
// row means the primary has stopped answering.
const primaryUnreachableIdleThreshold = 3

// errMemberAuthRefused marks a member answering 401 or 403: its stored token is
// not accepted, which no retry fixes.
var errMemberAuthRefused = errors.New("frontdesk: member refused the stored admin token")

// primaryConfigHash resolves the designated primary, loads its admin token, and
// reads its current syncable-config hash together with its per-section hashes.
// ok is false when the primary was removed, lost its token, refused it, or is
// unreachable, in which case the caller skips this round and retries later.
//
// The outcome feeds the auto-sync's idle state (Poller.setAutoSyncIdle), which
// the staleness grading reads, and a change of it is logged once rather than
// per pass. A primary that is gone, has no token, or refuses it marks the
// auto-sync idle at once; one that merely fails to answer does so only after
// primaryUnreachableIdleThreshold passes in a row; a pass cut short by its own
// context records nothing.
func (s *Server) primaryConfigHash(ctx context.Context, cfg AutoSyncConfig) (primary *Member, token, hash string, sections map[string]string, ok bool) {
	primary, token, err := s.memberTokenOrErr(ctx, cfg.PrimaryID)
	cannotRun := errors.Is(err, ErrNotFound) || errors.Is(err, ErrValidation)
	if err == nil {
		hash, sections, err = s.fetchMemberConfigVersion(ctx, primary, token)
		cannotRun = errors.Is(err, errMemberAuthRefused)
	}
	if err != nil {
		debuglog.Debug("frontdesk: auto-sync: primary unavailable", "primary_id", cfg.PrimaryID, "error", err)
		if ctx.Err() != nil {
			return nil, "", "", nil, false
		}
		failures := s.primaryReadFailures.Add(1)
		if (cannotRun || failures >= primaryUnreachableIdleThreshold) && s.markAutoSyncIdle(ctx) {
			debuglog.Warn("frontdesk: auto-sync is enabled but cannot run: primary unavailable", "primary_id", cfg.PrimaryID, "error", err)
		}
		return nil, "", "", nil, false
	}
	if s.clearAutoSyncIdle() {
		debuglog.Info("frontdesk: auto-sync can run again: primary available", "member", primary.Name)
	}
	return primary, token, hash, sections, true
}

// markAutoSyncIdle records the enabled auto-sync as unable to run and reports
// whether it was running until now. The first idle verdict after this process
// started is dated back to the fleet's last sync (the start of this process if
// it never synced): the auto-sync may have been idle through the restart, and
// dating it now would let a Front Desk restarted daily never report the fleet
// stale. Later verdicts follow a pass that ran, so they are dated now.
func (s *Server) markAutoSyncIdle(ctx context.Context) bool {
	var since time.Time // zero: the poller dates it now
	if !s.poller.autoSyncIdleObserved() {
		since = s.startedAt
		if state, found, err := s.store.GetFleetSyncState(ctx); err == nil {
			if members, err := s.store.ListMembers(ctx); err == nil {
				if last, have := fleetLastSync(members, state.LastRunAt, found); have {
					since = last
				}
			}
		}
	}
	return s.poller.setAutoSyncIdle(true, since)
}

// clearAutoSyncIdle records the auto-sync as running (or off) and starts the
// primary's failure count over. It reports whether it was idle until now.
func (s *Server) clearAutoSyncIdle() bool {
	s.primaryReadFailures.Store(0)
	return s.poller.setAutoSyncIdle(false, time.Time{})
}
