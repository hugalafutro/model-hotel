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

	primary, primaryToken, err := s.memberTokenOrErr(ctx, cfg.PrimaryID)
	if err != nil {
		// The designated primary was removed or lost its token. The loop cannot
		// proceed without a source; stay quiet at debug and reset the window.
		debuglog.Debug("frontdesk: auto-sync: primary unavailable", "error", err)
		return ""
	}

	hash, err := s.fetchMemberConfigVersion(ctx, primary, primaryToken)
	if err != nil {
		debuglog.Debug("frontdesk: auto-sync: read primary version", "member", primary.Name, "error", err)
		return ""
	}

	// Unchanged since the last fleet-wide apply: nothing to propagate.
	if hash == cfg.LastHash {
		return hash
	}
	// Coalesce: only act once the primary's config has settled (the same hash two
	// ticks running), so a multi-step edit session triggers one sync rather than
	// one per intermediate save.
	if hash != prev {
		return hash
	}

	applied, allConverged := s.applyAutoSync(ctx, primary, primaryToken)
	// Record the hash as applied only once every reachable member converged onto
	// it. If a member was unreachable, leave the marker so the next tick retries;
	// already-converged members are skipped by their dry-run diff, so the retry
	// costs only cheap probes, never repeated backups or imports.
	if allConverged {
		if err := s.store.SetAutoSyncLastHash(ctx, hash); err != nil {
			debuglog.Warn("frontdesk: auto-sync: record applied hash", "error", err)
		}
	}
	if applied > 0 {
		s.emit(ctx, Event{
			Type: "config.auto_synced", Severity: "info", Source: "frontdesk",
			Message: fmt.Sprintf("Auto-synced %d member(s): %s", applied, autoSyncReason),
		})
	}
	return hash
}

// applyAutoSync pushes the primary's config to every other tokened member that
// needs it. It returns how many members it actually re-synced and whether every
// reachable member ended up converged (the signal autoSyncOnce uses to decide
// whether to record the applied hash). Each member that needs the new config is
// snapshotted first; a member whose pre-sync backup fails is skipped, not
// overwritten.
func (s *Server) applyAutoSync(ctx context.Context, primary *Member, primaryToken string) (applied int, allConverged bool) {
	export, err := s.fetchMemberExport(ctx, primary, primaryToken)
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
			continue
		}
		// Decide whether this member needs the new config from its own dry-run
		// diff, which compares by name and so is valid across instances.
		res, status, err := s.pushMemberImport(ctx, m, token, export, true)
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
			continue // already converged: no backup, no import
		}

		// Snapshot before overwriting so a bad propagation is recoverable. A failed
		// backup blocks this member's overwrite rather than risking an unrecoverable
		// replace.
		if err := s.backupMember(ctx, m, token); err != nil {
			debuglog.Warn("frontdesk: auto-sync: pre-sync backup failed, skipping member", "member", m.Name, "error", err)
			s.emit(ctx, Event{
				Type: "config.sync_failed", Severity: "warning", Source: "frontdesk",
				Message: fmt.Sprintf("Skipped %s: pre-sync backup failed", m.Name), MemberID: m.ID,
			})
			allConverged = false
			continue
		}

		// applyMemberConfig stamps the member's last-sync marker with this reason on
		// success, so the Members table shows when and why it last converged. Per-
		// member success events are suppressed here (emitSuccessEvent=false); the
		// loop emits one roll-up below so a fleet sync does not toast per member.
		out := s.applyMemberConfig(ctx, m, token, export, autoSyncReason, false)
		if !out.OK {
			allConverged = false
			continue // applyMemberConfig already emitted the per-member failure
		}
		applied++
	}
	return applied, allConverged
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
