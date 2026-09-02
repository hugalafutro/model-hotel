package main

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
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
	// The warm also seeds the credential mask's held set, synchronously, so
	// the set is complete before the listener opens.
	if !slices.Contains(util.HeldSecrets(), "sk-test-warm") {
		t.Fatal("warmCaches did not hold the provider key for the credential mask")
	}
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

	t.Run("slider_value_deletes", func(t *testing.T) {
		// The dashboard slider stores day counts as "<hours>h"; every stop
		// must prune both tables.
		if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM app_logs`); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (state, created_at) VALUES ('completed', now() - interval '3 days')`); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO app_logs (timestamp, level, source, message, created_at) VALUES (now() - interval '3 days', 'info', 'test', 'old', now() - interval '3 days')`); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		setRetention("48h")
		logRetentionPass(pool, settingsRepo)
		for _, table := range []string{"request_logs", "app_logs"} {
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
				t.Fatalf("count failed: %v", err)
			}
			if n != 0 {
				t.Errorf("expected 48h retention to delete the 3-day-old %s row, got %d rows", table, n)
			}
		}
	})

	t.Run("garbage_value_keeps_rows_and_warns_once", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (state, created_at) VALUES ('completed', now() - interval '3 days')`); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		var logs strings.Builder
		debuglog.SetHandler(slog.NewTextHandler(&logs, nil))
		defer debuglog.SetHandler(debuglog.StdoutHandler())
		setRetention("soon")
		logRetentionPass(pool, settingsRepo)
		logRetentionPass(pool, settingsRepo)
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM request_logs`).Scan(&n); err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if n != 1 {
			t.Errorf("expected an unparseable retention to keep the row, got %d rows", n)
		}
		if got := strings.Count(logs.String(), "not understood"); got != 1 {
			t.Errorf("expected exactly one warning for a repeated bad value, got %d in:\n%s", got, logs.String())
		}
	})

	t.Run("zero_duration_disables_silently", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO request_logs (state, created_at) VALUES ('completed', now() - interval '3 days')`); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		var logs strings.Builder
		debuglog.SetHandler(slog.NewTextHandler(&logs, nil))
		defer debuglog.SetHandler(debuglog.StdoutHandler())
		setRetention("0s")
		logRetentionPass(pool, settingsRepo)
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM request_logs`).Scan(&n); err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if n != 1 {
			t.Errorf("expected \"0s\" to disable retention, got %d rows", n)
		}
		if strings.Contains(logs.String(), "not understood") {
			t.Errorf("\"0s\" is the disabled form, not a bad value; got warning:\n%s", logs.String())
		}
	})

	t.Run("other_retention_windows", func(t *testing.T) {
		for _, v := range []string{"1h", "24h", "720h"} {
			setRetention(v)
			logRetentionPass(pool, settingsRepo)
		}
	})

	t.Run("db_error_only_logs", func(t *testing.T) {
		setRetention("24h")
		logRetentionPass(closedTestPool(t).Pool(), settingsRepo)
	})
}

func TestParseLogRetention(t *testing.T) {
	cases := []struct {
		raw     string
		want    time.Duration
		enabled bool
		bad     bool
	}{
		{"", 0, false, false},
		{"0", 0, false, false},
		{"0s", 0, false, false},
		{"0h", 0, false, false},
		{"0h0m0s", 0, false, false},
		{"-1h", 0, false, false},
		{"soon", 0, false, true},
		{"6", 0, false, true},
		{"30", 0, false, true},
		{"1h", time.Hour, true, false},
		{"1h30m", 90 * time.Minute, true, false},
		{"1d", 24 * time.Hour, true, false},
		{"1w", 7 * 24 * time.Hour, true, false},
		{"1m", 30 * 24 * time.Hour, true, false},
		{"24h", 24 * time.Hour, true, false},
		{"48h", 48 * time.Hour, true, false},
		{"144h", 144 * time.Hour, true, false},
		{"168h0m0s", 168 * time.Hour, true, false},
		{"720h", 720 * time.Hour, true, false},
		{"720h0m0s", 720 * time.Hour, true, false},
	}
	for _, c := range cases {
		got, enabled, err := parseLogRetention(c.raw)
		if (err != nil) != c.bad || enabled != c.enabled || got != c.want {
			t.Errorf("parseLogRetention(%q) = (%v, %v, %v), want (%v, %v, bad=%v)", c.raw, got, enabled, err, c.want, c.enabled, c.bad)
		}
	}
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
	cb.RecordFailure(providerID, "pinned-provider", "", failover.Cause{})
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
	if got := cb.GetState(providerID, ""); got != failover.StateOpen {
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
	cb.RecordFailure(providerID, "pinned-provider", "", failover.Cause{})
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

// insertScheduledProvider writes a provider row straight to the table, with the
// key material the schema demands. sched is a YYYY-MM-DD date, or "" for a
// provider with no scheduled disable.
func insertScheduledProvider(t *testing.T, name string, enabled bool, sched string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := cmdTestDB.Pool().QueryRow(context.Background(), `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt, enabled, autodiscovery_enabled, scheduled_disable_on)
		VALUES ($1, 'https://example.invalid/v1', '\x00'::bytea, '\x00'::bytea, '\x00'::bytea, $2, true, NULLIF($3, '')::date)
		RETURNING id`, name, enabled, sched).Scan(&id)
	if err != nil {
		t.Fatalf("insert provider %q failed: %v", name, err)
	}
	return id
}

// assertProviderState checks the two columns the sweep owns.
func assertProviderState(t *testing.T, id uuid.UUID, label string, wantEnabled, wantScheduled bool) {
	t.Helper()
	var enabled bool
	var sched *time.Time
	if err := cmdTestDB.Pool().QueryRow(context.Background(),
		`SELECT enabled, scheduled_disable_on FROM providers WHERE id = $1`, id).Scan(&enabled, &sched); err != nil {
		t.Fatalf("%s: query failed: %v", label, err)
	}
	if enabled != wantEnabled {
		t.Errorf("%s: enabled = %v, want %v", label, enabled, wantEnabled)
	}
	if (sched != nil) != wantScheduled {
		t.Errorf("%s: scheduled_disable_on = %v, want scheduled=%v", label, sched, wantScheduled)
	}
}

// collectScheduledDisableEvents drains ch until want events of the scheduled
// disable type have arrived, returning the provider names they name.
func collectScheduledDisableEvents(t *testing.T, ch chan events.Event, want int) map[string]string {
	t.Helper()
	got := make(map[string]string, want)
	deadline := time.After(5 * time.Second)
	for len(got) < want {
		select {
		case ev := <-ch:
			if ev.Type != "provider.scheduled_disable" {
				continue
			}
			if ev.Severity != "warning" {
				t.Errorf("event severity = %q, want warning", ev.Severity)
			}
			name, _ := ev.Metadata["provider"].(string)
			if name == "" {
				t.Fatalf("event carries no provider name: %+v", ev.Metadata)
			}
			if _, dup := got[name]; dup {
				t.Fatalf("provider %q announced more than once", name)
			}
			id, _ := ev.Metadata["provider_id"].(string)
			got[name] = id
			if wantMsg := "Provider '" + name + "' disabled as scheduled"; ev.Message != wantMsg {
				t.Errorf("event message = %q, want %q", ev.Message, wantMsg)
			}
		case <-deadline:
			t.Fatalf("timed out after %d of %d provider.scheduled_disable events", len(got), want)
		}
	}
	return got
}

// assertNoScheduledDisableEvent fails if another disable event shows up in the
// grace window: the sweep announces exactly one event per fired provider.
func assertNoScheduledDisableEvent(t *testing.T, ch chan events.Event) {
	t.Helper()
	grace := time.After(200 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "provider.scheduled_disable" {
				t.Fatalf("unexpected scheduled disable event: %s", ev.Message)
			}
		case <-grace:
			return
		}
	}
}

func TestSweepScheduledDisables(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	ctx := context.Background()
	pool := cmdTestDB.Pool()
	providerRepo := provider.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)

	now := time.Now()
	due := insertScheduledProvider(t, "sched-due-yesterday", true, now.AddDate(0, 0, -1).Format("2006-01-02"))
	dueToday := insertScheduledProvider(t, "sched-due-today", true, now.Format("2006-01-02"))
	future := insertScheduledProvider(t, "sched-future", true, now.AddDate(0, 0, 2).Format("2006-01-02"))
	// A disabled provider with a leftover schedule only exists if written
	// directly to the DB, since the update path forces the date to NULL on
	// disable. The sweep must still leave it alone.
	alreadyOff := insertScheduledProvider(t, "sched-already-off", false, now.Format("2006-01-02"))
	unscheduled := insertScheduledProvider(t, "sched-unscheduled", true, "")

	// Cached rows carry the old enabled state; the sweep has to drop them the
	// way a manual disable does.
	if _, err := providerRepo.Get(ctx, due); err != nil {
		t.Fatalf("warm cache failed: %v", err)
	}
	if _, ok := provider.GetCachedByID(due); !ok {
		t.Fatal("setup: provider must be cached before the sweep")
	}

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	if n := sweepScheduledDisables(ctx, providerRepo, failoverRepo); n != 2 {
		t.Fatalf("disabled %d providers, want 2", n)
	}

	assertProviderState(t, due, "due-yesterday", false, false)
	assertProviderState(t, dueToday, "due-today", false, false)
	assertProviderState(t, future, "future", true, true)
	assertProviderState(t, alreadyOff, "already-off", false, true)
	assertProviderState(t, unscheduled, "unscheduled", true, false)

	if _, ok := provider.GetCachedByID(due); ok {
		t.Error("sweep left a stale provider cache entry behind")
	}

	fired := collectScheduledDisableEvents(t, ch, 2)
	if fired["sched-due-yesterday"] != due.String() {
		t.Errorf("due-yesterday event carries provider_id %q, want %s", fired["sched-due-yesterday"], due)
	}
	if fired["sched-due-today"] != dueToday.String() {
		t.Errorf("due-today event carries provider_id %q, want %s", fired["sched-due-today"], dueToday)
	}
	assertNoScheduledDisableEvent(t, ch)
}

func TestSweepScheduledDisables_NothingDue(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	ctx := context.Background()
	pool := cmdTestDB.Pool()
	future := insertScheduledProvider(t, "sched-nothing-due", true, time.Now().AddDate(0, 0, 3).Format("2006-01-02"))

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	if n := sweepScheduledDisables(ctx, provider.NewRepository(pool), failover.NewRepository(pool)); n != 0 {
		t.Fatalf("disabled %d providers, want 0", n)
	}
	assertProviderState(t, future, "future", true, true)
	assertNoScheduledDisableEvent(t, ch)
}

// TestSweepScheduledDisables_CompletesOnCancelledContext pins the shutdown
// behaviour: the sweep drops its caller's cancellation, because the UPDATE
// clears the schedule as it fires and a sweep abandoned midway would strand the
// disable with no event and no way for a later sweep to notice.
func TestSweepScheduledDisables_CompletesOnCancelledContext(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	pool := cmdTestDB.Pool()
	due := insertScheduledProvider(t, "sched-cancelled-ctx", true, time.Now().Format("2006-01-02"))

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if n := sweepScheduledDisables(ctx, provider.NewRepository(pool), failover.NewRepository(pool)); n != 1 {
		t.Fatalf("disabled %d providers under a cancelled context, want 1", n)
	}
	assertProviderState(t, due, "cancelled-ctx", false, false)
	if fired := collectScheduledDisableEvents(t, ch, 1); fired["sched-cancelled-ctx"] != due.String() {
		t.Errorf("event carries provider_id %q, want %s", fired["sched-cancelled-ctx"], due)
	}
}

func TestSweepScheduledDisablesDBError(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	broken := closedTestPool(t)
	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	// A dead pool only logs: no panic, nothing disabled, nothing announced.
	if n := sweepScheduledDisables(context.Background(), provider.NewRepository(broken.Pool()), failover.NewRepository(broken.Pool())); n != 0 {
		t.Fatalf("disabled %d providers on a dead pool, want 0", n)
	}
	assertNoScheduledDisableEvent(t, ch)
}

// TestScheduledDisableLoop_SweepsAtStartup pins the property a restart across
// midnight depends on: the loop sweeps once before its first tick, so a disable
// whose day arrived while the process was down still fires.
func TestScheduledDisableLoop_SweepsAtStartup(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	pool := cmdTestDB.Pool()
	due := insertScheduledProvider(t, "sched-loop-startup", true, time.Now().AddDate(0, 0, -1).Format("2006-01-02"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		// An hour-long tick guarantees the disable below is the startup
		// sweep's doing rather than a tick's.
		scheduledDisableLoop(ctx, provider.NewRepository(pool), failover.NewRepository(pool), time.Hour)
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for {
		var enabled bool
		if err := pool.QueryRow(ctx, `SELECT enabled FROM providers WHERE id = $1`, due).Scan(&enabled); err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if !enabled {
			break
		}
		select {
		case <-deadline:
			t.Fatal("scheduled disable loop never swept at startup")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled disable loop did not stop on context cancellation")
	}
}

// TestScheduledDisableLoop_SweepsOnTick covers the ticker arm: a schedule that
// becomes due after the startup sweep still fires without a restart.
func TestScheduledDisableLoop_SweepsOnTick(t *testing.T) {
	if cmdTestDB == nil {
		t.Fatal("test DB unavailable")
	}
	wipeDiscoveryState(t)
	pool := cmdTestDB.Pool()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		scheduledDisableLoop(ctx, provider.NewRepository(pool), failover.NewRepository(pool), 10*time.Millisecond)
		close(done)
	}()

	// Inserted after the loop started, so only a tick can pick it up.
	time.Sleep(50 * time.Millisecond)
	due := insertScheduledProvider(t, "sched-loop-tick", true, time.Now().Format("2006-01-02"))

	deadline := time.After(5 * time.Second)
	for {
		var enabled bool
		if err := pool.QueryRow(ctx, `SELECT enabled FROM providers WHERE id = $1`, due).Scan(&enabled); err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if !enabled {
			break
		}
		select {
		case <-deadline:
			t.Fatal("scheduled disable loop never swept on tick")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled disable loop did not stop on context cancellation")
	}
}
