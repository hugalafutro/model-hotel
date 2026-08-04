package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// This file holds Front Desk's backup-facing calls onto a member's own backup
// API (GET /api/backups, DELETE /api/backups/{filename}).
//
// Front Desk never creates a backup. Members back themselves up on their own
// schedule, so the dumps on each member are the only snapshot of its config.

const (
	memberBackupsPath = "/api/backups"

	// memberBackupOriginFrontDesk is the origin a member reports for a dump a
	// fleet caller asked it to take. It is the only value the fleet prune deletes,
	// and it must match internal/api's backupOriginFrontDesk.
	memberBackupOriginFrontDesk = "frontdesk"

	// memberBackupTimeout bounds a single member backup call: one listing read or
	// one file delete. More generous than the health probe because a member with
	// thousands of dumps takes longer to enumerate them than to answer /health,
	// and far short of the import relay because neither call does real work.
	memberBackupTimeout = 30 * time.Second
)

// memberBackupEntry is the subset of a member's backup-listing entry Front Desk
// reads. Origin is the member's own classification of the file ("manual",
// "scheduled" or "frontdesk"); it is authoritative and is never re-derived here
// from the filename, so an operator's manual backup that happens to be named
// with the word frontdesk in it is not a prune target.
type memberBackupEntry struct {
	Filename  string `json:"filename"`
	CreatedAt string `json:"created_at"`
	Origin    string `json:"origin"`
}

// listMemberBackups reads a member's backup listing. The response is read
// through the shared body cap, so a listing too large to fit fails to parse and
// is returned as an error rather than a partial set: every caller here either
// deletes or judges, and both must act on the whole listing or none of it.
func (s *Server) listMemberBackups(ctx context.Context, m *Member, token string) ([]memberBackupEntry, error) {
	status, body, err := s.callMemberWith(ctx, s.backupClient, http.MethodGet, m.URL, memberBackupsPath, token, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("member backup listing returned %d", status)
	}
	var entries []memberBackupEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		// Don't wrap the decoder error: it can echo a fragment of the response.
		return nil, errors.New("frontdesk: parse member backup listing")
	}
	return entries, nil
}

// backupPruneResult is one member's outcome from a fleet prune run. Deleted
// counts files actually removed (on a dry run, the files that would be removed);
// Failed counts frontdesk-origin files the member refused to delete.
type backupPruneResult struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	Deleted  int    `json:"deleted"`
	Failed   int    `json:"failed"`
	Error    string `json:"error,omitempty"`
}

// pruneFrontDeskBackups deletes every frontdesk-origin backup from every member
// Front Desk can authenticate to. It is operator-triggered and never automatic:
// deleting backups is destructive, so it stays a deliberate action.
//
// ?dryRun=1 counts what would go without deleting anything, so the UI can name
// the number in its confirmation step. Each member is independent: one that
// cannot be read, or that refuses a delete, is reported in its own result and
// leaves the rest of the fleet to be pruned.
func (s *Server) pruneFrontDeskBackups(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dryRun") != ""
	// The run continues even if the caller hangs up, so a client with a short HTTP
	// timeout cannot abandon a member half-pruned with no record of the run.
	ctx := context.WithoutCancel(r.Context())
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	results := make([]backupPruneResult, 0, len(members))
	deleted, failed := 0, 0
	for _, m := range members {
		token, ok, err := s.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			continue // no stored token: the backup API needs admin auth
		}
		res := s.pruneMemberFrontDeskBackups(ctx, m, token, dryRun)
		deleted += res.Deleted
		failed += res.Failed
		results = append(results, res)
	}

	if !dryRun {
		// One audit event per run, attributed to whoever authenticated it the same
		// way a manual sync is. It records the attempt even when nothing was found,
		// because the operator asked for a destructive action either way.
		actor := actorFromContext(ctx)
		s.emit(ctx, Event{
			Type: "backup.pruned", Severity: "info", Source: "frontdesk",
			Message: fmt.Sprintf("Deleted %s taken by Front Desk across %s (%s)",
				util.Count(deleted, "backup", "backups"),
				util.Count(len(results), "member", "members"), actor),
			Metadata: map[string]any{
				"deleted": deleted, "failed": failed, "members": len(results), "actor": actor,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": deleted, "failed": failed, "results": results,
	})
}

// pruneMemberFrontDeskBackups removes one member's frontdesk-origin backups.
// Selection is on the origin the member reports and nothing else: a filename
// match would destroy a manual backup an operator named with the word frontdesk
// in it, and manual, scheduled and unrecognised entries are all left alone.
func (s *Server) pruneMemberFrontDeskBackups(ctx context.Context, m *Member, token string, dryRun bool) backupPruneResult {
	res := backupPruneResult{MemberID: m.ID, Name: m.Name}
	entries, err := s.listMemberBackups(ctx, m, token)
	if err != nil {
		debuglog.Debug("frontdesk: backup prune: read listing", "member", m.Name, "error", err)
		res.Error = "could not read this member's backup listing"
		return res
	}
	for _, e := range entries {
		if e.Origin != memberBackupOriginFrontDesk {
			continue
		}
		if dryRun {
			res.Deleted++
			continue
		}
		path := memberBackupsPath + "/" + url.PathEscape(e.Filename)
		status, _, err := s.callMemberWith(ctx, s.backupClient, http.MethodDelete, m.URL, path, token, nil)
		switch {
		case err == nil && status == http.StatusNoContent:
			res.Deleted++
		case err == nil && status == http.StatusNotFound:
			// Already gone. The goal state holds, so it is not a failure, and it is
			// not counted as a deletion this run either.
		default:
			res.Failed++
		}
	}
	if res.Failed > 0 {
		res.Error = fmt.Sprintf("%s could not be deleted", util.Count(res.Failed, "backup", "backups"))
	}
	return res
}
