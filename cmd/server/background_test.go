package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

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
		}, func(context.Context) {}, time.Millisecond)
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
		}, func(context.Context) {}, time.Millisecond)
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

// TestQuotaPollLoop_DisabledAtStartClearsQuotaAdvice verifies that a poll loop
// which starts with polling disabled clears quota advice immediately, rather
// than waiting for a poll pass that (by construction of the disabled branch)
// will never happen.
func TestQuotaPollLoop_DisabledAtStartClearsQuotaAdvice(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var clears atomic.Int32
	done := make(chan struct{})
	go func() {
		quotaPollLoop(ctx, settingsRepo, func(context.Context) {}, func(context.Context) {
			clears.Add(1)
		}, time.Millisecond)
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for clears.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("poll loop starting disabled never cleared quota advice")
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

// TestQuotaPollLoop_TransitionToDisabledClearsQuotaAdvice verifies that a poll
// loop which starts enabled clears quota advice exactly when it transitions
// into the disabled state (not before, while it is still actively polling).
func TestQuotaPollLoop_TransitionToDisabledClearsQuotaAdvice(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "20"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var clears atomic.Int32
	done := make(chan struct{})
	go func() {
		quotaPollLoop(ctx, settingsRepo, func(context.Context) {}, func(context.Context) {
			clears.Add(1)
		}, time.Millisecond)
		close(done)
	}()

	// Give the loop a moment to start in the enabled state.
	time.Sleep(50 * time.Millisecond)
	if clears.Load() != 0 {
		t.Fatal("an enabled poll loop must not clear quota advice")
	}

	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for clears.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("poll loop never cleared quota advice after disabling")
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

// quotaPinAdvisor pins any provider to a deadline far enough out that a circuit
// opening against it always carries a quota override.
type quotaPinAdvisor struct{ at time.Time }

func (q quotaPinAdvisor) ResetsAt(uuid.UUID) (time.Time, bool) { return q.at, true }

func quotaPinnedFor(cb *failover.CircuitBreaker, id uuid.UUID) bool {
	for _, s := range cb.Status() {
		if s.ProviderID == id.String() {
			return s.QuotaPinned
		}
	}
	return false
}

// TestQuotaPollLoop_DisabledSpanReleasesQuotaPins wires the loop to a real
// circuit breaker so the assertion is the operator-visible outcome — the pin is
// gone — rather than "a callback fired".
//
// Turning polling off is documented as switching the feature off on this node.
// If it only stopped *new* pins, a provider pinned moments earlier would stay
// benched for up to 24 hours after the operator disabled the thing that benched
// it, with nothing left running that could ever release it.
func TestQuotaPollLoop_DisabledSpanReleasesQuotaPins(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settingsRepo := newTestSettingsRepo()
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "20"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	cb := failover.NewCircuitBreaker(nil)
	cb.Threshold = 1
	cb.Cooldown = time.Minute
	cb.SetQuotaAdvisor(quotaPinAdvisor{at: time.Now().Add(6 * time.Hour)})
	providerID := uuid.New()
	cb.RecordFailure(providerID, "pinned-provider")
	if !quotaPinnedFor(cb, providerID) {
		t.Fatal("setup: a 6h deadline against a 1m cooldown must pin the circuit")
	}

	var disables atomic.Int32
	done := make(chan struct{})
	go func() {
		quotaPollLoop(ctx, settingsRepo, func(context.Context) {}, func(context.Context) {
			disables.Add(1)
			cb.ReleaseAllQuotaPins()
		}, time.Millisecond)
		close(done)
	}()

	// While polling is enabled the pin is nobody's business but the poller's.
	time.Sleep(50 * time.Millisecond)
	if !quotaPinnedFor(cb, providerID) {
		t.Fatal("an enabled poll loop must leave pins alone")
	}

	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for quotaPinnedFor(cb, providerID) {
		select {
		case <-deadline:
			t.Fatal("disabling polling never released the quota pin")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The circuit itself is untouched: releasing a pin shortens a cooldown, it
	// does not decide the provider is healthy.
	if got := cb.GetState(providerID); got != failover.StateOpen {
		t.Errorf("got state %v after the release, want open", got)
	}

	// Repeated settings events during the same disabled span must not re-run the
	// hook: it is guarded once per span, and a loop that re-ran it on every
	// wakeup would be doing unbounded work for a setting that has not changed.
	for range 3 {
		if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
			t.Fatalf("set failed: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if got := disables.Load(); got != 1 {
		t.Errorf("got %d disable hook call(s) in one disabled span, want 1", got)
	}

	// Re-enabling arms a fresh span: the pin the poller applies from here is
	// governed by the ordinary recovery rules again, and disabling once more
	// releases it a second time.
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "20"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	// Let the loop actually leave the disabled branch first, so the release
	// below is unambiguously the second span's doing.
	time.Sleep(50 * time.Millisecond)
	cb.Reset(providerID)
	cb.RecordFailure(providerID, "pinned-provider")
	if !quotaPinnedFor(cb, providerID) {
		t.Fatal("setup: the circuit must carry a pin again before the second disable")
	}
	if err := settingsRepo.Set(ctx, "quota_refresh_interval_min", "0"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	secondDeadline := time.After(5 * time.Second)
	for quotaPinnedFor(cb, providerID) {
		select {
		case <-secondDeadline:
			t.Fatal("a second disabled span never released the new pin")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := disables.Load(); got != 2 {
		t.Errorf("got %d disable hook call(s) across two disabled spans, want 2", got)
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
