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
	SchemaVersionOK bool             `json:"schema_version_ok"`
	MasterKeyOK     bool             `json:"master_key_ok"`
	Applied         bool             `json:"applied"`
	Diff            memberConfigDiff `json:"diff"`
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
}

// added/updated/removed total the diff across all entity kinds.
func (d memberConfigDiff) counts() (added, updated, removed int) {
	for _, e := range []memberEntityDiff{d.Providers, d.VirtualKeys, d.Settings, d.FailoverGroups} {
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

	results := make([]syncResultItem, 0)
	for _, m := range members {
		if m.ID == primary.ID || !m.HasToken {
			continue // the source, and token-less members (flagged in preview), are skipped
		}
		token, ok, err := s.store.MemberToken(r.Context(), m.ID)
		if err != nil || !ok {
			continue
		}
		// Snapshot a member that will actually change before the destructive replace,
		// the same recoverability guarantee the auto-syncer gives. A failed backup
		// leaves the member untouched and reported rather than risking an
		// unrecoverable overwrite.
		if failed := s.backupChangingMember(r.Context(), m, token, export); failed != nil {
			results = append(results, *failed)
			continue
		}
		results = append(results, s.applyMemberConfig(r.Context(), m, token, export, wizardSyncReason, true))
	}
	s.recordFleetSyncRun(r.Context(), primary, results)
	writeJSON(w, http.StatusOK, map[string]any{"primary_id": primary.ID, "results": results})
}

// backupChangingMember snapshots a member before the wizard overwrites it, giving
// the manual sync the same recoverability guarantee the auto-syncer already has. A
// quick dry-run gates the backup: a member that is unreachable, version/MASTER_KEY-
// blocked, or already converged is not needlessly snapshotted (nor mislabeled
// "backup failed"), it falls through to applyMemberConfig which does the authoritative
// import and classification. It returns a non-nil result only when the snapshot was
// attempted and failed, in which case the member is left untouched and reported,
// never overwritten.
func (s *Server) backupChangingMember(ctx context.Context, m *Member, token string, export []byte) *syncResultItem {
	preview, status, err := s.pushMemberImport(ctx, m, token, export, true)
	if err != nil || status != http.StatusOK || !preview.SchemaVersionOK || !preview.MasterKeyOK {
		return nil // unreachable or blocked: let applyMemberConfig report the real cause
	}
	if added, updated, removed := preview.Diff.counts(); added+updated+removed == 0 {
		return nil // already converged: no overwrite, so nothing to snapshot
	}
	if err := s.backupMember(ctx, m, token); err != nil {
		debuglog.Warn("frontdesk: wizard sync: pre-sync backup failed, skipping member", "member", m.Name, "error", err)
		s.emit(ctx, Event{
			Type: "config.sync_failed", Severity: "warning", Source: "frontdesk",
			Message: fmt.Sprintf("Skipped %s: pre-sync backup failed", m.Name), MemberID: m.ID,
		})
		return &syncResultItem{MemberID: m.ID, Name: m.Name, Error: "pre-sync backup failed; this member was left unchanged"}
	}
	return nil
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
func (s *Server) applyMemberConfig(ctx context.Context, m *Member, token string, export []byte, reason string, emitSuccessEvent bool) syncResultItem {
	res := syncResultItem{MemberID: m.ID, Name: m.Name}
	out, status, err := s.pushMemberImport(ctx, m, token, export, false)
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
	case !out.Applied:
		res.Error = "this member did not apply the config"
	default:
		res.OK = true
	}

	if res.OK {
		if err := s.store.SetMemberLastSync(ctx, m.ID, time.Now().UTC(), reason); err != nil {
			debuglog.Warn("frontdesk: stamp member last-sync", "member", m.Name, "error", err)
		}
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
func (s *Server) pushMemberImport(ctx context.Context, m *Member, token string, export []byte, dryRun bool) (memberImportResult, int, error) {
	path := memberConfigImportPath
	if dryRun {
		path += "?dryRun=1"
	}
	// The import client gets a longer deadline than the health probe: a real
	// import runs model discovery on the member, which routinely exceeds the 4s
	// probe timeout, and timing out there would mislabel a successful import as
	// "could not reach this member".
	status, body, err := s.callMemberWith(ctx, s.syncClient, http.MethodPost, m.URL, path, token, strings.NewReader(string(export)))
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
