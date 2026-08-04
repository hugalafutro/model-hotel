package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// This file implements HA auto config sync: the "set and forget" half of fleet
// config replication. Where configsync.go runs a manual, wizard-driven, double-
// confirmed sync, the auto-syncer watches the operator-designated primary on a
// background tick and, when its config changes, propagates it to every other
// member by itself.
//
// Drift is detected by the primary's own config hash (GET /api/config/version):
// the hash is compared against the hash last applied to the fleet, both read
// from the same instance, so it is a valid "changed since" signal.
//
// Whether an individual member holds that config is decided the same way, by
// reading the member's own hash and comparing it with the primary's. The hash
// covers the syncable payload alone, which carries names and stable model refs
// rather than instance-local ids or timestamps, so it is comparable across
// instances: equal hashes mean identical config. That comparison, not the
// member's report of its own import, is the convergence criterion. A member is
// pushed to on one pass and verified on the next, so a member that claims a
// clean apply while its config differs is still caught, and so is one running
// older code that reports nothing at all.
//
// A member whose hash differs (or cannot be read) falls back to its own dry-run
// diff to decide what to push. That diff is presence-based, so it seldom reads as
// empty; a member that can never converge is kept from re-importing on every tick
// by incompleteRetryInterval.
//
// No request or prompt content is ever read; only provider/key names and counts
// flow, exactly as in the manual sync.

const (
	memberConfigVersionPath = "/api/config/version"

	// autoSyncIntervalSecs is how often the auto-syncer samples the primary. It
	// is deliberately slower than the health poll: each apply runs member-side
	// discovery, so a tight loop would be wasteful, and "set and forget"
	// convergence within a few tens of seconds of an edit is ample.
	autoSyncIntervalSecs = 15

	// autoSyncReason is stamped on each member's last-sync record and shown in the
	// Members table tooltip, so the operator sees why an automatic sync fired.
	autoSyncReason = "the primary's config changed"

	// autoSyncKickReason is stamped instead when a sync is triggered by the
	// operator turning auto-sync on (or repointing the primary), so the marker
	// reflects the deliberate enable rather than a primary edit.
	autoSyncKickReason = "auto-sync was enabled"

	// autoSyncKickTimeout caps the detached convergence pass fired when auto-sync
	// is enabled, so a stuck member cannot leak the goroutine. Generous: a pass
	// imports config into every drifted member in turn.
	autoSyncKickTimeout = 5 * time.Minute

	// autoSyncStaleThreshold is how long the fleet may go unsynced, with auto-sync
	// off, before Front Desk warns that the replicas may be drifting from the
	// primary. A day is long enough that a brief manual-sync gap never trips it,
	// short enough that a fleet left un-synced surfaces within the same day.
	autoSyncStaleThreshold = 24 * time.Hour

	// incompleteRetryInterval rate-limits the re-push of a member that committed a
	// config and still does not hold it. The member is never counted as converged,
	// so the fleet badge, the alert and the unrecorded fleet hash all persist; this
	// only stops a member that cannot converge from driving a full re-import, and
	// the member-side model discovery it runs, on every 15 second tick. A member
	// whose discovery catches up converges within one interval.
	incompleteRetryInterval = 10 * time.Minute

	// autoSyncFaultyThreshold is the second staleness tier: with auto-sync off, a
	// fleet unsynced this long is treated as faulty by the fleet state machine
	// (fleetstate.go), not merely degraded. Three days: long enough that a weekend
	// gap alone never trips it, short enough that a forgotten fleet escalates
	// within the working week.
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
// true exactly when autoSyncStaleTier returns 1 or higher (see it for the full
// rule and rationale). Retained as the boolean that the config.autosync_stale
// watchdog and the autosync payload's Stale flag consume.
func autoSyncStale(cfg AutoSyncConfig, lastSync time.Time, haveSync bool, now time.Time) bool {
	return autoSyncStaleTier(cfg, lastSync, haveSync, now) >= 1
}

// RunAutoSync samples the designated primary on a fixed tick and propagates its
// config to the fleet when it changes. It blocks until ctx is cancelled and is
// started once, alongside the poller. The loop owns the small amount of state
// (the previously observed hash) used to coalesce a burst of edits into one sync.
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
// observed on the previous tick; the returned value is the hash to carry into
// the next tick. It never returns an error: every failure path logs and leaves
// the fleet untouched, to be retried on the next tick.
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

	// Unchanged since the last fleet-wide apply: nothing to propagate. The fleet
	// is already converged (LastHash is recorded only once every reachable member
	// matched it, and the primary still reports it), so this is the quiet steady
	// state. Advance each reachable member's live "verified in sync" heartbeat so
	// the Members table shows auto-sync is running even when there is nothing to
	// write. No DB write, no event: last_config_sync_at moves only on a real sync.
	if hash == cfg.LastHash {
		s.markFleetVerified(ctx, cfg.PrimaryID)
		return hash
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
// loop's coalescing gate. It is fired when the operator explicitly enables
// auto-sync (or repoints the primary) so the fleet converges in seconds instead
// of waiting up to two ticks: the operator opted in deliberately, so there is no
// mid-edit ambiguity for coalescing to guard against. Safe to run in its own
// goroutine with a detached context: it reuses the same primary read and dry-run
// diff as the loop, and is a no-op when auto-sync is off or has no primary. It
// never returns an error; failures log and the loop retries.
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
		// The designated primary was removed or lost its token. We cannot
		// proceed without a source; stay quiet at debug and reset the window.
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

// convergeFleet pushes the primary's config to every member that needs it,
// records the applied hash once the whole reachable fleet has converged, and
// emits one roll-up event tagged with reason. Shared by the tick loop and the
// enable-time kick so both take the identical apply/record/emit path. gen is the
// rearm generation captured before the member list was read; the hash is recorded
// only if it is still current, so a rearm (member add, token update, enable, or
// repoint) that landed mid-pass is never clobbered by this older pass.
func (s *Server) convergeFleet(ctx context.Context, primary *Member, primaryToken, hash, reason string, gen int64) {
	applied, allConverged := s.applyAutoSync(ctx, primary, primaryToken, hash, reason, gen)
	// Record the hash as applied only once every reachable member has been seen to
	// serve it. If a member was unreachable, leave the marker so the next tick
	// retries. A member that is reachable but never converges has its re-push
	// bounded by incompleteRetryInterval, so it cannot drive an import on every
	// tick.
	if allConverged {
		switch ok, err := s.store.RecordAutoSyncHash(ctx, hash, gen); {
		case err != nil:
			debuglog.Warn("frontdesk: auto-sync: record applied hash", "error", err)
		case !ok:
			// A rearm landed mid-pass and bumped the generation: leave the cleared
			// marker so the next tick converges with the fresh member list/primary.
			debuglog.Debug("frontdesk: auto-sync: skipped stale hash record after rearm")
		}
	}
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

// markFleetVerified advances the live "verified in sync" heartbeat for every
// reachable, non-primary member. It is called on the quiet auto-sync tick (the
// fleet is already converged and the primary has not drifted), so the Members
// table shows auto-sync is running even when nothing needs writing. Members the
// health poller does not currently see up are skipped: their heartbeat freezes,
// which honestly says "not verified right now" while the red health badge
// explains why. In-memory only, no DB write, so it is cheap to run every tick.
func (s *Server) markFleetVerified(ctx context.Context, primaryID string) {
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		debuglog.Debug("frontdesk: auto-sync: list members for verify heartbeat", "error", err)
		return
	}
	now := time.Now().UTC()
	for _, m := range members {
		if m.ID == primaryID {
			continue // the primary is the source; it is not "in sync with" itself
		}
		s.poller.SetAutoSyncVerifiedIfReachable(m.ID, now)
	}
}

// applyAutoSync pushes the primary's config to every other tokened member that
// does not already hold it. It returns how many members it actually re-synced and
// whether every reachable member is verified converged (the signal autoSyncOnce
// uses to decide whether to record the applied hash). hash is the primary's
// current config hash, and comparing it with each member's own is the convergence
// criterion: a member matching it is converged and left untouched, a member that
// differs is not, whatever it reported about its own import. reason is stamped
// onto each synced member's last-sync marker.
//
// The pass that pushes a member does not verify it; the next pass's hash query
// does. So a fleet converges one tick later than a pass that trusted the member's
// answer, and in exchange convergence is measured rather than claimed.
//
// gen is the rearm generation captured before this pass began. A rearm (member
// add, token update, enable, or primary repoint) bumps it, so the pass re-checks
// it twice per member: once at the top of the loop, and again right before the
// mutating import (after the slow dry-run, which is where a repoint is most
// likely to slip in). It aborts the moment the generation changes,
// so a slow pass cannot import the captured (now-stale) primary's config into a
// member the operator has just repointed away from. The only window no pre-check
// can close is the in-flight import call itself; the rearm's own pass converges
// that member on the next tick. Members synced before the change were current when
// written; the rearm's own pass converges the rest. allConverged is forced false
// on abort so no hash is recorded.
func (s *Server) applyAutoSync(ctx context.Context, primary *Member, primaryToken, hash, reason string, gen int64) (applied int, allConverged bool) {
	// A transient gen read shouldn't abort a valid pass, so a read error reports
	// "not stale" and the generation-guarded hash record stays the backstop.
	stale := func() bool {
		cur, err := s.store.AutoSyncGen(ctx)
		return err == nil && cur != gen
	}
	if stale() {
		return 0, false // a rearm already landed: don't push the stale export at all
	}

	// passCtx is the cancellation point the pre-import gates alone cannot provide: a
	// watcher cancels it the instant a rearm/repoint moves the generation, so an
	// import already in flight is aborted rather than completing a now-stale write.
	// All member HTTP calls below run under passCtx; the deferred cancel stops the
	// watcher when the pass returns, so it never outlives this call.
	//
	// rearmCh is captured before watchRearm re-reads the generation, so a rearm that
	// lands in that gap still wakes the watcher (the channel is closed, not missed)
	// rather than slipping through an interval-poll window.
	rearmCh := s.rearmChan()
	passCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.watchRearm(passCtx, rearmCh, gen, cancel)

	export, err := s.fetchMemberExport(passCtx, primary, primaryToken)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync: read primary export", "member", primary.Name, "error", err)
		return 0, false
	}
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync: list members", "error", err)
		return 0, false
	}

	primaryVer := s.poller.MemberVersion(primary.ID)
	allConverged = true
	for _, m := range members {
		if m.ID == primary.ID {
			continue // the source is never written to
		}
		if stale() {
			// A rearm/repoint landed mid-pass: stop before importing the stale export
			// into any further member. Force not-converged so the hash is not recorded
			// and the rearm's own pass takes over.
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass after rearm", "synced", applied)
			allConverged = false
			break
		}
		if !m.HasToken {
			// A tokenless member can't be authenticated to, so it is skipped without
			// flipping allConverged: counting it as not-converged would re-probe the
			// whole fleet every tick for as long as it stayed tokenless. The skip is
			// safe because the tokenless -> tokened transition only happens through
			// createMember / patchMember, both of which call rearmAutoSync to clear the
			// applied hash and force this pass to re-run once the member is syncable.
			continue
		}
		token, ok, err := s.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			// The member has token ciphertext (HasToken was true) but it could not be
			// loaded or decrypted: a MASTER_KEY mismatch on the stored token, a transient
			// DB error, or the token cleared in the race after the snapshot. Unlike a
			// tokenless member there is no membership event that will re-arm the loop, so
			// this must NOT count as converged: hold off and retry on the next tick.
			debuglog.Debug("frontdesk: auto-sync: member token unavailable, will retry", "member", m.Name, "loaded", ok, "error", err)
			allConverged = false
			continue
		}
		// Version gate: never push the primary's config onto a member running a
		// different app version. An older primary's export omits settings the newer
		// member legitimately has, and the member-side converge-delete would drop
		// them. Hold the member (no push), do not count it converged, and emit
		// config.sync_held once on the transition into held. Fails closed on an
		// unknown version. Re-evaluated every pass, so it resumes automatically.
		memberVer := s.poller.MemberVersion(m.ID)
		if versionSkew(primaryVer, memberVer) {
			s.holdMemberForSkew(ctx, m, primaryVer, memberVer)
			allConverged = false
			continue
		}
		s.clearSyncHold(m.ID)
		// Ask the member what config it actually holds, and let that answer decide
		// whether it converged. Every member serves the same content hash over the same
		// syncable payload (providers, virtual keys, syncable settings, custom failover
		// groups, users), every list under a total order and carrying names and stable
		// model refs rather than instance-local ids or timestamps, so an equal hash
		// means this member holds exactly this config. The member's own report of its
		// import is not consulted here: a member that reports a clean apply while its
		// config differs is the incident this loop exists to catch. The dry-run cannot
		// establish convergence either, since computeDiff keys on presence, so a member
		// that already matches still reports every shared entity as updated.
		memberHash, verErr := s.fetchMemberConfigVersion(passCtx, m, token)
		if verErr == nil && memberHash == hash {
			// Converged, measured rather than claimed. This member must NOT touch
			// allConverged: counting a matching member as unconverged would leave the
			// fleet hash unrecorded and put every healthy member back on a per-tick
			// re-import. Close out any divergence it was carrying, which emits
			// config.sync_recovered once on the way out.
			s.clearMemberIncomplete(ctx, m)
			s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
			continue
		}
		// From here the member does not hold this config (or could not be asked), so
		// the fleet has not converged this pass whatever else happens below.
		allConverged = false
		if verErr != nil {
			// An unread hash proves nothing in either direction: the member is neither
			// counted converged nor flagged as diverged. It falls through to the dry-run,
			// which reports an unreachable or erroring member properly.
			debuglog.Debug("frontdesk: auto-sync: read member config version", "member", m.Name, "error", verErr)
		} else if s.hasBeenPushedSinceReset(m.ID) {
			// A measured divergence in a member that has already committed this config
			// once: it failed to converge rather than merely not having been reached yet.
			// resetIncompleteRetries zeroes that signal whenever the primary's config
			// moves, so the pass right after an operator edit never flags anyone and an
			// ordinary edit cannot turn the fleet badge amber for a tick.
			s.markMemberIncomplete(ctx, m)
		}
		// A member that keeps missing after a push would otherwise drive a full
		// re-import, and the member-side model discovery it runs, every tick: its
		// dry-run diff is never zero. It stays not-converged (the fleet hash is still
		// withheld and the badge and alert persist); only the push is rate-limited.
		if s.shouldSkipIncompleteRetry(m.ID, time.Now()) {
			continue
		}
		// Decide whether this member needs the new config from its own dry-run
		// diff, which compares by name and so is valid across instances. The dry-run
		// is never fenced, so the generation is not sent on it.
		res, status, err := s.pushMemberImport(passCtx, m, token, export, true, gen)
		if err != nil {
			debuglog.Debug("frontdesk: auto-sync: member unreachable, will retry", "member", m.Name, "status", status, "error", err)
			continue
		}
		if !res.SchemaVersionOK || !res.MasterKeyOK {
			// A version skew or MASTER_KEY mismatch blocks this member. The manual
			// wizard surfaces these explicitly; here we just hold off and retry.
			debuglog.Debug("frontdesk: auto-sync: member not syncable", "member", m.Name)
			continue
		}
		added, updated, removed := res.Diff.counts()
		if added+updated+removed == 0 {
			// The dry-run has nothing to write, yet the hash above says this member does
			// not hold the primary's config (or could not be asked). Nothing to push, and
			// nothing that would make the member match, so it is left alone and stays
			// unconverged. No heartbeat either: the hash, not the diff, is what "verified
			// in sync" means.
			debuglog.Debug("frontdesk: auto-sync: member differs but its diff is empty", "member", m.Name)
			continue
		}

		// Front Desk takes no snapshot before overwriting a member; members back
		// themselves up on their own schedule when the operator has enabled backups.
		// The trade is accepted deliberately: a bad config propagation cannot be
		// rolled back from a snapshot Front Desk just took, and in exchange no member
		// accumulates a pg_dump per sync pass.

		// Final staleness gate, tightest to the mutation: a rearm or primary repoint
		// can land during this member's (slow) dry-run diff. Re-check here so we
		// never even start an import the operator has invalidated. The narrow window
		// between this check and the import committing on the member is closed by
		// passCtx: the watchRearm goroutine cancels it, aborting the in-flight
		// request.
		if stale() {
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass before import", "synced", applied)
			break
		}

		// applyMemberConfig stamps the member's last-sync marker with this reason on
		// success, so the Members table shows when and why it last converged. Per-
		// member success events are suppressed here (emitSuccessEvent=false); the
		// loop emits one roll-up below so a fleet sync does not toast per member. It
		// runs under passCtx so a rearm landing mid-import cancels the request, and
		// carries gen so the member's commit fence can refuse this push outright if a
		// newer generation already won the in-flight race (out.Stale, handled there
		// as a benign supersede rather than a failure).
		out := s.applyMemberConfig(passCtx, m, token, export, reason, false, gen)
		if out.OK || out.Incomplete {
			// The member took the config, whether or not it built all of it. Remember
			// when, and what it said it could not build: that stamp bounds the re-push
			// and tells the next pass this member has had its chance, so a hash that
			// still differs then is a real failure to converge rather than a member
			// nobody has reached yet. A push the member refused or never received is
			// deliberately not stamped, so a transient failure is retried on the next
			// tick rather than waiting out the interval.
			s.recordSyncAttempt(m.ID, out.Unapplied)
		}
		if !out.OK {
			// Not applied by this pass: a failure (already surfaced by
			// applyMemberConfig) or a benign fence supersede. Either way the hash is
			// not recorded for this generation and the authoritative pass takes over.
			continue
		}
		applied++
	}
	return applied, allConverged
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
// primary's config to: enough to bound the re-push, to describe the divergence in
// operator terms, and to know whether the member currently counts as diverged.
type incompleteState struct {
	// lastAttempt is when this member last committed a config Front Desk pushed.
	// Zero means "push it on the next pass": either it has not taken this config
	// yet, or resetIncompleteRetries cleared the timer. Non-zero is therefore also
	// the "this member has had its chance" signal that keeps a member nobody has
	// reached yet from being flagged.
	lastAttempt time.Time
	// lastUnapplied names the custom failover groups the member reported it could
	// not build on that import, so the alert raised on a later pass can still be
	// specific. Empty when the member named none, which is also what a divergence
	// with no member-side explanation looks like.
	lastUnapplied []string
	// diverged is true once the member's own config hash has been measured against
	// the primary's and differed after it took the config. It is what the fleet
	// badge and the edge-triggered alert read; a member that has merely been pushed
	// to is not diverged.
	diverged bool
}

// recordSyncAttempt remembers that a member committed a config Front Desk pushed,
// along with whatever it reported it could not build. The stamp bounds the
// re-push (shouldSkipIncompleteRetry) and tells the next pass this member has
// already had this config (hasBeenPushedSinceReset), so a hash that still differs
// then is a failure to converge rather than a member nobody has reached.
func (s *Server) recordSyncAttempt(memberID string, unapplied []string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	st.lastAttempt = time.Now()
	st.lastUnapplied = unapplied
	s.syncIncomplete[memberID] = st
}

// hasBeenPushedSinceReset reports whether this member has committed the config
// since the last reset (a primary edit or the operator's enable-time kick). It is
// the whole no-flap guard: a member that has not been reached yet is diverged for
// the most ordinary of reasons and must never be flagged for it.
func (s *Server) hasBeenPushedSinceReset(memberID string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return !s.syncIncomplete[memberID].lastAttempt.IsZero()
}

// markMemberIncomplete records a member whose own config hash differs from the
// primary's after it took that config, and emits config.sync_incomplete once on
// the transition in. The member is re-checked on every later pass, so a
// level-triggered event here would alert again on each one until it converges.
func (s *Server) markMemberIncomplete(ctx context.Context, m *Member) {
	s.syncIncompleteMu.Lock()
	st := s.syncIncomplete[m.ID]
	already := st.diverged
	st.diverged = true
	s.syncIncomplete[m.ID] = st
	// The names ride the metadata as a list either way, never as null, so consumers
	// see one shape. Copied out under the lock so the emit below reads a stable list.
	names := append([]string{}, st.lastUnapplied...)
	s.syncIncompleteMu.Unlock()
	if already {
		return
	}
	// A member that named the groups it could not build gets the specific wording.
	// Everything else is a divergence Front Desk measured but the member did not
	// explain: it committed the config, reported nothing wrong (or nothing at all),
	// and still does not match. A count there would read "could not build 0 failover
	// group(s)" and tell the operator nothing is wrong while the fleet is degraded.
	msg := fmt.Sprintf("%s applied the config but does not match the primary's config", m.Name)
	if len(names) > 0 {
		msg = fmt.Sprintf("%s applied the config but could not build %d failover group(s)", m.Name, len(names))
	}
	s.emit(ctx, Event{
		Type: "config.sync_incomplete", Severity: "warning", Source: "frontdesk",
		Message:  msg,
		MemberID: m.ID,
		Metadata: map[string]any{"unapplied": names},
	})
}

// clearMemberIncomplete forgets everything about a member whose hash now matches
// the primary's and emits config.sync_recovered once on the transition out, so a
// later divergence re-emits config.sync_incomplete. A member that was never
// flagged emits nothing, keeping ordinary successful passes quiet. Dropping the
// whole entry is deliberate: a converged member has no retry to bound and no
// divergence to describe.
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
// calculation, mirroring heldSnapshot. Members that have merely been pushed to
// are not in it: only a measured divergence degrades the fleet. If auto-sync is
// disabled while members are diverged the set stays frozen rather than clearing,
// so it degrades the fleet state rather than reporting a false ok, and it clears
// when auto-sync resumes and the member converges.
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

// resetIncompleteRetries drops every retry timer so each member is pushed again
// on the next pass. The entries themselves stay, so the fleet badge and the
// edge-triggered alert are unaffected. Called when the operator's intent changes
// (the primary's config moved, or auto-sync was just enabled), where waiting out
// the interval would delay a deliberate edit. Clearing the timer also clears the
// "this member has had its chance" signal, so the pass right after a deliberate
// change flags nobody.
func (s *Server) resetIncompleteRetries() {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	for id, st := range s.syncIncomplete {
		st.lastAttempt = time.Time{}
		s.syncIncomplete[id] = st
	}
}

// watchRearm cancels a convergence pass the moment its rearm generation goes
// stale, giving an in-flight member import a real cancellation point: a repoint
// or rearm (which bumps auto_sync_gen) lands while applyMemberConfig is mid-flight
// and the HTTP request is aborted instead of finishing a now-stale write.
//
// rearmCh is the in-process broadcast closed by signalRearm, so the wake is
// synchronous with the generation bump rather than gated on a poll interval. The
// generation is re-read first to close the gap between the caller capturing gen and
// this watcher starting (a rearm there has already moved auto_sync_gen but its
// channel close may predate our capture); after that, the channel close is the
// signal. The watcher exits the instant ctx is done (the deferred cancel in
// applyAutoSync), so it never outlives the pass. A transient read error is ignored:
// the generation-guarded hash write stays the backstop if cancellation is missed.
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
// GET /api/config/version. The hash changes if and only if a synced entity
// changed, and every member computes it over the same deterministically ordered
// payload, so it serves two purposes: read from the designated primary it is the
// cheap drift signal that starts a pass, and read from an individual member it
// answers whether that member already holds the primary's config, which is what
// lets applyAutoSync leave a converged member untouched.
func (s *Server) fetchMemberConfigVersion(ctx context.Context, m *Member, token string) (string, error) {
	status, body, err := s.callMember(ctx, http.MethodGet, m.URL, memberConfigVersionPath, token, nil)
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
