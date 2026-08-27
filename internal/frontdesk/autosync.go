package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
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
	// lastSync comes back from SQLite as time.Unix(0, at).UTC() (store_fleet.go),
	// so it has no monotonic reading and a backwards step of this host's clock
	// leaves it dated after now. A raw subtraction would then be negative, which
	// is under both thresholds, and a fleet that stopped syncing entirely would
	// report tier 0 for as long as the step lasted - silencing the
	// config.autosync_stale watchdog and the degraded/faulty fleet state
	// together. util.TrustedAge absorbs ordinary skew and refuses the rest.
	age, aged := util.TrustedAge(now, lastSync)
	if !aged {
		// Same answer as a fleet with no recorded sync at all: the timestamp
		// cannot vouch for freshness, but neither can it honestly age far enough
		// to claim tier 2.
		return 1
	}
	switch {
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
