package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// HA auto config sync: the "set and forget" half of fleet config replication.
// configsync.go runs the manual, wizard-driven sync; this reads the designated
// primary on a background tick and propagates its config to every other member.
//
// A pass runs every settled tick, so drift on a member is measured as well as
// drift on the primary. A settled fleet costs one hash read per member: a member
// whose hash matches is skipped before any dry run or import, and the primary's
// export is read only once a member is found that needs it.
//
// Convergence is decided by comparing the member's own hash
// (GET /api/config/version) with the primary's, not by the member's report of its
// own import. The hash covers the syncable payload alone, under a total order and
// carrying names and stable model refs rather than instance-local ids, so equal
// hashes mean identical config. A member is pushed to on one pass and verified on
// the next.
//
// A member whose hash differs, or cannot be read, falls back to its own dry-run
// diff to decide what to push. That diff is presence-based, so it seldom reads as
// empty; incompleteRetryInterval bounds a member that can never converge.
//
// No request or prompt content is ever read; only provider/key names and counts.

const (
	memberConfigVersionPath = "/api/config/version"

	// autoSyncIntervalSecs is how often the auto-syncer samples the primary.
	// Slower than the health poll: an apply runs member-side discovery.
	autoSyncIntervalSecs = 15

	// autoSyncReason is stamped on each synced member's last-sync record and shown
	// in the Members table tooltip. Subject-free: the same string is the tail of the
	// fleet-wide roll-up event ("Auto-synced 2 members: ..."), where a singular
	// subject would have no referent.
	autoSyncReason = "did not hold the primary's config"

	// autoSyncKickReason is stamped instead when the operator turns auto-sync on
	// or repoints the primary.
	autoSyncKickReason = "auto-sync was enabled"

	// autoSyncKickTimeout caps the detached pass fired when auto-sync is enabled,
	// so a stuck member cannot leak the goroutine. Generous: the pass imports into
	// every drifted member in turn.
	autoSyncKickTimeout = 5 * time.Minute

	// autoSyncStaleThreshold is how long the fleet may go unsynced, with auto-sync
	// off, before Front Desk warns of possible drift. Long enough that a brief
	// manual-sync gap never trips it.
	autoSyncStaleThreshold = 24 * time.Hour

	// incompleteRetryInterval rate-limits the re-push of a member that took a config
	// and still does not hold it. Only the push is bounded: the member stays
	// unconverged, so the badge and the alert persist. Without it a member that
	// cannot converge would drive a full re-import, and the member-side discovery it
	// runs, every tick.
	incompleteRetryInterval = 10 * time.Minute

	// autoSyncFaultyThreshold is the second staleness tier: with auto-sync off, a
	// fleet unsynced this long is faulty rather than merely degraded
	// (fleetstate.go). Three days, so a weekend gap alone never trips it.
	autoSyncFaultyThreshold = 72 * time.Hour
)

// autoSyncStaleTier grades the fleet's silent-drift risk: 0 fresh (or auto-sync
// on / no primary designated, where staleness is meaningless), 1 unsynced beyond
// autoSyncStaleThreshold (degraded), 2 beyond autoSyncFaultyThreshold (faulty).
// A fleet with no recorded sync at all is tier 1: there is no timestamp to age
// against, so it cannot honestly escalate to tier 2. haveSync is false when no
// successful sync has ever been recorded.
func autoSyncStaleTier(cfg AutoSyncConfig, lastSync time.Time, haveSync bool, now time.Time) int {
	if cfg.Enabled || cfg.PrimaryID == "" {
		return 0
	}
	if !haveSync {
		return 1
	}
	switch age := now.Sub(lastSync); {
	case age > autoSyncFaultyThreshold:
		return 2
	case age > autoSyncStaleThreshold:
		return 1
	}
	return 0
}

// autoSyncStale reports whether the fleet's config is at risk of silent drift:
// true exactly when autoSyncStaleTier returns 1 or higher. Consumed by the
// config.autosync_stale watchdog and the autosync payload's Stale flag.
func autoSyncStale(cfg AutoSyncConfig, lastSync time.Time, haveSync bool, now time.Time) bool {
	return autoSyncStaleTier(cfg, lastSync, haveSync, now) >= 1
}

// RunAutoSync samples the designated primary on a fixed tick and converges the
// fleet on its config. It blocks until ctx is cancelled and is started once,
// alongside the poller. The loop owns the small amount of state (the previously
// observed hash) used to coalesce a burst of edits into one sync.
func (s *Server) RunAutoSync(ctx context.Context) {
	ticker := time.NewTicker(autoSyncIntervalSecs * time.Second)
	defer ticker.Stop()
	var prev string // primary hash seen on the previous tick (coalescing window)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prev = s.autoSyncOnce(ctx, prev)
		}
	}
}

// autoSyncOnce performs one auto-sync sample. prev is the primary config hash
// observed on the previous tick; the returned value is the hash to carry into the
// next tick. It never returns an error: every failure path logs and leaves the
// fleet untouched for the next tick.
//
// Every tick on which the primary has settled runs a convergence pass, whether or
// not the primary's config moved. A member is only measured by a pass, so a fleet
// that stopped running them would stop noticing a member drifting on its own.
func (s *Server) autoSyncOnce(ctx context.Context, prev string) string {
	cfg, err := s.store.GetAutoSync(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync: read config", "error", err)
		return ""
	}
	if !cfg.Enabled || cfg.PrimaryID == "" {
		return "" // disabled or no primary designated: nothing to do
	}

	primary, primaryToken, hash, ok := s.primaryConfigHash(ctx, cfg)
	if !ok {
		return ""
	}

	// Coalesce: only act once the primary's config has settled (the same hash two
	// ticks running), so a multi-step edit session triggers one sync rather than
	// one per intermediate save.
	if hash != prev {
		// The primary moved: let every incomplete member retry on the next pass
		// rather than waiting out its interval.
		s.resetIncompleteRetries()
		return hash
	}

	s.convergeFleet(ctx, primary, primaryToken, hash, autoSyncReason, cfg.Gen)
	return hash
}

// forceAutoSyncNow runs one convergence pass immediately, bypassing the tick
// loop's coalescing gate: an operator enabling auto-sync or repointing the primary
// is a deliberate act, with no mid-edit ambiguity to coalesce against. Safe in its
// own goroutine with a detached context, and a no-op when auto-sync is off or has
// no primary. Failures log and the loop retries.
func (s *Server) forceAutoSyncNow(ctx context.Context) {
	cfg, err := s.store.GetAutoSync(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync kick: read config", "error", err)
		return
	}
	if !cfg.Enabled || cfg.PrimaryID == "" {
		return
	}
	primary, primaryToken, hash, ok := s.primaryConfigHash(ctx, cfg)
	if !ok {
		return
	}
	// A kick is a deliberate operator action, so it retries every incomplete
	// member now rather than leaving it inside its retry interval.
	s.resetIncompleteRetries()
	s.convergeFleet(ctx, primary, primaryToken, hash, autoSyncKickReason, cfg.Gen)
}

// primaryConfigHash resolves the designated primary, loads its admin token, and
// reads its current syncable-config hash. ok is false (with a debug log) when
// the primary was removed, lost its token, or is unreachable, in which case the
// caller skips this round and retries later.
func (s *Server) primaryConfigHash(ctx context.Context, cfg AutoSyncConfig) (primary *Member, token, hash string, ok bool) {
	primary, token, err := s.memberTokenOrErr(ctx, cfg.PrimaryID)
	if err != nil {
		// No source to sync from: the primary was removed or lost its token.
		debuglog.Debug("frontdesk: auto-sync: primary unavailable", "error", err)
		return nil, "", "", false
	}
	hash, err = s.fetchMemberConfigVersion(ctx, primary, token)
	if err != nil {
		debuglog.Debug("frontdesk: auto-sync: read primary version", "member", primary.Name, "error", err)
		return nil, "", "", false
	}
	return primary, token, hash, true
}

// convergeFleet pushes the primary's config to every member that needs it and
// emits one roll-up event tagged with reason. Shared by the tick loop and the
// enable-time kick. gen is the rearm generation captured before the member list
// was read; applyAutoSync aborts on it, so a rearm landing mid-pass stops this
// pass rather than letting it finish a stale write.
//
// Nothing fleet-wide is recorded. Convergence is per member: the verified-in-sync
// heartbeat when a member's hash matches, the diverged flag and amber badge when
// it does not.
func (s *Server) convergeFleet(ctx context.Context, primary *Member, primaryToken, hash, reason string, gen int64) {
	applied := s.applyAutoSync(ctx, primary, primaryToken, hash, reason, gen)
	if applied > 0 {
		noun := "members"
		if applied == 1 {
			noun = "member"
		}
		s.emit(ctx, Event{
			Type: "config.auto_synced", Severity: "info", Source: "frontdesk",
			Message: fmt.Sprintf("Auto-synced %d %s: %s", applied, noun, reason),
		})
	}
}

// applyAutoSync pushes the primary's config to every other tokened member that
// does not already hold it, and returns how many members it actually re-synced.
// hash is the primary's current config hash, and comparing it with each member's
// own is the convergence criterion: a member matching it is converged and left
// untouched, a member that differs is not, whatever it reported about its own
// import. reason is stamped onto each synced member's last-sync marker.
//
// The pass that pushes a member does not verify it; the next pass's hash query
// does, one tick later.
//
// gen is the rearm generation captured before this pass began. A rearm (member
// add, token update, enable, or primary repoint) bumps it, and the pass re-checks
// it twice per member: at the top of the loop, and again right before the mutating
// import, after the slow dry-run where a repoint is most likely to slip in. It
// aborts the moment the generation changes, so a slow pass cannot import a
// now-stale config into a member the operator has just repointed away from. The
// in-flight import call is the one window no pre-check can close; passCtx covers
// it, and the rearm's own pass converges whatever is left.
func (s *Server) applyAutoSync(ctx context.Context, primary *Member, primaryToken, hash, reason string, gen int64) (applied int) {
	// A read error reports "not stale": a transient DB failure must not abort an
	// otherwise valid pass.
	stale := func() bool {
		cur, err := s.store.AutoSyncGen(ctx)
		return err == nil && cur != gen
	}
	if stale() {
		return 0 // a rearm already landed: don't push the stale export at all
	}

	// passCtx is the cancellation point the pre-import gates cannot provide: a
	// watcher cancels it the instant a rearm moves the generation, aborting an
	// import already in flight. Every member HTTP call below runs under it, and the
	// deferred cancel stops the watcher when the pass returns.
	//
	// rearmCh is captured before watchRearm re-reads the generation, so a rearm
	// landing in that gap still wakes the watcher: the channel is closed, not
	// missed.
	rearmCh := s.rearmChan()
	passCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.watchRearm(passCtx, rearmCh, gen, cancel)

	// The primary's export is fetched on demand: a settled fleet never needs it, and
	// building and shipping the whole envelope every tick is the bulk of what an idle
	// fleet would otherwise cost. Memoised, so a pass pushing to several members
	// still reads the primary once; a failed read is memoised too and aborts the
	// pass rather than being retried per member.
	var (
		export    []byte
		exportErr error
		exported  bool
	)
	primaryExport := func() ([]byte, error) {
		if !exported {
			export, exportErr = s.fetchMemberExport(passCtx, primary, primaryToken)
			exported = true
			if exportErr != nil {
				debuglog.Warn("frontdesk: auto-sync: read primary export", "member", primary.Name, "error", exportErr)
			}
		}
		return export, exportErr
	}

	members, err := s.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync: list members", "error", err)
		return 0
	}

	primaryVer := s.poller.MemberVersion(primary.ID)
	for _, m := range members {
		if m.ID == primary.ID {
			continue // the source is never written to
		}
		if stale() {
			// A rearm/repoint landed mid-pass: stop before importing the stale export
			// into any further member, and leave the rest to the rearm's own pass.
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass after rearm", "synced", applied)
			break
		}
		if !m.HasToken {
			// Cannot be authenticated to, so it can be measured in neither direction.
			// Every pass re-reads the member list, so it is measured like any other from
			// the moment it has a token.
			continue
		}
		token, ok, err := s.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			// Token ciphertext exists but could not be loaded or decrypted: a MASTER_KEY
			// mismatch, a transient DB error, or the token cleared since the snapshot.
			// Nothing can be measured or pushed, so leave it for the next tick.
			debuglog.Debug("frontdesk: auto-sync: member token unavailable, will retry", "member", m.Name, "loaded", ok, "error", err)
			continue
		}
		// Version gate: never push onto a member running a different app version. An
		// older primary's export omits settings the newer member legitimately has, and
		// the member-side converge-delete would drop them. Fails closed on an unknown
		// version, and is re-evaluated every pass, so it resumes automatically.
		memberVer := s.poller.MemberVersion(m.ID)
		if versionSkew(primaryVer, memberVer) {
			s.holdMemberForSkew(ctx, m, primaryVer, memberVer)
			continue
		}
		s.clearSyncHold(m.ID)
		// Ask the member what config it actually holds. Every member hashes the same
		// syncable payload (providers, virtual keys, syncable settings, custom failover
		// groups, users) under a total order, carrying names and stable model refs
		// rather than instance-local ids, so an equal hash means this member holds
		// exactly this config. Neither the member's own report of its import nor the
		// dry-run can establish that: the report can claim a clean apply while the
		// config differs, and computeDiff keys on presence, so a matching member still
		// reports every shared entity as updated.
		memberHash, verErr := s.fetchMemberConfigVersion(passCtx, m, token)
		if verErr == nil && memberHash == hash {
			// Converged. Close out any divergence it carried, which emits
			// config.sync_recovered once on the way out, and move the verified-in-sync
			// heartbeat. Only a hash match moves it, so it means "measured holding the
			// primary's config", never "written to".
			s.clearMemberIncomplete(ctx, m)
			s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
			continue
		}
		// From here the member does not hold this config, or could not be asked.
		if verErr != nil {
			// An unread hash proves nothing either way: neither converged nor flagged.
			// Falls through to the dry-run, which reports the real failure.
			debuglog.Debug("frontdesk: auto-sync: read member config version", "member", m.Name, "error", verErr)
		} else if s.hasBeenPushedSinceReset(m.ID) {
			// Measured divergence in a member that has already committed this config: a
			// failure to converge, not a member nobody has reached yet.
			// resetIncompleteRetries zeroes that signal whenever the primary's config
			// moves, so an ordinary edit never turns the badge amber for a tick.
			s.markMemberIncomplete(ctx, m)
		}
		// Only the push is rate-limited. The member stays unconverged, so the badge
		// and the alert persist.
		if s.shouldSkipIncompleteRetry(m.ID, time.Now()) {
			continue
		}
		// First point on the pass that needs the primary's config.
		export, err := primaryExport()
		if err != nil {
			break // the source is unreadable: no member can converge this pass
		}
		// What this member needs is decided by its own dry-run diff, which compares by
		// name and so is valid across instances. The dry-run is never fenced, so the
		// generation is not sent on it.
		res, status, err := s.pushMemberImport(passCtx, m, token, export, true, gen)
		if err != nil {
			debuglog.Debug("frontdesk: auto-sync: member unreachable, will retry", "member", m.Name, "status", status, "error", err)
			continue
		}
		if !res.SchemaVersionOK || !res.MasterKeyOK {
			// Version skew or MASTER_KEY mismatch. The manual wizard surfaces these
			// explicitly; here the member is held and retried.
			debuglog.Debug("frontdesk: auto-sync: member not syncable", "member", m.Name)
			continue
		}
		added, updated, removed := res.Diff.counts()
		if added+updated+removed == 0 {
			// Nothing to write, yet the hash says this member does not hold the primary's
			// config, or could not be asked. Nothing here would make it match, so it stays
			// unconverged and gets no heartbeat: the hash decides that, not the diff.
			debuglog.Debug("frontdesk: auto-sync: member differs but its diff is empty", "member", m.Name)
			continue
		}

		// Front Desk takes no snapshot before overwriting a member; members back
		// themselves up on their own schedule. The trade is deliberate: a bad config
		// propagation cannot be rolled back from a snapshot Front Desk just took, and
		// in exchange no member accumulates a pg_dump per sync pass.

		// Final staleness gate, tightest to the mutation: a rearm can land during this
		// member's slow dry-run. The window between here and the commit on the member
		// is covered by passCtx, which watchRearm cancels.
		if stale() {
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass before import", "synced", applied)
			break
		}

		// applyMemberConfig stamps the member's last-sync marker with this reason.
		// Per-member success events are suppressed (emitSuccessEvent=false) in favour
		// of one roll-up below. gen lets the member's commit fence refuse this push if
		// a newer generation already won the in-flight race (out.Stale, a benign
		// supersede).
		out := s.applyMemberConfig(passCtx, m, token, export, reason, false, gen)
		if out.OK || out.Incomplete || out.TimedOut {
			// The member received the config, whether or not it built all of it and
			// whether or not it answered in time. The stamp bounds the re-push and tells
			// the next pass this member has had its chance, so a hash that still differs
			// then is a failure to converge rather than a member nobody reached. A
			// timed-out member is very likely still importing, so it counts too.
			//
			// A push the member refused or never received is deliberately not stamped,
			// so a transient failure retries next tick instead of waiting out the
			// interval.
			s.recordSyncAttempt(m.ID, out.Unapplied, out.Partial)
		}
		if !out.OK {
			// A failure, already surfaced by applyMemberConfig, or a benign fence
			// supersede. The next pass measures this member again either way.
			continue
		}
		applied++
	}
	return applied
}

// holdMemberForSkew marks a member as held for version skew and emits
// config.sync_held once on the transition into held (edge-triggered, mirroring
// the poller's versionFailures pattern), so a member that stays skewed does not
// re-alert every pass. The hold itself is enforced by the caller skipping the
// push; this only tracks and reports it.
func (s *Server) holdMemberForSkew(ctx context.Context, m *Member, primaryVer, memberVer string) {
	s.syncHeldMu.Lock()
	already := s.syncHeld[m.ID]
	s.syncHeld[m.ID] = true
	s.syncHeldMu.Unlock()
	if already {
		return
	}
	debuglog.Debug("frontdesk: auto-sync: holding member for version skew",
		"member", m.Name, "primary_version", primaryVer, "member_version", memberVer)
	s.emit(ctx, Event{
		Type: "config.sync_held", Severity: "warning", Source: "frontdesk",
		Message:  fmt.Sprintf("Held sync to %s: its app version differs from the primary's", m.Name),
		MemberID: m.ID,
		Metadata: map[string]any{"primary_version": primaryVer, "member_version": memberVer},
	})
}

// clearSyncHold forgets a member's held-for-skew state so a future divergence
// re-emits config.sync_held. Called whenever the member is not skewed on a pass.
func (s *Server) clearSyncHold(memberID string) {
	s.syncHeldMu.Lock()
	delete(s.syncHeld, memberID)
	s.syncHeldMu.Unlock()
}

// incompleteState is what Front Desk remembers about a member it has given the
// primary's config to: enough to bound the re-push, describe the divergence, and
// know whether the member counts as diverged.
type incompleteState struct {
	// lastAttempt is when this member last committed a config Front Desk pushed.
	// Zero means "push on the next pass", and doubles as the "has not had its
	// chance yet" signal that keeps an unreached member from being flagged.
	lastAttempt time.Time
	// lastUnapplied names the custom failover groups the member reported it could
	// not build, so an alert raised on a later pass can still be specific. Empty
	// when it named none, which is also what an unexplained divergence looks like.
	lastUnapplied []string
	// lastPartial names the custom failover groups the member built with fewer
	// entries than the primary sent, so the alert can say which group is short.
	lastPartial []string
	// diverged is true once the member's own hash has been measured against the
	// primary's and differed after it took the config. The fleet badge and the
	// edge-triggered alert read this; a member merely pushed to is not diverged.
	diverged bool
}

// recordSyncAttempt remembers that a member received a config Front Desk pushed,
// with whatever it reported it could not build or built short. The stamp bounds the
// re-push (shouldSkipIncompleteRetry) and marks the member as having had its chance
// (hasBeenPushedSinceReset).
func (s *Server) recordSyncAttempt(memberID string, unapplied, partial []string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	st.lastAttempt = time.Now()
	st.lastUnapplied = unapplied
	st.lastPartial = partial
	s.syncIncomplete[memberID] = st
}

// hasBeenPushedSinceReset reports whether this member has received the config
// since the last reset (a primary edit or the enable-time kick). This is the whole
// no-flap guard: a member not yet reached must never be flagged for diverging.
func (s *Server) hasBeenPushedSinceReset(memberID string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return !s.syncIncomplete[memberID].lastAttempt.IsZero()
}

// markMemberIncomplete records a member whose own config hash differs from the
// primary's after it took that config, and emits config.sync_incomplete once on the
// transition in. Edge-triggered: the member is re-checked every pass, so a
// level-triggered event would alert on each one until it converged.
func (s *Server) markMemberIncomplete(ctx context.Context, m *Member) {
	s.syncIncompleteMu.Lock()
	st := s.syncIncomplete[m.ID]
	already := st.diverged
	st.diverged = true
	s.syncIncomplete[m.ID] = st
	// Copied under the lock, and always lists rather than null, so consumers see one
	// shape and the emit below reads stable slices.
	names := append([]string{}, st.lastUnapplied...)
	partial := append([]string{}, st.lastPartial...)
	s.syncIncompleteMu.Unlock()
	if already {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_incomplete", Severity: "warning", Source: "frontdesk",
		Message:  divergenceMessage(m.Name, names, partial),
		MemberID: m.ID,
		Metadata: map[string]any{"unapplied": names, "partial": partial},
	})
}

// divergenceMessageMaxNames bounds how many group names each clause of
// divergenceMessage spells out, so a member short across dozens of groups cannot
// produce a message dozens of names long. The event Metadata carries the full lists
// untruncated.
const divergenceMessageMaxNames = 5

// joinCapped renders names as a comma-separated list, capped at limit entries,
// with a trailing count of however many were left off.
func joinCapped(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:limit], ", "), len(names)-limit)
}

// divergenceMessage renders the operator-facing reason a member does not hold the
// primary's config, from the two things it can report about its own import.
//
// unapplied are custom failover groups it could not build at all; partial are
// groups it built with fewer entries than the primary sent, so it fails over across
// fewer providers for them. Both name their groups rather than only counting them,
// because the names point at the routing that is missing or thinner. Unapplied is
// the more severe case, and carries the count as well.
//
// With neither, the divergence is one Front Desk measured but the member did not
// explain: it committed the config, reported nothing wrong, and still does not
// match. A count there would read "could not build 0 failover group(s)".
func divergenceMessage(member string, unapplied, partial []string) string {
	var clauses []string
	if len(unapplied) > 0 {
		clauses = append(clauses, fmt.Sprintf("could not build %d failover group(s): %s",
			len(unapplied), joinCapped(unapplied, divergenceMessageMaxNames)))
	}
	if len(partial) > 0 {
		clauses = append(clauses, fmt.Sprintf("built %s with fewer entries than the primary has",
			joinCapped(partial, divergenceMessageMaxNames)))
	}
	if len(clauses) == 0 {
		return fmt.Sprintf("%s applied the config but does not match the primary's config", member)
	}
	return fmt.Sprintf("%s applied the config but %s", member, strings.Join(clauses, ", and "))
}

// clearMemberIncomplete forgets everything about a member whose hash now matches
// the primary's and emits config.sync_recovered once on the transition out, so a
// later divergence re-emits config.sync_incomplete. A member that was never
// flagged emits nothing, keeping ordinary successful passes quiet. Dropping the
// whole entry is deliberate: a converged member has no retry to bound and no
// divergence to describe.
//
// The only way out of the flagged state, and it runs from the auto-sync loop
// alone. A manual sync does not clear the flag: its only evidence is the member's
// own report of its import, which is the trust this criterion replaces. With
// auto-sync off a flagged member keeps its amber badge however many times the
// wizard runs, until a pass measures it as matching.
func (s *Server) clearMemberIncomplete(ctx context.Context, m *Member) {
	s.syncIncompleteMu.Lock()
	was := s.syncIncomplete[m.ID].diverged
	delete(s.syncIncomplete, m.ID)
	s.syncIncompleteMu.Unlock()
	if !was {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_recovered", Severity: "success", Source: "frontdesk",
		Message: fmt.Sprintf("%s now holds the primary's config", m.Name), MemberID: m.ID,
	})
}

// incompleteSnapshot copies the diverged set under its lock for the fleet state
// calculation, mirroring heldSnapshot. Members merely pushed to are not in it:
// only a measured divergence degrades the fleet. Disabling auto-sync freezes the
// set rather than clearing it, so the fleet does not report a false ok.
func (s *Server) incompleteSnapshot() map[string]bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	out := make(map[string]bool, len(s.syncIncomplete))
	for id, st := range s.syncIncomplete {
		if st.diverged {
			out[id] = true
		}
	}
	return out
}

// shouldSkipIncompleteRetry reports whether a member's re-push is still
// rate-limited. The caller keeps the member not-converged either way; this only
// suppresses the import.
func (s *Server) shouldSkipIncompleteRetry(memberID string, now time.Time) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st, ok := s.syncIncomplete[memberID]
	if !ok || st.lastAttempt.IsZero() {
		return false
	}
	return now.Sub(st.lastAttempt) < incompleteRetryInterval
}

// resetIncompleteRetries drops every retry timer so each member is pushed again on
// the next pass. The entries stay, so the badge and the alert are unaffected.
// Called when the operator's intent changes (the primary's config moved, or
// auto-sync was enabled), where waiting out the interval would delay a deliberate
// edit. It also clears the "has had its chance" signal, so the pass right after
// such a change flags nobody.
func (s *Server) resetIncompleteRetries() {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	for id, st := range s.syncIncomplete {
		st.lastAttempt = time.Time{}
		s.syncIncomplete[id] = st
	}
}

// watchRearm cancels a convergence pass the moment its rearm generation goes
// stale, so a rearm landing while applyMemberConfig is mid-flight aborts the HTTP
// request instead of finishing a now-stale write.
//
// rearmCh is the in-process broadcast closed by signalRearm, so the wake is
// synchronous with the generation bump rather than gated on a poll. The generation
// is re-read first to close the gap between the caller capturing gen and this
// watcher starting, where the channel close may predate the capture. The watcher
// exits as soon as ctx is done, so it never outlives the pass. A transient read
// error is ignored; the pass's own stale() gates remain.
func (s *Server) watchRearm(ctx context.Context, rearmCh <-chan struct{}, gen int64, cancel context.CancelFunc) {
	if cur, err := s.store.AutoSyncGen(ctx); err == nil && cur != gen {
		cancel()
		return
	}
	select {
	case <-ctx.Done():
	case <-rearmCh:
		// A rearm/repoint broadcast woke us. It only fires after auto_sync_gen has
		// moved, so any wake means this pass is stale: cancel it.
		cancel()
	}
}

// fetchMemberConfigVersion reads a member's syncable-config hash from
// GET /api/config/version. The hash changes if and only if a synced entity changed,
// and every member computes it over the same deterministically ordered payload, so
// it serves two purposes: from the primary it is the drift signal that starts a
// pass, and from a member it answers whether that member holds the primary's
// config.
//
// It uses readClient, not the health-probe client: the handler builds and hashes
// the entire config envelope, the same work as /api/config/export, so it needs an
// interactive-read budget. Every member is read once per tick, so a probe timeout
// here would leave a slow but healthy member permanently unmeasured.
func (s *Server) fetchMemberConfigVersion(ctx context.Context, m *Member, token string) (string, error) {
	status, body, err := s.callMemberWith(ctx, s.readClient, http.MethodGet, m.URL, memberConfigVersionPath, token, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("member config-version returned %d", status)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("frontdesk: parse member config-version: %w", err)
	}
	if v.Version == "" {
		return "", errors.New("frontdesk: empty member config-version")
	}
	return v.Version, nil
}
