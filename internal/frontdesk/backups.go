package frontdesk

import (
	"context"
	"fmt"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
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

	// memberBackupStaleGrace is how far past its own interval a member's newest
	// scheduled backup may be before it counts as unprotected. A member's newest
	// dump is routinely a little over one interval old: its scheduler dates the
	// next run from the previous dump's file time, which is when that dump
	// finished, so each cycle is the interval plus the dump's duration. A restart
	// adds its downtime and a minute's startup delay, and a tick that finds a
	// manual backup or restore holding the backup lock retries up to five
	// minutes later (internal/api/backup_scheduler.go). Judging at exactly the
	// interval flagged a healthy member whenever a pass landed in that gap.
	memberBackupStaleGrace = time.Hour

	// backupWatchInterval is how often every member's listing is re-read. The
	// signal's threshold is at least a day, so a tighter tick would add member load
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
	var entries []memberBackupEntry
	if err := getMemberJSON(ctx, s.backupClient, maxMemberBackupListBody, m.URL, memberBackupsPath, token,
		"member backup listing", &entries); err != nil {
		return nil, err
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
		token, ok := s.store.MemberTokenOf(ctx, m)
		if !ok {
			continue // no stored token: the backup API needs admin auth
		}
		entries, err := s.listMemberBackups(ctx, m, token)
		if err != nil {
			debuglog.Debug("frontdesk: backup watch: read listing", "member", m.Name, "error", err)
			continue
		}
		interval, err := s.memberBackupInterval(ctx, m, token)
		if err != nil {
			debuglog.Debug("frontdesk: backup watch: read settings", "member", m.Name, "error", err)
			continue
		}
		newest, found := newestScheduledBackup(entries)
		// The timestamp is parsed straight out of the member's own listing, so a
		// member with a fast clock (or a bad created_at) can report a backup
		// dated in the future. A raw subtraction then returns a NEGATIVE
		// duration, which never exceeds the threshold, so the staleness alert
		// would be permanently silenced for exactly the member most likely to be
		// misconfigured. util.TrustedAge treats an impossible age as stale.
		age, aged := util.TrustedAge(time.Now(), newest)
		if !found || !aged || age > backupStaleAfter(interval) {
			s.markBackupStale(ctx, m, newest, found, backupStaleAfter(interval))
			continue
		}
		s.clearBackupStale(ctx, m)
	}
}

// backupStaleAfter is how old a member's newest scheduled backup may be before
// the member counts as unprotected: its own interval, never judged tighter than
// the member's default of a day or looser than the weekly maximum its scheduler
// also caps at, plus the grace for the dump's duration and the scheduler's lag.
// A member backing up more often is thereby only flagged after a missed day, a
// member on a weekly schedule is not flagged six days of every seven, and a
// member reporting an absurd interval is still flagged.
func backupStaleAfter(interval time.Duration) time.Duration {
	return min(max(interval, settings.DefaultBackupInterval), settings.MaxBackupInterval) + memberBackupStaleGrace
}

// memberBackupInterval reads the member's backup_interval setting, parsed with
// the member's own rules. Unset or unparseable reads as the member's default of
// a day, since that is the interval the member's scheduler then runs on. A
// failed read is an error: the member is not judged on a guess.
func (s *Server) memberBackupInterval(ctx context.Context, m *Member, token string) (time.Duration, error) {
	var payload map[string]any
	if err := getMemberJSON(ctx, s.backupClient, maxMemberRespBody, m.URL, memberSettingsPath, token, "member settings", &payload); err != nil {
		return 0, err
	}
	raw, _ := payload["backup_interval"].(string)
	if interval, perr := util.ParseDuration(raw); raw != "" && perr == nil {
		return interval, nil
	}
	return settings.DefaultBackupInterval, nil
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
// carries an empty newest_backup_at. window is the staleness threshold the member
// was judged against, named in the message.
func (s *Server) markBackupStale(ctx context.Context, m *Member, newest time.Time, found bool, window time.Duration) {
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
		Message:  fmt.Sprintf("%s has no database backup from the last %g hours", m.Name, window.Hours()),
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
