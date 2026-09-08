package main

// Background maintenance loops for the server binary: the periodic discovery
// scheduler, quota polling, stale request-log cleanup, log retention, scheduled
// provider disables, and WebAuthn session pruning. Each runs for the app
// lifetime and exits on ctx cancellation.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// backgroundJoinBudget is what shutdown gives the join: the sweep ceiling plus
// a margin. Every loop selects on the root context, but the passes they run
// deliberately do not — logRetentionPass, staleLogCleanupPass and
// sweepScheduledDisables all issue their statements on the group's drain
// context — so a pass already running when the cancel lands ignores it and
// finishes the statement it started. Of those three only the sweep carries a
// ceiling of its own (sweepScheduledDisableTimeout), and this budget is derived
// from it so the sweep is waited out rather than cut a tick short; the margin
// covers the tick that started it plus the loop wiring around it. The other two
// carry no ceiling, so the budget does not wait for them, it ENDS them: Wait
// cancels the drain context on its way out, and a DELETE or UPDATE still
// running at that point is cancelled, not awaited. A loop still running when
// the budget expires is named in the log and shutdown proceeds rather than
// hanging.
const backgroundJoinBudget = sweepScheduledDisableTimeout + 5*time.Second

// backgroundGroup tracks the process-lifetime loops so shutdown can cancel the
// root context and then join them before anything they read is released. A
// loop started with a bare `go` is invisible here, so it would keep running
// against a pool the shutdown path is closing.
type backgroundGroup struct {
	wg sync.WaitGroup
	// mu guards running, which is read from Wait on the shutdown goroutine
	// while the loops themselves are removing their own names.
	mu      sync.Mutex
	running map[string]bool
	// joined closes when every member has returned. Created on the first Wait
	// and reused, because a Wait that times out leaves its joiner parked in
	// wg.Wait and a fresh one per call would accumulate them.
	joinOnce sync.Once
	joined   chan struct{}
	// drainCtx is what the detached maintenance passes run their statements on,
	// created on first use and guarded by mu. Wait cancels it.
	drainCtx    context.Context
	drainCancel context.CancelFunc
}

// drainContext returns the context a maintenance pass runs its statements on
// when it deliberately outlives the loop that called it. It keeps parent's
// values but not its cancellation, so at runtime a pass that has begun finishes
// its statement however long the database takes, exactly as it does with no
// shutdown in sight. Wait is what ends it: a pass stalled on the database is
// cancelled when the join budget expires, so it cannot outlast the join and
// write into a pool this shutdown is about to close.
func (g *backgroundGroup) drainContext(parent context.Context) context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.initDrain(parent)
	return g.drainCtx
}

// initDrain creates the drain context on first use. Callers hold mu.
func (g *backgroundGroup) initDrain(parent context.Context) {
	if g.drainCtx == nil {
		g.drainCtx, g.drainCancel = context.WithCancel(context.WithoutCancel(parent))
	}
}

// Go starts f as a named member of the group.
func (g *backgroundGroup) Go(name string, f func()) {
	g.mu.Lock()
	if g.running == nil {
		g.running = make(map[string]bool)
	}
	g.running[name] = true
	g.mu.Unlock()

	g.wg.Go(func() {
		defer func() {
			g.mu.Lock()
			delete(g.running, name)
			g.mu.Unlock()
		}()
		f()
	})
}

// Wait joins every member and returns nil, or gives up after budget and returns
// the names still running, sorted. The caller logs them: a loop that outlives
// its cancellation is an operational fact worth seeing, not a reason to block
// the process from exiting.
//
// The joiner is started once and its channel reused, so a Wait that gives up
// does not leave one goroutine parked in wg.Wait per call.
func (g *backgroundGroup) Wait(budget time.Duration) []string {
	g.mu.Lock()
	g.initDrain(context.Background())
	cancelDrain := g.drainCancel
	g.mu.Unlock()
	// The detached passes end here and not before. While the server is up they
	// run unbounded, which is what keeps a slow sweep from dropping work. This
	// is a cut, not a ceiling: a pass still running when the budget expires is
	// cancelled here rather than awaited, so its statement is rolled back and
	// the pool it holds is released before the close below it. Cancelled on both
	// paths, since a join that returned nil has already seen every member out
	// and there is nothing left to end.
	defer cancelDrain()

	g.joinOnce.Do(func() {
		g.joined = make(chan struct{})
		go func() {
			g.wg.Wait()
			close(g.joined)
		}()
	})
	joined := g.joined

	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-joined:
		return nil
	case <-timer.C:
		g.mu.Lock()
		defer g.mu.Unlock()
		return slices.Sorted(maps.Keys(g.running))
	}
}

// settingsIntervalLoop runs tick on a timer whose period comes from a setting,
// and reacts to changes of that setting immediately via sub rather than waiting
// for the current timer to expire. An interval of 0 ("Disabled") truly disables
// the loop rather than falling back to a default: it blocks on the subscription
// until a non-zero value arrives.
//
// onDisabled, when non-nil, runs once per disabled span (including a loop that
// starts disabled), so a caller that has to release state on the way into the
// disabled state is not asked to do it again on every wakeup.
//
// The loop owns sub and unsubscribes on return.
func settingsIntervalLoop(ctx context.Context, sub *settings.Subscription, readInterval func() time.Duration, tick, onDisabled func()) {
	defer sub.Unsubscribe()

	// A nil timer channel blocks forever in select, which is exactly the
	// disabled state: only the settings subscription and the context
	// cancellation can wake us.
	var timer *time.Timer
	var timerC <-chan time.Time
	applyInterval := func(d time.Duration) {
		if d <= 0 {
			if timer != nil {
				timer.Stop()
				timer, timerC = nil, nil
			}
			return
		}
		if timer == nil {
			timer = time.NewTimer(d)
		} else {
			timer.Reset(d)
		}
		timerC = timer.C
	}

	interval := readInterval()
	applyInterval(interval)

	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	// disabledHandled tracks whether onDisabled has already run for the
	// current disabled span.
	disabledHandled := false

	for {
		if interval <= 0 {
			if onDisabled != nil && !disabledHandled {
				onDisabled()
				disabledHandled = true
			}
			// Blocked until the setting changes or the server shuts down:
			// the main select is unreachable while timerC is nil.
			select {
			case <-sub.Events():
				interval = readInterval()
				applyInterval(interval)
			case <-ctx.Done():
				return
			}
			continue
		}
		disabledHandled = false

		select {
		case <-timerC:
			tick()
			// Re-read the interval in case it changed since the last
			// subscription event was processed.
			interval = readInterval()
			applyInterval(interval)

		case <-sub.Events():
			// Re-read from the DB (the source of truth) rather than
			// parsing the event value, which may be empty if the setting
			// was deleted or the lookup failed.
			if newInterval := readInterval(); newInterval != interval {
				interval = newInterval
				applyInterval(interval)
			}

		case <-ctx.Done():
			return
		}
	}
}

// every runs f on a ticker of period d until ctx is done. A tick that lands
// together with the cancellation starts nothing: select picks at random between
// two ready cases, so without this check a pass could begin AFTER shutdown has
// cancelled ctx, and the join budget would already be counting down against a
// ceiling that had not started yet.
func every(ctx context.Context, d time.Duration, f func()) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			f()
		case <-ctx.Done():
			return
		}
	}
}

// discoverySchedulerLoop runs periodic discovery based on the settings
// interval. The first run waits a full interval so it doesn't bypass the
// discovery_on_startup setting: when that is true, the startup runner already
// handles immediate discovery; when false, we must not discover on startup
// either.
func discoverySchedulerLoop(ctx context.Context, settingsRepo *settings.Repository, runDisc func(source string) DiscoveryResult) {
	const defaultInterval = 6 * time.Hour

	settingsIntervalLoop(ctx, settingsRepo.Subscribe(),
		func() time.Duration {
			return settingsRepo.GetDuration(context.Background(), "discovery_interval", defaultInterval)
		},
		func() {
			publishDiscoveryEvent("Scheduled", runDisc("scheduled"))
		},
		nil)
}

// quotaPollLoop periodically refreshes provider quota snapshots based on the
// quota_refresh_interval_min setting. Like discoverySchedulerLoop, the first
// run waits a full interval and an interval of 0 disables polling.
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
	settingsIntervalLoop(ctx, settingsRepo.Subscribe(),
		func() time.Duration {
			return time.Duration(settingsRepo.GetInt(context.Background(), "quota_refresh_interval_min", 5)) * unit
		},
		func() { pollOnce(ctx) },
		func() { onDisabled(ctx) })
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
func staleLogCleanupLoop(ctx, drainCtx context.Context, pool *pgxpool.Pool, settingsRepo *settings.Repository, serverStartTime time.Time) {
	every(ctx, 5*time.Minute, func() {
		staleLogCleanupPass(drainCtx, pool, settingsRepo, serverStartTime)
	})
}

// staleLogCleanupPass runs one stale-log sweep; a stale_request_timeout of 0
// disables the age-based check for this cycle.
//
// drainCtx is the group's drain context rather than the loop's, so a sweep
// already under way when the server is asked to stop still finishes its UPDATE.
// The shutdown join is what ends it if the database has stalled.
func staleLogCleanupPass(drainCtx context.Context, pool *pgxpool.Pool, settingsRepo *settings.Repository, serverStartTime time.Time) {
	staleTimeout := settingsRepo.GetDuration(drainCtx, "stale_request_timeout", 30*time.Minute)
	if staleTimeout <= 0 {
		return
	}
	// The age cutoff is computed here, like the server-start cutoff beside it,
	// rather than handed to Postgres as an interval string to parse back.
	cutoff := time.Now().Add(-staleTimeout)
	tag, err := pool.Exec(drainCtx, `
		UPDATE request_logs
		SET state = 'failed', error_kind = 'internal', error_message = 'request interrupted (stale)'
		WHERE state IN ('pending', 'streaming')
		  AND (created_at < $1 OR created_at < $2)`,
		serverStartTime, cutoff)
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
// the log_retention setting; a disabled or unparseable value skips the cycle.
func logRetentionLoop(ctx, drainCtx context.Context, pool *pgxpool.Pool, settingsRepo *settings.Repository) {
	every(ctx, time.Hour, func() {
		logRetentionPass(drainCtx, pool, settingsRepo)
	})
}

// errRetentionUnparseable marks a log_retention value that is neither a
// duration nor a legacy token; a disabled value is not an error.
var errRetentionUnparseable = errors.New("log_retention: not a duration")

// parseLogRetention turns the log_retention setting into a retention window.
// The dashboard stores the day slider as a Go duration in hours ("48h",
// "168h0m0s"); the pre-slider dropdown wrote 1d/1w/1m, and installs that never
// touched the setting since may still hold one. Note that "1m" is that legacy
// 30-day token, not one minute; a minute-scale window is "60m" or "1h".
//
// enabled=false means the sweep must not run: "", "0", or any duration that
// parsed to zero or negative ("0s" is the usual disabled form for duration
// settings). err is set only when the value cannot be read at all.
func parseLogRetention(raw string) (window time.Duration, enabled bool, err error) {
	switch raw {
	case "", "0":
		return 0, false, nil
	case "1d":
		return 24 * time.Hour, true, nil
	case "1w":
		return 7 * 24 * time.Hour, true, nil
	case "1m":
		return 30 * 24 * time.Hour, true, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, errRetentionUnparseable
	}
	if d <= 0 {
		return 0, false, nil
	}
	return d, true, nil
}

// retentionWarned remembers the last log_retention value the sweep warned
// about, so a bad value costs one warning, not one per hour forever. That
// matters because warnings land in app_logs and a bad value is exactly the
// case where the sweep never prunes them. Guarded because tests drive the
// pass directly, not only from the loop goroutine.
var retentionWarned struct {
	sync.Mutex
	value string
}

// logRetentionPass runs one retention sweep. A disabled value skips silently;
// a value the sweep cannot read skips too, but says so once, because a
// retention setting that silently never fires is a failure the operator cannot
// see from the dashboard.
//
// drainCtx carries no deadline of its own. The first sweep after retention is
// enabled, or the first after an outage, deletes a whole backlog and takes as
// long as it takes; a ceiling here would cancel that DELETE, roll it back and
// leave the pass failing identically every hour while both tables grew. The
// shutdown join is the only thing that ends it, which is what keeps a stalled
// database from outliving the join.
func logRetentionPass(drainCtx context.Context, pool *pgxpool.Pool, settingsRepo *settings.Repository) {
	retention := settingsRepo.GetWithDefault(drainCtx, "log_retention", "")
	window, enabled, err := parseLogRetention(retention)
	if err != nil {
		retentionWarned.Lock()
		unseen := retentionWarned.value != retention
		retentionWarned.value = retention
		retentionWarned.Unlock()
		if unseen {
			debuglog.Warn("retention: log_retention value not understood, skipping sweep", "retention", retention)
		}
		return
	}
	if !enabled {
		return
	}
	cutoff := time.Now().Add(-window)
	for _, table := range []string{"request_logs", "app_logs"} {
		tag, err := pool.Exec(drainCtx,
			`DELETE FROM `+table+` WHERE created_at < $1`, cutoff)
		if err != nil {
			debuglog.Error("retention: delete of old entries failed", "table", table, "error", err)
			continue
		}
		debuglog.Info("retention: deleted old entries", "table", table, "retention", retention, "rows", tag.RowsAffected())
	}
}

// sweepScheduledDisableTimeout bounds a sweep that outlives its caller's
// context. A sweep that has begun deliberately ignores the cancellation (see
// sweepScheduledDisables), so this ceiling is the only thing that ends it, and
// it is the one ceiling backgroundJoinBudget is derived from: the join waits the
// sweep out instead of naming it and letting the pool close on the UPDATE it has
// already committed. The other two detached passes have no ceiling and are not
// waited out at all, they are cancelled when the budget expires. The work here
// is one UPDATE over tens of rows plus a failover sync over every enabled model,
// which on a large fleet is the part that needs the room.
const sweepScheduledDisableTimeout = 30 * time.Second

// sweepScheduledDisables fires every due scheduled disable and returns how many
// providers it disabled. It mirrors the manual-disable path in
// api.UpdateProvider: the repo call invalidates the caches, the failover sync
// removes the provider's models from auto-created groups, and one warning event
// per provider tells the operator what happened.
//
// A sweep that has begun finishes even while the server is shutting down, which
// is why drainCtx is the group's drain context and not the loop's. The UPDATE
// clears scheduled_disable_on as it fires, so the disable is only ever due once:
// a cancellation landing between that commit and the sync would leave the
// failover groups carrying models of a provider that is already off, and no
// later sweep re-derives them. That committed UPDATE and the sync behind it are
// what the detachment protects. It does not protect the event: shutdown closes
// the event bus before it joins this loop, so a sweep firing during shutdown
// publishes into a closed bus and the operator's notification is lost however
// long the join waits. Accepted, because the disable itself is persisted and the
// dashboard shows the provider as disabled on the next start.
// scheduledDisableLoop keeps selecting on the original context, so the loop
// itself still exits at once.
func sweepScheduledDisables(drainCtx context.Context, providerRepo *provider.Repository, failoverRepo *failover.Repository) int {
	sweepCtx, cancel := context.WithTimeout(drainCtx, sweepScheduledDisableTimeout)
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
		// A permanent desync: the providers are off and no later sweep
		// re-derives the groups they are still listed in.
		debuglog.Error("scheduled disable: failover sync failed", "error", err)
	}
	return len(disabled)
}

// scheduledDisableLoop runs sweepScheduledDisables once at startup (a restart
// that straddled midnight must still fire the disable) and then on every tick.
// A one-minute tick keeps "as soon as the date flips" honest at negligible cost:
// the sweep is a single UPDATE on a table of tens of rows.
func scheduledDisableLoop(ctx, drainCtx context.Context, providerRepo *provider.Repository, failoverRepo *failover.Repository, tick time.Duration) {
	sweep := func() { sweepScheduledDisables(drainCtx, providerRepo, failoverRepo) }
	sweep()
	every(ctx, tick, sweep)
}
