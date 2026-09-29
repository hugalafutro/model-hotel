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
	if s.clearAutoSyncIdle(ctx) {
		debuglog.Info("frontdesk: auto-sync can run again: primary available", "member", primary.Name)
	}
	return primary, token, hash, sections, true
}

// markAutoSyncIdle records the enabled auto-sync as unable to run and reports
// whether it was running until now. A new idle spell is persisted
// (Store.SetAutoSyncIdleSince), so the first idle verdict after this process
// started takes the spell a previous process recorded, and a Front Desk
// restarted daily still reports a fleet idle for days as stale. With nothing on
// record that verdict is dated to this process's start, not further back: a
// primary slow to answer after a restart is not an idle spell that began at the
// fleet's last sync. Later spells follow a pass that ran, so they are dated now.
func (s *Server) markAutoSyncIdle(ctx context.Context) bool {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	var since time.Time // zero: the poller dates it now
	if !s.poller.autoSyncIdleObserved() {
		since = s.startedAt
		if persisted, err := s.store.AutoSyncIdleSince(ctx); err != nil {
			debuglog.Warn("frontdesk: auto-sync: read idle since", "error", err)
		} else if !persisted.IsZero() {
			since = persisted
		}
	}
	changed := s.poller.setAutoSyncIdle(true, since)
	if changed {
		s.persistAutoSyncIdle(ctx, s.poller.autoSyncIdle())
	}
	return changed
}

// clearAutoSyncIdle records the auto-sync as running (or off), drops the
// persisted idle spell, and starts the primary's failure count over. It reports
// whether it was idle until now. The first verdict of a process clears the
// record even when nothing changed in memory, since a spell a previous process
// persisted is over too.
func (s *Server) clearAutoSyncIdle(ctx context.Context) bool {
	s.primaryReadFailures.Store(0)
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	first := !s.poller.autoSyncIdleObserved()
	changed := s.poller.setAutoSyncIdle(false, time.Time{})
	if changed || first {
		s.persistAutoSyncIdle(ctx, time.Time{})
	}
	return changed
}

// persistAutoSyncIdle writes the idle spell's start, zero to clear it. A failed
// write is logged: the in-memory verdict stands, and only a restart reads the
// record.
func (s *Server) persistAutoSyncIdle(ctx context.Context, since time.Time) {
	if err := s.store.SetAutoSyncIdleSince(ctx, since); err != nil {
		debuglog.Warn("frontdesk: auto-sync: persist idle since", "error", err)
	}
}
