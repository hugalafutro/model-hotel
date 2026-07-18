package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// BackupHandler manages PostgreSQL database backups via pg_dump
// and restores via pg_restore.
type BackupHandler struct {
	databaseURL       string
	backupDir         string
	backupMu          sync.Mutex
	adminMgr          AdminAuthenticator
	settingsRepo      SettingsStore
	sessionMgr        WebAuthnSessionManager // set via SetSessionAuth; nil when WebAuthn not wired (raw admin token still accepted when TOTP off)
	totpEnabled       func() bool            // set via SetSessionAuth; nil -> treated as false (TOTP off) so raw admin token is accepted
	schedulerCancelMu sync.Mutex
	schedulerCancel   context.CancelFunc
}

// NewBackupHandler creates a new BackupHandler.
// backupDir is the directory where backup files are stored (typically DATA_DIR/backups).
func NewBackupHandler(databaseURL, backupDir string, adminMgr AdminAuthenticator, settingsRepo SettingsStore) *BackupHandler {
	absDir, err := filepath.Abs(backupDir)
	if err != nil {
		absDir = backupDir // fallback to original path
	}
	return &BackupHandler{
		databaseURL:  databaseURL,
		backupDir:    absDir,
		adminMgr:     adminMgr,
		settingsRepo: settingsRepo,
	}
}

// SetSessionAuth wires the WebAuthn session manager and TOTP-enabled flag so
// restore (a destructive, second independent auth gate via multipart form
// field) honors 2FA: when TOTP is enabled, a raw admin token in the form field
// is rejected and a session token from /totp/login is required instead. Mirrors
// Handler.AuthMiddleware's gate. Called after NewBackupHandler in Handler.Register.
func (h *BackupHandler) SetSessionAuth(sessionMgr WebAuthnSessionManager, totpEnabled func() bool) {
	h.sessionMgr = sessionMgr
	h.totpEnabled = totpEnabled
}

// Register registers backup routes on the given router.
func (h *BackupHandler) Register(r chi.Router) {
	r.Route("/backups", func(r chi.Router) {
		r.Get("/", h.ListBackups)
		r.Post("/", h.CreateBackup)
		r.Post("/restore", h.RestoreBackup)
		r.Get("/{filename}", h.DownloadBackup)
		r.Delete("/{filename}", h.DeleteBackup)
		r.Post("/prune-preview", h.PrunePreview)
		r.Post("/prune", h.ApplyPrune)
	})
}

// backupEntry represents a backup file in the listing response.
type backupEntry struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
	// Origin records who created the backup: "manual" (an operator clicked
	// "Create backup") or "scheduled" (the GFS rotation scheduler). Derived
	// from the filename marker; only "_auto" files read as scheduled, so files
	// predating origin tracking read as "manual" and are spared from rotation.
	Origin string `json:"origin"`
}

// CreateBackup runs pg_dump and saves the output to a timestamped file. An
// operator-initiated call records origin "manual"; Front Desk passes
// ?origin=frontdesk so the snapshot it takes before an HA config sync is badged
// distinctly (and, like manual backups, spared from GFS rotation). "auto" is
// scheduler-internal and never accepted here.
func (h *BackupHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	origin := "manual"
	if r.URL.Query().Get("origin") == backupOriginFrontDesk {
		origin = backupOriginFrontDesk
	}
	if !h.backupMu.TryLock() {
		respondError(w, "backup already in progress", nil, http.StatusConflict)
		return
	}
	defer h.backupMu.Unlock()

	// Ensure backup directory exists
	if err := os.MkdirAll(h.backupDir, 0o750); err != nil {
		respondError(w, "failed to create backup directory", err, http.StatusInternalServerError)
		return
	}

	// Check that pg_dump is available
	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		respondError(w, "pg_dump not found - install postgresql-client package", err, http.StatusPreconditionFailed)
		return
	}

	filename := generateBackupFilename(origin)
	path := filepath.Join(h.backupDir, filename)

	// Use a dedicated 10-minute timeout so large databases don't get killed
	// by the chi request timeout middleware (~60s).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := h.buildDumpCommand(ctx, pgDumpPath, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Clean up partial file
		_ = os.Remove(path)
		// Log full pg_dump output server-side only (may contain connection details)
		debuglog.Error("backup: pg_dump failed", "output", strings.TrimSpace(string(output)), "error", err)
		respondError(w, "pg_dump failed - check server logs for details", nil, http.StatusInternalServerError)
		return
	}

	// Stat the file for the response
	info, err := os.Stat(path)
	if err != nil {
		respondError(w, fmt.Sprintf("backup created but failed to stat file %q", filename), err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("backup: created", "filename", filename, "size_bytes", info.Size())
	events.Publish(events.Event{
		Type:     "backup.created",
		Severity: "success",
		Source:   "backup",
		Message:  fmt.Sprintf("Database backup created: %s (%s)", filename, util.FormatBytes(info.Size())),
		Metadata: map[string]any{"filename": filename, "size_bytes": info.Size()},
	})

	writeJSONCreated(w, backupEntry{
		Filename:  filename,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime().Format(time.RFC3339),
		Origin:    backupOrigin(filename),
	})
}

// ListBackups returns all backup files sorted by creation time (newest first).
func (h *BackupHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, []backupEntry{})
			return
		}
		respondError(w, "failed to read backup directory", err, http.StatusInternalServerError)
		return
	}

	var backups []backupEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".dump") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupEntry{
			Filename:  entry.Name(),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
			Origin:    backupOrigin(entry.Name()),
		})
	}

	// Sort newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt > backups[j].CreatedAt
	})

	if backups == nil {
		backups = []backupEntry{}
	}
	writeJSON(w, backups)
}

// validateBackupFilename sanitizes the filename and resolves it to an absolute path
// within the backup directory. Returns empty string if validation fails.
func (h *BackupHandler) validateBackupFilename(filename string) string {
	if strings.ContainsAny(filename, "/\\\r\n") || !strings.HasSuffix(filename, ".dump") {
		return ""
	}
	path := filepath.Join(h.backupDir, filename)
	absPath, err := filepath.Abs(path)
	if err != nil || !strings.HasPrefix(absPath, h.backupDir+string(filepath.Separator)) {
		return ""
	}
	return absPath
}

// DownloadBackup serves a backup file for download.
func (h *BackupHandler) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")

	absPath := h.validateBackupFilename(filename)
	if absPath == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}

	debuglog.Info("backup: downloaded", "filename", filename)

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, absPath)
}

// buildDumpCommand creates a pg_dump command with the password stripped from
// the connection URL and passed via PGPASSWORD instead. The caller is
// responsible for running the command and handling errors.
func (h *BackupHandler) buildDumpCommand(ctx context.Context, pgDumpPath, filePath string) *exec.Cmd {
	connURL := h.databaseURL
	var envPassword string
	if u, err := url.Parse(h.databaseURL); err == nil && u.User != nil {
		if pass, ok := u.User.Password(); ok && pass != "" {
			envPassword = pass
			u.User = url.User(u.User.Username())
			connURL = u.String()
		}
	}
	//nolint:gosec // pgDumpPath is a configured binary path, not arbitrary user input
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+filePath,
		connURL,
	)
	if envPassword != "" {
		cmd.Env = append(os.Environ(), "PGPASSWORD="+envPassword)
	}
	return cmd
}

// generateBackupFilename creates a timestamped backup filename carrying its
// origin ("manual" or "auto") as a trailing segment. parseBackupTimestamp only
// reads the date/time segments, so the extra suffix does not affect parsing.
func generateBackupFilename(origin string) string {
	now := time.Now()
	return fmt.Sprintf(
		"backup_%s_%04d_%s.dump",
		now.Format("20060102_150405"),
		now.Nanosecond()/100000,
		origin,
	)
}

// backupOriginFrontDesk marks a backup taken by Front Desk before an HA config
// sync. It is both the ?origin= value CreateBackup accepts and the value
// backupOrigin reports for "_frontdesk" files.
const backupOriginFrontDesk = "frontdesk"

// backupOrigin reports who created a backup. The scheduler's files carry "_auto"
// and read as "scheduled"; Front Desk's pre-sync snapshots carry "_frontdesk" and
// read as "frontdesk"; everything else, manual "_manual" files and any predating
// origin tracking, reads as "manual". Erring toward manual keeps GFS rotation
// from pruning backups it cannot prove it created, which is the safe default for
// legacy files; like manual, "frontdesk" files are never rotation targets.
func backupOrigin(filename string) string {
	stem := strings.TrimSuffix(filename, ".dump")
	switch {
	case strings.HasSuffix(stem, "_auto"):
		return "scheduled"
	case strings.HasSuffix(stem, "_"+backupOriginFrontDesk):
		return backupOriginFrontDesk
	default:
		return "manual"
	}
}

// scheduledBackups drops manual (and legacy, which read as manual) backups so
// GFS rotation only ever classifies and prunes scheduler-written files. Manual
// backups were created deliberately and must survive rotation untouched.
func scheduledBackups(backups []backupEntry) []backupEntry {
	out := make([]backupEntry, 0, len(backups))
	for _, b := range backups {
		if backupOrigin(b.Filename) == "scheduled" {
			out = append(out, b)
		}
	}
	return out
}

// DeleteBackup removes a backup file.
func (h *BackupHandler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	h.backupMu.Lock()
	defer h.backupMu.Unlock()

	filename := chi.URLParam(r, "filename")

	absPath := h.validateBackupFilename(filename)
	if absPath == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	if err := os.Remove(absPath); os.IsNotExist(err) {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	} else if err != nil {
		respondError(w, fmt.Sprintf("failed to delete backup %q", filename), err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("backup: deleted", "filename", filename)
	events.Publish(events.Event{
		Type:     "backup.deleted",
		Severity: "info",
		Source:   "backup",
		Message:  fmt.Sprintf("Backup deleted: %s", filename),
		Metadata: map[string]any{"filename": filename},
	})

	w.WriteHeader(http.StatusNoContent)
}
