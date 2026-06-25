package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// This file implements the Front Desk side of HA Phase 5 fleet config sync. It
// orchestrates the member-side /api/config/export + /api/config/import endpoints
// (see internal/api/configsync.go): pull the chosen primary's config, then push
// it to every other member so the fleet converges to one configuration.
//
// It mirrors the admin-token sync flow (admintoken.go) and reuses its primary
// concept and netguard-protected probe client. It is a SEPARATE action from
// token sync: config replace can remove providers/keys on a replica, so it must
// never ride along with a routine token rotation. No key material is ever
// returned to the browser or logged; only names and counts.

const (
	memberConfigExportPath = "/api/config/export"
	memberConfigImportPath = "/api/config/import"
)

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
	Providers   memberEntityDiff `json:"providers"`
	VirtualKeys memberEntityDiff `json:"virtual_keys"`
	Settings    memberEntityDiff `json:"settings"`
}

// added/updated/removed total the diff across all entity kinds.
func (d memberConfigDiff) counts() (added, updated, removed int) {
	for _, e := range []memberEntityDiff{d.Providers, d.VirtualKeys, d.Settings} {
		added += len(e.Added)
		updated += len(e.Updated)
		removed += len(e.Removed)
	}
	return added, updated, removed
}

// configPreviewItem is one member's projected outcome before a sync.
type configPreviewItem struct {
	MemberID    string `json:"member_id"`
	Name        string `json:"name"`
	Disposition string `json:"disposition"` // matches | overwrite | blocked
	Added       int    `json:"added"`
	Updated     int    `json:"updated"`
	Removed     int    `json:"removed"`
	Note        string `json:"note,omitempty"`
}

// configSyncPreview pulls the primary's config once, then asks every other
// member for a dry-run diff so the UI can show, by name, what each will gain,
// overwrite, or lose before anything is written.
func (s *Server) configSyncPreview(w http.ResponseWriter, r *http.Request) {
	primaryID := r.URL.Query().Get("primary")
	primary, primaryToken, err := s.memberTokenOrErr(r.Context(), primaryID)
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

	items := make([]configPreviewItem, 0, len(members))
	for _, m := range members {
		items = append(items, s.previewMemberConfig(r.Context(), m, primary.ID, export))
	}
	writeJSON(w, http.StatusOK, map[string]any{"primary_id": primary.ID, "items": items})
}

// previewMemberConfig classifies one member relative to the primary's config.
func (s *Server) previewMemberConfig(ctx context.Context, m *Member, primaryID string, export []byte) configPreviewItem {
	item := configPreviewItem{MemberID: m.ID, Name: m.Name}
	if m.ID == primaryID {
		item.Disposition = dispMatches // the source itself
		return item
	}
	if !m.HasToken {
		item.Disposition = dispBlocked
		return item
	}
	token, ok, err := s.store.MemberToken(ctx, m.ID)
	if err != nil || !ok {
		item.Disposition = dispBlocked
		return item
	}

	res, err := s.pushMemberImport(ctx, m, token, export, true)
	if err != nil {
		item.Disposition = dispBlocked
		item.Note = "could not reach this member"
		return item
	}
	// Check schema before MASTER_KEY: a member rejects a bad schema_version (422)
	// before it ever runs the MASTER_KEY canary, so on that path master_key_ok is
	// an unevaluated false. Testing it first would misreport a version skew as a
	// key mismatch and send the operator chasing the wrong problem.
	if !res.SchemaVersionOK {
		item.Disposition = dispBlocked
		item.Note = "version mismatch with the primary"
		return item
	}
	if !res.MasterKeyOK {
		item.Disposition = dispBlocked
		item.Note = "MASTER_KEY does not match the primary"
		return item
	}
	item.Added, item.Updated, item.Removed = res.Diff.counts()
	if item.Added == 0 && item.Updated == 0 && item.Removed == 0 {
		item.Disposition = dispMatches
	} else {
		item.Disposition = dispOverwrite
	}
	return item
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
		results = append(results, s.applyMemberConfig(r.Context(), m, token, export))
	}
	writeJSON(w, http.StatusOK, map[string]any{"primary_id": primary.ID, "results": results})
}

// applyMemberConfig imports the primary's config onto one member and records an
// audit event for the outcome.
func (s *Server) applyMemberConfig(ctx context.Context, m *Member, token string, export []byte) syncResultItem {
	res := syncResultItem{MemberID: m.ID, Name: m.Name}
	out, err := s.pushMemberImport(ctx, m, token, export, false)
	switch {
	case err != nil:
		res.Error = "could not reach this member"
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
		s.emit(ctx, Event{
			Type: "config.synced", Severity: "info", Source: "frontdesk",
			Message: fmt.Sprintf("Config synced to %s", m.Name), MemberID: m.ID,
		})
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
// surface a precise disposition.
func (s *Server) pushMemberImport(ctx context.Context, m *Member, token string, export []byte, dryRun bool) (memberImportResult, error) {
	path := memberConfigImportPath
	if dryRun {
		path += "?dryRun=1"
	}
	status, body, err := s.callMember(ctx, http.MethodPost, m.URL, path, token, strings.NewReader(string(export)))
	if err != nil {
		return memberImportResult{}, err
	}
	switch status {
	case http.StatusOK, http.StatusConflict, http.StatusUnprocessableEntity:
		var res memberImportResult
		if err := json.Unmarshal(body, &res); err != nil {
			return memberImportResult{}, errors.New("frontdesk: parse member import response")
		}
		return res, nil
	default:
		return memberImportResult{}, fmt.Errorf("member config-import returned %d", status)
	}
}
