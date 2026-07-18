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
		// Initial delay to let the server fully start
		select {
		case <-schedCtx.Done():
			return
		case <-time.After(1 * time.Minute):
		}

		for {
			// Re-read settings each tick for dynamic updates
			enabled := h.settingsRepo.GetBool(schedCtx, "backup_enabled", false)

			sleep := backupSchedulerIdlePoll
			if enabled {
				h.runScheduledBackup(schedCtx)
				sleep = max(h.settingsRepo.GetDuration(schedCtx, "backup_interval", 24*time.Hour), 5*time.Minute)
			}

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

	cmd := h.buildDumpCommand(dumpCtx, pgDumpPath, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(path)
		debuglog.Error("backup: scheduled pg_dump failed", "output", strings.TrimSpace(string(output)), "error", err)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		debuglog.Error("backup: scheduled backup stat failed", "error", err)
		return
	}

	debuglog.Info("backup: scheduled backup created", "filename", filename, "size_bytes", info.Size())
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
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			debuglog.Error("backup: failed to prune", "filename", b.Filename, "error", err)
		} else {
			debuglog.Info("backup: pruned", "filename", b.Filename)
		}
	}
}
