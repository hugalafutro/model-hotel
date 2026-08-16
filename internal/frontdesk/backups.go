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

// This file holds the watchdog for a member with no recent scheduled backup. It
// reads a member's own listing (GET /api/backups) and never writes to it.
//
// Front Desk never creates a backup. Members back themselves up on their own
// schedule, so those dumps are the only snapshot of a member's config and their
// age is the only honest measure of whether it is protected.

const (
	memberBackupsPath = "/api/backups"

	// memberBackupOriginScheduled is the origin a member reports for its own
	// rotation scheduler's dumps, and the only origin the staleness watchdog counts.
	// The member derives it from the "_auto" filename marker but reports the word
	// "scheduled" (internal/api.backupOrigin).
	memberBackupOriginScheduled = "scheduled"

	// memberBackupTimeout bounds one member backup-listing read. More generous
	// than the health probe, because a member with thousands of dumps takes
	// longer to enumerate them than to answer /health.
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
	// tracks accumulated history rather than the shape of a document, and the
	// watchdog needs the newest entry regardless of how long a member's history has
	// grown. At roughly 135 bytes an entry the shared 1 MiB cap stops around 7,600
	// dumps; 16 MiB reaches past 120,000, and past that a member reports
	// errMemberRespTooLarge.
	maxMemberBackupListBody = 16 << 20
)

// memberBackupEntry is the subset of a member's backup-listing entry Front Desk
// reads. Origin is the member's own classification ("manual", "scheduled" or
// "frontdesk"), authoritative and never re-derived from the filename, so a manual
// backup named with the word frontdesk in it is not mistaken for one.
type memberBackupEntry struct {
	Filename  string `json:"filename"`
	CreatedAt string `json:"created_at"`
	Origin    string `json:"origin"`
}

// listMemberBackups reads a member's backup listing under maxMemberBackupListBody.
// A listing past even that limit is returned as errMemberRespTooLarge rather than
// a partial set: the watchdog judges a member on its whole listing or not at all,
// never on a truncated prefix that could hide the actually-newest entry.
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
