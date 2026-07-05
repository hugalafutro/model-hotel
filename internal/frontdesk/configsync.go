package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// This file implements the Front Desk side of HA fleet config sync. It
// orchestrates the member-side /api/config/export + /api/config/import endpoints
// (see internal/api/configsync.go): pull the chosen primary's config, then push
// it to every other member so the fleet converges to one configuration.
//
// Config replace can remove providers/keys on a replica, so it is a deliberate,
// primary-driven, double-confirmed action. No key material is ever returned to
// the browser or logged; only names and counts.

const (
	memberConfigExportPath = "/api/config/export"
	memberConfigImportPath = "/api/config/import"

	// fleetSourceGenHeader carries the monotonic source generation (auto_sync_gen)
	// on a real import so the member's commit fence can refuse an out-of-order
	// stale push. It must match internal/api.fleetSourceGenHeader. An older member
	// ignores it, so sending it is always safe.
	fleetSourceGenHeader = "X-Fleet-Source-Gen"

	// wizardSyncReason is stamped on a member's last-config-sync marker when the
	// operator drives the sync through the wizard (vs the automatic loop).
	wizardSyncReason = "manual sync from the wizard"
)

// syncResultItem is one member's outcome from a fleet sync action.
type syncResultItem struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// memberImportResult mirrors internal/api.importResponse so Front Desk can read
// a member's import/dry-run outcome.
type memberImportResult struct {
	SchemaVersionOK bool `json:"schema_version_ok"`
	MasterKeyOK     bool `json:"master_key_ok"`
	Applied         bool `json:"applied"`
	// Stale is true when the member's commit fence refused this import because a
	// newer source generation already applied (a rearm/repoint superseded this
	// push). It is a benign, expected outcome, not a sync failure.
	Stale bool             `json:"stale,omitempty"`
	Diff  memberConfigDiff `json:"diff"`
}

type memberEntityDiff struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}

type memberConfigDiff struct {
	Providers      memberEntityDiff `json:"providers"`
	VirtualKeys    memberEntityDiff `json:"virtual_keys"`
	Settings       memberEntityDiff `json:"settings"`
	FailoverGroups memberEntityDiff `json:"failover_groups"`
	Users          memberEntityDiff `json:"users"`
}

// added/updated/removed total the diff across all entity kinds.
func (d memberConfigDiff) counts() (added, updated, removed int) {
	for _, e := range []memberEntityDiff{d.Providers, d.VirtualKeys, d.Settings, d.FailoverGroups, d.Users} {
		added += len(e.Added)
		updated += len(e.Updated)
		removed += len(e.Removed)
	}
	return added, updated, removed
}

// configSync pulls the primary's config and applies it to every other member
// Front Desk can authenticate to. Each member is independent: a failure leaves
// that member untouched and is reported.
func (s *Server) configSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrimaryID string `json:"primary_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	primary, primaryToken, err := s.memberTokenOrErr(r.Context(), req.PrimaryID)
	if err != nil {
		writeError(w, err)
		return
	}
	export, err := s.fetchMemberExport(r.Context(), primary, primaryToken)
	if err != nil {
		http.Error(w, "could not read the primary's config", http.StatusBadGateway)
		return
	}

	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	// Stamp this manual sync with the current source generation so it advances the
	// members' commit fence: a stale auto-sync that was in flight when the operator
	// ran the wizard cannot regress a member to the older config afterwards. The
	// generation only increases on a rearm, so it is never older than one a prior
	// auto-sync applied, and an equal generation still applies (not refused).
	gen, err := s.store.AutoSyncGen(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	results := make([]syncResultItem, 0)
	for _, m := range members {
		if m.ID == primary.ID || !m.HasToken {
			continue // the source, and token-less members (flagged in preview), are skipped
		}
		token, ok, err := s.store.MemberToken(r.Context(), m.ID)
		if err != nil || !ok {
			continue
		}
		// Gate the destructive replace on a dry-run: a member that will actually
		// change is snapshotted first (the same recoverability guarantee the
		// auto-syncer gives), an already-converged member is reported without an
		// import, and a member whose backup fails is left untouched and reported.
		if item, proceed := s.prepareMemberSync(r.Context(), m, token, export); !proceed {
			results = append(results, *item)
			continue
		}
		results = append(results, s.applyMemberConfig(r.Context(), m, token, export, wizardSyncReason, true, gen))
	}
	s.recordFleetSyncRun(r.Context(), primary, results)
	writeJSON(w, http.StatusOK, map[string]any{"primary_id": primary.ID, "results": results})
}

// prepareMemberSync runs the dry-run that gates the wizard's destructive replace
// and reports whether the caller should proceed to applyMemberConfig. It returns
// (item, proceed):
//
//   - proceed=true (item nil): either the member is reachable, syncable, and
//     changing, and has just been snapshotted; or it is unreachable / version- or
//     MASTER_KEY-blocked. In both cases the caller runs applyMemberConfig, which
//     performs the authoritative import and reports the real outcome. A blocked or
//     unreachable member cannot be destructively written (its import is refused),
//     so letting it fall through costs nothing and yields a precise error.
//   - proceed=false (item set): the member is already converged (reported OK, no
//     import) or its pre-sync backup failed (reported as left unchanged). The
//     caller skips applyMemberConfig and records item as-is.
//
// Crucially, a converged member is skipped entirely rather than handed to a no-op
// import: re-importing it would reopen the window where a member edited between the
// dry-run and the real import gets overwritten without the snapshot this gate is
// meant to guarantee. This mirrors the auto-syncer, which also skips converged
// members outright.
func (s *Server) prepareMemberSync(ctx context.Context, m *Member, token string, export []byte) (*syncResultItem, bool) {
	preview, status, err := s.pushMemberImport(ctx, m, token, export, true, 0) // dry-run: gen unused (no fence header)
	if err != nil || status != http.StatusOK || !preview.SchemaVersionOK || !preview.MasterKeyOK {
		return nil, true // unreachable or blocked: let applyMemberConfig report the real cause
	}
	if added, updated, removed := preview.Diff.counts(); added+updated+removed == 0 {
		// Already in sync: no backup, no import, and no last_config_sync_at stamp
		// (nothing was written; that column means a real config write). Advance the
		// live "verified in sync" heartbeat so the Members table shows the wizard
		// just confirmed this member matches the primary, matching the auto path.
		s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
		return &syncResultItem{MemberID: m.ID, Name: m.Name, OK: true}, false
	}
	if err := s.backupMember(ctx, m, token); err != nil {
		debuglog.Warn("frontdesk: wizard sync: pre-sync backup failed, skipping member", "member", m.Name, "error", err)
		s.emit(ctx, Event{
			Type: "config.sync_failed", Severity: "warning", Source: "frontdesk",
			Message: fmt.Sprintf("Skipped %s: pre-sync backup failed", m.Name), MemberID: m.ID,
		})
		return &syncResultItem{MemberID: m.ID, Name: m.Name, Error: "pre-sync backup failed; this member was left unchanged"}, false
	}
	return nil, true // snapshotted: proceed to the authoritative import
}

// recordFleetSyncRun stamps the last-run marker when a sync action updated at
// least one member, so the wizard can show it has run before. A persistence
// failure is non-fatal: the sync itself already succeeded, so it is logged and
// swallowed rather than surfaced.
func (s *Server) recordFleetSyncRun(ctx context.Context, primary *Member, results []syncResultItem) {
	changed := false
	for _, r := range results {
		if r.OK {
			changed = true
			break
		}
	}
	if !changed {
		return
	}
	if err := s.store.SetFleetSyncState(ctx, primary.ID, primary.Name, time.Now().UTC()); err != nil {
		debuglog.Warn("frontdesk: record fleet sync state", "error", err)
	}
}

// applyMemberConfig imports the primary's config onto one member and records an
// audit event for the outcome. On success it stamps the member's last-config-sync
// marker with reason (shown in the Members table), so both the wizard and the
// auto-sync loop record why and when a member last converged.
//
// emitSuccessEvent controls only the per-member success event: the wizard wants
// one event per member (it is a deliberate operator action), but the background
// auto-syncer sets it false and emits a single roll-up instead, so a fleet sync
// does not toast once per member. Failure events always fire regardless, since a
// member left behind is worth surfacing in either path.
func (s *Server) applyMemberConfig(ctx context.Context, m *Member, token string, export []byte, reason string, emitSuccessEvent bool, sourceGen int64) syncResultItem {
	res := syncResultItem{MemberID: m.ID, Name: m.Name}
	out, status, err := s.pushMemberImport(ctx, m, token, export, false, sourceGen)
	switch {
	case err != nil && status == 0:
		res.Error = "could not reach this member"
	case err != nil:
		// The member answered, just with a status we cannot apply: surface it so a
		// wrong stored token or a member-side error is not mislabeled "offline".
		res.Error = fmt.Sprintf("this member rejected the request (HTTP %d)", status)
	case !out.SchemaVersionOK:
		// Schema is checked before MASTER_KEY: a 422 short-circuits before the
		// canary, leaving master_key_ok an unevaluated false (see previewMemberConfig).
		res.Error = "version mismatch with the primary"
	case !out.MasterKeyOK:
		res.Error = "MASTER_KEY does not match the primary"
	case out.Stale:
		// The member's commit fence refused this push because a newer source
		// generation already applied. This is the expected, benign outcome of a
		// rearm/repoint landing mid-flight: the superseding pass is authoritative,
		// so do not stamp last-sync, do not count it as converged, and do not emit a
		// failure event. res.OK stays false (with no Error) so the caller leaves the
		// member for the newer pass; a soft note documents the disposition.
		res.Error = "superseded by a newer sync"
		debuglog.Debug("frontdesk: config sync superseded by a newer generation", "member", m.Name, "source_gen", sourceGen)
		// Counted under its own label: a fence supersede is benign, and folding it
		// into "err" would make routine rearms look like sync failures on a graph.
		recordConfigSync("superseded")
		return res
	case !out.Applied:
		res.Error = "this member did not apply the config"
	default:
		res.OK = true
	}

	if res.OK {
		recordConfigSync("ok")
	} else {
		recordConfigSync("err")
	}

	if res.OK {
		if err := s.store.SetMemberLastSync(ctx, m.ID, time.Now().UTC(), reason); err != nil {
			debuglog.Warn("frontdesk: stamp member last-sync", "member", m.Name, "error", err)
		}
		// A real write also confirms the member is in sync now: advance the live
		// heartbeat alongside the persisted last_config_sync_at stamp.
		s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
		if emitSuccessEvent {
			s.emit(ctx, Event{
				Type: "config.synced", Severity: "info", Source: "frontdesk",
				Message: fmt.Sprintf("Config synced to %s", m.Name), MemberID: m.ID,
			})
		}
	} else {
		debuglog.Warn("frontdesk: config sync failed", "member", m.Name, "error", res.Error)
		s.emit(ctx, Event{
			Type: "config.sync_failed", Severity: "warning", Source: "frontdesk",
			Message: fmt.Sprintf("Failed to sync config to %s", m.Name), MemberID: m.ID,
		})
	}
	return res
}

// fetchMemberExport reads a member's config envelope as raw JSON so it can be
// re-posted to replicas verbatim (preserving the base64 key ciphertext).
func (s *Server) fetchMemberExport(ctx context.Context, m *Member, token string) ([]byte, error) {
	status, body, err := s.callMember(ctx, http.MethodGet, m.URL, memberConfigExportPath, token, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("member config-export returned %d", status)
	}
	return body, nil
}

// pushMemberImport posts the config envelope to a member. dryRun=true asks for a
// diff without writing. A 409 (MASTER_KEY mismatch) or 422 (schema) is parsed
// into the result rather than treated as a transport error, so the caller can
// surface a precise disposition. The returned status is the member's HTTP status
// (0 on a transport failure where the member never answered), so the caller can
// tell a genuinely unreachable member from one that answered with a rejecting
// code (e.g. 401/403 wrong token, 500) and report the real cause.
func (s *Server) pushMemberImport(ctx context.Context, m *Member, token string, export []byte, dryRun bool, sourceGen int64) (memberImportResult, int, error) {
	path := memberConfigImportPath
	var headers [][2]string
	if dryRun {
		path += "?dryRun=1"
		// A dry run is read-only and never fenced, so the source-generation header
		// is deliberately omitted: it carries no meaning for a preview.
	} else {
		// Stamp the commit fence on the real import so the member can refuse a
		// stale, out-of-order push (a primary repoint that lands mid-flight).
		headers = append(headers, [2]string{fleetSourceGenHeader, strconv.FormatInt(sourceGen, 10)})
	}
	// The import client gets a longer deadline than the health probe: a real
	// import runs model discovery on the member, which routinely exceeds the 4s
	// probe timeout, and timing out there would mislabel a successful import as
	// "could not reach this member".
	status, body, err := s.callMemberWith(ctx, s.syncClient, http.MethodPost, m.URL, path, token, strings.NewReader(string(export)), headers...)
	if err != nil {
		return memberImportResult{}, 0, err
	}
	switch status {
	case http.StatusOK, http.StatusConflict, http.StatusUnprocessableEntity:
		var res memberImportResult
		if err := json.Unmarshal(body, &res); err != nil {
			return memberImportResult{}, status, errors.New("frontdesk: parse member import response")
		}
		return res, status, nil
	default:
		return memberImportResult{}, status, fmt.Errorf("member config-import returned %d", status)
	}
}
