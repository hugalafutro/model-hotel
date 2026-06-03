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
	databaseURL string
	backupDir   string
	backupMu    sync.Mutex
	adminMgr    AdminAuthenticator
}

// NewBackupHandler creates a new BackupHandler.
// backupDir is the directory where backup files are stored (typically DATA_DIR/backups).
func NewBackupHandler(databaseURL, backupDir string, adminMgr AdminAuthenticator) *BackupHandler {
	absDir, err := filepath.Abs(backupDir)
	if err != nil {
		absDir = backupDir // fallback to original path
	}
	return &BackupHandler{
		databaseURL: databaseURL,
		backupDir:   absDir,
		adminMgr:    adminMgr,
	}
}

// Register registers backup routes on the given router.
func (h *BackupHandler) Register(r chi.Router) {
	r.Route("/backups", func(r chi.Router) {
		r.Get("/", h.ListBackups)
		r.Post("/", h.CreateBackup)
		r.Post("/restore", h.RestoreBackup)
		r.Get("/{filename}", h.DownloadBackup)
		r.Delete("/{filename}", h.DeleteBackup)
	})
}

// backupEntry represents a backup file in the listing response.
type backupEntry struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
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

	now := time.Now()
	filename := fmt.Sprintf("backup_%s_%04d.dump", now.Format("20060102_150405"), now.Nanosecond()/100000)
	path := filepath.Join(h.backupDir, filename)

	// Extract password from DATABASE_URL and pass via env var to avoid
	// exposing it in the process command line (visible via ps).
	// Use a dedicated 10-minute timeout so large databases don't get killed
	// by the chi request timeout middleware (~60s).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	//nolint:gosec // pgDumpPath is a configured binary path, not arbitrary user input
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+path,
		h.databaseURL,
	)
	// Pass password via environment instead of cmdline argument
	if u, err := url.Parse(h.databaseURL); err == nil && u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}

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

	// Limit upload size (100MB)
	r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024)

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondBadRequest(w, "failed to parse multipart form", err)
		return
	}

	// Validate admin token from form field
	adminToken := r.FormValue("admin_token")
	if adminToken == "" || !h.adminMgr.Validate(adminToken) {
		respondError(w, "invalid admin token", nil, http.StatusUnauthorized)
		return
	}

	// Get uploaded file
	file, _, err := r.FormFile("dump")
	if err != nil {
		respondBadRequest(w, "missing dump file", err)
		return
	}
	//nolint:errcheck // cleanup: multipart file handle
	defer file.Close()

	// Ensure backup directory exists
	if err := os.MkdirAll(h.backupDir, 0o750); err != nil {
		respondError(w, "failed to create backup directory", err, http.StatusInternalServerError)
		return
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp(h.backupDir, "restore-*.dump")
	if err != nil {
		respondError(w, "failed to create temp file", err, http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	//nolint:errcheck // cleanup: temp file removed after processing
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, file); err != nil {
		//nolint:errcheck // error path: closing after copy failure
		tmpFile.Close() //nolint:errcheck,gosec // error path: closing after copy failure
		respondError(w, "failed to save uploaded file", err, http.StatusInternalServerError)
		return
	}
	tmpFile.Close() //nolint:errcheck,gosec // cleanup: file fully written, closing for pg_restore

	// Step 1: Validate dump format with pg_restore --list
	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		respondError(w, "pg_restore not found - install postgresql-client package", err, http.StatusPreconditionFailed)
		return
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
		return
	}

	// Step 2: Check for dangerous objects
	entries := parseTOC(listStdout.String())
	dangerous := checkDangerousObjects(entries)
	if len(dangerous) > 0 {
		debuglog.Warn("backup: restore rejected - dangerous objects in dump", "objects", strings.Join(dangerous, ", "))
		respondBadRequest(w, fmt.Sprintf("dump contains dangerous objects: %s", strings.Join(dangerous, ", ")), nil)
		return
	}

	// Step 3: Extract and compare migrations
	schemaEntry := findSchemaMigrationsEntry(entries)
	if schemaEntry == 0 {
		debuglog.Warn("backup: restore rejected - no schema_migrations in dump")
		respondBadRequest(w, "dump does not contain schema_migrations table - not a model-hotel backup", nil)
		return
	}

	dumpMigrations, err := extractMigrationNames(tmpPath, schemaEntry)
	if err != nil {
		respondError(w, "failed to extract migration info from dump", err, http.StatusInternalServerError)
		return
	}

	unknownMigrations := compareMigrations(dumpMigrations)
	if len(unknownMigrations) > 0 {
		debuglog.Warn("backup: restore rejected - newer version dump", "unknown_migrations", strings.Join(unknownMigrations, ", "))
		respondBadRequest(w, fmt.Sprintf(
			"dump is from a newer version (unknown migrations: %s). Downgrade restore is not supported.",
			strings.Join(unknownMigrations, ", "),
		), nil)
		return
	}

	// Step 4: Run pg_restore --clean --if-exists
	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer restoreCancel()

	//nolint:gosec // pgRestorePath is a configured binary path
	restoreCmd := exec.CommandContext(restoreCtx, pgRestorePath,
		"--clean",
		"--if-exists",
		"--no-password",
		"-d", h.databaseURL,
		tmpPath,
	)
	// Pass password via environment
	if u, err := url.Parse(h.databaseURL); err == nil && u.User != nil {
		if pass, ok := u.User.Password(); ok {
			restoreCmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}

	var restoreStderr bytes.Buffer
	restoreCmd.Stderr = &restoreStderr

	if err := restoreCmd.Run(); err != nil {
		debuglog.Error("backup: pg_restore failed", "output", strings.TrimSpace(restoreStderr.String()), "error", err)
		respondError(w, "pg_restore failed - check server logs for details", err, http.StatusInternalServerError)
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
