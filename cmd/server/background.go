package main

// Background maintenance loops for the server binary: the periodic discovery
// scheduler, quota polling, stale request-log cleanup, log retention, scheduled
// provider disables, and WebAuthn session pruning. Each runs for the app
// lifetime and exits on ctx cancellation.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// discoverySchedulerLoop runs periodic discovery based on the settings
// interval. The first run waits a full interval so it doesn't bypass the
// discovery_on_startup setting: when that is true, the startup runner already
// handles immediate discovery; when false, we must not discover on startup
// either.
//
// The timer reacts immediately to discovery_interval changes via the settings
// subscription channel, instead of waiting for the current timer to expire.
// An interval of 0 ("Disabled") truly disables periodic discovery rather than
// resetting to a default: the loop blocks on the subscription channel until a
// non-zero value arrives.
func discoverySchedulerLoop(ctx context.Context, settingsRepo *settings.Repository, runDisc func(source string) DiscoveryResult) {
	const defaultInterval = 6 * time.Hour

	readInterval := func() time.Duration {
		return settingsRepo.GetDuration(context.Background(), "discovery_interval", defaultInterval)
	}

	settingsSub := settingsRepo.Subscribe()
	defer settingsSub.Unsubscribe()

	// applyInterval sets up the timer for the given interval. If the
	// interval is <= 0 (disabled) the timer is stopped and set to nil.
	// A nil timer channel blocks forever in select, which is exactly what
	// we want for the disabled state — only the settings subscription
	// and the context cancellation can wake us.
	var timer *time.Timer
	var timerC <-chan time.Time
	applyInterval := func(d time.Duration) {
		if d <= 0 {
			// Transitioning to disabled: stop and drain the existing timer.
			if timer != nil {
				timer.Stop()
				// Drain the channel if the timer already fired, so the
				// receive does not leak into the next cycle.
				select {
				case <-timer.C:
				default:
				}
				timer = nil
				timerC = nil
			}
		} else {
			// Transitioning to enabled: reset (or create) the timer.
			if timer != nil {
				timer.Stop()
				select {
				case <-timer.C:
				default:
				}
				timer.Reset(d)
			} else {
				timer = time.NewTimer(d)
				// NewTimer already starts with duration d; no Reset needed.
			}
			timerC = timer.C
		}
	}

	interval := readInterval()
	applyInterval(interval)

	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		if interval <= 0 {
			// Discovery is disabled. Block until the setting changes
			// or the server shuts down. We cannot reach the main
			// select because timerC is nil (blocks forever).
			select {
			case <-settingsSub.Events():
				interval = readInterval()
				applyInterval(interval)
			case <-ctx.Done():
				return
			}
			continue
		}

		select {
		case <-timerC:
			result := runDisc("scheduled")
			publishDiscoveryEvent("Scheduled", result)
			// Re-read interval in case it changed since the last
			// subscription event was processed.
			interval = readInterval()
			applyInterval(interval)

		case <-settingsSub.Events():
			// Re-read from DB (the source of truth) rather than
			// parsing the event value, which may be empty if the
			// setting was deleted or the lookup failed.
			newInterval := readInterval()
			if newInterval != interval {
				interval = newInterval
				applyInterval(interval)
			}

		case <-ctx.Done():
			return
		}
	}
}

// quotaPollLoop periodically refreshes provider quota snapshots based on the
// quota_refresh_interval_min setting. It mirrors discoverySchedulerLoop: the
// first run waits a full interval, the timer reacts immediately to interval
// changes via the settings subscription channel, and an interval of 0
// ("Disabled") truly disables polling — the loop blocks on the subscription
// channel until a non-zero value arrives.
//
// onDisabled is invoked once whenever the loop enters (or starts in) the
// disabled state. pollOnce — and with it api.Handler.RefreshQuotaAdvice — is
// never called while disabled, so without this the last computed quota advice
// map would otherwise be retained in memory for the rest of the process
// lifetime, potentially pinning a circuit's cooldown to a deadline computed
// from data that predates the disable. It also releases the pins already in
// force: nothing will ever report a recovery once polling is off, so a pin left
// standing would be served out to its ceiling on evidence the operator
// deliberately stopped collecting.
//
// onDisabled does no upstream fetches (it is api.Handler.DisableQuotaAdvice: a
// map swap and an in-memory pass over the breaker), so it is cheap enough to
// call redundantly; it is deliberately not called again for as long as the loop
// stays disabled.
func quotaPollLoop(ctx context.Context, settingsRepo *settings.Repository, pollOnce, onDisabled func(context.Context), unit time.Duration) {
	readInterval := func() time.Duration {
		return time.Duration(settingsRepo.GetInt(context.Background(), "quota_refresh_interval_min", 5)) * unit
	}

	settingsSub := settingsRepo.Subscribe()
	defer settingsSub.Unsubscribe()

	// applyInterval sets up the timer for the given interval. If the
	// interval is <= 0 (disabled) the timer is stopped and set to nil.
	// A nil timer channel blocks forever in select, which is exactly what
	// we want for the disabled state — only the settings subscription
	// and the context cancellation can wake us.
	var timer *time.Timer
	var timerC <-chan time.Time
	applyInterval := func(d time.Duration) {
		if d <= 0 {
			// Transitioning to disabled: stop and drain the existing timer.
			if timer != nil {
				timer.Stop()
				// Drain the channel if the timer already fired, so the
				// receive does not leak into the next cycle.
				select {
				case <-timer.C:
				default:
				}
				timer = nil
				timerC = nil
			}
		} else {
			// Transitioning to enabled: reset (or create) the timer.
			if timer != nil {
				timer.Stop()
				select {
				case <-timer.C:
				default:
				}
				timer.Reset(d)
			} else {
				timer = time.NewTimer(d)
				// NewTimer already starts with duration d; no Reset needed.
			}
			timerC = timer.C
		}
	}

	interval := readInterval()
	applyInterval(interval)

	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	// advisorCleared tracks whether onDisabled has already run for the
	// current disabled span, so repeated settings events while still
	// disabled don't call it again on every wakeup.
	advisorCleared := false

	for {
		if interval <= 0 {
			if !advisorCleared {
				onDisabled(ctx)
				advisorCleared = true
			}
			// Polling is disabled. Block until the setting changes
			// or the server shuts down. We cannot reach the main
			// select because timerC is nil (blocks forever).
			select {
			case <-settingsSub.Events():
				interval = readInterval()
				applyInterval(interval)
			case <-ctx.Done():
				return
			}
			continue
		}
		advisorCleared = false

		select {
		case <-timerC:
			pollOnce(ctx)
			// Re-read interval in case it changed since the last
			// subscription event was processed.
			interval = readInterval()
			applyInterval(interval)

		case <-settingsSub.Events():
			// Re-read from DB (the source of truth) rather than
			// parsing the event value, which may be empty if the
			// setting was deleted or the lookup failed.
			newInterval := readInterval()
			if newInterval != interval {
				interval = newInterval
				applyInterval(interval)
			}

		case <-ctx.Done():
			return
		}
	}
}

// staleLogCleanupLoop periodically marks rows stuck in "pending"/"streaming"
// as "failed". Two strategies are combined in a single pass:
//
//  1. Server-start-time check: any in-progress row that predates this
//     process is definitively orphaned (the previous process is dead).
//     This has zero false-positive risk regardless of request duration.
//
//  2. Age-based check: rows older than stale_request_timeout (default
//     30m, configurable via Settings) are also marked failed. This
//     catches in-process orphans (e.g. a panic skips the final
//     updateRequestLog). The timeout is generous to avoid killing
//     legitimate long-running streaming requests.
func staleLogCleanupLoop(ctx context.Context, pool *pgxpool.Pool, settingsRepo *settings.Repository, serverStartTime time.Time) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		staleLogCleanupPass(pool, settingsRepo, serverStartTime)
		timer.Reset(5 * time.Minute)
	}
}

// staleLogCleanupPass runs one stale-log sweep; a stale_request_timeout of 0
// disables the age-based check for this cycle.
func staleLogCleanupPass(pool *pgxpool.Pool, settingsRepo *settings.Repository, serverStartTime time.Time) {
	staleTimeout := settingsRepo.GetDuration(context.Background(), "stale_request_timeout", 30*time.Minute)
	if staleTimeout <= 0 {
		return
	}
	// Build a PostgreSQL-safe interval string from the parsed duration.
	// Truncate to whole seconds to avoid fractional-unit issues (e.g. "30.5 minutes").
	totalSecs := int64(staleTimeout.Seconds())
	hours := totalSecs / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60
	intervalStr := fmt.Sprintf("%d hours %d minutes %d seconds", hours, mins, secs)
	tag, err := pool.Exec(context.Background(), `
		UPDATE request_logs
		SET state = 'failed', error_kind = 'internal', error_message = 'request interrupted (stale)'
		WHERE state IN ('pending', 'streaming')
		  AND (created_at < $1 OR created_at < NOW() - $2::interval)`,
		serverStartTime, intervalStr)
	if err == nil && tag.RowsAffected() > 0 {
		debuglog.Info("retention: stale log cleanup", "rows", tag.RowsAffected())
		events.Publish(events.Event{
			Type:     "logs.stale_cleanup",
			Severity: "warning",
			Message:  fmt.Sprintf("Marked %d stale %s as interrupted", tag.RowsAffected(), util.Plural(int(tag.RowsAffected()), "request", "requests")),
			Metadata: map[string]any{"count": tag.RowsAffected()},
		})
	} else if err != nil {
		debuglog.Error("retention: stale log cleanup failed", "error", err)
	}
}

// logRetentionLoop hourly deletes request_logs and app_logs rows older than
// the log_retention setting; an empty or unrecognised value (including "0"
// for disabled) skips the cycle.
func logRetentionLoop(ctx context.Context, pool *pgxpool.Pool, settingsRepo *settings.Repository) {
	timer := time.NewTimer(1 * time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		logRetentionPass(pool, settingsRepo)
		timer.Reset(1 * time.Hour)
	}
}

// logRetentionPass runs one retention sweep; an empty or unrecognised
// log_retention value (including "0" for disabled) skips the cycle.
func logRetentionPass(pool *pgxpool.Pool, settingsRepo *settings.Repository) {
	retention := settingsRepo.GetWithDefault(context.Background(), "log_retention", "")
	if retention == "" {
		return
	}
	var cutoff time.Time
	switch retention {
	case "1h":
		cutoff = time.Now().Add(-1 * time.Hour)
	case "1d", "24h":
		cutoff = time.Now().Add(-24 * time.Hour)
	case "1w", "168h":
		cutoff = time.Now().Add(-7 * 24 * time.Hour)
	case "1m", "720h":
		cutoff = time.Now().Add(-30 * 24 * time.Hour)
	default:
		return
	}
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM request_logs WHERE created_at < $1`, cutoff)
	if err == nil {
		debuglog.Info("retention: log retention deleted old entries", "retention", retention, "rows", tag.RowsAffected())
	}
	// Clean app_logs with same retention
	tag, err = pool.Exec(context.Background(),
		`DELETE FROM app_logs WHERE created_at < $1`, cutoff)
	if err == nil {
		debuglog.Info("retention: app log retention deleted old entries", "retention", retention, "rows", tag.RowsAffected())
	}
}

// sweepScheduledDisableTimeout bounds a sweep that outlives its caller's
// context, so shutdown still terminates promptly.
const sweepScheduledDisableTimeout = 30 * time.Second

// sweepScheduledDisables fires every due scheduled disable and returns how many
// providers it disabled. It mirrors the manual-disable path in
// api.UpdateProvider: the repo call invalidates the caches, the failover sync
// removes the provider's models from auto-created groups, and one warning event
// per provider tells the operator what happened.
//
// A sweep that has begun finishes even while the server is shutting down, which
// is why the caller's cancellation is dropped here the way api.UpdateProvider
// drops the request's. The UPDATE clears scheduled_disable_on as it fires, so
// the disable is only ever due once: a cancellation landing between that commit
// and the drained rows would lose the operator's event permanently — no later
// sweep can re-derive it — and leave the failover groups carrying models of a
// provider that is already off. scheduledDisableLoop keeps selecting on the
// original context, so the loop itself still exits at once.
func sweepScheduledDisables(ctx context.Context, providerRepo *provider.Repository, failoverRepo *failover.Repository) int {
	sweepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sweepScheduledDisableTimeout)
	defer cancel()

	disabled, err := providerRepo.DisableDueScheduled(sweepCtx)
	if err != nil || len(disabled) == 0 {
		return 0
	}
	for _, p := range disabled {
		debuglog.Info("scheduled disable: provider disabled", "provider", p.Name)
		events.Publish(events.Event{
			Type:     "provider.scheduled_disable",
			Severity: "warning",
			Message:  fmt.Sprintf("Provider '%s' disabled as scheduled", p.Name),
			Metadata: map[string]any{"provider": p.Name, "provider_id": p.ID.String()},
		})
	}
	if _, err := failoverRepo.SyncAllModels(sweepCtx); err != nil {
		debuglog.Info("scheduled disable: failover sync failed", "error", err)
	}
	return len(disabled)
}

// scheduledDisableLoop runs sweepScheduledDisables once at startup (a restart
// that straddled midnight must still fire the disable) and then on every tick.
// A one-minute tick keeps "as soon as the date flips" honest at negligible cost:
// the sweep is a single UPDATE on a table of tens of rows.
func scheduledDisableLoop(ctx context.Context, providerRepo *provider.Repository, failoverRepo *failover.Repository, tick time.Duration) {
	sweepScheduledDisables(ctx, providerRepo, failoverRepo)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sweepScheduledDisables(ctx, providerRepo, failoverRepo)
		case <-ctx.Done():
			return
		}
	}
}

// webauthnSessionCleanupLoop prunes expired WebAuthn sessions hourly.
func webauthnSessionCleanupLoop(webauthnRepo *webauthn.Repository) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := webauthnRepo.CleanupExpiredSessions(context.Background()); err != nil {
			debuglog.Error("webauthn: session cleanup failed", "error", err)
		} else if n > 0 {
			debuglog.Info("webauthn: cleaned up expired sessions", "count", n)
		}
	}
}
