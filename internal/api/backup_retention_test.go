package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// ────────────────────────────────────────────────────────────────────────
// Son/Father/Grandfather Rotation Algorithm Tests
// ────────────────────────────────────────────────────────────────────────

func TestParseBackupTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:    "valid standard format",
			input:   "backup_20240115_120000_001.dump",
			want:    time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "valid with different sequence",
			input:   "backup_20231231_235959_999.dump",
			want:    time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "invalid garbage",
			input:   "garbage.dump",
			wantErr: true,
		},
		{
			name:    "missing time part",
			input:   "backup_20240115.dump",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "valid without sequence number",
			input:   "backup_20240601_090000.dump",
			want:    time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBackupTimestamp(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseBackupTimestamp(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseBackupTimestamp(%q) unexpected error: %v", tt.input, err)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseBackupTimestamp(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestBackupOrigin(t *testing.T) {
	cases := map[string]string{
		"backup_20240115_120000_0010_manual.dump":    "manual",
		"backup_20240115_120000_0010_auto.dump":      "scheduled",
		"backup_20240115_120000_0010_frontdesk.dump": "frontdesk",
		"backup_20240115_120000_0010.dump":           "manual", // predates origin tracking
		"backup_20240115_120000_manual.dump":         "manual",
	}
	for name, want := range cases {
		if got := backupOrigin(name); got != want {
			t.Errorf("backupOrigin(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestGenerateBackupFilenameOrigin(t *testing.T) {
	manual := generateBackupFilename("manual")
	if !strings.HasSuffix(manual, "_manual.dump") {
		t.Errorf("manual filename %q missing _manual suffix", manual)
	}
	if got := backupOrigin(manual); got != "manual" {
		t.Errorf("backupOrigin(%q) = %q, want manual", manual, got)
	}
	if got := backupOrigin(generateBackupFilename("auto")); got != "scheduled" {
		t.Errorf("auto backup origin = %q, want scheduled", got)
	}
	// Front Desk's pre-sync snapshots round-trip to "frontdesk" and, like manual,
	// are not GFS rotation targets (only "_auto" files are scheduled).
	fd := generateBackupFilename(backupOriginFrontDesk)
	if got := backupOrigin(fd); got != backupOriginFrontDesk {
		t.Errorf("frontdesk backup origin = %q, want %q", got, backupOriginFrontDesk)
	}
	if _, err := parseBackupTimestamp(fd); err != nil {
		t.Errorf("parseBackupTimestamp(%q) failed: %v", fd, err)
	}
	// The origin segment must not break timestamp parsing (GFS classification).
	if _, err := parseBackupTimestamp(manual); err != nil {
		t.Errorf("parseBackupTimestamp(%q) failed: %v", manual, err)
	}
}

func TestClassifyBackupsExemptsManual(t *testing.T) {
	now := time.Date(2024, 1, 30, 12, 0, 0, 0, time.UTC)
	// An old manual backup and an old legacy (no-marker) backup would both land
	// in Prune by age alone; only the scheduled one should ever be classified.
	backups := []backupEntry{
		{Filename: "backup_20240101_120000_0001_manual.dump"}, // 29d old, manual
		{Filename: "backup_20240101_120000_0003.dump"},        // legacy -> manual
		{Filename: "backup_20240130_110000_0002_auto.dump"},   // recent, scheduled
	}
	res := classifyBackups(scheduledBackups(backups), 7, 4, 3, now)

	tiers := append(append(append(append([]backupEntry{}, res.Son...),
		res.Father...), res.Grandfather...), res.Prune...)
	for _, b := range tiers {
		if backupOrigin(b.Filename) == "manual" {
			t.Errorf("manual/legacy backup %q must be exempt from GFS, found in classification", b.Filename)
		}
	}
	if len(tiers) != 1 {
		t.Errorf("expected only the 1 scheduled backup classified, got %d: %+v", len(tiers), tiers)
	}

	// Every tier must be a non-nil slice so the JSON payload carries [] not
	// null even when filtering leaves nothing to classify; the enable-confirm
	// modal reads prune.length directly and crashes on null.
	manualOnly := classifyBackups(scheduledBackups([]backupEntry{
		{Filename: "backup_20240101_120000_0001_manual.dump"},
	}), 7, 4, 3, now)
	if manualOnly.Son == nil || manualOnly.Father == nil ||
		manualOnly.Grandfather == nil || manualOnly.Prune == nil {
		t.Errorf("classification tiers must be non-nil, got %+v", manualOnly)
	}
}

func TestMostRecentEntry(t *testing.T) {
	t.Run("empty list returns nil", func(t *testing.T) {
		result := mostRecentEntry(nil, nil)
		if result != nil {
			t.Errorf("expected nil for empty list, got %+v", result)
		}
	})

	t.Run("empty slice returns nil", func(t *testing.T) {
		result := mostRecentEntry([]backupEntry{}, nil)
		if result != nil {
			t.Errorf("expected nil for empty slice, got %+v", result)
		}
	})

	t.Run("single entry returns that entry", func(t *testing.T) {
		entry := backupEntry{Filename: "backup_20240101_120000_001.dump", SizeBytes: 100}
		ts := map[string]time.Time{
			"backup_20240101_120000_001.dump": time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		}
		result := mostRecentEntry([]backupEntry{entry}, ts)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Filename != entry.Filename {
			t.Errorf("expected filename %q, got %q", entry.Filename, result.Filename)
		}
	})

	t.Run("multiple entries returns most recent", func(t *testing.T) {
		entries := []backupEntry{
			{Filename: "backup_20240101_080000_001.dump", SizeBytes: 100},
			{Filename: "backup_20240101_120000_001.dump", SizeBytes: 200},
			{Filename: "backup_20240101_100000_001.dump", SizeBytes: 150},
		}
		ts := map[string]time.Time{
			"backup_20240101_080000_001.dump": time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC),
			"backup_20240101_120000_001.dump": time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			"backup_20240101_100000_001.dump": time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		}
		result := mostRecentEntry(entries, ts)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Filename != "backup_20240101_120000_001.dump" {
			t.Errorf("expected most recent entry, got %q", result.Filename)
		}
		if result.SizeBytes != 200 {
			t.Errorf("expected size 200, got %d", result.SizeBytes)
		}
	})
}

func TestClassifyBackups(t *testing.T) {
	t.Run("empty backup list", func(t *testing.T) {
		result := classifyBackups(nil, 7, 4, 3, time.Now())
		if len(result.Son) != 0 {
			t.Errorf("expected 0 son, got %d", len(result.Son))
		}
		if len(result.Father) != 0 {
			t.Errorf("expected 0 father, got %d", len(result.Father))
		}
		if len(result.Grandfather) != 0 {
			t.Errorf("expected 0 grandfather, got %d", len(result.Grandfather))
		}
		if len(result.Prune) != 0 {
			t.Errorf("expected 0 prune, got %d", len(result.Prune))
		}
	})

	t.Run("single backup is son", func(t *testing.T) {
		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", time.Now().Format("20060102_150405")), SizeBytes: 100},
		}
		result := classifyBackups(backups, 7, 4, 3, time.Now())
		if len(result.Son) != 1 {
			t.Fatalf("expected 1 son, got %d", len(result.Son))
		}
		if result.Son[0].Filename != backups[0].Filename {
			t.Errorf("expected son to be %q, got %q", backups[0].Filename, result.Son[0].Filename)
		}
		if len(result.Prune) != 0 {
			t.Errorf("expected 0 prune, got %d", len(result.Prune))
		}
	})

	t.Run("backups from today only are all son", func(t *testing.T) {
		now := time.Now()
		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", now.Format("20060102_150405")), SizeBytes: 100},
			{Filename: fmt.Sprintf("backup_%s_002.dump", now.Format("20060102_150405")), SizeBytes: 200},
		}
		// With sonRetention=1, only the most recent from today is kept as son.
		// The other one from the same day is not kept (only one son per day).
		result := classifyBackups(backups, 1, 4, 3, time.Now())
		if len(result.Son) != 1 {
			t.Fatalf("expected 1 son (most recent from today), got %d", len(result.Son))
		}
	})

	t.Run("multiple backups same day keeps most recent as son", func(t *testing.T) {
		now := time.Now()
		dayKey := now.Format("20060102")
		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_080000_001.dump", dayKey), SizeBytes: 100},
			{Filename: fmt.Sprintf("backup_%s_120000_002.dump", dayKey), SizeBytes: 200},
			{Filename: fmt.Sprintf("backup_%s_160000_003.dump", dayKey), SizeBytes: 300},
		}
		result := classifyBackups(backups, 7, 4, 3, time.Now())
		if len(result.Son) != 1 {
			t.Fatalf("expected 1 son, got %d", len(result.Son))
		}
		if result.Son[0].Filename != backups[2].Filename {
			t.Errorf("expected most recent backup as son, got %q", result.Son[0].Filename)
		}
		// The remaining 2 backups from the same day are NOT sons (only 1 per day),
		// but they may be kept as father (same ISO week) or grandfather (same month).
		// Verify none of the non-most-recent backups are in the son tier.
		sonFiles := make(map[string]bool)
		for _, s := range result.Son {
			sonFiles[s.Filename] = true
		}
		for i := range 2 {
			if sonFiles[backups[i].Filename] {
				t.Errorf("backup %q should NOT be in son tier (only most recent per day)", backups[i].Filename)
			}
		}
	})

	t.Run("backups spanning multiple days", func(t *testing.T) {
		now := time.Now()
		yesterday := now.AddDate(0, 0, -1)

		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", now.Format("20060102_150405")), SizeBytes: 100},
			{Filename: fmt.Sprintf("backup_%s_001.dump", yesterday.Format("20060102_150405")), SizeBytes: 200},
		}
		result := classifyBackups(backups, 7, 4, 3, time.Now())
		if len(result.Son) != 2 {
			t.Fatalf("expected 2 sons (one per day), got %d", len(result.Son))
		}
		if len(result.Prune) != 0 {
			t.Errorf("expected 0 prune, got %d", len(result.Prune))
		}
	})

	t.Run("backups older than all retention periods are pruned", func(t *testing.T) {
		// Create backups from 60 days ago, with retention of 1 day son, 0 father, 0 grandfather
		old := time.Now().AddDate(0, 0, -60)
		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", old.Format("20060102_150405")), SizeBytes: 100},
		}
		result := classifyBackups(backups, 1, 0, 0, time.Now())
		if len(result.Son) != 0 {
			t.Errorf("expected 0 son (too old), got %d", len(result.Son))
		}
		if len(result.Prune) != 1 {
			t.Fatalf("expected 1 prune, got %d", len(result.Prune))
		}
		if result.Prune[0].Filename != backups[0].Filename {
			t.Errorf("expected %q to be pruned, got %q", backups[0].Filename, result.Prune[0].Filename)
		}
	})

	t.Run("son to father to grandfather to prune tier flow", func(t *testing.T) {
		now := time.Now()

		// Today's backup → son
		todayBackup := backupEntry{
			Filename:  fmt.Sprintf("backup_%s_001.dump", now.Format("20060102_150405")),
			SizeBytes: 100,
		}
		// 10 days ago → father (not in son's daily range but in weekly range)
		tenDaysAgo := now.AddDate(0, 0, -10)
		weekBackup := backupEntry{
			Filename:  fmt.Sprintf("backup_%s_001.dump", tenDaysAgo.Format("20060102_150405")),
			SizeBytes: 200,
		}
		// 3 months ago → grandfather (not in son or father but in monthly range)
		threeMonthsAgo := now.AddDate(0, -3, 0)
		monthBackup := backupEntry{
			Filename:  fmt.Sprintf("backup_%s_001.dump", threeMonthsAgo.Format("20060102_150405")),
			SizeBytes: 300,
		}
		// 8 months ago → prune (beyond all retention)
		eightMonthsAgo := now.AddDate(0, -8, 0)
		pruneBackup := backupEntry{
			Filename:  fmt.Sprintf("backup_%s_001.dump", eightMonthsAgo.Format("20060102_150405")),
			SizeBytes: 400,
		}

		backups := []backupEntry{todayBackup, weekBackup, monthBackup, pruneBackup}
		result := classifyBackups(backups, 1, 5, 4, time.Now())

		// Today should be son
		if len(result.Son) < 1 {
			t.Fatalf("expected at least 1 son, got %d", len(result.Son))
		}
		foundToday := false
		for _, s := range result.Son {
			if s.Filename == todayBackup.Filename {
				foundToday = true
			}
		}
		if !foundToday {
			t.Errorf("today's backup should be in son tier")
		}

		// 8 months ago should be pruned (beyond grandfatherRetention=4)
		if len(result.Prune) < 1 {
			t.Fatalf("expected at least 1 prune, got %d", len(result.Prune))
		}
		foundPrune := false
		for _, p := range result.Prune {
			if p.Filename == pruneBackup.Filename {
				foundPrune = true
			}
		}
		if !foundPrune {
			t.Errorf("8-month-old backup should be in prune tier, prune list: %v", result.Prune)
		}
	})

	t.Run("unparseable filenames go to prune", func(t *testing.T) {
		backups := []backupEntry{
			{Filename: "garbage.dump", SizeBytes: 50},
		}
		result := classifyBackups(backups, 7, 4, 3, time.Now())
		if len(result.Prune) != 1 {
			t.Fatalf("expected 1 prune (unparseable), got %d", len(result.Prune))
		}
		if result.Prune[0].Filename != "garbage.dump" {
			t.Errorf("expected garbage.dump in prune, got %q", result.Prune[0].Filename)
		}
	})

	t.Run("zero retention prunes everything except current day/week/month", func(t *testing.T) {
		now := time.Now()
		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", now.Format("20060102_150405")), SizeBytes: 100},
		}
		// sonRetention=1 keeps today, fatherRetention=0 and grandfatherRetention=0 don't add more
		result := classifyBackups(backups, 1, 0, 0, time.Now())
		if len(result.Son) != 1 {
			t.Fatalf("expected 1 son (today), got %d", len(result.Son))
		}
		if len(result.Father) != 0 {
			t.Errorf("expected 0 father, got %d", len(result.Father))
		}
		if len(result.Grandfather) != 0 {
			t.Errorf("expected 0 grandfather, got %d", len(result.Grandfather))
		}
		if len(result.Prune) != 0 {
			t.Errorf("expected 0 prune, got %d", len(result.Prune))
		}
	})

	t.Run("backups from yesterday with daily retention 2", func(t *testing.T) {
		now := time.Now()
		yesterday := now.AddDate(0, 0, -1)

		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_090000_001.dump", now.Format("20060102")), SizeBytes: 100},
			{Filename: fmt.Sprintf("backup_%s_120000_002.dump", now.Format("20060102")), SizeBytes: 200},
			{Filename: fmt.Sprintf("backup_%s_090000_001.dump", yesterday.Format("20060102")), SizeBytes: 150},
			{Filename: fmt.Sprintf("backup_%s_150000_002.dump", yesterday.Format("20060102")), SizeBytes: 250},
		}
		result := classifyBackups(backups, 2, 0, 0, time.Now())
		// With sonRetention=2, we keep the most recent from today and yesterday
		if len(result.Son) != 2 {
			t.Fatalf("expected 2 sons, got %d", len(result.Son))
		}
		// Should keep 12:00 today and 15:00 yesterday (most recent per day)
		sonFiles := make(map[string]bool)
		for _, s := range result.Son {
			sonFiles[s.Filename] = true
		}
		if !sonFiles[backups[1].Filename] {
			t.Errorf("expected %q in son", backups[1].Filename)
		}
		if !sonFiles[backups[3].Filename] {
			t.Errorf("expected %q in son", backups[3].Filename)
		}
	})

	t.Run("son excludes father tier duplicates", func(t *testing.T) {
		now := time.Now()
		twoWeeksAgo := now.AddDate(0, 0, -14)

		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", now.Format("20060102_150405")), SizeBytes: 100},
			{Filename: fmt.Sprintf("backup_%s_001.dump", twoWeeksAgo.Format("20060102_150405")), SizeBytes: 200},
		}
		// sonRetention=1 keeps today; the 2-week-old is NOT a son.
		// fatherRetention=4 should cover the ISO week of 2 weeks ago.
		result := classifyBackups(backups, 1, 4, 0, time.Now())

		if len(result.Son) != 1 {
			t.Fatalf("expected 1 son, got %d", len(result.Son))
		}
		if result.Son[0].Filename != backups[0].Filename {
			t.Errorf("expected today as son, got %q", result.Son[0].Filename)
		}
		// The 2-week-old backup should be father (not son), or pruned if week not in range
		sonFiles := make(map[string]bool)
		for _, s := range result.Son {
			sonFiles[s.Filename] = true
		}
		if sonFiles[backups[1].Filename] {
			t.Errorf("2-week-old backup should NOT be in son tier")
		}
	})

	t.Run("father tier uses year+week composite to avoid year-boundary issue", func(t *testing.T) {
		// Simulate early January: fatherRetention=4 looks back into previous year's weeks.
		// Without year+week composites, week 52 from 2024 and week 52 from 2023 would collide.
		jan6 := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
		dec2025Week52 := time.Date(2025, 12, 22, 12, 0, 0, 0, time.UTC) // ISO week 52 of 2025
		dec2024Week52 := time.Date(2024, 12, 23, 12, 0, 0, 0, time.UTC) // ISO week 52 of 2024

		backups := []backupEntry{
			{Filename: fmt.Sprintf("backup_%s_001.dump", jan6.Format("20060102_150405")), SizeBytes: 100},
			{Filename: fmt.Sprintf("backup_%s_001.dump", dec2025Week52.Format("20060102_150405")), SizeBytes: 200},
			{Filename: fmt.Sprintf("backup_%s_002.dump", dec2024Week52.Format("20060102_150405")), SizeBytes: 300},
		}

		// sonRetention=1 keeps today; fatherRetention=4 includes the last 4 ISO weeks.
		result := classifyBackups(backups, 1, 4, 0, jan6)

		// The 2024 week-52 backup should be pruned, not promoted to father.
		pruneFiles := make(map[string]bool)
		for _, p := range result.Prune {
			pruneFiles[p.Filename] = true
		}
		if !pruneFiles[backups[2].Filename] {
			t.Errorf("2024 week-52 backup should be pruned (too old for fatherRetention=4)")
		}
	})
}

func TestGetRetentionSettings(t *testing.T) {
	t.Parallel()

	t.Run("nil settings repo returns defaults", func(t *testing.T) {
		t.Parallel()
		h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)
		son, father, grandfather := h.getRetentionSettings(context.Background())
		if son != 7 {
			t.Errorf("expected son=7, got %d", son)
		}
		if father != 4 {
			t.Errorf("expected father=4, got %d", father)
		}
		if grandfather != 3 {
			t.Errorf("expected grandfather=3, got %d", grandfather)
		}
	})

	t.Run("custom values from settings repo", func(t *testing.T) {
		t.Parallel()
		ss := &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
				switch key {
				case "backup_son_retention":
					return "14"
				case "backup_father_retention":
					return "8"
				case "backup_grandfather_retention":
					return "6"
				}
				return defaultValue
			},
		}
		h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)
		son, father, grandfather := h.getRetentionSettings(context.Background())
		if son != 14 {
			t.Errorf("expected son=14, got %d", son)
		}
		if father != 8 {
			t.Errorf("expected father=8, got %d", father)
		}
		if grandfather != 6 {
			t.Errorf("expected grandfather=6, got %d", grandfather)
		}
	})

	t.Run("invalid values fall back to defaults", func(t *testing.T) {
		t.Parallel()
		ss := &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
				switch key {
				case "backup_son_retention":
					return "abc" // non-numeric
				case "backup_father_retention":
					return "0" // must be >= 0, so 0 is valid
				case "backup_grandfather_retention":
					return "-1" // negative, invalid
				}
				return defaultValue
			},
		}
		h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)
		son, father, grandfather := h.getRetentionSettings(context.Background())
		if son != 7 {
			t.Errorf("invalid son 'abc' should fall back to default 7, got %d", son)
		}
		if father != 0 {
			t.Errorf("father '0' is >= 0 and thus valid, expected 0, got %d", father)
		}
		if grandfather != 3 {
			t.Errorf("invalid grandfather '-1' should fall back to default 3, got %d", grandfather)
		}
	})

	t.Run("son must be positive zero falls back", func(t *testing.T) {
		t.Parallel()
		ss := &mockSettingsStore{
			getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
				if key == "backup_son_retention" {
					return "0" // son must be > 0
				}
				return defaultValue
			},
		}
		h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)
		son, _, _ := h.getRetentionSettings(context.Background())
		if son != 7 {
			t.Errorf("son=0 is not > 0, should fall back to default 7, got %d", son)
		}
	})
}

func TestPrunePreview(t *testing.T) {
	t.Parallel()

	t.Run("empty backup dir returns empty classification", func(t *testing.T) {
		t.Parallel()
		r, _ := setupBackupRouterWithSettings(t, nil)

		req := httptest.NewRequest(http.MethodPost, "/backups/prune-preview", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result backupClassification
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if len(result.Son) != 0 {
			t.Errorf("expected empty son, got %d", len(result.Son))
		}
		if len(result.Father) != 0 {
			t.Errorf("expected empty father, got %d", len(result.Father))
		}
		if len(result.Grandfather) != 0 {
			t.Errorf("expected empty grandfather, got %d", len(result.Grandfather))
		}
		if len(result.Prune) != 0 {
			t.Errorf("expected empty prune, got %d", len(result.Prune))
		}
	})

	t.Run("classifies backups into tiers", func(t *testing.T) {
		t.Parallel()
		r, dir := setupBackupRouterWithSettings(t, nil)

		// Create backups spanning several days so classification is non-trivial.
		now := time.Now()
		names := []string{
			fmt.Sprintf("backup_%s_001_auto.dump", now.Format("20060102_150405")),
			fmt.Sprintf("backup_%s_001_auto.dump", now.AddDate(0, 0, -1).Format("20060102_150405")),
			fmt.Sprintf("backup_%s_001_auto.dump", now.AddDate(0, 0, -30).Format("20060102_150405")),
			fmt.Sprintf("backup_%s_001_auto.dump", now.AddDate(0, -3, 0).Format("20060102_150405")),
		}
		for _, name := range names {
			//nolint:gosec // test-only: permissive perms acceptable
			if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		req := httptest.NewRequest(http.MethodPost, "/backups/prune-preview", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result backupClassification
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		total := len(result.Son) + len(result.Father) + len(result.Grandfather) + len(result.Prune)
		if total != len(names) {
			t.Errorf("expected %d total classified entries, got %d", len(names), total)
		}
	})

	t.Run("non-existent backup dir returns empty classification", func(t *testing.T) {
		t.Parallel()
		// Create a handler that points to a non-existent directory.
		h := NewBackupHandler("postgres://x", "/nonexistent/path/backup_test", &mockAdminAuth{}, nil)
		r := chi.NewRouter()
		h.Register(r)

		req := httptest.NewRequest(http.MethodPost, "/backups/prune-preview", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result backupClassification
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if len(result.Prune) != 0 || len(result.Son) != 0 || len(result.Father) != 0 || len(result.Grandfather) != 0 {
			t.Error("non-existent dir should return all-empty classification")
		}
	})
}

func TestApplyPrune(t *testing.T) {
	t.Parallel()

	t.Run("no prunable backups returns empty prune list", func(t *testing.T) {
		t.Parallel()
		r, _ := setupBackupRouterWithSettings(t, nil)

		req := httptest.NewRequest(http.MethodPost, "/backups/prune", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result backupClassification
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if len(result.Prune) != 0 {
			t.Errorf("expected empty prune list, got %d", len(result.Prune))
		}
	})

	t.Run("prunable backups are deleted from disk", func(t *testing.T) {
		t.Parallel()
		r, dir := setupBackupRouterWithSettings(t, nil)

		// Create an old scheduler backup that falls outside retention periods.
		oldTime := time.Now().AddDate(-2, 0, 0)
		oldName := fmt.Sprintf("backup_%s_001_auto.dump", oldTime.Format("20060102_150405"))
		//nolint:gosec // test-only: permissive perms acceptable
		if err := os.WriteFile(filepath.Join(dir, oldName), []byte("old-backup"), 0o644); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/backups/prune", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var result backupClassification
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		// The old backup should appear in the prune list and be gone from disk.
		found := false
		for _, p := range result.Prune {
			if p.Filename == oldName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("old backup %q should be in prune list", oldName)
		}

		if _, err := os.Stat(filepath.Join(dir, oldName)); !os.IsNotExist(err) {
			t.Error("old backup file should have been deleted from disk")
		}
	})

	t.Run("conflict when lock is held", func(t *testing.T) {
		t.Parallel()
		h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)
		r := chi.NewRouter()
		h.Register(r)

		// Hold the mutex to simulate a concurrent backup operation.
		h.backupMu.Lock()
		defer h.backupMu.Unlock()

		req := httptest.NewRequest(http.MethodPost, "/backups/prune", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Errorf("expected 409 Conflict, got %d", w.Code)
		}
	})
}

func TestListBackupFiles_InfoError(t *testing.T) {
	// On Linux, os.DirEntry.Info() on a dangling symlink does not return an error
	// (it returns info about the symlink itself). To trigger the Info() error path,
	// we create a file, read the directory, then delete the file before calling Info().
	// However, ReadDir reads everything at once, so a race-based approach is unreliable.
	//
	// Instead, verify that listBackupFiles gracefully handles a dangling symlink
	// (which on some OSes may cause Info() to fail). On Linux, the broken symlink
	// will be included because Info() succeeds, but this is acceptable behavior.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.dump"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a symlink to a non-existent target
	if err := os.Symlink("/nonexistent/target.dump", filepath.Join(dir, "broken.dump")); err != nil {
		t.Fatalf("symlink not supported: %v", err)
	}

	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, nil)
	backups, err := h.listBackupFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// On Linux, both files appear (dangling symlink's Info() succeeds with symlink metadata).
	// On other OSes, the broken symlink may be skipped. Just verify no crash and at least
	// the real file appears.
	found := false
	for _, b := range backups {
		if b.Filename == "test.dump" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected test.dump in results, got %v", backups)
	}
}

func TestListBackupFiles_DirEntryFilter(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory with .dump suffix (should be filtered by IsDir)
	if err := os.Mkdir(filepath.Join(dir, "subdir.dump"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a non-.dump file (should be filtered by HasSuffix)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a valid .dump file
	if err := os.WriteFile(filepath.Join(dir, "backup_20240101_120000_001.dump"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, nil)
	backups, err := h.listBackupFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backups) != 1 || backups[0].Filename != "backup_20240101_120000_001.dump" {
		t.Errorf("expected 1 backup, got %v", backups)
	}
}

func TestPrunePreview_WithSettingsRepo(t *testing.T) {
	dir := t.TempDir()
	// Create several backup files to classify
	for _, name := range []string{
		"backup_20240601_120000_001_auto.dump",
		"backup_20240608_120000_002_auto.dump",
		"backup_20240501_120000_003_auto.dump",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			switch key {
			case "backup_son_retention":
				return "1"
			case "backup_father_retention":
				return "1"
			case "backup_grandfather_retention":
				return "1"
			default:
				return defaultValue
			}
		},
	}
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("POST", "/backups/prune-preview", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result backupClassification
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	total := len(result.Son) + len(result.Father) + len(result.Grandfather) + len(result.Prune)
	if total != 3 {
		t.Errorf("expected 3 total backups, got %d", total)
	}
}

func TestApplyPrune_WithSettingsRepo(t *testing.T) {
	dir := t.TempDir()
	// Create backup files: some will be pruned with aggressive retention
	for _, name := range []string{
		"backup_20240601_120000_001_auto.dump",
		"backup_20240608_120000_002_auto.dump",
		"backup_20240501_120000_003_auto.dump",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			switch key {
			case "backup_son_retention":
				return "1"
			case "backup_father_retention":
				return "0"
			case "backup_grandfather_retention":
				return "0"
			default:
				return defaultValue
			}
		},
	}
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("POST", "/backups/prune", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result backupClassification
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify at least one file was pruned (deleted from disk)
	prunedCount := len(result.Prune)
	if prunedCount == 0 {
		t.Error("expected at least one backup to be in prune list")
	}
	// Verify files are actually removed from disk
	for _, b := range result.Prune {
		if _, err := os.Stat(filepath.Join(dir, b.Filename)); !os.IsNotExist(err) {
			t.Errorf("expected pruned file %s to be removed from disk", b.Filename)
		}
	}
}

func TestApplyPrune_RemoveError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backup_20240101_120000_001.dump"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			switch key {
			case "backup_son_retention":
				return "0"
			case "backup_father_retention":
				return "0"
			case "backup_grandfather_retention":
				return "0"
			default:
				return defaultValue
			}
		},
	}
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)

	// Make parent dir read-only so Remove fails
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("cannot chmod temp dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	req := httptest.NewRequest("POST", "/backups/prune", http.NoBody)
	w := httptest.NewRecorder()
	h.ApplyPrune(w, req)

	// Should still succeed (errors are logged but not fatal)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPrunePreview_ListBackupFilesError(t *testing.T) {
	// Use a file path as backupDir so os.ReadDir fails with a non-IsNotExist error
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewBackupHandler("postgres://x", filePath, &mockAdminAuth{}, nil)

	req := httptest.NewRequest("POST", "/backups/prune-preview", http.NoBody)
	w := httptest.NewRecorder()
	h.PrunePreview(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestApplyPrune_ListBackupFilesError(t *testing.T) {
	// Use a file path as backupDir so os.ReadDir fails with a non-IsNotExist error
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewBackupHandler("postgres://x", filePath, &mockAdminAuth{}, nil)

	req := httptest.NewRequest("POST", "/backups/prune", http.NoBody)
	w := httptest.NewRecorder()
	h.ApplyPrune(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListBackupFiles_ReadDirNotExists(t *testing.T) {
	// listBackupFiles should return empty slice when dir doesn't exist (os.IsNotExist path)
	h := NewBackupHandler("postgres://x", filepath.Join(t.TempDir(), "nonexistent"), &mockAdminAuth{}, nil)
	backups, err := h.listBackupFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(backups))
	}
}

func TestGetRetentionSettings_WithSettingsRepo(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			switch key {
			case "backup_son_retention":
				return "3"
			case "backup_father_retention":
				return "2"
			case "backup_grandfather_retention":
				return "1"
			default:
				return defaultValue
			}
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	son, father, grandfather := h.getRetentionSettings(context.Background())
	if son != 3 {
		t.Errorf("expected son=3, got %d", son)
	}
	if father != 2 {
		t.Errorf("expected father=2, got %d", father)
	}
	if grandfather != 1 {
		t.Errorf("expected grandfather=1, got %d", grandfather)
	}
}

func TestGetRetentionSettings_NilSettingsRepo(t *testing.T) {
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)

	son, father, grandfather := h.getRetentionSettings(context.Background())
	if son != 7 {
		t.Errorf("expected default son=7, got %d", son)
	}
	if father != 4 {
		t.Errorf("expected default father=4, got %d", father)
	}
	if grandfather != 3 {
		t.Errorf("expected default grandfather=3, got %d", grandfather)
	}
}

func TestGetRetentionSettings_InvalidValues(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			switch key {
			case "backup_son_retention":
				return "not-a-number"
			case "backup_father_retention":
				return "-5"
			case "backup_grandfather_retention":
				return "0"
			default:
				return defaultValue
			}
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	son, father, grandfather := h.getRetentionSettings(context.Background())
	// Invalid son value falls back to default
	if son != 7 {
		t.Errorf("expected default son=7 for invalid value, got %d", son)
	}
	// Negative father value fails v >= 0 check, falls back to default
	if father != 4 {
		t.Errorf("expected default father=4 for negative value, got %d", father)
	}
	// grandfather=0 is valid (v >= 0)
	if grandfather != 0 {
		t.Errorf("expected grandfather=0, got %d", grandfather)
	}
}
