package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

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

	// Coerce every tier to a non-nil slice so the JSON payload serializes []
	// rather than null. keepMostRecentPerBucket returns nil for empty tiers and
	// Prune stays nil when nothing is pruned; the enable-confirm modal reads
	// prune.length directly and crashes on null.
	if result.Son == nil {
		result.Son = []backupEntry{}
	}
	if result.Father == nil {
		result.Father = []backupEntry{}
	}
	if result.Grandfather == nil {
		result.Grandfather = []backupEntry{}
	}
	if result.Prune == nil {
		result.Prune = []backupEntry{}
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
