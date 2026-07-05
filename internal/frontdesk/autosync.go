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
// member by itself, snapshotting each member first so a bad propagation is
// recoverable.
//
// Drift is detected by the primary's own config hash (GET /api/config/version):
// the hash is compared against the hash last applied to the fleet, both read
// from the same instance, so it is a valid same-instance "changed since" signal.
// Whether an individual member needs the new config is decided by the member's
// own dry-run diff (never by comparing hashes across instances, which embed
// instance-local ids/timestamps), so an already-converged member is skipped with
// no backup and no import.
//
// No request or prompt content is ever read; only provider/key names and counts
// flow, exactly as in the manual sync.

const (
	memberConfigVersionPath = "/api/config/version"
	memberBackupsPath       = "/api/backups"

	// frontDeskBackupOrigin is the ?origin= value Front Desk passes so its
	// pre-sync snapshots are badged "FD" on the member and spared GFS rotation.
	// It must match internal/api.backupOriginFrontDesk.
	frontDeskBackupOrigin = "frontdesk"

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
	// snapshots and imports config on every drifted member in turn.
	autoSyncKickTimeout = 5 * time.Minute
)

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
// goroutine with a detached context: it reuses the same primary read, per-member
// backup, and dry-run diff as the loop, and is a no-op when auto-sync is off or
// has no primary. It never returns an error; failures log and the loop retries.
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
	applied, allConverged := s.applyAutoSync(ctx, primary, primaryToken, reason, gen)
	// Record the hash as applied only once every reachable member converged onto
	// it. If a member was unreachable, leave the marker so the next tick retries;
	// already-converged members are skipped by their dry-run diff, so the retry
	// costs only cheap probes, never repeated backups or imports.
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
		s.emit(ctx, Event{
			Type: "config.auto_synced", Severity: "info", Source: "frontdesk",
			Message: fmt.Sprintf("Auto-synced %d member(s): %s", applied, reason),
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
	snap := s.poller.Snapshot()
	now := time.Now().UTC()
	for _, m := range members {
		if m.ID == primaryID {
			continue // the primary is the source; it is not "in sync with" itself
		}
		if st, ok := snap[m.ID]; ok && st.Health.Known && st.Health.Healthy {
			s.poller.SetAutoSyncVerified(m.ID, now)
		}
	}
}

// applyAutoSync pushes the primary's config to every other tokened member that
// needs it. It returns how many members it actually re-synced and whether every
// reachable member ended up converged (the signal autoSyncOnce uses to decide
// whether to record the applied hash). Each member that needs the new config is
// snapshotted first; a member whose pre-sync backup fails is skipped, not
// overwritten. reason is stamped onto each synced member's last-sync marker.
//
// gen is the rearm generation captured before this pass began. A rearm (member
// add, token update, enable, or primary repoint) bumps it, so the pass re-checks
// it twice per member: once at the top of the loop, and again right before the
// mutating import (after the slow dry-run and pre-sync backup, which is where a
// repoint is most likely to slip in). It aborts the moment the generation changes,
// so a slow pass cannot import the captured (now-stale) primary's config into a
// member the operator has just repointed away from. The only window no pre-check
// can close is the in-flight import call itself; the rearm's own pass converges
// that member on the next tick. Members synced before the change were current when
// written; the rearm's own pass converges the rest. allConverged is forced false
// on abort so no hash is recorded.
func (s *Server) applyAutoSync(ctx context.Context, primary *Member, primaryToken, reason string, gen int64) (applied int, allConverged bool) {
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
		// Decide whether this member needs the new config from its own dry-run
		// diff, which compares by name and so is valid across instances. The dry-run
		// is never fenced, so the generation is not sent on it.
		res, status, err := s.pushMemberImport(passCtx, m, token, export, true, gen)
		if err != nil {
			debuglog.Debug("frontdesk: auto-sync: member unreachable, will retry", "member", m.Name, "status", status, "error", err)
			allConverged = false
			continue
		}
		if !res.SchemaVersionOK || !res.MasterKeyOK {
			// A version skew or MASTER_KEY mismatch blocks this member. The manual
			// wizard surfaces these explicitly; here we just hold off and retry.
			debuglog.Debug("frontdesk: auto-sync: member not syncable", "member", m.Name)
			allConverged = false
			continue
		}
		added, updated, removed := res.Diff.counts()
		if added+updated+removed == 0 {
			// Already in sync (the member self-converged via its own discovery, or
			// a prior pass wrote it). No backup, no import, not counted as applied,
			// and no per-member event. Nothing was written, so last_config_sync_at
			// stays put (it means a real config write); only advance the live
			// "verified in sync" heartbeat so the Members table shows this member
			// was just confirmed against the primary.
			s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
			continue
		}

		// Snapshot before overwriting so a bad propagation is recoverable. A failed
		// backup blocks this member's overwrite rather than risking an unrecoverable
		// replace.
		if err := s.backupMember(passCtx, m, token); err != nil {
			debuglog.Warn("frontdesk: auto-sync: pre-sync backup failed, skipping member", "member", m.Name, "error", err)
			s.emit(ctx, Event{
				Type: "config.sync_failed", Severity: "warning", Source: "frontdesk",
				Message: fmt.Sprintf("Skipped %s: pre-sync backup failed", m.Name), MemberID: m.ID,
			})
			allConverged = false
			continue
		}

		// Final staleness gate, tightest to the mutation: a rearm or primary repoint
		// can land during this member's (slow) dry-run diff and pre-sync backup. Re-
		// check here so we never even start an import the operator has invalidated.
		// The narrow window between this check and the import committing on the member
		// is closed by passCtx: the watchRearm goroutine cancels it, aborting the
		// in-flight request. The backup just taken is harmless: a recoverable snapshot.
		if stale() {
			debuglog.Debug("frontdesk: auto-sync: aborting stale pass before import", "synced", applied)
			allConverged = false
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
		if !out.OK {
			// Not converged by this pass: a failure (already surfaced by
			// applyMemberConfig) or a benign fence supersede. Either way the hash is
			// not recorded for this generation and the authoritative pass takes over.
			allConverged = false
			continue
		}
		applied++
	}
	return applied, allConverged
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
// changed, so it is the cheap drift signal for the designated primary.
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

// backupMember asks a member to snapshot itself before Front Desk overwrites its
// config, tagging the backup with origin=frontdesk so it is badged distinctly and
// spared from GFS rotation. It uses backupClient, whose deadline exceeds the
// member's own pg_dump budget, so a large member completes its snapshot rather
// than timing out every tick and leaving an orphaned dump holding the backup lock.
func (s *Server) backupMember(ctx context.Context, m *Member, token string) error {
	status, _, err := s.callMemberWith(ctx, s.backupClient, http.MethodPost, m.URL, memberBackupsPath+"?origin="+frontDeskBackupOrigin, token, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("member backup returned %d", status)
	}
	return nil
}
