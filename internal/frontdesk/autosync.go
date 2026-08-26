package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
	// in the Members table tooltip. Subject-free: the same string rides in the
	// fleet-wide roll-up event ("Auto-synced 2 members: ...", possibly followed by
	// a per-member section detail), where a singular subject would have no
	// referent.
	autoSyncReason = "did not hold the primary's config"

	// autoSyncKickReason is stamped instead when the operator turns auto-sync on
	// or repoints the primary.
	autoSyncKickReason = "auto-sync was enabled"

	// unconfirmedSyncReason is stamped when a pass measures a member holding the
	// primary's exact hash after a push whose answer was lost (the relay deadline
	// expired, or a proxy in between answered 5xx while the member was still
	// importing). The hash match proves the push landed, so the last-sync marker
	// the push itself could not stamp is stamped at verification time instead.
	unconfirmedSyncReason = "verified holding the primary's config after an unconfirmed push"

	// autoSyncKickTimeout caps the detached pass fired when auto-sync is enabled,
	// so a stuck member cannot leak the goroutine. Generous: the pass imports into
	// every drifted member in turn, and each import may legitimately run up to
	// memberSyncTimeout on a slow member, so the cap covers several of those.
	autoSyncKickTimeout = 15 * time.Minute

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

	// unreadableHashThreshold is how long a member's config hash must stay
	// continuously unreadable before Front Desk reports the member unmeasured. An
	// unread hash proves nothing on its own, so a single slow or failed read must
	// not alert; a hash that never reads means the member's convergence can never be
	// established, which is worth the same badge as a measured divergence. Longer
	// than a member restart or a slow import, both of which answer again on their
	// own.
	unreadableHashThreshold = 10 * time.Minute

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
		// Disabled or no primary designated: nothing to do, but that IS this
		// tick's verdict — no holds can exist, so the fleet state's sync inputs
		// count as observed (fleetInputsWarm).
		s.autoSyncEvaluated.Store(true)
		return ""
	}

	primary, primaryToken, hash, primarySections, ok := s.primaryConfigHash(ctx, cfg)
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

	s.convergeFleet(ctx, primary, primaryToken, hash, primarySections, autoSyncReason, cfg.Gen)
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
	primary, primaryToken, hash, primarySections, ok := s.primaryConfigHash(ctx, cfg)
	if !ok {
		return
	}
	// A kick is a deliberate operator action, so it retries every incomplete
	// member now rather than leaving it inside its retry interval.
	s.resetIncompleteRetries()
	s.convergeFleet(ctx, primary, primaryToken, hash, primarySections, autoSyncKickReason, cfg.Gen)
}

// primaryConfigHash resolves the designated primary, loads its admin token, and
// reads its current syncable-config hash together with its per-section hashes.
// ok is false (with a debug log) when the primary was removed, lost its token,
// or is unreachable, in which case the caller skips this round and retries
// later.
func (s *Server) primaryConfigHash(ctx context.Context, cfg AutoSyncConfig) (primary *Member, token, hash string, sections map[string]string, ok bool) {
	primary, token, err := s.memberTokenOrErr(ctx, cfg.PrimaryID)
	if err != nil {
		// No source to sync from: the primary was removed or lost its token.
		debuglog.Debug("frontdesk: auto-sync: primary unavailable", "error", err)
		return nil, "", "", nil, false
	}
	hash, sections, err = s.fetchMemberConfigVersion(ctx, primary, token)
	if err != nil {
		debuglog.Debug("frontdesk: auto-sync: read primary version", "member", primary.Name, "error", err)
		return nil, "", "", nil, false
	}
	return primary, token, hash, sections, true
}

// convergeFleet pushes the primary's config to every member that needs it and
// emits one roll-up event tagged with reason. Shared by the tick loop and the
// enable-time kick. gen is the rearm generation captured before the member list
// was read; applyAutoSync aborts on it, so a rearm landing mid-pass stops this
// pass rather than letting it finish a stale write.
//
// The roll-up names, per synced member, which config sections its pre-push
// measurement found differing ("replica: failover groups"), so the operator can
// see what the repair was for; whether the push then converged the member is
// the next pass's verdict, not this event's. primarySections is read once at
// pass start, so a primary edit landing mid-pass can understate what the
// lazily-fetched export actually carried, the same staleness window hash
// already has. A member measured without section detail (an older app version
// on either side, or a hash that could not be read before the push) is counted
// but carries no parenthetical.
//
// Nothing fleet-wide is recorded. Convergence is per member: the verified-in-sync
// heartbeat when a member's hash matches, the diverged flag and amber badge when
// it does not.
func (s *Server) convergeFleet(ctx context.Context, primary *Member, primaryToken, hash string, primarySections map[string]string, reason string, gen int64) {
	details := s.applyAutoSync(ctx, primary, primaryToken, hash, primarySections, reason, gen)
	// The pass has judged every member, so the version-skew hold and
	// incomplete-apply sets now reflect observations, not a cold start.
	s.autoSyncEvaluated.Store(true)
	if len(details) == 0 {
		return
	}
	noun := "members"
	if len(details) == 1 {
		noun = "member"
	}
	message := fmt.Sprintf("Auto-synced %d %s: %s", len(details), noun, reason)
	if d := describeSectionDetails(details); d != "" {
		message += " (" + d + ")"
	}
	meta := make([]any, 0, len(details))
	for _, det := range details {
		entry := map[string]any{"member_id": det.id, "name": det.name}
		if len(det.sections) > 0 {
			entry["sections"] = det.sections
		}
		meta = append(meta, entry)
	}
	s.emit(ctx, Event{
		Type: "config.auto_synced", Severity: "info", Source: "frontdesk",
		Message:  message,
		Metadata: map[string]any{"members": meta},
	})
}

// syncedMemberDetail records one member a pass re-synced: who it was and which
// config sections its pre-push measurement found differing from the primary's
// (nil when no section detail was available).
type syncedMemberDetail struct {
	id, name string
	sections []string
}

// configSections lists the syncable payload's sections in payload order, keyed
// exactly as the member's /api/config/version "sections" map keys them, with
// the operator-facing label each renders as in the auto-synced roll-up.
var configSections = []struct{ key, label string }{
	{"providers", "providers"},
	{"virtual_keys", "virtual keys"},
	{"settings", "settings"},
	{"failover_groups", "failover groups"},
	{"users", "users"},
	{"disabled_models", "disabled models"},
	{"enabled_models", "pinned models"},
}

// differingSections names the payload sections whose hashes disagree, in payload
// order. Either side missing its section map (an older app version's response)
// answers nil: with nothing to compare, the honest claim is "no detail", never
// "everything differs".
func differingSections(primary, member map[string]string) []string {
	if len(primary) == 0 || len(member) == 0 {
		return nil
	}
	var out []string
	for _, sec := range configSections {
		if primary[sec.key] != member[sec.key] {
			out = append(out, sec.key)
		}
	}
	return out
}

// describeSectionDetails renders the per-member section detail for the roll-up
// message: "replica: failover groups, disabled models; other: providers".
// Members with no section detail are left out; an empty result means the
// message carries no parenthetical at all. Labels come from configSections,
// which is also the only vocabulary differingSections emits.
func describeSectionDetails(details []syncedMemberDetail) string {
	parts := make([]string, 0, len(details))
	for _, det := range details {
		labels := make([]string, 0, len(det.sections))
		for _, sec := range configSections {
			if slices.Contains(det.sections, sec.key) {
				labels = append(labels, sec.label)
			}
		}
		// Guarding on labels, not det.sections, so a detail carrying only keys
		// this build does not label can never render a dangling "name: ".
		if len(labels) == 0 {
			continue
		}
		parts = append(parts, det.name+": "+strings.Join(labels, ", "))
	}
	return strings.Join(parts, "; ")
}

// applyAutoSync pushes the primary's config to every other tokened member that
// does not already hold it, and returns a detail per member it actually
// re-synced. hash is the primary's current config hash, and comparing it with
// each member's own is the convergence criterion: a member matching it is
// converged and left untouched, a member that differs is not, whatever it
// reported about its own import. reason is stamped onto each synced member's
// last-sync marker.
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
func (s *Server) applyAutoSync(ctx context.Context, primary *Member, primaryToken, hash string, primarySections map[string]string, reason string, gen int64) (applied []syncedMemberDetail) {
	// A read error reports "not stale": a transient DB failure must not abort an
	// otherwise valid pass.
	stale := func() bool {
		cur, err := s.store.AutoSyncGen(ctx)
		return err == nil && cur != gen
	}
	if stale() {
		return nil // a rearm already landed: don't push the stale export at all
	}

	// passCtx is the cancellation point the pre-import gates cannot provide: a
	// watcher cancels it the instant a rearm moves the generation, aborting an
	// import already in flight. Every member HTTP call below runs under it, and the
	// deferred stop cancels the watcher and waits for it when the pass returns.
	//
	// rearmCh is captured before watchRearm re-reads the generation, so a rearm
	// landing in that gap still wakes the watcher: the channel is closed, not
	// missed.
	rearmCh := s.rearmChan()
	passCtx, cancel := context.WithCancel(ctx)
	stopWatch := s.startRearmWatch(passCtx, rearmCh, gen, cancel)
	defer stopWatch()

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
		return nil
	}

	primaryBuild := s.poller.memberBuildOf(primary.ID)
	s.warnIfBuildGateDegraded(primaryBuild)
	for _, m := range members {
		if m.ID == primary.ID {
			// The source is never written to, but a hold announced before this
			// member's promotion must still close here: no other pass will ever
			// reach it, and an unclosed config.sync_held would stay the new
			// primary's newest event forever.
			s.closeSyncHold(ctx, m, fmt.Sprintf("%s is no longer held for sync: it is now the primary", m.Name))
			continue
		}
		if stale() {
			// A rearm/repoint landed mid-pass: stop before importing the stale export
			// into any further member, and leave the rest to the rearm's own pass.
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass after rearm", "synced", len(applied))
			break
		}
		// Build gate: never push onto a member running a different build. An older
		// primary's export omits settings the newer member legitimately has, and
		// the member-side converge-delete would drop them. The version decides it
		// where the versions differ; where they match (as they always do on a
		// self-built "dev" fleet) the commit does. Fails closed on an unknown
		// build, and is re-evaluated every pass, so it resumes automatically.
		//
		// Decided before the token guards, because the verdict comes from the poller,
		// not the token: a held member whose token was cleared must still close its
		// hold once versions realign, or the "held" story outlives the skew. Entering
		// a hold stays behind the guards: sync to a tokenless member is not held, it
		// is impossible, and holds on unsyncable members would be noise.
		build := s.poller.memberBuildOf(m.ID)
		skewed := buildSkew(primaryBuild, build)
		if !skewed {
			// "Resumed" is only claimed when a token exists to resume with; for a
			// tokenless member the builds realigned but sync stays impossible.
			message := fmt.Sprintf("%s is no longer held for sync: its build matches the primary's again", m.Name)
			if m.HasToken {
				message = fmt.Sprintf("Resumed sync to %s: its build matches the primary's again", m.Name)
			}
			s.closeSyncHold(ctx, m, message)
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
		if skewed {
			s.holdMemberForSkew(ctx, m, primaryBuild, build)
			continue
		}
		converged, measured, differing := s.measureMember(ctx, passCtx, m, token, hash, primarySections)
		if converged {
			continue
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
		// An empty diff is only a reason to skip when nothing contradicts it. The diff
		// covers providers, virtual keys and settings, so a member differing in anything
		// else the envelope carries (a custom failover group, a per-model disable)
		// reports nothing to write while its hash correctly says it is out of sync.
		// Importing on the hash is what converges those; the retry interval bounds a
		// member the import cannot fix. With no hash either, there is no evidence in
		// either direction, so the member is left for the next pass rather than written
		// to on a guess.
		added, updated, removed := res.Diff.counts()
		if added+updated+removed == 0 && !measured {
			debuglog.Debug("frontdesk: auto-sync: member diff is empty and its hash is unreadable", "member", m.Name)
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
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass before import", "synced", len(applied))
			break
		}

		// applyMemberConfig stamps the member's last-sync marker with this reason.
		// Per-member success events are suppressed (emitSuccessEvent=false) in favour
		// of one roll-up below. gen lets the member's commit fence refuse this push if
		// a newer generation already won the in-flight race (out.Stale, a benign
		// supersede).
		out := s.applyMemberConfig(passCtx, m, token, export, reason, false, gen, hash)
		if out.OK || out.Incomplete || out.Unconfirmed {
			// The member received the config, whether or not it built all of it and
			// whether or not it answered in time. The stamp bounds the re-push and tells
			// the next pass this member has had its chance, so a hash that still differs
			// then is a failure to converge rather than a member nobody reached. An
			// unconfirmed push (timed out, or answered 5xx by a proxy mid-import)
			// counts too: the member is very likely still importing, and re-pushing
			// every tick would restart that import and its discovery each time.
			//
			// A push the member refused or never received is deliberately not stamped,
			// so a transient failure retries next tick instead of waiting out the
			// interval.
			s.recordSyncAttempt(m.ID, out.Unapplied, out.Partial, out.UnappliedModels)
		}
		if !out.OK {
			// A failure, already surfaced by applyMemberConfig, or a benign fence
			// supersede. The next pass measures this member again either way.
			continue
		}
		applied = append(applied, syncedMemberDetail{id: m.ID, name: m.Name, sections: differing})
	}
	return applied
}

// measureMember asks a member what config it actually holds and records what the
// answer means. It reports whether the member is converged, so the caller can move
// on, and whether its hash could be read at all. On a measured divergence it also
// names the payload sections whose hashes disagree with primarySections (nil when
// either side carries no section detail), which the caller threads into the
// auto-synced roll-up.
//
// The hash is the criterion because nothing else establishes convergence. Every
// member hashes the same syncable payload (providers, virtual keys, syncable
// settings, custom failover groups, users, per-model disables) under a total
// order, carrying names and stable model refs rather than instance-local ids, so
// an equal hash means this member holds exactly this config. The member's own
// report of its import can claim a clean apply while the config differs, and
// computeDiff keys on presence, so a matching member still reports every shared
// entity as updated.
//
// ctx carries the events this emits; passCtx is the cancellable pass context the
// member call runs under.
func (s *Server) measureMember(ctx, passCtx context.Context, m *Member, token, hash string, primarySections map[string]string) (converged, measured bool, differing []string) {
	memberHash, memberSections, err := s.fetchMemberConfigVersion(passCtx, m, token)
	if err != nil {
		// One unread hash proves nothing either way: neither converged nor flagged. A
		// hash that stays unreadable is different, and is the fault itself: the
		// member's convergence can never be established, so leaving it unflagged would
		// hide it behind a clean fleet indefinitely.
		//
		// Deliberately NOT gated on the member having had the config, unlike the
		// measured branch below. That guard exists so a member nobody has reached is
		// not blamed for diverging, and a member nobody has reached never gets here:
		// it has no polled version, and versionSkew fails closed on an unknown one, so
		// the caller holds it for skew and never asks for its hash.
		//
		// What does get here without ever having been pushed is a member that is up and
		// version-matched but broken for config sync in both directions: its hash read
		// fails and so does its import (a MASTER_KEY or schema mismatch, or a member
		// erroring on both endpoints). Every one of those paths only logs, so requiring
		// a successful push first would leave that member unflagged and the fleet green
		// while it holds unknown config. That is the failure this whole check exists to
		// end, and it is the more dangerous case, because the member looks healthy.
		debuglog.Debug("frontdesk: auto-sync: read member config version", "member", m.Name, "error", err)
		if s.recordUnreadableHash(m.ID, unreadableCause(err), time.Now()) {
			s.markMemberUnmeasured(ctx, m)
		}
		return false, false, nil
	}
	// Measurable again: stop any unreadable clock before deciding anything else, so
	// a member that answers once never carries a stale one.
	s.clearUnreadableHash(m.ID)
	if memberHash == hash {
		// Converged. Close out any divergence it carried, which emits
		// config.sync_recovered once on the way out, and move the verified-in-sync
		// heartbeat. Only a hash match moves it, so it means "measured holding the
		// primary's config", never "written to".
		s.clearMemberIncomplete(ctx, m)
		if s.hasUnconfirmedPush(m.ID, hash) {
			// The member holds the primary's exact config, so the push whose answer
			// was lost did land: record the sync its own stamp missed, at the moment
			// it was proven rather than guessed. An idle converged fleet never gets
			// here (no push, no flag), so the marker still means a real write. A
			// failed stamp keeps the flag, and the next converged pass retries it.
			if err := s.store.SetMemberLastSync(ctx, m.ID, time.Now().UTC(), unconfirmedSyncReason); err != nil {
				debuglog.Warn("frontdesk: stamp member last-sync on verified convergence", "member", m.Name, "error", err)
			} else {
				s.clearUnconfirmedPush(m.ID, hash)
			}
		}
		s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
		return true, true, nil
	}
	if s.hasBeenPushedSinceReset(m.ID) {
		// Measured divergence in a member that has already committed this config: a
		// failure to converge, not a member nobody has reached yet.
		// resetIncompleteRetries zeroes that signal whenever the primary's config
		// moves, so an ordinary edit never turns the badge amber for a tick.
		s.markMemberIncomplete(ctx, m)
	}
	return false, true, differingSections(primarySections, memberSections)
}

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
	// lastUnappliedModels names the per-model disables the member could not apply
	// for want of the model, as provider/model_id. It is what explains a hash that
	// keeps differing on a member whose groups are all fine.
	lastUnappliedModels []string
	// diverged is true once the member is known not to hold the primary's config:
	// either its own hash was measured and differed after it took the config, or its
	// hash has been unreadable long enough that convergence can no longer be
	// established. The fleet badge and the edge-triggered alert read this; a member
	// merely pushed to is not diverged.
	diverged bool
	// unreadableSince is when this member's config hash first failed to read, with
	// every read since having failed too. Zero once one succeeds.
	unreadableSince time.Time
	// lastReadErr is why the most recent hash read failed, carried into the alert so
	// it names the cause rather than only the symptom.
	lastReadErr string
	// divergedKind is which of the two the last emitted alert described. The badge
	// is the same either way, but the alert bodies contradict each other, so a
	// member that moves from one to the other has to say so.
	divergedKind divergenceKind
}

// divergenceKind is how a member came to be known not to hold the primary's
// config: measured against its own hash, or never measurable at all.
type divergenceKind int

const (
	// divergedMeasured: the member's hash was read and differed.
	divergedMeasured divergenceKind = iota
	// divergedUnmeasurable: the member's hash could not be read at all.
	divergedUnmeasurable
)

// recordSyncAttempt remembers that a member received a config Front Desk pushed,
// with whatever it reported it could not build or built short. The stamp bounds the
// re-push (shouldSkipIncompleteRetry) and marks the member as having had its chance
// (hasBeenPushedSinceReset).
func (s *Server) recordSyncAttempt(memberID string, unapplied, partial, unappliedModels []string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	st.lastAttempt = time.Now()
	st.lastUnapplied = unapplied
	st.lastPartial = partial
	st.lastUnappliedModels = unappliedModels
	s.syncIncomplete[memberID] = st
}

// markUnconfirmedPush remembers that a member's latest real config push, carrying
// the primary config identified by hash, got no usable answer, so its last-sync
// marker could not be stamped even though the import may have completed
// member-side. A later pass that measures the member holding exactly that hash
// stamps the marker then and clears this. An empty hash records nothing: the
// caller had no hash for what it pushed (the wizard when the primary's hash was
// unreadable), and a stamp that cannot be tied to the pushed config must not be
// promised.
func (s *Server) markUnconfirmedPush(memberID, hash string) {
	if hash == "" {
		return
	}
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	s.unconfirmedSync[memberID] = hash
}

// clearUnconfirmedPush forgets a member's unconfirmed push of exactly this
// config once a sync stamp has landed for it: either a later push of it was
// confirmed and stamped itself, or the verification pass stamped on the hash
// match. Compare-and-delete rather than delete: between the caller reading the
// flag and stamping, a concurrent push (the wizard runs on its own goroutine)
// can lose ITS answer and re-mark the member with a newer hash, and that entry
// must survive to earn its own stamp.
func (s *Server) clearUnconfirmedPush(memberID, hash string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	if hash != "" && s.unconfirmedSync[memberID] == hash {
		delete(s.unconfirmedSync, memberID)
	}
}

// hasUnconfirmedPush reports whether this member has a lost-answer push of
// exactly this config behind it, still waiting for the stamp its lost answer
// denied it. Bound to the hash so a push of an older config can never be
// "proven" by convergence on a newer one, and a definite member-side failure
// (nothing committed) can never be stamped just because the member later came to
// hold the primary's config some other way.
func (s *Server) hasUnconfirmedPush(memberID, hash string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return hash != "" && s.unconfirmedSync[memberID] == hash
}

// hasBeenPushedSinceReset reports whether this member has received the config
// since the last reset (a primary edit or the enable-time kick). This is the whole
// no-flap guard: a member not yet reached must never be flagged for diverging.
func (s *Server) hasBeenPushedSinceReset(memberID string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return !s.syncIncomplete[memberID].lastAttempt.IsZero()
}

// flagDiverged marks a member as not holding the primary's config and returns a
// copy of its state along with whether it was already flagged, so the caller emits
// the transition event exactly once. The slices are copied and are always lists
// rather than null, so consumers see one shape and the caller reads them outside
// the lock.
func (s *Server) flagDiverged(memberID string, kind divergenceKind) (incompleteState, bool) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	// Edge-triggered on the flag AND on which kind it is: a member that stops being
	// measurable, or becomes measurable and is then found to differ, has a new thing
	// to say. Without the second half the operator's last alert would keep insisting
	// the member "cannot be measured ... unknown" after Front Desk had measured it
	// and could name the groups, which is the same over-claiming in reverse.
	already := st.diverged && st.divergedKind == kind
	st.diverged = true
	st.divergedKind = kind
	s.syncIncomplete[memberID] = st
	st.lastUnapplied = append([]string{}, st.lastUnapplied...)
	st.lastPartial = append([]string{}, st.lastPartial...)
	st.lastUnappliedModels = append([]string{}, st.lastUnappliedModels...)
	return st, already
}

// markMemberIncomplete records a member whose own config hash differs from the
// primary's after it took that config, and emits config.sync_incomplete once on the
// transition in. Edge-triggered: the member is re-checked every pass, so a
// level-triggered event would alert on each one until it converged.
func (s *Server) markMemberIncomplete(ctx context.Context, m *Member) {
	st, already := s.flagDiverged(m.ID, divergedMeasured)
	if already {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_incomplete", Severity: "warning", Source: "frontdesk",
		Message:  divergenceMessage(m.Name, st.lastUnapplied, st.lastPartial, st.lastUnappliedModels),
		MemberID: m.ID,
		Metadata: map[string]any{
			"unapplied": st.lastUnapplied, "partial": st.lastPartial,
			"unapplied_models": st.lastUnappliedModels, "unreadable": false,
		},
	})
}

// markMemberUnmeasured records a member whose config hash has been unreadable past
// unreadableHashThreshold, and emits config.sync_incomplete once on the transition
// in, exactly as a measured divergence does.
//
// It shares that event type and the amber badge deliberately. Both mean the same
// thing to an operator: this member is not known to hold the primary's config, and
// its routing cannot be trusted to match. Splitting them would add a wire code
// every consumer must learn to say something the message and the unreadable flag
// already say. The distinction that matters, measured wrong versus not measurable,
// is in the message and the metadata.
func (s *Server) markMemberUnmeasured(ctx context.Context, m *Member) {
	st, already := s.flagDiverged(m.ID, divergedUnmeasurable)
	if already {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_incomplete", Severity: "warning", Source: "frontdesk",
		Message:  unmeasuredMessage(m.Name, st.lastReadErr),
		MemberID: m.ID,
		// The name lists stay present and empty: nothing was reported unbuilt here,
		// and one shape per event type keeps consumers from special-casing.
		Metadata: map[string]any{
			"unapplied": st.lastUnapplied, "partial": st.lastPartial,
			"unapplied_models": st.lastUnappliedModels,
			"unreadable":       true, "error": st.lastReadErr,
			"unreadable_since": st.unreadableSince,
		},
	})
}

// unreadableCause renders why a hash read failed, for an alert body. A transport
// failure arrives as a *url.Error carrying the member's full URL, and this cause
// is rendered into the config.sync_incomplete message, which is the field the
// Apprise dispatcher sends offsite (internal/alert.dispatcher uses ev.Message as
// the body). Member names already travel in events; their addresses do not, and a
// LAN hostname and port should not start reaching a notification provider as a
// side effect of this check. The unredacted error still goes to the local log.
func unreadableCause(err error) string {
	if _, ok := errors.AsType[*url.Error](err); ok {
		if isTimeout(err) {
			return "the member did not answer in time"
		}
		return "the member could not be reached"
	}
	return err.Error()
}

// recordUnreadableHash notes that a member's config hash could not be read, and
// reports whether it has now been unreadable long enough to flag the member. The
// first failure only starts the clock, so one slow read never alerts.
func (s *Server) recordUnreadableHash(memberID, cause string, now time.Time) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	if st.unreadableSince.IsZero() {
		st.unreadableSince = now
	}
	st.lastReadErr = cause
	s.syncIncomplete[memberID] = st
	return now.Sub(st.unreadableSince) >= unreadableHashThreshold
}

// clearUnreadableHash stops a member's unreadable clock once a read succeeds. It
// leaves the rest of the member's state alone: a hash that reads but differs is a
// measured divergence, whose retry timer and reported group names must survive.
func (s *Server) clearUnreadableHash(memberID string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st, ok := s.syncIncomplete[memberID]
	if !ok || st.unreadableSince.IsZero() {
		return
	}
	st.unreadableSince = time.Time{}
	st.lastReadErr = ""
	s.syncIncomplete[memberID] = st
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
// fewer providers for them; unappliedModels are per-model disables it has no model
// to apply. All three name their subjects rather than only counting them, because
// the names point at the routing that is missing, thinner, or unmatched. Unapplied
// is the most severe case, and carries the count as well.
//
// With none, the divergence is one Front Desk measured but the member did not
// explain: it committed the config, reported nothing wrong, and still does not
// match. A count there would read "could not build 0 failover group(s)".
func divergenceMessage(member string, unapplied, partial, unappliedModels []string) string {
	var clauses []string
	if len(unapplied) > 0 {
		clauses = append(clauses, fmt.Sprintf("could not build %d failover group(s): %s",
			len(unapplied), joinCapped(unapplied, divergenceMessageMaxNames)))
	}
	if len(partial) > 0 {
		clauses = append(clauses, fmt.Sprintf("built %s with fewer entries than the primary has",
			joinCapped(partial, divergenceMessageMaxNames)))
	}
	if len(unappliedModels) > 0 {
		clauses = append(clauses, fmt.Sprintf("does not hold %s, which the primary has switched off",
			joinCapped(unappliedModels, divergenceMessageMaxNames)))
	}
	if len(clauses) == 0 {
		return fmt.Sprintf("%s applied the config but does not match the primary's config", member)
	}
	return fmt.Sprintf("%s applied the config but %s", member, strings.Join(clauses, ", and "))
}

// unmeasuredMessage renders the operator-facing line for a member whose config
// hash cannot be read. It says unknown rather than wrong, because that is what
// Front Desk knows, and carries the read failure so the operator can tell an
// unreachable endpoint from a member too old to serve it.
func unmeasuredMessage(member, cause string) string {
	if cause == "" {
		return fmt.Sprintf("%s cannot be measured: Front Desk cannot read its config hash, so whether it holds the primary's config is unknown", member)
	}
	return fmt.Sprintf("%s cannot be measured: Front Desk cannot read its config hash (%s), so whether it holds the primary's config is unknown", member, cause)
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

// fetchMemberConfigVersion reads a member's syncable-config hash from
// GET /api/config/version. The hash changes if and only if a synced entity changed,
// and every member computes it over the same deterministically ordered payload, so
// it serves two purposes: from the primary it is the drift signal that starts a
// pass, and from a member it answers whether that member holds the primary's
// config.
//
// It uses readClient, not the health-probe client: the handler builds the entire
// config envelope and hashes it whole and per section, at least the work of
// /api/config/export, so it needs an interactive-read budget. Every member is
// read once per tick, so a probe timeout here would leave a slow but healthy
// member permanently unmeasured.
//
// The per-section hash map rides in the same response on current members; an
// older member answers without it, and the nil map means "no section detail",
// never "everything differs".
func (s *Server) fetchMemberConfigVersion(ctx context.Context, m *Member, token string) (string, map[string]string, error) {
	status, body, err := s.callMemberWith(ctx, s.readClient, http.MethodGet, m.URL, memberConfigVersionPath, token, nil)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("member config-version returned %d", status)
	}
	var v struct {
		Version  string            `json:"version"`
		Sections map[string]string `json:"sections"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", nil, fmt.Errorf("frontdesk: parse member config-version: %w", err)
	}
	if v.Version == "" {
		return "", nil, errors.New("frontdesk: empty member config-version")
	}
	return v.Version, v.Sections, nil
}
