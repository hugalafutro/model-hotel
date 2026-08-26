package frontdesk

import (
	"context"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// warnIfBuildGateDegraded says so, once, when the primary reports no usable
// commit.
//
// buildSkew falls back to comparing versions alone when either side's commit is
// missing or "unknown", and an unstamped PRIMARY degrades every verdict in the
// fleet at once: with all members on the "dev" placeholder, that fallback passes
// everything, which is the behaviour this gate exists to replace. The wizard
// surfaces the same condition as commit_vouched and asks the operator to
// acknowledge it, but auto-sync never prompts, so without this line the loop
// would quietly push on the old, blind comparison.
//
// A build reaches this state by skipping the Makefile: the Dockerfile defaults
// COMMIT to "unknown", and only the Makefile and CI pass the real SHA.
//
// Edge-triggered like the hold state it sits beside, because a pass runs on
// every tick and a per-pass warning would bury itself.
func (s *Server) warnIfBuildGateDegraded(primary memberBuild) {
	degraded := !stampedCommit(primary.Commit)
	s.syncHeldMu.Lock()
	repeat := degraded == s.ungatedCommitWarned
	s.ungatedCommitWarned = degraded
	s.syncHeldMu.Unlock()
	if repeat || !degraded {
		return
	}
	debuglog.Warn("frontdesk: auto-sync: primary reports no build commit, "+
		"config sync is gated on the app version alone",
		"primary_version", primary.Version)
}

// holdMemberForSkew marks a member as held for version skew and emits
// config.sync_held once on the transition into held (edge-triggered, mirroring
// the poller's versionFailures pattern), so a member that stays skewed does not
// re-alert every pass. The hold itself is enforced by the caller skipping the
// push; this only tracks and reports it.
func (s *Server) holdMemberForSkew(ctx context.Context, m *Member, primary, member memberBuild) {
	s.syncHeldMu.Lock()
	already := s.syncHeld[m.ID]
	s.syncHeld[m.ID] = true
	s.syncHeldMu.Unlock()
	if already {
		return
	}
	if s.heldPerLog(ctx, m) {
		// The previous process already announced this hold and it never closed:
		// the member is continuing a hold, not entering one, so re-emitting
		// config.sync_held would duplicate the alert after every restart.
		return
	}
	debuglog.Debug("frontdesk: auto-sync: holding member for build skew",
		"member", m.Name, "primary_build", primary.describe(), "member_build", member.describe())
	s.emit(ctx, Event{
		Type: "config.sync_held", Severity: "warning", Source: "frontdesk",
		Message:  fmt.Sprintf("Held sync to %s: its build differs from the primary's", m.Name),
		MemberID: m.ID,
		// Both halves of each build ride in the metadata: on a fleet where every
		// version reads "dev", the commit is the only field that says what
		// differed, and an operator reading the event needs to see it.
		Metadata: map[string]any{
			"primary_version": primary.Version, "member_version": member.Version,
			"primary_commit": primary.Commit, "member_commit": member.Commit,
		},
	})
}

// closeSyncHold forgets a member's held-for-skew state so a future divergence
// re-emits config.sync_held, and emits config.sync_recovered once on the
// transition out. Without the closing event, config.sync_held stays the
// member's newest event indefinitely, and every consumer that leads with it
// (Bellhop's member pill, the events feed) keeps telling a "held" story about a
// member the live status shows verified in sync. Called whenever the member is
// not skewed on a pass, and for the primary itself, which may carry a hold
// from before its promotion; a member that was never held emits nothing. The
// persisted log is consulted too (heldPerLog), so a hold announced before a
// restart — syncHeld itself is in-memory — is also closed out.
//
// message is the operator-facing reason the hold is over, supplied by the call
// site because only it knows which story is true: sync resuming, versions
// realigning on a member sync cannot reach, or the member's promotion. It must
// keep to one of the two shapes Bellhop's name-stripper knows (the member name
// leading the sentence, or a " to <name>" target).
func (s *Server) closeSyncHold(ctx context.Context, m *Member, message string) {
	s.syncHeldMu.Lock()
	was := s.syncHeld[m.ID]
	delete(s.syncHeld, m.ID)
	s.syncHeldMu.Unlock()
	if !was && !s.heldPerLog(ctx, m) {
		return
	}
	ev := Event{
		Type: "config.sync_recovered", Severity: "success", Source: "frontdesk",
		Message: message, MemberID: m.ID,
	}
	// Inserted and published by hand rather than through emit: the publish must
	// wait for the insert. On a failed insert the log still ends with the member
	// held, so the memoised log verdict is dropped and the next pass re-reads
	// the log, finds the hold still open, and retries — and holding the bus
	// publish back with it keeps SSE consumers and the alert dispatcher at one
	// event per close instead of one per retry. The in-memory hold stays
	// cleared either way: the fleet state is already right.
	stored, err := s.store.InsertEvent(ctx, ev)
	if err != nil {
		debuglog.Warn("frontdesk: persist event", "type", ev.Type, "error", err)
		s.syncHeldMu.Lock()
		delete(s.holdLogChecked, m.ID)
		s.syncHeldMu.Unlock()
		return
	}
	logEvent(stored)
	s.bus.Publish(busEvent(stored))
}

// heldPerLog reports whether the event log left this member inside an open
// hold: a config.sync_held with no config.sync_recovered after it, decided by
// one newest-of-the-two-types read under the store's deterministic tie-break.
// The in-memory syncHeld map is authoritative from the moment a pass touches
// the member, so the log is read at most once per member per process (a read
// error is not memoised and retries on the next pass, and closeSyncHold drops
// the memo again when its closing event fails to persist).
func (s *Server) heldPerLog(ctx context.Context, m *Member) bool {
	s.syncHeldMu.Lock()
	done := s.holdLogChecked[m.ID]
	s.syncHeldMu.Unlock()
	if done {
		return false
	}
	newest, found, err := s.store.NewestEventOfTypes(ctx, m.ID, "config.sync_held", "config.sync_recovered")
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync: read persisted hold state", "member", m.Name, "error", err)
		return false
	}
	s.syncHeldMu.Lock()
	s.holdLogChecked[m.ID] = true
	s.syncHeldMu.Unlock()
	return found && newest.Type == "config.sync_held"
}
