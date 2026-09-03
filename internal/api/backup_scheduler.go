package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ── Scheduler ────────────────────────────────────────────────────────

// backupSchedulerIdlePoll is how often the scheduler re-checks the
// backup_enabled setting while disabled, so that toggling backups on at
// runtime takes effect promptly instead of waiting a full backup_interval.
const backupSchedulerIdlePoll = 1 * time.Minute

// StartScheduler starts the periodic backup scheduler goroutine.
//
// The goroutine always runs (regardless of the current backup_enabled
// value) and re-reads backup_enabled and backup_interval from the settings
// store on every tick. This lets the toggle take effect at runtime without
// a server restart: when disabled it polls on a short idle interval; when
// enabled it creates a backup and applies the rotation scheme, then sleeps
// for backup_interval.
func (h *BackupHandler) StartScheduler(ctx context.Context) {
	if h.settingsRepo == nil {
		return
	}
	// Guard against double-launch leaking the previous goroutine.
	h.schedulerCancelMu.Lock()
	if h.schedulerCancel != nil {
		h.schedulerCancelMu.Unlock()
		return
	}

	schedCtx, cancel := context.WithCancel(ctx)
	h.schedulerCancel = cancel
	h.schedulerCancelMu.Unlock()
	debuglog.Info("backup: scheduler started")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				debuglog.Error("backup: scheduler panic recovered", "panic", r)
				// Reset so StartScheduler can restart the scheduler.
				h.schedulerCancelMu.Lock()
				h.schedulerCancel = nil
				h.schedulerCancelMu.Unlock()
			}
		}()
		h.removeStalePartials()
		// Initial delay to let the server fully start
		select {
		case <-schedCtx.Done():
			return
		case <-time.After(1 * time.Minute):
		}

		for {
			sleep := h.schedulerTick(schedCtx)

			select {
			case <-schedCtx.Done():
				debuglog.Info("backup: scheduler stopped")
				return
			case <-time.After(sleep):
			}
		}
	}()
}

// StopScheduler stops the periodic backup scheduler.
func (h *BackupHandler) StopScheduler() {
	h.schedulerCancelMu.Lock()
	defer h.schedulerCancelMu.Unlock()
	if h.schedulerCancel != nil {
		h.schedulerCancel()
		h.schedulerCancel = nil
	}
}

// schedulerTick runs one scheduler cycle and returns how long to sleep before
// the next. Settings are re-read on every tick so a change takes effect
// without a restart. The interval runs from the last scheduled backup on
// disk, not from process start: a restart inside the interval sleeps out the
// remainder instead of paying a fresh pg_dump every deploy.
func (h *BackupHandler) schedulerTick(ctx context.Context) time.Duration {
	if !h.settingsRepo.GetBool(ctx, "backup_enabled", false) {
		return backupSchedulerIdlePoll
	}
	interval := max(h.settingsRepo.GetDuration(ctx, "backup_interval", 24*time.Hour), 5*time.Minute)
	if wait := h.scheduledBackupWait(interval, time.Now()); wait > 0 {
		debuglog.Info("backup: last scheduled backup is recent, waiting", "wait", wait.Round(time.Second).String())
		return wait
	}
	h.runScheduledBackup(ctx)
	return interval
}

// scheduledBackupWait returns how long the scheduler still has to wait before
// the next scheduled backup is due, judged by the newest scheduler-written
// dump in the backup directory, or zero when one is due now. Manual and Front
// Desk backups do not count: they are the operator's, and a scheduled backup
// is due on its own clock regardless. An unreadable directory reads as due.
func (h *BackupHandler) scheduledBackupWait(interval time.Duration, now time.Time) time.Duration {
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		return 0
	}
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dump") || backupOrigin(e.Name()) != "scheduled" {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return 0
	}
	// A dump stamped in the future (a clock step, a copied volume) is no
	// anchor at all: trusting it would park the scheduler for as long as the
	// stamp is ahead, so it counts as due instead.
	if newest.After(now) {
		debuglog.Warn("backup: newest scheduled backup is stamped in the future, treating a backup as due", "stamp", newest.Format(time.RFC3339))
		return 0
	}
	return max(newest.Add(interval).Sub(now), 0)
}

// removeStalePartials deletes dumps a previous process was killed in the
// middle of writing. They are invisible to the listing, so nothing else would
// ever reclaim them. It runs under the backup lock so a scheduler restarted
// beside a manual backup in progress cannot sweep that backup's partial; when
// the lock is busy the sweep simply waits for the next start.
func (h *BackupHandler) removeStalePartials() {
	if !h.backupMu.TryLock() {
		return
	}
	defer h.backupMu.Unlock()
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), backupPartialSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(h.backupDir, e.Name())); err == nil {
			debuglog.Info("backup: removed a partial dump from an interrupted run", "filename", e.Name())
		}
	}
}

// runScheduledBackup creates a backup and applies the rotation scheme.
// It uses the same pg_dump logic as CreateBackup but without HTTP request/response.
func (h *BackupHandler) runScheduledBackup(ctx context.Context) {
	if !h.backupMu.TryLock() {
		debuglog.Warn("backup: scheduler skip, operation in progress")
		return
	}
	defer h.backupMu.Unlock()

	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		debuglog.Error("backup: scheduled backup failed, pg_dump not found", "error", err)
		return
	}

	if err := os.MkdirAll(h.backupDir, 0o750); err != nil {
		debuglog.Error("backup: scheduled backup failed, mkdir", "error", err)
		return
	}

	filename := generateBackupFilename("auto")
	path := filepath.Join(h.backupDir, filename)

	dumpCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	if output, err := h.runDump(dumpCtx, pgDumpPath, path); err != nil {
		debuglog.Error("backup: scheduled pg_dump failed", "output", output, "error", err)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		debuglog.Error("backup: scheduled backup stat failed", "error", err)
		return
	}

	signed := h.signFinishedDump(path, filename)

	debuglog.Info("backup: scheduled backup created", "filename", filename, "size_bytes", info.Size(), "signed", signed)
	events.Publish(events.Event{
		Type:     "backup.created",
		Severity: "success",
		Source:   "backup",
		Message:  fmt.Sprintf("Scheduled backup created: %s (%s)", filename, util.FormatBytes(info.Size())),
		Metadata: map[string]any{"filename": filename, "size_bytes": info.Size()},
	})

	// Apply rotation
	backups, err := h.listBackupFiles()
	if err != nil {
		debuglog.Error("backup: failed to list backups for rotation", "error", err)
		return
	}
	son, father, grandfather := h.getRetentionSettings(ctx)
	classification := classifyBackups(scheduledBackups(backups), son, father, grandfather, time.Now())

	for _, b := range classification.Prune {
		absPath := h.validateBackupFilename(b.Filename)
		if absPath == "" {
			continue
		}
		if err := removeBackupWithSignature(absPath); err != nil && !os.IsNotExist(err) {
			debuglog.Error("backup: failed to prune", "filename", b.Filename, "error", err)
		} else {
			debuglog.Info("backup: pruned", "filename", b.Filename)
		}
	}
}
