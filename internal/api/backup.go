package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	masterKey         string                 // set via SetSigningKey; empty disables backup signing and verification
	schedulerCancelMu sync.Mutex
	schedulerCancel   context.CancelFunc
	// schedulerStopped is the running scheduler's join channel, so a second
	// StartScheduler call is handed the goroutine that exists rather than a
	// closed channel that would tell it there is nothing to wait for.
	schedulerStopped chan struct{}
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

// SetSigningKey wires the master key used to derive the backup signing key, so
// dumps are signed on creation and verified on the way back out. Left empty
// (no MASTER_KEY configured) signing is skipped entirely and backups behave as
// they did before signing existed.
func (h *BackupHandler) SetSigningKey(masterKey string) {
	h.masterKey = masterKey
}

// signFinishedDump signs a completed dump, reporting whether a signature now
// exists. A signing failure never discards the dump: an unsigned backup is far
// better than no backup, so the failure is logged and surfaced instead.
func (h *BackupHandler) signFinishedDump(path, filename string) bool {
	if h.masterKey == "" {
		return false
	}
	if err := signBackupFile(path, h.masterKey); err != nil {
		debuglog.Error("backup: failed to sign", "filename", filename, "error", err)
		events.Publish(events.Event{
			Type:     "backup.unsigned",
			Severity: "warning",
			Source:   "backup",
			Message:  fmt.Sprintf("Backup %s was created but could not be signed; it cannot be integrity-checked later", filename),
			Metadata: map[string]any{"filename": filename},
		})
		return false
	}
	return true
}

// Register registers backup routes on the given router.
func (h *BackupHandler) Register(r chi.Router) {
	r.Route("/backups", func(r chi.Router) {
		r.Get("/", h.ListBackups)
		r.Post("/", h.CreateBackup)
		r.Post("/restore", h.RestoreBackup)
		r.Get("/{filename}", h.DownloadBackup)
		r.Get("/{filename}/signature", h.BackupSignature)
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
	// Signed reports whether a signature sidecar exists for this backup, not
	// whether it verifies: listing only stats the sidecar, because hashing every
	// dump on every dashboard load would read the whole backup directory. The
	// signature is actually checked on download, where a single file is read
	// anyway and a mismatch can stop the transfer.
	Signed bool `json:"signed"`
	// modTime is CreatedAt before formatting, for the callers that compare
	// or do arithmetic on it (the listing's order, the scheduler's anchor).
	modTime time.Time
}

// hasSignature reports whether a usable signature sidecar exists beside a dump.
// Cheap (a stat), unlike verifying it. A directory in the sidecar's place is not
// a signature: counting it would report a backup as signed while every download
// of it failed.
func hasSignature(absPath string) bool {
	info, err := os.Stat(absPath + backupSignatureExt)
	return err == nil && info.Mode().IsRegular()
}

// CreateBackup runs pg_dump and saves the output to a timestamped file. An
// operator-initiated call records origin "manual"; a fleet caller may pass
// ?origin=frontdesk so its snapshot is badged distinctly (and, like manual
// backups, spared from GFS rotation). "auto" is scheduler-internal and never
// accepted here.
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

	// A dedicated budget, detached from the chi request timeout (~60s). The
	// request itself still waits for the dump, which is why this path takes
	// the cheaper compression level: see buildDumpCommand.
	entry, err := h.createDump(context.Background(), origin, requestDumpCompression, "Database backup created")
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errPgDumpMissing) {
			status = http.StatusPreconditionFailed
		}
		// createDump has already logged the underlying cause with the detail
		// (pg_dump output, connection URLs) that must not leave the server, so
		// this writes the client-safe message without respondError's second
		// log line for the 500s.
		http.Error(w, err.Error(), status)
		return
	}

	writeJSONCreated(w, entry)
}

// errPgDumpMissing marks the one createDump failure the HTTP path answers with
// 412 rather than 500: the postgresql-client package is not installed.
var errPgDumpMissing = errors.New("pg_dump not found - install postgresql-client package")

// createDump writes one pg_dump into the backup directory under its own
// budget, signs the finished file, and publishes backup.created with
// eventPrefix naming the origin in the dashboard message. Every failure is
// logged here with its full detail and returned with a message safe to hand a
// client.
func (h *BackupHandler) createDump(ctx context.Context, origin, compression, eventPrefix string) (backupEntry, error) {
	if err := os.MkdirAll(h.backupDir, 0o750); err != nil {
		debuglog.Error("backup: mkdir failed", "origin", origin, "error", err)
		return backupEntry{}, errors.New("failed to create backup directory")
	}

	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		debuglog.Error("backup: pg_dump not found", "origin", origin, "error", err)
		return backupEntry{}, errPgDumpMissing
	}

	filename := generateBackupFilename(origin)
	path := filepath.Join(h.backupDir, filename)

	dumpCtx, cancel := context.WithTimeout(ctx, backupDumpBudget)
	defer cancel()

	if output, err := h.runDump(dumpCtx, pgDumpPath, path, compression); err != nil {
		// The full pg_dump output stays server-side: it may carry connection details.
		debuglog.Error("backup: pg_dump failed", "origin", origin, "output", output, "error", err)
		return backupEntry{}, errors.New("pg_dump failed - check server logs for details")
	}

	info, err := os.Stat(path)
	if err != nil {
		debuglog.Error("backup: stat failed", "filename", filename, "error", err)
		return backupEntry{}, fmt.Errorf("backup created but failed to stat file %q", filename)
	}

	signed := h.signFinishedDump(path, filename)

	debuglog.Info("backup: created", "filename", filename, "size_bytes", info.Size(), "signed", signed, "origin", origin)
	events.Publish(events.Event{
		Type:     "backup.created",
		Severity: "success",
		Source:   "backup",
		Message:  fmt.Sprintf("%s: %s (%s)", eventPrefix, filename, util.FormatBytes(info.Size())),
		Metadata: map[string]any{"filename": filename, "size_bytes": info.Size()},
	})

	return backupEntry{
		Filename:  filename,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime().Format(time.RFC3339),
		Origin:    backupOrigin(filename),
		Signed:    signed,
	}, nil
}

// ListBackups returns all backup files sorted by creation time (newest first).
func (h *BackupHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := h.listBackupFiles()
	if err != nil {
		respondError(w, "failed to read backup directory", err, http.StatusInternalServerError)
		return
	}
	// The signature stat is the listing's alone: the rotation and the
	// scheduler read the same files and never look at the sidecars.
	for i := range backups {
		backups[i].Signed = hasSignature(filepath.Join(h.backupDir, backups[i].Filename))
	}
	writeJSON(w, backups)
}

// validateBackupFilename sanitizes the filename and resolves it to an absolute path
// within the backup directory. Returns empty string if validation fails.
func (h *BackupHandler) validateBackupFilename(filename string) string {
	if strings.ContainsAny(filename, "/\\\r\n\x00") || !strings.HasSuffix(filename, ".dump") {
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
//
// Opens the file up front and serves from that handle rather than stat-ing the
// path and letting http.ServeFile reopen it. A stat-then-open pair races the
// delete and retention-prune paths: the file can vanish between the two checks
// and the client gets a truncated or failed transfer. Holding an open
// descriptor removes the window - an unlink during the transfer leaves this
// handle readable until it is closed. Deliberately does NOT take backupMu:
// downloads are long-lived, and the create/restore/prune paths TryLock and give
// up when busy, so a slow client would silently cancel scheduled backups for
// the length of its transfer.
func (h *BackupHandler) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	filename, absPath, ok := h.backupPathParam(w, r)
	if !ok {
		return
	}

	//nolint:gosec // G304: absPath is validated above - a bare .dump basename resolved under backupDir
	f, err := os.Open(absPath)
	if os.IsNotExist(err) {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	} else if err != nil {
		respondError(w, fmt.Sprintf("failed to open backup %q", filename), err, http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	// ServeContent needs a size and modtime for Content-Length and conditional
	// requests; take them from the open handle so they describe the bytes about
	// to be sent rather than whatever the path points at now.
	info, err := f.Stat()
	if err != nil {
		respondError(w, fmt.Sprintf("failed to stat backup %q", filename), err, http.StatusInternalServerError)
		return
	}

	// Download is where a backup leaves this server on its way to a restore, so
	// it is where tampering in the backup directory has to be caught. A dump
	// whose signature does not match is not served at all: handing it over and
	// hoping the operator notices later is how a planted dump gets restored.
	// An unsigned dump still goes out (backups predating signing, and dumps this
	// instance never signed, must stay usable); the listing marks those.
	//
	// Verified through the handle already open above, not by re-reading the
	// path: checking one inode and serving another is a race an attacker with
	// write access to this directory wins by swapping the file after the open.
	status, verr := verifyBackupHandle(f, absPath, h.masterKey)
	if verr != nil {
		respondError(w, fmt.Sprintf("failed to verify backup %q", filename), verr, http.StatusInternalServerError)
		return
	}
	if status == backupSigInvalid {
		debuglog.Error("backup: refused to serve, signature mismatch", "filename", filename)
		events.Publish(events.Event{
			Type:     "backup.integrity_failed",
			Severity: "error",
			Source:   "backup",
			Message:  fmt.Sprintf("Backup %s failed its integrity check and was not served: its contents changed after it was signed", filename),
			Metadata: map[string]any{"filename": filename},
		})
		respondError(w, fmt.Sprintf("backup %q failed its integrity check: contents changed after signing", filename), nil, http.StatusConflict)
		return
	}

	debuglog.Info("backup: downloaded", "filename", filename, "signature", status == backupSigValid)

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

// backupSignatureResponse is the body of GET /backups/{filename}/signature.
type backupSignatureResponse struct {
	// Signature is the dump's sidecar contents, verbatim: the value the restore
	// endpoint expects in its "signature" form field.
	Signature string `json:"signature"`
}

// BackupSignature serves a backup's signature sidecar so the operator can carry
// it to a restore without shell access to the backup directory. The sidecar is
// the only proof of the dump's integrity that survives the round trip through a
// download and a re-upload, and the restore form has nowhere else to get it.
// The signature is not verified here: this hands over what is on disk, and the
// restore checks it against the uploaded bytes. Unsigned backups get a 404,
// which is what the listing's "signed: false" already promises.
func (h *BackupHandler) BackupSignature(w http.ResponseWriter, r *http.Request) {
	filename, absPath, ok := h.backupPathParam(w, r)
	if !ok {
		return
	}
	//nolint:gosec // G703: absPath comes from backupPathParam, which resolves a bare .dump basename under backupDir
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	} else if err != nil {
		respondError(w, fmt.Sprintf("failed to stat backup %q", filename), err, http.StatusInternalServerError)
		return
	}

	contents, found, err := readSignatureSidecar(absPath)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to read signature for %q", filename), err, http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "backup is not signed", http.StatusNotFound)
		return
	}
	// A sidecar that is not a signature at all is a server-side problem
	// (corruption, or something else wrote the file); handing it over would
	// have the dashboard blame the operator's paste for it.
	if _, ok := decodeSignature(string(contents)); !ok {
		respondError(w, fmt.Sprintf("signature sidecar for %q is not a valid signature", filename), nil, http.StatusInternalServerError)
		return
	}
	writeJSON(w, backupSignatureResponse{Signature: strings.TrimSpace(string(contents))})
}

// backupPartialSuffix marks a dump pg_dump is still writing. The name does
// not end in ".dump", so the listing, rotation, restore and the scheduler's
// interval anchor all ignore it until it is renamed onto its final name.
const backupPartialSuffix = ".partial"

// runDump writes a dump to path, via a partial name renamed onto path only
// once pg_dump completed, so a process killed mid-dump leaves nothing that
// reads as a backup. A failed dump leaves nothing behind and returns pg_dump's
// trimmed output with the error for the caller to log; a failed rename keeps
// the completed partial, the only good copy, and names it in the error.
func (h *BackupHandler) runDump(ctx context.Context, pgDumpPath, path, compression string) (string, error) {
	partial := path + backupPartialSuffix
	output, err := h.buildDumpCommand(ctx, pgDumpPath, partial, compression).CombinedOutput()
	if err != nil {
		_ = os.Remove(partial)
		return strings.TrimSpace(string(output)), err
	}
	if err := os.Rename(partial, path); err != nil {
		return "", fmt.Errorf("rename completed dump %s into place: %w", filepath.Base(partial), err)
	}
	return "", nil
}

// Compression levels for the custom-format dump, one per caller. The format
// is already compressed (zlib level 6 by default), so wrapping the file in
// gzip afterwards gains about one percent; only zstd still buys anything.
// Measured on a 72 MB database: zlib 6 gave 5.1 MB in 1 s, zstd 12 gave
// 4.5 MB in 1 s, zstd 19 gave 4.0 MB in 13 s.
//
// The scheduled dump runs in the background under backupDumpBudget, so it
// takes the top level: those are the backups that accumulate. A dump taken on
// request (the dashboard button, a Front Desk snapshot) keeps the client
// waiting and sits behind whatever proxy timeout fronts the dashboard, so it
// takes the level that costs no more time than today. Either way the file
// stays a custom-format dump that pg_restore 16 and later read unchanged, so
// the restore and signature paths know nothing about it.
const (
	scheduledDumpCompression = "zstd:19"
	requestDumpCompression   = "zstd:12"
)

// buildDumpCommand creates a pg_dump command with the password stripped from
// the connection URL and passed via PGPASSWORD instead. The caller is
// responsible for running the command and handling errors.
func (h *BackupHandler) buildDumpCommand(ctx context.Context, pgDumpPath, filePath, compression string) *exec.Cmd {
	connURL, env := pgCommandEnv(h.databaseURL)
	//nolint:gosec // pgDumpPath is a configured binary path, not arbitrary user input
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--compress="+compression,
		"--no-password",
		"--file="+filePath,
		connURL,
	)
	cmd.Env = env
	return cmd
}

// backupDumpBudget bounds one pg_dump run, manual or scheduled: at the
// measured 13 s per 72 MB of raw data at zstd 19, thirty minutes covers a
// database of roughly ten gigabytes.
const backupDumpBudget = 30 * time.Minute

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

// backupOriginFrontDesk marks a backup a fleet caller asked this member to take:
// both the ?origin= value CreateBackup accepts and the value backupOrigin reports
// for "_frontdesk" files. Front Desk's sync path does not call it, but an older
// Front Desk or an operator tool may, and existing files must keep reading back.
const backupOriginFrontDesk = "frontdesk"

// backupOrigin reports who created a backup. The scheduler's files carry "_auto"
// and read as "scheduled"; fleet-requested snapshots carry "_frontdesk" and
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

// DeleteBackup removes a backup file. Like every other holder of backupMu it
// refuses rather than queues behind a running dump, restore or prune: a
// scheduled zstd 19 dump can hold the lock for minutes, longer than the
// request timeout a queued delete would hit.
func (h *BackupHandler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupMu.TryLock() {
		respondError(w, "a backup operation is in progress", nil, http.StatusConflict)
		return
	}
	defer h.backupMu.Unlock()

	filename, absPath, ok := h.backupPathParam(w, r)
	if !ok {
		return
	}

	if err := removeBackupWithSignature(absPath); os.IsNotExist(err) {
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

// pgCommandEnv splits a connection URL for a pg_dump / pg_restore invocation:
// the password moves out of the URL (where it would show up in the process
// list) into PGPASSWORD. env is nil when the URL carries no password, leaving
// the command on the parent environment.
func pgCommandEnv(databaseURL string) (connURL string, env []string) {
	connURL = databaseURL
	u, err := url.Parse(databaseURL)
	if err != nil || u.User == nil {
		return connURL, nil
	}
	pass, ok := u.User.Password()
	if !ok || pass == "" {
		return connURL, nil
	}
	u.User = url.User(u.User.Username())
	return u.String(), append(os.Environ(), "PGPASSWORD="+pass)
}

// backupPathParam reads the {filename} route parameter and resolves it under
// the backup directory, writing a 400 and returning ok=false when the name
// does not survive validation.
func (h *BackupHandler) backupPathParam(w http.ResponseWriter, r *http.Request) (filename, absPath string, ok bool) {
	filename = chi.URLParam(r, "filename")
	absPath = h.validateBackupFilename(filename)
	if absPath == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return "", "", false
	}
	return filename, absPath, true
}
