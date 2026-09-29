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
// The outcome feeds the auto-sync's idle state (recordPrimaryVerdict), which
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
		if s.recordPrimaryVerdict(ctx, cfg, false, cannotRun) {
			debuglog.Warn("frontdesk: auto-sync is enabled but cannot run: primary unavailable", "primary_id", cfg.PrimaryID, "error", err)
		}
		return nil, "", "", nil, false
	}
	if s.recordPrimaryVerdict(ctx, cfg, true, false) {
		debuglog.Info("frontdesk: auto-sync can run again: primary available", "member", primary.Name)
	}
	return primary, token, hash, sections, true
}

// recordPrimaryVerdict applies one read of the primary to the idle state and
// reports whether the auto-sync flipped between running and idle. A failed read
// counts towards primaryUnreachableIdleThreshold, or marks the auto-sync idle at
// once when cannotRun. The verdict is dropped when the auto-sync setup the
// read was made for (cfg) is no longer the stored one: a PUT that repointed the
// primary or switched auto-sync while the read was in flight has already
// started the idle question over (clearAutoSyncIdle), and the old primary's
// answer says nothing about the new setup. The check runs under idleMu, which
// that PUT's reset takes too, so a verdict either lands before the reset or
// sees the change.
func (s *Server) recordPrimaryVerdict(ctx context.Context, cfg AutoSyncConfig, available, cannotRun bool) bool {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	cur, err := s.store.GetAutoSync(ctx)
	if err != nil || cur != cfg {
		return false
	}
	if available {
		return s.clearAutoSyncIdleLocked(ctx)
	}
	s.primaryReadFailures++
	if !cannotRun && s.primaryReadFailures < primaryUnreachableIdleThreshold {
		return false
	}
	return s.markAutoSyncIdleLocked(ctx)
}

// markAutoSyncIdleLocked records the enabled auto-sync as unable to run and
// reports whether it was running until now. The caller holds idleMu. A new idle
// spell is persisted (Store.SetAutoSyncIdleSince), so the first idle verdict
// after this process started takes the spell a previous process recorded, and a
// Front Desk restarted daily still reports a fleet idle for days as stale. With
// nothing on record that verdict is dated to this process's start, not further
// back: a primary slow to answer after a restart is not an idle spell that
// began at the fleet's last sync. Later spells follow a pass that ran, so they
// are dated now.
//
// A record that cannot be read leaves the spell dated to the start
// provisionally (idleSpellUndated): each later idle verdict reads it again, and
// the first read that succeeds re-dates the spell to the persisted one. Until
// then the record is not overwritten, since it may hold an older spell.
func (s *Server) markAutoSyncIdleLocked(ctx context.Context) bool {
	var since time.Time // zero: the poller dates it now
	first := !s.poller.autoSyncIdleObserved()
	if first || s.idleSpellUndated {
		since = s.startedAt
		persisted, err := s.store.AutoSyncIdleSince(ctx)
		switch {
		case err != nil:
			debuglog.Warn("frontdesk: auto-sync: read idle since", "error", err)
			s.idleSpellUndated = true
		default:
			if !persisted.IsZero() {
				since = persisted
			}
			if s.idleSpellUndated {
				// This spell began while the record was unreadable: date it now
				// from the record, and write the date if the record held none.
				s.poller.redateAutoSyncIdle(since)
				s.idleSpellUndated = false
				s.idleRecordDirty = persisted.IsZero()
			}
		}
	}
	changed := s.poller.setAutoSyncIdle(true, since)
	if (changed || s.idleRecordDirty) && !s.idleSpellUndated {
		s.persistAutoSyncIdle(ctx, s.poller.autoSyncIdle())
	}
	return changed
}

// clearAutoSyncIdle records the auto-sync as running (or off), drops the
// persisted idle spell, and starts the primary's failure count over. It reports
// whether it was idle until now.
func (s *Server) clearAutoSyncIdle(ctx context.Context) bool {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	return s.clearAutoSyncIdleLocked(ctx)
}

// clearAutoSyncIdleLocked is clearAutoSyncIdle with idleMu held. The first
// verdict of a process clears the record even when nothing changed in memory,
// since a spell a previous process persisted is over too, and so does any
// verdict after a failed write.
func (s *Server) clearAutoSyncIdleLocked(ctx context.Context) bool {
	s.primaryReadFailures = 0
	s.idleSpellUndated = false
	first := !s.poller.autoSyncIdleObserved()
	changed := s.poller.setAutoSyncIdle(false, time.Time{})
	if changed || first || s.idleRecordDirty {
		s.persistAutoSyncIdle(ctx, time.Time{})
	}
	return changed
}

// persistAutoSyncIdle writes the idle spell's start, zero to clear it. The
// caller holds idleMu. A failed write is logged and leaves idleRecordDirty set,
// so the next verdict writes its state again; the in-memory verdict stands
// meanwhile.
func (s *Server) persistAutoSyncIdle(ctx context.Context, since time.Time) {
	err := s.store.SetAutoSyncIdleSince(ctx, since)
	s.idleRecordDirty = err != nil
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync: persist idle since", "error", err)
	}
}
