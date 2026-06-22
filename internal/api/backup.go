package api

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
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

// CreateBackup runs pg_dump and saves the output to a timestamped file.
func (h *BackupHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
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

	filename := generateBackupFilename("manual")
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
		Metadata: map[string]interface{}{"filename": filename, "size_bytes": info.Size()},
	})

	writeJSONCreated(w, backupEntry{
		Filename:  filename,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime().Format(time.RFC3339),
		Origin:    "manual",
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

// backupOrigin reports who created a backup. Only files the scheduler wrote
// carry the "_auto" marker and count as "scheduled"; everything else, manual
// "_manual" files and any predating origin tracking, reads as "manual". Erring
// toward manual keeps GFS rotation from pruning backups it cannot prove it
// created, which is the safe default for legacy files.
func backupOrigin(filename string) string {
	if strings.HasSuffix(strings.TrimSuffix(filename, ".dump"), "_auto") {
		return "scheduled"
	}
	return "manual"
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
		Metadata: map[string]interface{}{"filename": filename},
	})

	w.WriteHeader(http.StatusNoContent)
}

// restoreResult is returned after a successful restore.
type restoreResult struct {
	MigrationCount int `json:"migration_count"`
	KnownCount     int `json:"known_count"`
}

// tocEntry represents a parsed entry from pg_restore --list output.
type tocEntry struct {
	EntryNumber int
	ObjectType  string
	Schema      string
	Name        string
}

// dangerousObjectTypes are PostgreSQL object types that should not appear
// in a legitimate model-hotel backup. Their presence suggests a tampered
// or non-application dump.
var dangerousObjectTypes = map[string]bool{
	"FUNCTION":          true,
	"AGGREGATE":         true,
	"TRIGGER":           true,
	"EXTENSION":         true,
	"PROCEDURE":         true,
	"OPERATOR":          true,
	"CAST":              true,
	"COLLATION":         true,
	"CONVERSION":        true,
	"DOMAIN":            true,
	"EVENT TRIGGER":     true,
	"FOREIGN DATA":      true,
	"FOREIGN TABLE":     true,
	"MATERIALIZED VIEW": true,
	"SERVER":            true,
	"TYPE":              true,
}

// twoWordPrefixes maps first words that combine with a second word to form
// a two-word object type (e.g. TABLE+DATA=TABLE DATA, FK+CONSTRAINT=FK CONSTRAINT).
var twoWordPrefixes = map[string]map[string]bool{
	"TABLE":        {"DATA": true},
	"FK":           {"CONSTRAINT": true},
	"SEQUENCE":     {"SET": true, "OWNED": true},
	"DEFAULT":      {"ACL": true, "PRIVILEGES": true},
	"EVENT":        {"TRIGGER": true},
	"FOREIGN":      {"TABLE": true, "DATA": true, "SERVER": true},
	"MATERIALIZED": {"VIEW": true},
}

// parseTOC parses the output of pg_restore --list into structured entries.
// Only extracts entry number, object type, schema, and name. The name
// extraction varies by type: for TABLE/TABLE DATA it's the table name,
// for CONSTRAINT/FK CONSTRAINT it's the constraint name (last before owner).
// For other types with 4+ after-type fields (e.g. INDEX), the Name field
// may be incorrect; only TABLE, TABLE DATA, and CONSTRAINT names are
// considered reliable for lookups.
func parseTOC(listOutput string) []tocEntry {
	var entries []tocEntry
	scanner := bufio.NewScanner(strings.NewReader(listOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, ";", 2)
		if len(parts) != 2 {
			continue
		}
		var entryNum int
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &entryNum); err != nil {
			continue
		}
		right := strings.TrimSpace(parts[1])
		fields := strings.Fields(right)
		if len(fields) < 3 {
			continue
		}
		objType := fields[2]
		typeWordCount := 1
		if qualifiers, ok := twoWordPrefixes[fields[2]]; ok && len(fields) > 3 && qualifiers[fields[3]] {
			objType = fields[2] + " " + fields[3]
			typeWordCount = 2
		}

		// After type: schema [table_name] object_name [owner]
		// For TABLE/TABLE DATA: schema name owner
		// For CONSTRAINT/FK CONSTRAINT: schema table_name constraint_name owner
		// For INDEX: schema index_name owner (or schema table_name index_name for some)
		// Name extraction varies by type and field count:
		// TABLE/TABLE DATA/SEQUENCE: schema name [owner] -> name is afterType[1]
		// CONSTRAINT/FK CONSTRAINT: schema table_name constraint_name [owner]
		//   -> with owner (4+): name is afterType[len-2]
		//   -> without owner (3): name is afterType[2]
		schema := ""
		name := ""
		afterType := fields[2+typeWordCount:]
		switch {
		case len(afterType) >= 4:
			// schema table_name object_name owner (constraint types with owner)
			schema = afterType[0]
			if objType == "FK CONSTRAINT" || objType == "CONSTRAINT" {
				name = afterType[len(afterType)-2]
			} else {
				name = afterType[1]
			}
		case len(afterType) == 3:
			// schema table_name object_name (constraint types without owner)
			schema = afterType[0]
			if objType == "FK CONSTRAINT" || objType == "CONSTRAINT" {
				name = afterType[2]
			} else {
				name = afterType[1]
			}
		case len(afterType) == 2:
			// schema name (no owner or name is last)
			schema = afterType[0]
			name = afterType[1]
		case len(afterType) == 1:
			name = afterType[0]
		}

		entries = append(entries, tocEntry{
			EntryNumber: entryNum,
			ObjectType:  objType,
			Schema:      schema,
			Name:        name,
		})
	}
	return entries
}

// checkDangerousObjects scans TOC entries for object types that should
// not appear in a legitimate application backup.
func checkDangerousObjects(entries []tocEntry) []string {
	var found []string
	for _, e := range entries {
		if dangerousObjectTypes[e.ObjectType] {
			found = append(found, fmt.Sprintf("%s %s.%s", e.ObjectType, e.Schema, e.Name))
		}
	}
	return found
}

// findSchemaMigrationsEntry returns the TOC entry number for the
// schema_migrations TABLE DATA entry, or 0 if not found.
func findSchemaMigrationsEntry(entries []tocEntry) int {
	for _, e := range entries {
		if e.ObjectType == "TABLE DATA" && e.Name == "schema_migrations" {
			return e.EntryNumber
		}
	}
	return 0
}

// extractMigrationNames runs pg_restore with a filtered list to extract
// only the schema_migrations table data, then parses the COPY block
// to find the migration names stored in the dump.
func extractMigrationNames(dumpPath string, schemaMigrationsEntry int) ([]string, error) {
	filterContent := fmt.Sprintf("%d;\n", schemaMigrationsEntry)
	filterFile, err := os.CreateTemp("", "restore-filter-*.txt")
	if err != nil {
		return nil, fmt.Errorf("failed to create filter file: %w", err)
	}
	//nolint:errcheck // cleanup: filter file removed after pg_restore
	defer os.Remove(filterFile.Name())

	if _, err := filterFile.WriteString(filterContent); err != nil {
		filterFile.Close() //nolint:errcheck,gosec // error path: closing after write failure
		return nil, fmt.Errorf("failed to write filter file: %w", err)
	}
	if err := filterFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close filter file: %w", err)
	}

	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		return nil, fmt.Errorf("pg_restore not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	//nolint:gosec // pgRestorePath is a configured binary path
	cmd := exec.CommandContext(ctx, pgRestorePath,
		"-L", filterFile.Name(),
		"-f", "-",
		dumpPath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pg_restore filter failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	return parseMigrationNamesFromSQL(stdout.String()), nil
}

// parseMigrationNamesFromSQL extracts migration names from the SQL output
// of a filtered pg_restore. Looks for the COPY public.schema_migrations
// block and extracts the name column (second field, tab-separated).
func parseMigrationNamesFromSQL(sqlOutput string) []string {
	var names []string
	inCopy := false
	scanner := bufio.NewScanner(strings.NewReader(sqlOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COPY public.schema_migrations") {
			inCopy = true
			continue
		}
		if inCopy {
			if line == "\\." {
				break
			}
			fields := strings.Split(line, "\t")
			if len(fields) >= 2 {
				names = append(names, fields[1])
			}
		}
	}
	return names
}

// compareMigrations checks the dump's migration names against the app's
// known migrations. Returns unknown migrations (present in dump but not
// in the app), indicating the dump is from a newer version.
func compareMigrations(dumpMigrations []string) []string {
	knownSet := make(map[string]struct{})
	for _, m := range db.KnownMigrations() {
		knownSet[m] = struct{}{}
	}

	var unknown []string
	for _, m := range dumpMigrations {
		if _, ok := knownSet[m]; !ok {
			unknown = append(unknown, m)
		}
	}
	return unknown
}

// RestoreBackup validates and restores a database backup from an uploaded .dump file.
// The request must include the admin token in the multipart form for explicit confirmation.
// Pre-restore validation:
//  1. pg_restore --list validates the dump format and checks for dangerous objects
//  2. Extracts schema_migrations from the dump and compares against known migrations
//  3. Rejects dumps from newer versions (unknown migrations)
//
// After successful restore, the process exits so Docker can restart it with fresh caches.
func (h *BackupHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	if !h.backupMu.TryLock() {
		respondError(w, "backup or restore already in progress", nil, http.StatusConflict)
		return
	}
	defer h.backupMu.Unlock()

	tmpPath, ok := h.saveUploadedDump(w, r)
	if !ok {
		return
	}
	//nolint:errcheck // cleanup: temp file removed after processing
	defer os.Remove(tmpPath)

	pgRestorePath, dumpMigrations, ok := validateRestoreDump(w, tmpPath)
	if !ok {
		return
	}

	if !h.runPgRestore(w, pgRestorePath, tmpPath) {
		return
	}

	debuglog.Info("backup: restored", "migrations_in_dump", len(dumpMigrations))
	events.Publish(events.Event{
		Type:     "backup.restored",
		Severity: "success",
		Source:   "backup",
		Message:  "Database restored successfully. Restarting...",
		Metadata: map[string]interface{}{"migration_count": len(dumpMigrations)},
	})

	// Respond before exiting so the client gets the success response
	result := restoreResult{
		MigrationCount: len(dumpMigrations),
		KnownCount:     len(db.KnownMigrations()),
	}
	writeJSON(w, result)

	// Exit the process so Docker restarts it with fresh caches and
	// re-runs any missing migrations against the restored database.
	go func() {
		time.Sleep(500 * time.Millisecond) // give the HTTP response time to flush
		os.Remove(tmpPath)                 //nolint:errcheck,gosec // best-effort cleanup before exit
		os.Exit(0)
	}()
}

// saveUploadedDump validates the multipart upload (size limit, admin token,
// dump file) and streams it to a temp file in the backup dir, returning the temp
// path for the caller to clean up. It writes the appropriate HTTP error and
// returns ok=false on any failure (removing the temp file if it was created).
func (h *BackupHandler) saveUploadedDump(w http.ResponseWriter, r *http.Request) (tmpPath string, ok bool) {
	// Limit upload size (100MB)
	r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024)

	// Parse multipart form (32MB max in-memory)
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // bounded by MaxBytesReader above
		respondBadRequest(w, "failed to parse multipart form", err)
		return "", false
	}

	// Validate admin token from form field. When TOTP 2FA is enabled, the raw
	// admin token is a first factor only and must not unlock this destructive
	// op; a session token from /totp/login is required. Mirrors AuthMiddleware's
	// gate so the form-field guard cannot be used to bypass 2FA.
	adminToken := r.FormValue("admin_token")
	if adminToken == "" {
		debuglog.Warn("auth: backup restore with missing admin token", "remote_addr", r.RemoteAddr)
		respondError(w, "invalid admin token", nil, http.StatusUnauthorized)
		return "", false
	}
	authed := false
	totpOn := h.totpEnabled != nil && h.totpEnabled()
	if !totpOn && h.adminMgr.Validate(adminToken) {
		authed = true
	} else if h.sessionMgr != nil && h.sessionMgr.Validate(r.Context(), adminToken) {
		authed = true
	}
	if !authed {
		// respondError stays silent for a 401 with no err, so log the failed
		// restore attempt here (remote address only, never the token).
		debuglog.Warn("auth: backup restore with invalid admin token", "remote_addr", r.RemoteAddr)
		respondError(w, "invalid admin token", nil, http.StatusUnauthorized)
		return "", false
	}

	// Get uploaded file
	file, _, err := r.FormFile("dump")
	if err != nil {
		respondBadRequest(w, "missing dump file", err)
		return "", false
	}
	//nolint:errcheck // cleanup: multipart file handle
	defer file.Close()

	// Ensure backup directory exists
	if err := os.MkdirAll(h.backupDir, 0o750); err != nil {
		respondError(w, "failed to create backup directory", err, http.StatusInternalServerError)
		return "", false
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp(h.backupDir, "restore-*.dump")
	if err != nil {
		respondError(w, "failed to create temp file", err, http.StatusInternalServerError)
		return "", false
	}
	tmpPath = tmpFile.Name()

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()        //nolint:errcheck,gosec // error path: closing after copy failure
		_ = os.Remove(tmpPath) // error path: discard partial temp file
		respondError(w, "failed to save uploaded file", err, http.StatusInternalServerError)
		return "", false
	}
	tmpFile.Close() //nolint:errcheck,gosec // cleanup: file fully written, closing for pg_restore

	return tmpPath, true
}

// validateRestoreDump inspects a saved dump with pg_restore --list and rejects
// it unless it is a safe, same-or-older model-hotel backup: no dangerous
// objects, contains schema_migrations, and no migrations newer than this build.
// It writes the appropriate HTTP error and returns ok=false on rejection;
// otherwise it returns the dump's migration names.
func validateRestoreDump(w http.ResponseWriter, tmpPath string) (pgRestorePath string, dumpMigrations []string, ok bool) {
	// Step 1: Validate dump format with pg_restore --list
	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		respondError(w, "pg_restore not found - install postgresql-client package", err, http.StatusPreconditionFailed)
		return "", nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	//nolint:gosec // pgRestorePath is a configured binary path
	listCmd := exec.CommandContext(ctx, pgRestorePath, "--list", tmpPath)
	var listStdout, listStderr bytes.Buffer
	listCmd.Stdout = &listStdout
	listCmd.Stderr = &listStderr

	if err := listCmd.Run(); err != nil {
		respondBadRequest(w, "invalid dump file: pg_restore --list failed", err)
		return "", nil, false
	}

	// Step 2: Check for dangerous objects
	entries := parseTOC(listStdout.String())
	dangerous := checkDangerousObjects(entries)
	if len(dangerous) > 0 {
		debuglog.Warn("backup: restore rejected - dangerous objects in dump", "objects", strings.Join(dangerous, ", "))
		respondBadRequest(w, fmt.Sprintf("dump contains dangerous objects: %s", strings.Join(dangerous, ", ")), nil)
		return "", nil, false
	}

	// Step 3: Extract and compare migrations
	schemaEntry := findSchemaMigrationsEntry(entries)
	if schemaEntry == 0 {
		debuglog.Warn("backup: restore rejected - no schema_migrations in dump")
		respondBadRequest(w, "dump does not contain schema_migrations table - not a model-hotel backup", nil)
		return "", nil, false
	}

	dumpMigrations, err = extractMigrationNames(tmpPath, schemaEntry)
	if err != nil {
		respondError(w, "failed to extract migration info from dump", err, http.StatusInternalServerError)
		return "", nil, false
	}

	unknownMigrations := compareMigrations(dumpMigrations)
	if len(unknownMigrations) > 0 {
		debuglog.Warn("backup: restore rejected - newer version dump", "unknown_migrations", strings.Join(unknownMigrations, ", "))
		respondBadRequest(w, fmt.Sprintf(
			"dump is from a newer version (unknown migrations: %s). Downgrade restore is not supported.",
			strings.Join(unknownMigrations, ", "),
		), nil)
		return "", nil, false
	}

	return pgRestorePath, dumpMigrations, true
}

// runPgRestore runs pg_restore --clean --if-exists against the configured
// database, stripping the password from the connection URL onto PGPASSWORD. The
// pg_restore path is resolved once by validateRestoreDump and threaded in. It
// writes an HTTP error and returns false if the restore command fails.
func (h *BackupHandler) runPgRestore(w http.ResponseWriter, pgRestorePath, tmpPath string) bool {
	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer restoreCancel()

	// Strip password from connection URL for command line (same as pg_dump above)
	restoreConnURL := h.databaseURL
	var restoreEnvPassword string
	if u, err := url.Parse(h.databaseURL); err == nil && u.User != nil {
		if pass, ok := u.User.Password(); ok && pass != "" {
			restoreEnvPassword = pass
			u.User = url.User(u.User.Username())
			restoreConnURL = u.String()
		}
	}

	//nolint:gosec // pgRestorePath is a configured binary path
	restoreCmd := exec.CommandContext(restoreCtx, pgRestorePath,
		"--clean",
		"--if-exists",
		"--no-password",
		"-d", restoreConnURL,
		tmpPath,
	)
	if restoreEnvPassword != "" {
		restoreCmd.Env = append(os.Environ(), "PGPASSWORD="+restoreEnvPassword)
	}

	var restoreStderr bytes.Buffer
	restoreCmd.Stderr = &restoreStderr

	if err := restoreCmd.Run(); err != nil {
		debuglog.Error("backup: pg_restore failed", "output", strings.TrimSpace(restoreStderr.String()), "error", err)
		respondError(w, "pg_restore failed - check server logs for details", err, http.StatusInternalServerError)
		return false
	}

	return true
}

// ── Son/Father/Grandfather Rotation ──────────────────────────────────

// backupClassification holds the result of classifying backups into
// son (daily), father (weekly), and grandfather (monthly) retention tiers.
type backupClassification struct {
	Son         []backupEntry `json:"son"`
	Father      []backupEntry `json:"father"`
	Grandfather []backupEntry `json:"grandfather"`
	Prune       []backupEntry `json:"prune"`
}

// parseBackupTimestamp extracts the timestamp from a backup filename.
// Expected format: backup_YYYYMMDD_HHmmss_NNN.dump
func parseBackupTimestamp(filename string) (time.Time, error) {
	base := strings.TrimSuffix(filename, ".dump")
	parts := strings.SplitN(base, "_", 4)
	if len(parts) < 3 {
		return time.Time{}, fmt.Errorf("invalid backup filename format: %s", filename)
	}
	return time.Parse("20060102_150405", parts[1]+"_"+parts[2])
}

// classifyBackups sorts backups into son/father/grandfather retention tiers.
// Backups not belonging to any tier are placed in the Prune list.
//
// The algorithm:
//   - Son: keep the most recent backup from each of the last N days
//   - Father: keep the most recent backup from each of the last M ISO weeks
//     (excluding weeks that already have a son)
//   - Grandfather: keep the most recent backup from each of the last P months
//     (excluding months that already have a son or father)
func classifyBackups(backups []backupEntry, sonRetention, fatherRetention, grandfatherRetention int, now time.Time) backupClassification {
	result := backupClassification{}

	// Track which backup filenames are kept in each tier
	kept := make(map[string]bool)

	// Parse timestamps and index by filename
	timestamps := make(map[string]time.Time)
	for _, b := range backups {
		ts, err := parseBackupTimestamp(b.Filename)
		if err != nil {
			// Cannot parse timestamp; mark for pruning
			result.Prune = append(result.Prune, b)
			continue
		}
		timestamps[b.Filename] = ts
	}

	// ── Son (daily) ──
	// Keep the most recent backup from each of the last sonRetention calendar days.
	dayKeys := make(map[string]bool)
	for i := 0; i < sonRetention; i++ {
		dayKeys[now.AddDate(0, 0, -i).Format("2006-01-02")] = true
	}
	result.Son = keepMostRecentPerBucket(backups, timestamps, kept, dayKeys, func(ts time.Time) string {
		return ts.Format("2006-01-02")
	})

	// ── Father (weekly) ──
	// Keep the most recent backup from each of the last fatherRetention ISO weeks
	// that is NOT already kept as a son. The i=0 iteration covers the current
	// week, so no separate "current week" entry is needed.
	isoWeekKey := func(ts time.Time) string {
		y, iw := ts.ISOWeek()
		return fmt.Sprintf("%d-%d", y, iw)
	}
	isoWeekSet := make(map[string]bool)
	t := now
	for i := 0; i < fatherRetention; i++ {
		isoWeekSet[isoWeekKey(t)] = true
		t = t.AddDate(0, 0, -7)
	}
	result.Father = keepMostRecentPerBucket(backups, timestamps, kept, isoWeekSet, isoWeekKey)

	// ── Grandfather (monthly) ──
	// Keep the most recent backup from each of the last grandfatherRetention months
	// that is NOT already kept as a son or father.
	monthKeys := make(map[string]bool)
	for i := 0; i < grandfatherRetention; i++ {
		monthKeys[now.AddDate(0, -i, 0).Format("2006-01")] = true
	}
	result.Grandfather = keepMostRecentPerBucket(backups, timestamps, kept, monthKeys, func(ts time.Time) string {
		return ts.Format("2006-01")
	})

	// ── Prune: everything not kept ──
	for _, b := range backups {
		if !kept[b.Filename] && timestamps[b.Filename].IsZero() {
			// Already added to Prune above (parse error)
			continue
		}
		if !kept[b.Filename] {
			result.Prune = append(result.Prune, b)
		}
	}

	return result
}

// keepMostRecentPerBucket implements one GFS retention tier: it buckets the
// not-yet-kept backups by keyFn, and for each bucket whose key is in wantKeys
// keeps the most recent entry, marking it in kept. Buckets are visited in
// descending lexicographic key order so the returned tier is deterministic
// (exact period order for zero-padded day/month keys; ISO week keys are not
// zero-padded, matching the ordering the per-tier code always used).
// Backups without a parsed timestamp are skipped (they are pruned elsewhere).
func keepMostRecentPerBucket(backups []backupEntry, timestamps map[string]time.Time, kept, wantKeys map[string]bool, keyFn func(time.Time) string) []backupEntry {
	buckets := make(map[string][]backupEntry)
	for _, b := range backups {
		ts, ok := timestamps[b.Filename]
		if !ok || kept[b.Filename] {
			continue
		}
		key := keyFn(ts)
		if !wantKeys[key] {
			continue
		}
		buckets[key] = append(buckets[key], b)
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	var tier []backupEntry
	for _, k := range keys {
		picked := mostRecentEntry(buckets[k], timestamps)
		if picked != nil && !kept[picked.Filename] {
			tier = append(tier, *picked)
			kept[picked.Filename] = true
		}
	}
	return tier
}

// mostRecentEntry returns the entry with the most recent timestamp.
func mostRecentEntry(entries []backupEntry, timestamps map[string]time.Time) *backupEntry {
	if len(entries) == 0 {
		return nil
	}
	best := &entries[0]
	bestTS := timestamps[best.Filename]
	for i := 1; i < len(entries); i++ {
		ts := timestamps[entries[i].Filename]
		if ts.After(bestTS) {
			best = &entries[i]
			bestTS = ts
		}
	}
	return best
}

// getRetentionSettings returns the current retention settings from the settings store.
func (h *BackupHandler) getRetentionSettings(ctx context.Context) (son, father, grandfather int) {
	son = 7
	father = 4
	grandfather = 3

	if h.settingsRepo != nil {
		if v, err := strconv.Atoi(h.settingsRepo.GetWithDefault(ctx, "backup_son_retention", "7")); err == nil && v > 0 {
			son = v
		}
		if v, err := strconv.Atoi(h.settingsRepo.GetWithDefault(ctx, "backup_father_retention", "4")); err == nil && v >= 0 {
			father = v
		}
		if v, err := strconv.Atoi(h.settingsRepo.GetWithDefault(ctx, "backup_grandfather_retention", "3")); err == nil && v >= 0 {
			grandfather = v
		}
	}
	return
}

// PrunePreview returns which backups would be pruned under the current
// son/father/grandfather rotation scheme without actually deleting anything.
func (h *BackupHandler) PrunePreview(w http.ResponseWriter, r *http.Request) {
	backups, err := h.listBackupFiles()
	if err != nil {
		respondError(w, "failed to list backups", err, http.StatusInternalServerError)
		return
	}

	son, father, grandfather := h.getRetentionSettings(r.Context())
	classification := classifyBackups(scheduledBackups(backups), son, father, grandfather, time.Now())
	writeJSON(w, classification)
}

// ApplyPrune runs the rotation and deletes backups that fall outside the
// son/father/grandfather retention scheme.
func (h *BackupHandler) ApplyPrune(w http.ResponseWriter, r *http.Request) {
	if !h.backupMu.TryLock() {
		respondError(w, "backup operation already in progress", nil, http.StatusConflict)
		return
	}
	defer h.backupMu.Unlock()

	backups, err := h.listBackupFiles()
	if err != nil {
		respondError(w, "failed to list backups", err, http.StatusInternalServerError)
		return
	}

	son, father, grandfather := h.getRetentionSettings(r.Context())
	classification := classifyBackups(scheduledBackups(backups), son, father, grandfather, time.Now())

	var pruned []string
	for _, b := range classification.Prune {
		absPath := h.validateBackupFilename(b.Filename)
		if absPath == "" {
			continue
		}
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			debuglog.Error("backup: failed to prune", "filename", b.Filename, "error", err)
			continue
		}
		pruned = append(pruned, b.Filename)
		debuglog.Info("backup: pruned", "filename", b.Filename)
	}

	if len(pruned) > 0 {
		events.Publish(events.Event{
			Type:     "backup.pruned",
			Severity: "info",
			Source:   "backup",
			Message:  fmt.Sprintf("Pruned %d backup(s): %s", len(pruned), strings.Join(pruned, ", ")),
			Metadata: map[string]interface{}{"pruned_count": len(pruned), "filenames": pruned},
		})
	}

	writeJSON(w, classification)
}

// listBackupFiles reads all backup entries from disk (newest first).
func (h *BackupHandler) listBackupFiles() ([]backupEntry, error) {
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []backupEntry{}, nil
		}
		return nil, err
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

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt > backups[j].CreatedAt
	})

	if backups == nil {
		backups = []backupEntry{}
	}
	return backups, nil
}

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
				sleep = h.settingsRepo.GetDuration(schedCtx, "backup_interval", 24*time.Hour)
				if sleep < 5*time.Minute {
					sleep = 5 * time.Minute
				}
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
		Metadata: map[string]interface{}{"filename": filename, "size_bytes": info.Size()},
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
