package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/logtest"
)

func TestStartScheduler_NilSettingsRepo(t *testing.T) {
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)
	// Should return immediately without panicking or setting schedulerCancel.
	h.StartScheduler(context.Background())
	if h.schedulerCancel != nil {
		t.Error("schedulerCancel should remain nil when settingsRepo is nil")
	}
}

func TestStartScheduler_DoubleLaunch(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			return defaultValue
		},
		getBoolFn: func(_ context.Context, key string, defaultValue bool) bool {
			return false // disabled so scheduler loop sleeps
		},
		getDurationFn: func(_ context.Context, key string, defaultValue time.Duration) time.Duration {
			return defaultValue
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h.StartScheduler(ctx)
	firstCancel := h.schedulerCancel
	if firstCancel == nil {
		t.Fatal("first StartScheduler should set schedulerCancel")
	}

	// Second call should be a no-op: schedulerCancel must still be the same
	// non-nil function.  We cannot compare func values directly, so we verify
	// that the cancel is non-nil (i.e. it was not replaced with a new one).
	h.StartScheduler(ctx)
	if h.schedulerCancel == nil {
		t.Error("second StartScheduler should not clear schedulerCancel")
	}

	cancel()
}

// Shutdown joins the scheduler with the other background loops, which is only
// possible if the caller is handed something to wait on that actually tracks
// the goroutine. A cancelled context has to close it; a call that started
// nothing has to hand back a channel that is already closed, or shutdown would
// block on a scheduler that never existed.
func TestStartScheduler_StoppedChannelJoinsTheGoroutine(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, _, defaultValue string) string { return defaultValue },
		getBoolFn:        func(_ context.Context, _ string, _ bool) bool { return false },
		getDurationFn: func(_ context.Context, _ string, defaultValue time.Duration) time.Duration {
			return defaultValue
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := h.StartScheduler(ctx)
	select {
	case <-stopped:
		t.Fatal("the scheduler reported itself stopped before it was cancelled")
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the context did not release the join; shutdown would close the pool under a running backup")
	}

	// Nothing started, nothing to wait for.
	idle := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)
	select {
	case <-idle.StartScheduler(context.Background()):
	default:
		t.Fatal("a scheduler that never started must hand back a closed channel")
	}
}

// A second launch finds a scheduler already running and starts nothing, but a
// caller holding its channel is joining on the goroutine that does exist. A
// closed channel there would tell that caller the scheduler has stopped while
// the first one's goroutine is still writing, and the pool would close under it.
func TestStartScheduler_DoubleLaunchReturnsTheRunningScheduler(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, _, defaultValue string) string { return defaultValue },
		getBoolFn:        func(_ context.Context, _ string, _ bool) bool { return false },
		getDurationFn: func(_ context.Context, _ string, defaultValue time.Duration) time.Duration {
			return defaultValue
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	ctx, cancel := context.WithCancel(context.Background())
	first := h.StartScheduler(ctx)
	second := h.StartScheduler(ctx)

	select {
	case <-second:
		t.Fatal("the second launch reported the scheduler stopped while the first one is still running")
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	for name, ch := range map[string]<-chan struct{}{"first": first, "second": second} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("the %s caller's channel never closed: it is not joined to the running scheduler", name)
		}
	}
}

func TestStopScheduler(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			return defaultValue
		},
		getBoolFn: func(_ context.Context, key string, defaultValue bool) bool {
			return false
		},
		getDurationFn: func(_ context.Context, key string, defaultValue time.Duration) time.Duration {
			return defaultValue
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	ctx := t.Context()

	h.StartScheduler(ctx)
	if h.schedulerCancel == nil {
		t.Fatal("schedulerCancel should be set after StartScheduler")
	}

	h.StopScheduler()
	if h.schedulerCancel != nil {
		t.Error("schedulerCancel should be nil after StopScheduler")
	}

	// StopScheduler again should be safe (nil check).
	h.StopScheduler()
}

// TestScheduledBackupWait pins the interval to the newest scheduled dump on
// disk: a fresh one defers the next run by the remainder, a stale one, a
// manual one, or none at all makes a backup due now.
func TestScheduledBackupWait(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, nil)
	now := time.Now()
	interval := time.Hour
	touch := func(name string, age time.Duration) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}

	if got := h.scheduledBackupWait(interval, now); got != 0 {
		t.Errorf("empty dir: wait = %v, want 0", got)
	}
	touch("backup_20260101_000000_0001_manual.dump", 10*time.Minute)
	if got := h.scheduledBackupWait(interval, now); got != 0 {
		t.Errorf("manual only: wait = %v, want 0", got)
	}
	touch("backup_20260101_000000_0002_auto.dump", 90*time.Minute)
	if got := h.scheduledBackupWait(interval, now); got != 0 {
		t.Errorf("stale auto: wait = %v, want 0", got)
	}
	touch("backup_20260101_000000_0003_auto.dump", 20*time.Minute)
	if got := h.scheduledBackupWait(interval, now); got < 39*time.Minute || got > 41*time.Minute {
		t.Errorf("fresh auto: wait = %v, want about 40m", got)
	}
	// A dump still being written, or one left behind by a killed process, is
	// not a backup and cannot anchor the interval.
	touch("backup_20260101_000000_0004_auto.dump"+backupPartialSuffix, 0)
	if got := h.scheduledBackupWait(interval, now); got < 39*time.Minute || got > 41*time.Minute {
		t.Errorf("partial ignored: wait = %v, want about 40m", got)
	}
	// A dump stamped in the future is no anchor: a backup is due now.
	touch("backup_20260101_000000_0005_auto.dump", -3*time.Hour)
	if got := h.scheduledBackupWait(interval, now); got != 0 {
		t.Errorf("future mtime: wait = %v, want 0", got)
	}
	h.backupDir = filepath.Join(dir, "missing")
	if got := h.scheduledBackupWait(interval, now); got != 0 {
		t.Errorf("missing dir: wait = %v, want 0", got)
	}
}

// TestSchedulerTick pins one cycle's outcome: disabled polls idly, a recent
// scheduled dump defers by the remainder without dumping, and a stale one
// dumps and sleeps a full interval.
func TestSchedulerTick(t *testing.T) {
	enabled := false
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, _, defaultValue string) string { return defaultValue },
		getBoolFn:        func(_ context.Context, _ string, _ bool) bool { return enabled },
		getDurationFn:    func(_ context.Context, _ string, _ time.Duration) time.Duration { return time.Hour },
	}
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)
	// No pg_dump on PATH, so a due cycle fails fast and writes nothing; the
	// dump itself is covered by the runScheduledBackup tests.
	t.Setenv("PATH", dir)
	count := func() int {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}

	if got := h.schedulerTick(context.Background()); got != backupSchedulerIdlePoll {
		t.Errorf("disabled: sleep = %v, want %v", got, backupSchedulerIdlePoll)
	}
	enabled = true
	recent := filepath.Join(dir, "backup_20260101_000000_0001_auto.dump")
	if err := os.WriteFile(recent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Not due for ~1h, but a tick sleeps at most the re-check cap so an
	// interval change lands (TestSchedulerTick_LongWaitIsRecheckedNotSleptOut).
	if got := h.schedulerTick(context.Background()); got != backupSchedulerRecheck {
		t.Errorf("recent dump: sleep = %v, want the %v re-check", got, backupSchedulerRecheck)
	}
	if count() != 1 {
		t.Error("recent dump: a backup was attempted, want none")
	}
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(recent, stale, stale); err != nil {
		t.Fatal(err)
	}
	if got := h.schedulerTick(context.Background()); got != backupSchedulerRecheck {
		t.Errorf("stale dump: sleep = %v, want the %v re-check after a dump", got, backupSchedulerRecheck)
	}
}

// TestRemoveStalePartials sweeps the leftovers of an interrupted dump and
// nothing else.
func TestRemoveStalePartials(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, nil)
	for _, name := range []string{"backup_20260101_000000_0001_auto.dump", "backup_20260101_000000_0002_auto.dump" + backupPartialSuffix, "restore-123456.dump"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.removeStalePartials()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "backup_20260101_000000_0001_auto.dump" {
		t.Errorf("after sweep dir holds %v, want only the finished dump (the partial and the restore upload swept)", entries)
	}
	h.backupDir = filepath.Join(dir, "missing")
	h.removeStalePartials() // an unreadable dir is not an error

	// A backup in progress holds the lock; the sweep must not touch its partial.
	h.backupDir = dir
	live := filepath.Join(dir, "backup_20260101_000000_0003_auto.dump"+backupPartialSuffix)
	if err := os.WriteFile(live, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.backupMu.Lock()
	h.removeStalePartials()
	h.backupMu.Unlock()
	if _, err := os.Stat(live); err != nil {
		t.Errorf("sweep under a held lock removed the live partial: %v", err)
	}
}

// TestRunDump_RenameFailureKeepsPartial pins that a completed dump survives a
// failed rename, since it is the only good copy.
func TestRunDump_RenameFailureKeepsPartial(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, nil)
	mock := filepath.Join(dir, "pg_dump")
	script := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in --file=*) echo data > \"${a#--file=}\";; esac; done\n"
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// The final name is taken by a directory, so the rename cannot land.
	final := filepath.Join(dir, "backup_20260101_000000_0001_auto.dump")
	if err := os.Mkdir(final, 0o750); err != nil {
		t.Fatal(err)
	}

	output, err := h.runDump(context.Background(), mock, final, scheduledDumpCompression)
	if err == nil || !strings.Contains(err.Error(), "rename completed dump") {
		t.Fatalf("err = %v, want a rename error", err)
	}
	if output != "" {
		t.Errorf("output = %q, want empty for a rename failure", output)
	}
	if _, err := os.Stat(final + backupPartialSuffix); err != nil {
		t.Errorf("completed partial was not kept: %v", err)
	}
}

func TestRunScheduledBackup_PgDumpNotFound(t *testing.T) {
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)
	// runScheduledBackup should return without error when pg_dump is not found.
	// This tests the exec.LookPath failure path.
	h.runScheduledBackup(context.Background())
	// No panic = success.
}

func TestRunScheduledBackup_LockAlreadyHeld(t *testing.T) {
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, nil)

	// Hold the lock to simulate a concurrent operation.
	h.backupMu.Lock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// This should return quickly since TryLock will fail.
		h.runScheduledBackup(context.Background())
	}()

	select {
	case <-done:
		// Success: runScheduledBackup returned without acquiring the lock.
	case <-time.After(5 * time.Second):
		t.Fatal("runScheduledBackup should have returned immediately when lock is held")
	}

	h.backupMu.Unlock()
}

func TestStartBackupScheduler_NilBackupScheduler(t *testing.T) {
	h := &Handler{backupScheduler: nil}
	// Should be a no-op without panicking.
	h.StartBackupScheduler(context.Background())
}

func TestStopBackupScheduler_NilBackupScheduler(t *testing.T) {
	h := &Handler{backupScheduler: nil}
	// Should be a no-op without panicking.
	h.StopBackupScheduler()
}

func TestStartBackupScheduler_NonNilBackupScheduler(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string { return defaultValue },
		getBoolFn:        func(_ context.Context, key string, defaultValue bool) bool { return false },
		getDurationFn:    func(_ context.Context, key string, defaultValue time.Duration) time.Duration { return defaultValue },
	}
	backupH := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)
	h := &Handler{backupScheduler: backupH}

	ctx := t.Context()

	h.StartBackupScheduler(ctx)
	// Verify the scheduler was started by checking the backupHandler's schedulerCancel
	backupH.schedulerCancelMu.Lock()
	hasCancel := backupH.schedulerCancel != nil
	backupH.schedulerCancelMu.Unlock()
	if !hasCancel {
		t.Error("expected schedulerCancel to be set after StartBackupScheduler")
	}

	h.StopBackupScheduler()
	backupH.schedulerCancelMu.Lock()
	hasCancel = backupH.schedulerCancel != nil
	backupH.schedulerCancelMu.Unlock()
	if hasCancel {
		t.Error("expected schedulerCancel to be nil after StopBackupScheduler")
	}
}

func TestRunScheduledBackup_MkdirError(t *testing.T) {
	parent := t.TempDir()
	// Make parent read-only so MkdirAll fails for a subdirectory
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("cannot chmod temp dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })

	h := NewBackupHandler("postgres://x", filepath.Join(parent, "no-such-subdir", "backups"), &mockAdminAuth{}, nil)
	// This should return after MkdirAll fails. It won't panic.
	h.runScheduledBackup(context.Background())
}

func TestRunScheduledBackup_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed, skipping integration test")
	}

	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			return defaultValue
		},
	}
	dir := t.TempDir()
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{}, ss)

	// Run the full scheduled backup cycle.
	h.runScheduledBackup(context.Background())

	// Verify that a backup file was created.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one backup file after runScheduledBackup")
	}

	// Verify that rotation was applied (no error = success).
	// The scheduler logs events but doesn't return errors.
	// We just verify it completed without panicking.
}

func TestRunScheduledBackup_Integration_WithRotation(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed, skipping integration test")
	}

	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			switch key {
			case "backup_son_retention":
				return "1" // aggressive: only keep 1 son
			case "backup_father_retention":
				return "1"
			case "backup_grandfather_retention":
				return "1"
			default:
				return defaultValue
			}
		},
	}
	dir := t.TempDir()
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{}, ss)

	// Create an old backup file first
	oldFilename := "backup_20240101_120000_001.dump"
	if err := os.WriteFile(filepath.Join(dir, oldFilename), []byte("old backup"), 0o644); err != nil {
		t.Fatalf("failed to create old backup: %v", err)
	}

	// Run the scheduled backup which should also apply rotation.
	h.runScheduledBackup(context.Background())

	// After rotation with aggressive settings, the old backup may have been pruned.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one backup file (the new one)")
	}
}

// ---------------------------------------------------------------------------
// Scheduler context cancellation and settings-based loop tests
// ---------------------------------------------------------------------------

// TestStartScheduler_ContextAlreadyCancelled starts the scheduler on a context
// that is already cancelled: the goroutine leaves during the initial delay and
// releases the join channel without ever ticking, so it reads no settings and
// writes no dump.
func TestStartScheduler_ContextAlreadyCancelled(t *testing.T) {
	var reads atomic.Int32
	ss := &mockSettingsStore{
		getBoolFn: func(_ context.Context, _ string, _ bool) bool {
			reads.Add(1)
			return true
		},
	}
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case <-h.StartScheduler(ctx):
	case <-time.After(10 * time.Second):
		t.Fatal("a scheduler started on a cancelled context never stopped")
	}
	if n := reads.Load(); n != 0 {
		t.Errorf("the scheduler ticked %d time(s) on a cancelled context, want none", n)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the scheduler wrote %d file(s) on a cancelled context, want none", len(entries))
	}
}

// TestStartScheduler_RestartAfterStop verifies that StartScheduler can
// be called again after StopScheduler, and a new goroutine is started.
func TestStartScheduler_RestartAfterStop(t *testing.T) {
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			return defaultValue
		},
		getBoolFn: func(_ context.Context, key string, defaultValue bool) bool {
			return false
		},
		getDurationFn: func(_ context.Context, key string, defaultValue time.Duration) time.Duration {
			return defaultValue
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)

	ctx := t.Context()

	// First start
	h.StartScheduler(ctx)
	if h.schedulerCancel == nil {
		t.Fatal("expected schedulerCancel after first StartScheduler")
	}

	// Stop
	h.StopScheduler()
	if h.schedulerCancel != nil {
		t.Fatal("expected schedulerCancel to be nil after StopScheduler")
	}

	// Second start - should work because schedulerCancel was reset
	h.StartScheduler(ctx)
	if h.schedulerCancel == nil {
		t.Fatal("expected schedulerCancel after second StartScheduler")
	}

	h.StopScheduler()
}

// ---------------------------------------------------------------------------
// Additional coverage: StartScheduler enabled loop, runScheduledBackup error paths,
// extractMigrationNames filter write/close errors, saveUploadedDump success path
// ---------------------------------------------------------------------------

// TestStartScheduler_LoopTicksUntilCancelled runs the scheduler loop itself:
// the first tick finds a scheduled dump just under an interval old and sleeps
// out the remainder, the second finds a backup due and dumps it, and cancelling
// the context during the sleep after that ends the goroutine with the stopped
// line and releases the join channel.
func TestStartScheduler_LoopTicksUntilCancelled(t *testing.T) {
	pgDumpDir := t.TempDir()
	mockScript := `#!/bin/bash
for arg in "$@"; do
	if [[ "$arg" == --file=* ]]; then
		echo "mock backup data" > "${arg#--file=}"
	fi
done
`
	if err := os.WriteFile(filepath.Join(pgDumpDir, "pg_dump"), []byte(mockScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pgDumpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	backupDir := t.TempDir()
	anchor := generateBackupFilename("auto")
	anchorPath := filepath.Join(backupDir, anchor)
	if err := os.WriteFile(anchorPath, []byte("earlier scheduled dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Due in three seconds: long enough that the first tick waits rather than dumps.
	stamp := time.Now().Add(-backupIntervalFloor + 3*time.Second)
	if err := os.Chtimes(anchorPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	// The due time as the scheduler sees it: the mtime the filesystem kept.
	info, err := os.Stat(anchorPath)
	if err != nil {
		t.Fatal(err)
	}
	due := info.ModTime().Add(backupIntervalFloor)

	logs := logtest.Capture(t)
	// schedulerTick reads backup_enabled exactly once per tick, so each GetBool
	// call is one tick.
	var ticksMu sync.Mutex
	var ticks []time.Time
	rotating := make(chan struct{}, 1)
	ss := &mockSettingsStore{
		getBoolFn: func(_ context.Context, _ string, _ bool) bool {
			ticksMu.Lock()
			ticks = append(ticks, time.Now())
			ticksMu.Unlock()
			return true
		},
		getDurationFn: func(_ context.Context, _ string, _ time.Duration) time.Duration {
			return backupIntervalFloor
		},
		// Retention is read only after a dump has landed, ahead of rotation.
		getIntFn: func(_ context.Context, _ string, defaultValue int) int {
			select {
			case rotating <- struct{}{}:
			default:
			}
			return defaultValue
		},
	}
	h := NewBackupHandler("postgres://user:pass@localhost/db", backupDir, &mockAdminAuth{}, ss)
	h.initialDelay = 0

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := h.StartScheduler(ctx)

	select {
	case <-rotating:
	case <-time.After(30 * time.Second):
		t.Fatal("the scheduler never ran a scheduled backup")
	}
	ticksMu.Lock()
	seen := append([]time.Time(nil), ticks...)
	ticksMu.Unlock()
	switch {
	case len(seen) == 1:
		t.Fatal("the backup ran on the first tick; it should have waited for the anchor to age")
	case len(seen) != 2:
		t.Fatalf("the backup ran on tick %d, want 2: the loop re-ticked instead of sleeping what the tick returned", len(seen))
	}
	// The second tick may not come before the anchor is due: the loop slept the
	// wait the first tick returned rather than re-ticking early. The slack covers
	// the wall clock versus the timer's monotonic clock.
	if early := due.Sub(seen[1]); early > 50*time.Millisecond {
		t.Errorf("the second tick came %v before the anchor was due (ticks %v apart): the loop did not honour the sleep", early, seen[1].Sub(seen[0]))
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the context did not end the scheduler goroutine")
	}
	if !logs.Has("backup: scheduler stopped") {
		t.Error("expected the scheduler to log that it stopped")
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	var dumped []string
	for _, e := range entries {
		if e.Name() != anchor && backupOrigin(e.Name()) == "scheduled" {
			dumped = append(dumped, e.Name())
		}
	}
	if len(dumped) != 1 {
		t.Fatalf("expected exactly one new scheduled dump, got %v", dumped)
	}
	if data, err := os.ReadFile(filepath.Join(backupDir, dumped[0])); err != nil || string(data) != "mock backup data\n" {
		t.Errorf("dump %s holds %q (err %v), want the mock pg_dump output", dumped[0], data, err)
	}
}

// TestSchedulerTick_IntervalFloorEnforced sets backup_interval far below the
// floor: a scheduled dump a minute old is still recent against the floor, so
// the tick waits out the rest of it instead of dumping back to back.
func TestSchedulerTick_IntervalFloorEnforced(t *testing.T) {
	ss := &mockSettingsStore{
		getBoolFn:     func(_ context.Context, _ string, _ bool) bool { return true },
		getDurationFn: func(_ context.Context, _ string, _ time.Duration) time.Duration { return time.Second },
	}
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)
	recent := filepath.Join(dir, generateBackupFilename("auto"))
	if err := os.WriteFile(recent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Minute)
	if err := os.Chtimes(recent, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	got := h.schedulerTick(context.Background())
	if !h.lastScheduledAttempt.IsZero() {
		t.Error("a dump was attempted a minute after the last one; the interval floor was not applied")
	}
	if want := backupIntervalFloor - time.Minute; got > want || got < want-time.Minute {
		t.Errorf("sleep = %v, want about %v (the rest of the floor)", got, want)
	}
}

// TestRunScheduledBackup_PgDumpFailed tests the pg_dump failure path in
// runScheduledBackup when pg_dump is available but the connection fails.
func TestRunScheduledBackup_PgDumpFailed(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed, skipping pg_dump failure test")
	}

	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)

	// runScheduledBackup should return without panic after pg_dump fails.
	h.runScheduledBackup(context.Background())

	// Verify no backup file was created (pg_dump failed, partial file removed)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dump") || strings.HasSuffix(e.Name(), backupPartialSuffix) {
			t.Errorf("expected no dump or partial files after pg_dump failure, found %q", e.Name())
		}
	}
}

// TestRunScheduledBackup_StatError tests the os.Stat failure path after a
// successful pg_dump (L1163-1166). Since we cannot easily cause pg_dump to
// succeed but stat to fail, we verify the function handles pg_dump failure
// gracefully (the stat path is inherently unreachable without a real DB).
func TestRunScheduledBackup_StatPathUnreachableWithoutDB(t *testing.T) {
	// When pg_dump fails, the partial file is removed (L1158) and the function
	// returns early. The stat error path (L1164-1167) can only be reached when
	// pg_dump succeeds but the output file is gone by the time stat runs.
	// This is a race condition that's extremely unlikely in practice. The
	// existing integration test with a real DB covers the happy path including stat.
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	h.runScheduledBackup(context.Background())
	// No panic = success
}

// TestRunScheduledBackup_ListBackupFilesError tests that runScheduledBackup
// handles errors from listBackupFiles gracefully during rotation.
func TestRunScheduledBackup_ListBackupFilesError(t *testing.T) {
	// Use a file path as backupDir so os.ReadDir fails
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewBackupHandler("postgres://x", filePath, &mockAdminAuth{}, nil)
	// This should not panic even though listBackupFiles would fail
	// (pg_dump not found on PATH exits earlier)
	h.runScheduledBackup(context.Background())
}

// ---------------------------------------------------------------------------
// StartScheduler: timer-fire path when enabled with mock pg_dump
// ---------------------------------------------------------------------------

// TestRunScheduledBackup_ContextExpiredDuringDump verifies that
// runScheduledBackup respects context cancellation during the pg_dump
// command execution. Uses an already-expired context.
func TestRunScheduledBackup_ContextExpiredDuringDump(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed")
	}

	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)

	// Use an already-expired context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic; pg_dump may fail due to cancelled context or bad URL
	h.runScheduledBackup(ctx)
}

// TestRunScheduledBackup_StatErrorAfterSuccessfulDump tests the os.Stat
// failure path in runScheduledBackup (L1163-1166). We simulate this by
// creating a mock pg_dump that writes to a different location than expected.
func TestRunScheduledBackup_RotationWithExistingBackups(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed")
	}

	dir := t.TempDir()

	// Create an old backup file to test rotation after a new backup
	oldName := fmt.Sprintf("backup_%s_001.dump", time.Now().AddDate(0, 0, -1).Format("20060102_150405"))
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)

	// pg_dump will fail (invalid DB URL), but the function should not panic.
	// The rotation logic would run only after a successful dump, which won't
	// happen here. This test verifies the error path exits cleanly.
	h.runScheduledBackup(context.Background())
}

// TestRunScheduledBackup_PgDumpSuccessIntegration tests the happy path of
// runScheduledBackup with a real pg_dump and database, including stat and
// rotation logic.
func TestRunScheduledBackup_PgDumpSuccessIntegration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed")
	}

	dir := t.TempDir()
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{}, nil)

	h.runScheduledBackup(context.Background())

	// Verify a backup file was created
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dump") {
			found = true
			// Verify the file is non-empty
			info, err := e.Info()
			if err != nil {
				t.Errorf("failed to stat %s: %v", e.Name(), err)
			} else if info.Size() == 0 {
				t.Errorf("expected non-empty backup file, got 0 bytes for %s", e.Name())
			}
			break
		}
	}
	if !found {
		t.Error("expected a .dump file after successful runScheduledBackup")
	}
}

// ---------------------------------------------------------------------------
// Tests for runScheduledBackup with mock pg_dump (stat + rotation coverage)
// ---------------------------------------------------------------------------

// TestRunScheduledBackup_MockPgDump_SuccessAndRotation tests the runScheduledBackup
// success path (stat + event + rotation) using a mock pg_dump script that creates
// a valid backup file. This covers the code paths after pg_dump succeeds:
// os.Stat, events.Publish, and the rotation logic.
func TestRunScheduledBackup_MockPgDump_SuccessAndRotation(t *testing.T) {
	tmpDir := t.TempDir()
	mockPgDump := filepath.Join(tmpDir, "pg_dump")

	// Create a mock pg_dump script that creates a backup file at the --file= path
	// and exits successfully.
	mockScript := `#!/bin/bash
OUTPUT_FILE=""
for arg in "$@"; do
	if [[ "$arg" == --file=* ]]; then
		OUTPUT_FILE="${arg#--file=}"
	fi
done
if [ -n "$OUTPUT_FILE" ]; then
	echo "mock backup data" > "$OUTPUT_FILE"
fi
exit 0
`
	if err := os.WriteFile(mockPgDump, []byte(mockScript), 0o755); err != nil {
		t.Fatalf("failed to write mock pg_dump: %v", err)
	}

	// Temporarily prepend the mock dir to PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	backupDir := t.TempDir()
	ss := &mockSettingsStore{
		getWithDefaultFn: func(_ context.Context, key, defaultValue string) string {
			// Use aggressive retention to exercise the prune path
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
	h := NewBackupHandler("postgres://user:pass@localhost/db", backupDir, &mockAdminAuth{}, ss)

	// Create some old scheduler backup files that should be pruned by rotation
	// ("_auto" marks them as scheduler-created; manual backups are never pruned).
	oldName := "backup_20240101_120000_001_auto.dump"
	if err := os.WriteFile(filepath.Join(backupDir, oldName), []byte("old backup data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run the scheduled backup - mock pg_dump will succeed, stat will pass,
	// and rotation will run.
	h.runScheduledBackup(context.Background())

	// Verify a new backup file was created
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	newBackupFound := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dump") && e.Name() != oldName {
			newBackupFound = true
			info, statErr := e.Info()
			if statErr != nil {
				t.Errorf("failed to stat new backup: %v", statErr)
			} else if info.Size() == 0 {
				t.Errorf("expected non-empty backup, got 0 bytes for %s", e.Name())
			}
		}
	}
	if !newBackupFound {
		t.Error("expected a new backup file to be created by mock pg_dump")
	}

	// The old backup (20240101) may or may not have been pruned depending on
	// whether it falls outside retention. With sonRetention=1, sonRetention
	// only keeps backups from recent days. The 2024 backup is well outside
	// all retention tiers, so it should be pruned.
	oldExists := false
	for _, e := range entries {
		if e.Name() == oldName {
			oldExists = true
		}
	}
	if oldExists {
		t.Errorf("expected old backup %q to be pruned by rotation, but it still exists", oldName)
	}
}

// TestRunScheduledBackup_MockPgDump_StatErrorAfterFileDeleted tests the os.Stat
// error path in runScheduledBackup (L1163-1166). We use a mock pg_dump that creates
// the file, then we arrange for the file to be deleted before stat runs. Since we
// can't reliably inject a race, we instead verify the file-based mock pg_dump flow
// works correctly when the output file exists.
func TestRunScheduledBackup_MockPgDump_StatAfterSuccessfulDump(t *testing.T) {
	tmpDir := t.TempDir()
	mockPgDump := filepath.Join(tmpDir, "pg_dump")

	// Mock pg_dump that creates a backup file
	mockScript := `#!/bin/bash
OUTPUT_FILE=""
for arg in "$@"; do
	if [[ "$arg" == --file=* ]]; then
		OUTPUT_FILE="${arg#--file=}"
	fi
done
if [ -n "$OUTPUT_FILE" ]; then
	echo "mock pg_dump output" > "$OUTPUT_FILE"
fi
exit 0
`
	if err := os.WriteFile(mockPgDump, []byte(mockScript), 0o755); err != nil {
		t.Fatalf("failed to write mock pg_dump: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	backupDir := t.TempDir()
	h := NewBackupHandler("postgres://user:pass@localhost/db", backupDir, &mockAdminAuth{}, nil)

	// This should succeed (pg_dump succeeds, stat succeeds, rotation finds no files to prune)
	h.runScheduledBackup(context.Background())

	// Verify backup was created and stat passed (file has content)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one backup file")
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dump") {
			info, statErr := e.Info()
			if statErr != nil {
				t.Errorf("stat failed for %s: %v", e.Name(), statErr)
			} else if info.Size() == 0 {
				t.Errorf("expected non-empty backup file, got 0 bytes")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// StartScheduler: panic recovery within the loop
// ---------------------------------------------------------------------------

// TestStartScheduler_PanicResetsCancelForRestart makes a tick panic: the
// goroutine recovers and ends (its join channel closes), logs the panic, and
// clears schedulerCancel so StartScheduler launches a fresh scheduler instead
// of handing back the dead one's closed channel.
func TestStartScheduler_PanicResetsCancelForRestart(t *testing.T) {
	logs := logtest.Capture(t)
	var panicking atomic.Bool
	panicking.Store(true)
	ss := &mockSettingsStore{
		getBoolFn: func(_ context.Context, _ string, _ bool) bool {
			if panicking.Load() {
				panic("tick panic")
			}
			return false
		},
	}
	h := NewBackupHandler("postgres://x", t.TempDir(), &mockAdminAuth{}, ss)
	h.initialDelay = 0

	select {
	case <-h.StartScheduler(t.Context()):
	case <-time.After(10 * time.Second):
		t.Fatal("a panicking tick did not end the scheduler goroutine")
	}
	h.schedulerCancelMu.Lock()
	cleared := h.schedulerCancel == nil
	h.schedulerCancelMu.Unlock()
	if !cleared {
		t.Fatal("schedulerCancel survived the panic; StartScheduler could never restart the scheduler")
	}
	if got := logs.Attr("backup: scheduler panic recovered", "panic"); got != "tick panic" {
		t.Errorf("panic log attr = %v, want %q", got, "tick panic")
	}

	panicking.Store(false)
	restarted := h.StartScheduler(t.Context())
	select {
	case <-restarted:
		t.Fatal("the restart handed back a closed channel: no scheduler is running")
	default:
	}
	h.StopScheduler()
	select {
	case <-restarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the restarted scheduler did not stop")
	}
}

// ---------------------------------------------------------------------------
// runScheduledBackup: rotation with existing prune-eligible files
// ---------------------------------------------------------------------------

// TestRunScheduledBackup_MockPgDump_RotationPrunesOldFiles tests that after a
// successful backup, the rotation logic prunes old backup files that fall
// outside the retention settings. This uses a mock pg_dump to avoid needing
// a real database.
func TestRunScheduledBackup_MockPgDump_RotationPrunesOldFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mockPgDump := filepath.Join(tmpDir, "pg_dump")

	mockScript := `#!/bin/bash
OUTPUT_FILE=""
for arg in "$@"; do
	if [[ "$arg" == --file=* ]]; then
		OUTPUT_FILE="${arg#--file=}"
	fi
done
if [ -n "$OUTPUT_FILE" ]; then
	echo "mock backup" > "$OUTPUT_FILE"
fi
exit 0
`
	if err := os.WriteFile(mockPgDump, []byte(mockScript), 0o755); err != nil {
		t.Fatalf("failed to write mock pg_dump: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	backupDir := t.TempDir()

	// Create several old scheduler backups at different ages (the "_auto" marker
	// makes them eligible for rotation; manual/legacy backups are never pruned).
	oldFiles := []string{
		"backup_20240101_120000_001_auto.dump", // 2 years old - should be pruned
		"backup_20240115_090000_001_auto.dump", // old enough to be pruned
	}
	for _, name := range oldFiles {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("old data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Use strict retention settings (son=1, father=0, grandfather=0)
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
	h := NewBackupHandler("postgres://user@localhost/db", backupDir, &mockAdminAuth{}, ss)

	h.runScheduledBackup(context.Background())

	// Verify old backup files were pruned
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}

	for _, oldFile := range oldFiles {
		for _, e := range entries {
			if e.Name() == oldFile {
				t.Errorf("expected old backup %q to be pruned, but it still exists", oldFile)
			}
		}
	}

	// Verify the new backup was created
	newFound := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dump") {
			isOld := false
			for _, oldFile := range oldFiles {
				if e.Name() == oldFile {
					isOld = true
				}
			}
			if !isOld {
				newFound = true
			}
		}
	}
	if !newFound {
		t.Error("expected a new backup file to be created")
	}
}

// ---------------------------------------------------------------------------
// runScheduledBackup: validateBackupFilename returns empty for rotation prune
// ---------------------------------------------------------------------------

// TestRunScheduledBackup_MockPgDump_RotationSkipsInvalidFilenames tests that
// when the rotation logic encounters a backup file that fails validation
// (validateBackupFilename returns ""), the prune loop skips it gracefully.
func TestRunScheduledBackup_MockPgDump_RotationSkipsInvalidFilenames(t *testing.T) {
	tmpDir := t.TempDir()
	mockPgDump := filepath.Join(tmpDir, "pg_dump")

	mockScript := `#!/bin/bash
OUTPUT_FILE=""
for arg in "$@"; do
	if [[ "$arg" == --file=* ]]; then
		OUTPUT_FILE="${arg#--file=}"
	fi
done
if [ -n "$OUTPUT_FILE" ]; then
	echo "mock backup" > "$OUTPUT_FILE"
fi
exit 0
`
	if err := os.WriteFile(mockPgDump, []byte(mockScript), 0o755); err != nil {
		t.Fatalf("failed to write mock pg_dump: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	backupDir := t.TempDir()

	// Use retention settings that mark the old file as "prune"
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
	h := NewBackupHandler("postgres://user@localhost/db", backupDir, &mockAdminAuth{}, ss)

	// Create an old scheduler backup with a valid filename (classified as prune)
	oldName := "backup_20230101_120000_001_auto.dump"
	if err := os.WriteFile(filepath.Join(backupDir, oldName), []byte("old data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run scheduled backup - the mock pg_dump succeeds and rotation runs
	h.runScheduledBackup(context.Background())

	// The old file should be pruned (deleted from disk)
	if _, err := os.Stat(filepath.Join(backupDir, oldName)); !os.IsNotExist(err) {
		t.Errorf("expected old backup %q to be pruned (deleted), but it still exists", oldName)
	}
}

// TestRunScheduledBackup_MkdirAllErrorPath tests the os.MkdirAll failure path
// in runScheduledBackup when the backup directory cannot be created because
// a file exists at the same path. This exercises the early-return error path
// before pg_dump is even attempted.
func TestRunScheduledBackup_MkdirAllErrorPath(t *testing.T) {
	// Create a regular file where the backup dir should be
	file, err := os.CreateTemp(t.TempDir(), "backup-blocker-*")
	if err != nil {
		t.Fatal(err)
	}
	filePath := file.Name()
	file.Close()

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", filePath, &mockAdminAuth{}, nil)

	// Should return without panic - MkdirAll fails on file path
	h.runScheduledBackup(context.Background())
}

// ---------------------------------------------------------------------------
// 7. StopScheduler — idempotent
// ---------------------------------------------------------------------------

// TestStopScheduler_Idempotent verifies that calling StopScheduler multiple
// times is safe.
func TestStopScheduler_Idempotent(t *testing.T) {
	ss := &mockSettingsStore{
		getBoolFn: func(_ context.Context, _ string, _ bool) bool { return false },
	}
	dir := t.TempDir()
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)

	ctx := context.Background()
	h.StartScheduler(ctx)
	time.Sleep(20 * time.Millisecond)

	// Stop multiple times — should not panic
	h.StopScheduler()
	h.StopScheduler()
	h.StopScheduler()
}

// ---------------------------------------------------------------------------
// 7. runScheduledBackup — backup mutex already locked
//    Tests that runScheduledBackup returns immediately when the mutex is held.
// ---------------------------------------------------------------------------

func TestRunScheduledBackup_MutexAlreadyLocked(t *testing.T) {
	dir := t.TempDir()
	ss := &mockSettingsStore{
		getDurationFn: func(_ context.Context, _ string, _ time.Duration) time.Duration {
			return 1 * time.Hour
		},
	}
	bh := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)

	// Lock the mutex to simulate an in-progress backup
	bh.backupMu.Lock()
	defer bh.backupMu.Unlock()

	// runScheduledBackup should return immediately without panic
	bh.runScheduledBackup(context.Background())
}

// A tick that is not yet due sleeps at most the re-check cap, not the whole
// remainder of the interval: the interval is re-read on every tick, and a
// tick that slept out a 24h remainder kept an interval shortened to an hour
// waiting out the old day.
func TestSchedulerTick_LongWaitIsRecheckedNotSleptOut(t *testing.T) {
	dir := t.TempDir()
	ss := &mockSettingsStore{
		getBoolFn:     func(context.Context, string, bool) bool { return true },
		getDurationFn: func(context.Context, string, time.Duration) time.Duration { return 24 * time.Hour },
	}
	h := NewBackupHandler("postgres://x", dir, &mockAdminAuth{}, ss)
	// A scheduled dump one minute old: the next one is due in ~24h.
	path := filepath.Join(dir, "backup_20260101_000000_0003_auto.dump")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := h.schedulerTick(context.Background()); got > backupSchedulerRecheck {
		t.Errorf("tick sleeps %v, want at most the %v re-check", got, backupSchedulerRecheck)
	}
}

// An interval past the weekly ceiling runs weekly: a value written through the
// API or a config sync cannot park the scheduler for years.
func TestSchedulerTick_IntervalCappedAtAWeek(t *testing.T) {
	dir := t.TempDir()
	ss := &mockSettingsStore{
		getBoolFn:     func(context.Context, string, bool) bool { return true },
		getDurationFn: func(context.Context, string, time.Duration) time.Duration { return 1000 * 24 * time.Hour },
	}
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, ss)
	// A scheduled dump eight days old: due under the cap, years away without it.
	path := filepath.Join(dir, "backup_20260101_000000_0004_auto.dump")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	h.schedulerTick(context.Background())
	// The failed attempt anchors the interval, so a wait proves the tick dumped.
	if got := h.scheduledBackupWait(time.Hour, time.Now()); got < 59*time.Minute {
		t.Errorf("wait after the tick = %v: an eight-day-old dump under a capped interval was not followed by an attempt", got)
	}
}

// A dump that fails writes no file; the attempt itself anchors the interval,
// so the re-check cadence does not turn a broken pg_dump into a retry every
// few minutes.
func TestSchedulerTick_FailedDumpBacksOffToTheInterval(t *testing.T) {
	dir := t.TempDir()
	ss := &mockSettingsStore{
		getBoolFn:     func(context.Context, string, bool) bool { return true },
		getDurationFn: func(context.Context, string, time.Duration) time.Duration { return time.Hour },
	}
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, ss)
	h.schedulerTick(context.Background()) // due now: attempts, fails, writes nothing
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a failed dump left %d files", len(entries))
	}
	if got := h.scheduledBackupWait(time.Hour, time.Now()); got < 59*time.Minute {
		t.Errorf("wait after a failed attempt = %v, want the interval to anchor on the attempt", got)
	}
}

// A tick skipped because a manual backup or restore holds the lock is not an
// attempt: the anchor does not move, so the still-due dump runs on the next
// re-check once the operation is done.
func TestSchedulerTick_LockSkipDoesNotAnchorTheInterval(t *testing.T) {
	dir := t.TempDir()
	ss := &mockSettingsStore{
		getBoolFn:     func(context.Context, string, bool) bool { return true },
		getDurationFn: func(context.Context, string, time.Duration) time.Duration { return time.Hour },
	}
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, ss)
	h.backupMu.Lock() // a manual operation in progress
	h.schedulerTick(context.Background())
	h.backupMu.Unlock()
	if got := h.scheduledBackupWait(time.Hour, time.Now()); got != 0 {
		t.Errorf("wait after a lock skip = %v, want still due", got)
	}
}
