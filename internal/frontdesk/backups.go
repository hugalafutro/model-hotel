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

// Front Desk's two backup-facing concerns, both reading a member's own listing
// (GET /api/backups): the operator-triggered fleet prune of frontdesk-origin
// dumps, and the watchdog for a member with no recent scheduled backup.
//
// Front Desk never creates a backup. Members back themselves up on their own
// schedule, so those dumps are the only snapshot of a member's config and their
// age is the only honest measure of whether it is protected.

const (
	memberBackupsPath = "/api/backups"

	// memberBackupOriginFrontDesk is the origin a member reports for a dump a fleet
	// caller asked it to take, and the only value the fleet prune deletes. Must
	// match internal/api's backupOriginFrontDesk.
	memberBackupOriginFrontDesk = "frontdesk"

	// memberBackupOriginScheduled is the origin a member reports for its own
	// rotation scheduler's dumps, and the only origin the staleness watchdog counts.
	// The member derives it from the "_auto" filename marker but reports the word
	// "scheduled" (internal/api.backupOrigin).
	memberBackupOriginScheduled = "scheduled"

	// memberBackupTimeout bounds one member backup call: a listing read or a file
	// delete. More generous than the health probe, because a member with thousands
	// of dumps takes longer to enumerate them than to answer /health.
	memberBackupTimeout = 30 * time.Second

	// memberBackupStaleAfter is how old a member's newest scheduled backup may be
	// before it counts as unprotected. A day matches the coarsest useful schedule,
	// so a daily rotation that ran once in the window stays quiet.
	memberBackupStaleAfter = 24 * time.Hour

	// backupWatchInterval is how often every member's listing is re-read. The
	// signal has a 24 hour threshold, so a tighter tick would add member load
	// without making the alert meaningfully earlier.
	backupWatchInterval = 15 * time.Minute

	// maxMemberBackupListBody is the read limit for a member's backup listing, far
	// above the shared maxMemberRespBody. This is the one member response whose size
	// tracks accumulated history rather than the shape of a document, and the prune
	// is needed most on the members holding the most. At roughly 135 bytes an entry
	// the shared 1 MiB cap stops around 7,600 dumps; 16 MiB reaches past 120,000,
	// and past that a member reports errMemberRespTooLarge.
	maxMemberBackupListBody = 16 << 20
)

// memberBackupEntry is the subset of a member's backup-listing entry Front Desk
// reads. Origin is the member's own classification ("manual", "scheduled" or
// "frontdesk"), authoritative and never re-derived from the filename, so a manual
// backup named with the word frontdesk in it is not a prune target.
type memberBackupEntry struct {
	Filename  string `json:"filename"`
	CreatedAt string `json:"created_at"`
	Origin    string `json:"origin"`
}

// listMemberBackups reads a member's backup listing under maxMemberBackupListBody.
// A listing past even that limit is returned as errMemberRespTooLarge rather than
// a partial set: every caller here either deletes or judges, and both must act on
// the whole listing or none of it.
func (s *Server) listMemberBackups(ctx context.Context, m *Member, token string) ([]memberBackupEntry, error) {
	status, body, err := s.callMemberLimited(ctx, s.backupClient, maxMemberBackupListBody,
		http.MethodGet, m.URL, memberBackupsPath, token, nil)
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

// countFrontDeskBackups returns how many entries carry the frontdesk origin.
func countFrontDeskBackups(entries []memberBackupEntry) int {
	n := 0
	for _, e := range entries {
		if e.Origin == memberBackupOriginFrontDesk {
			n++
		}
	}
	return n
}

// recordFrontDeskBackups remembers how many frontdesk-origin dumps a member's
// listing held the last time anything read it. A member that could not be read
// is left at its previous value rather than zeroed: an unanswered listing is not
// evidence that its dumps are gone.
func (s *Server) recordFrontDeskBackups(memberID string, n int) {
	s.backupStaleMu.Lock()
	s.frontDeskBackups[memberID] = n
	s.backupStaleMu.Unlock()
}

// frontDeskBackupCount (GET /api/fleet/backups/frontdesk-count) reports the
// fleet-wide number of frontdesk-origin dumps as of the last read of each
// member's listing. It never calls a member itself: the answer comes from the
// backup watchdog's periodic pass and from prune runs, so the Settings page can
// decide whether to show Fleet maintenance for free. Zero until the first pass
// after startup has read a listing.
func (s *Server) frontDeskBackupCount(w http.ResponseWriter, _ *http.Request) {
	total := 0
	s.backupStaleMu.Lock()
	for _, n := range s.frontDeskBackups {
		total += n
	}
	s.backupStaleMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"count": total})
}

// ---------------------------------------------------------------------------
// Fleet prune of frontdesk-origin backups
// ---------------------------------------------------------------------------

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
// Front Desk can authenticate to. Operator-triggered and never automatic, because
// deleting backups is destructive.
//
// ?dryRun=1 counts what would go without deleting anything, so the UI can name the
// number in its confirmation step. Every member gets a result row, and one that
// cannot be authenticated to, cannot be read, or refuses a delete carries its
// reason and leaves the rest of the fleet to be pruned.
func (s *Server) pruneFrontDeskBackups(w http.ResponseWriter, r *http.Request) {
	// Any value means a dry run, so ?dryRun=0 previews. Deliberately not parsed as a
	// boolean: the permissive reading fails to an unwanted preview, the strict one
	// to a fleet-wide delete from a typo.
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
	deleted, failed, pruned := 0, 0, 0
	for _, m := range members {
		token, ok, err := s.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			// Reported, not silently dropped: the operator asked about the whole fleet,
			// and a member with no stored token still keeps whatever backups it has.
			results = append(results, backupPruneResult{
				MemberID: m.ID, Name: m.Name,
				Error: "no stored admin token for this member",
			})
			continue
		}
		res := s.pruneMemberFrontDeskBackups(ctx, m, token, dryRun)
		deleted += res.Deleted
		failed += res.Failed
		pruned++
		results = append(results, res)
	}

	if !dryRun {
		// One audit event per run, attributed like a manual sync, recorded even when
		// nothing was found: the operator asked for a destructive action either way.
		// The count is members actually visited, so a tokenless one does not inflate
		// the reach of the run.
		actor := actorFromContext(ctx)
		s.emit(ctx, Event{
			Type: "backup.pruned", Severity: "info", Source: "frontdesk",
			Message: fmt.Sprintf("Deleted %s taken by Front Desk across %s (%s)",
				util.Count(deleted, "backup", "backups"),
				util.Count(pruned, "member", "members"), actor),
			Metadata: map[string]any{
				"deleted": deleted, "failed": failed, "members": pruned,
				"skipped": len(results) - pruned, "actor": actor,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": deleted, "failed": failed, "results": results,
	})
}

// pruneMemberFrontDeskBackups removes one member's frontdesk-origin backups.
// Selection is on the reported origin and nothing else, so manual, scheduled and
// unrecognised entries are all left alone.
func (s *Server) pruneMemberFrontDeskBackups(ctx context.Context, m *Member, token string, dryRun bool) backupPruneResult {
	res := backupPruneResult{MemberID: m.ID, Name: m.Name}
	entries, err := s.listMemberBackups(ctx, m, token)
	if err != nil {
		debuglog.Debug("frontdesk: backup prune: read listing", "member", m.Name, "error", err)
		// Separated from an unreadable or malformed listing because the operator's next
		// move differs: clearing some dumps on the member itself brings it back within
		// reach of this action.
		res.Error = "could not read this member's backup listing"
		if errors.Is(err, errMemberRespTooLarge) {
			res.Error = "this member holds more backups than Front Desk can list at once"
		}
		return res
	}
	// Either kind of run has just read the listing, so both refresh the count
	// Settings consults; a real run corrects it again below to what survived.
	s.recordFrontDeskBackups(m.ID, countFrontDeskBackups(entries))
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
			// Already gone: the goal state holds, so neither a failure nor a deletion.
		default:
			res.Failed++
		}
	}
	if !dryRun {
		// What the member refused to delete is what it still holds.
		s.recordFrontDeskBackups(m.ID, res.Failed)
	}
	if res.Failed > 0 {
		res.Error = fmt.Sprintf("%s could not be deleted", util.Count(res.Failed, "backup", "backups"))
	}
	return res
}

// ---------------------------------------------------------------------------
// Unprotected-member watchdog
// ---------------------------------------------------------------------------

// RunBackupWatch re-reads every member's backup listing on a fixed tick until ctx
// is cancelled. Started once at startup, alongside RunAutoSync. The first pass
// waits out one interval, mirroring RunFleetState, so a cancelled context costs no
// member calls; the 24 hour threshold makes the delay immaterial.
func (s *Server) RunBackupWatch(ctx context.Context) {
	ticker := time.NewTicker(backupWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkMemberBackups(ctx)
		}
	}
}

// checkMemberBackups judges each member on the age of its newest scheduled backup
// and drives the edge-triggered backup.stale / backup.recovered pair.
//
// Only a member whose listing was actually read is judged: one with no stored token
// or no answer has not been measured, and an unreachable member is health.down's to
// report. The result is an alert and nothing more, never fleet state, because a
// member with stale backups still serves traffic correctly.
func (s *Server) checkMemberBackups(ctx context.Context) {
	members, err := s.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: backup watch: list members", "error", err)
		return
	}
	for _, m := range members {
		token, ok, err := s.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			continue // no stored token: the backup API needs admin auth
		}
		entries, err := s.listMemberBackups(ctx, m, token)
		if err != nil {
			debuglog.Debug("frontdesk: backup watch: read listing", "member", m.Name, "error", err)
			continue
		}
		s.recordFrontDeskBackups(m.ID, countFrontDeskBackups(entries))
		newest, found := newestScheduledBackup(entries)
		if !found || time.Since(newest) > memberBackupStaleAfter {
			s.markBackupStale(ctx, m, newest, found)
			continue
		}
		s.clearBackupStale(ctx, m)
	}
}

// newestScheduledBackup returns the creation time of the most recent
// scheduled-origin entry. Only the member's own scheduler produces those, so a
// manual or frontdesk-origin file never stands in for one: neither is evidence
// that anything backs the member up on a schedule. An entry with an unparseable
// timestamp is skipped rather than treated as current.
func newestScheduledBackup(entries []memberBackupEntry) (time.Time, bool) {
	var newest time.Time
	found := false
	for _, e := range entries {
		if e.Origin != memberBackupOriginScheduled {
			continue
		}
		at, err := time.Parse(time.RFC3339, e.CreatedAt)
		if err != nil {
			continue
		}
		if !found || at.After(newest) {
			newest, found = at, true
		}
	}
	return newest, found
}

// markBackupStale records a member as unprotected and emits backup.stale once on
// the transition in, mirroring holdMemberForSkew: the member is re-read every pass,
// so a level-triggered event would re-alert until it was fixed. found reports
// whether newest is a real timestamp; a member with no scheduled backup at all
// carries an empty newest_backup_at.
func (s *Server) markBackupStale(ctx context.Context, m *Member, newest time.Time, found bool) {
	s.backupStaleMu.Lock()
	already := s.backupStale[m.ID]
	s.backupStale[m.ID] = true
	s.backupStaleMu.Unlock()
	if already {
		return
	}
	at := ""
	if found {
		at = newest.UTC().Format(time.RFC3339)
	}
	s.emit(ctx, Event{
		Type: "backup.stale", Severity: "warning", Source: "frontdesk",
		Message:  fmt.Sprintf("%s has no database backup from the last 24 hours", m.Name),
		MemberID: m.ID,
		Metadata: map[string]any{"newest_backup_at": at},
	})
}

// clearBackupStale forgets a member backing itself up again and emits
// backup.recovered once on the transition out, so a later lapse re-alerts. A member
// never flagged emits nothing.
func (s *Server) clearBackupStale(ctx context.Context, m *Member) {
	s.backupStaleMu.Lock()
	was := s.backupStale[m.ID]
	delete(s.backupStale, m.ID)
	s.backupStaleMu.Unlock()
	if !was {
		return
	}
	s.emit(ctx, Event{
		Type: "backup.recovered", Severity: "success", Source: "frontdesk",
		Message:  fmt.Sprintf("%s has a recent database backup again", m.Name),
		MemberID: m.ID,
	})
}
