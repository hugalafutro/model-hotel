package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
)

func newTestSettingsRepo() *settings.Repository {
	return settings.NewRepository(cmdTestDB.Pool())
}

func TestCleanupInterruptedRequests(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx := context.Background()
	pool := cmdTestDB.Pool()
	if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO request_logs (state, created_at) VALUES ('pending', now() - interval '1 hour')`); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	cleanupInterruptedRequests(pool, time.Now())

	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM request_logs`).Scan(&state); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if state != "failed" {
		t.Errorf("expected interrupted request marked failed, got %q", state)
	}
	waitForEvent(t, ch, "logs.stale_startup")
}

func TestCleanupInterruptedRequestsDBError(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	broken := closedTestPool(t)
	// Only logs; must not panic on a dead pool.
	cleanupInterruptedRequests(broken.Pool(), time.Now())
}

func TestWarmCaches(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	ctx := context.Background()
	deps := testDiscoveryDeps(t)

	// One enabled provider with real key material so the Argon2id warm runs.
	kp, err := auth.Encrypt("sk-test-warm", deps.cfg.MasterKey)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if _, err := deps.providerRepo.Create(ctx, provider.CreateProviderRequest{
		Name:    "cmdserver-warm-test",
		BaseURL: "http://127.0.0.1:1/v1",
		APIKey:  "sk-test-warm",
	}, kp.Ciphertext, kp.Nonce, kp.Salt); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	warmCaches(deps, newTestSettingsRepo())
}

func TestWarmCachesDBErrors(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	deps := testDiscoveryDeps(t)
	broken := closedTestPool(t)
	deps.pool = broken.Pool()
	deps.providerRepo = provider.NewRepository(broken.Pool())
	deps.modelRepo = model.NewRepository(broken.Pool())
	deps.failoverRepo = failover.NewRepository(broken.Pool())
	// Only logs the three list errors; must not panic on a dead pool.
	warmCaches(deps, newTestSettingsRepo())
}

func TestInitKeyCacheTTL(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx := context.Background()
	settingsRepo := newTestSettingsRepo()
	t.Cleanup(func() {
		auth.SetKeyCacheTTL(auth.DefaultKeyCacheTTL)
	})

	initKeyCacheTTL(settingsRepo)

	// A valid change is applied, an invalid one keeps the current value, and
	// unrelated keys are ignored — all delivered through the change callback.
	if err := settingsRepo.Set(ctx, "key_cache_ttl", "123ms"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := settingsRepo.Set(ctx, "key_cache_ttl", "bogus"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := settingsRepo.Set(ctx, "some_other_key", "x"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
}

func TestDiscoverySchedulerLoop(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "discovery_interval", "20ms"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		discoverySchedulerLoop(ctx, settingsRepo, func(source string) DiscoveryResult {
			if source != "scheduled" {
				t.Errorf("expected scheduled source, got %q", source)
			}
			runs.Add(1)
			return DiscoveryResult{}
		})
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for runs.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("scheduler never ran discovery")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// A different interval reaches the loop through the settings subscription
	// and resets the live timer.
	if err := settingsRepo.Set(ctx, "discovery_interval", "30ms"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Interval 0 disables the timer: the loop parks on the settings
	// subscription until cancellation.
	if err := settingsRepo.Set(ctx, "discovery_interval", "0s"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop on context cancellation")
	}
}

// TestDiscoverySchedulerLoopDisabledAtStart covers the branch where the
// scheduler starts with discovery disabled: it parks on the settings
// subscription immediately, wakes when a real interval arrives, and still
// stops on cancellation.
func TestDiscoverySchedulerLoopDisabledAtStart(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "discovery_interval", "0s"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		discoverySchedulerLoop(ctx, settingsRepo, func(string) DiscoveryResult {
			runs.Add(1)
			return DiscoveryResult{}
		})
		close(done)
	}()

	// Give the loop a moment to park in the disabled branch, then enable.
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 0 {
		t.Fatal("disabled scheduler must not run discovery")
	}
	if err := settingsRepo.Set(ctx, "discovery_interval", "20ms"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for runs.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("scheduler never woke from the disabled state")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop on context cancellation")
	}
}

func TestStaleLogCleanupPass(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx := context.Background()
	pool := cmdTestDB.Pool()
	settingsRepo := newTestSettingsRepo()

	t.Run("disabled_by_zero_timeout", func(t *testing.T) {
		if err := settingsRepo.Set(ctx, "stale_request_timeout", "0s"); err != nil {
			t.Fatalf("set failed: %v", err)
		}
		defer func() { _ = settingsRepo.Set(ctx, "stale_request_timeout", "30m") }()
		staleLogCleanupPass(pool, settingsRepo, time.Now())
	})

	t.Run("marks_stale_rows", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (state, created_at) VALUES ('streaming', now() - interval '2 hours')`); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		ch := events.DefaultBus.Subscribe()
		defer events.DefaultBus.Unsubscribe(ch)

		staleLogCleanupPass(pool, settingsRepo, time.Now())

		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM request_logs`).Scan(&state); err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if state != "failed" {
			t.Errorf("expected stale request marked failed, got %q", state)
		}
		waitForEvent(t, ch, "logs.stale_cleanup")
	})

	t.Run("db_error_only_logs", func(t *testing.T) {
		broken := closedTestPool(t)
		staleLogCleanupPass(broken.Pool(), settingsRepo, time.Now())
	})
}

func TestLogRetentionPass(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx := context.Background()
	pool := cmdTestDB.Pool()
	settingsRepo := newTestSettingsRepo()
	setRetention := func(v string) {
		t.Helper()
		if err := settingsRepo.Set(ctx, "log_retention", v); err != nil {
			t.Fatalf("set failed: %v", err)
		}
	}
	t.Cleanup(func() { _ = settingsRepo.Set(ctx, "log_retention", "") })

	t.Run("unset_skips", func(t *testing.T) {
		setRetention("")
		logRetentionPass(pool, settingsRepo)
	})

	t.Run("unrecognised_skips", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (state, created_at) VALUES ('completed', now() - interval '10 days')`); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		setRetention("0")
		logRetentionPass(pool, settingsRepo)
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM request_logs`).Scan(&n); err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if n != 1 {
			t.Errorf("expected disabled retention to keep the row, got %d rows", n)
		}
	})

	t.Run("deletes_old_rows", func(t *testing.T) {
		// The 10-day-old row from the previous subtest is older than 1 week.
		setRetention("1w")
		logRetentionPass(pool, settingsRepo)
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM request_logs`).Scan(&n); err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if n != 0 {
			t.Errorf("expected old row deleted, got %d rows", n)
		}
	})

	t.Run("other_retention_windows", func(t *testing.T) {
		for _, v := range []string{"1h", "24h", "720h"} {
			setRetention(v)
			logRetentionPass(pool, settingsRepo)
		}
	})
}

func TestQuotaPollLoop_RunsOnInterval(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	// Interpret the interval in milliseconds so the timer fires within the
	// test window instead of waiting real minutes.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "20"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		quotaPollLoop(ctx, settingsRepo, func(context.Context) {
			calls.Add(1)
		}, time.Millisecond)
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("poll loop never ran")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// A different interval reaches the loop through the settings subscription
	// and resets the live timer. Assert the loop keeps firing on the new cadence
	// rather than just sleeping and hoping.
	before := calls.Load()
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "30"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	resetDeadline := time.After(5 * time.Second)
	for calls.Load() <= before {
		select {
		case <-resetDeadline:
			t.Fatal("poll loop did not fire after interval reset")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Interval 0 disables the timer: the loop parks on the settings
	// subscription until cancellation.
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("poll loop did not stop on context cancellation")
	}
}

// TestQuotaPollLoopDisabledAtStart covers the branch where the poll loop
// starts with polling disabled: it parks on the settings subscription
// immediately, wakes when a real interval arrives, and still stops on
// cancellation.
func TestQuotaPollLoopDisabledAtStart(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		quotaPollLoop(ctx, settingsRepo, func(context.Context) {
			calls.Add(1)
		}, time.Millisecond)
		close(done)
	}()

	// Give the loop a moment to park in the disabled branch, then enable.
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("disabled poll loop must not run")
	}
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "20"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("poll loop never woke from disabled state")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("poll loop did not stop on context cancellation")
	}
}

func TestStaleLogCleanupLoopStopsOnCancel(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		staleLogCleanupLoop(ctx, cmdTestDB.Pool(), newTestSettingsRepo(), time.Now())
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stale log cleanup loop did not stop on cancellation")
	}
}

func TestLogRetentionLoopStopsOnCancel(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		logRetentionLoop(ctx, cmdTestDB.Pool(), newTestSettingsRepo())
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("log retention loop did not stop on cancellation")
	}
}
