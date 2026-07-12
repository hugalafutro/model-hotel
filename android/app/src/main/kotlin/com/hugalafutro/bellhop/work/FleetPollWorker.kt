package com.hugalafutro.bellhop.work

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ListenableWorker.Result
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetSnapshot
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberTransition
import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.data.diffFleet
import com.hugalafutro.bellhop.notify.FleetNotifier
import kotlinx.coroutines.flow.first
import java.util.concurrent.TimeUnit

/**
 * PollResult is the outcome of one backstop poll. It is separate from WorkManager's
 * Result so [pollFleet] can be unit-tested without the worker runtime: [Changed]
 * means the fetch succeeded and the snapshot was persisted (with any health edges
 * to notify on), [Unauthorized] means the device token is dead, and [Failed] is a
 * transient error worth a retry.
 */
sealed interface PollResult {
    data class Changed(
        val transitions: List<MemberTransition>,
    ) : PollResult

    data object Unauthorized : PollResult

    data object Failed : PollResult
}

/**
 * pollFleet is the testable core of the background backstop: fetch the fleet,
 * diff it against the last-seen snapshot, persist the new snapshot, and return the
 * health edges. It performs no notification or Android I/O itself, so it can run
 * against a MockWebServer-backed [FrontDeskClient] and a temp [MonitorStore]. The
 * snapshot is saved on every successful fetch (even with no transitions) so the
 * baseline keeps advancing; it is left untouched on failure so a transient error
 * doesn't erase the baseline and re-alert the whole fleet next run.
 */
suspend fun pollFleet(
    client: FrontDeskClient,
    fdUrl: String,
    token: String,
    monitorStore: MonitorStore,
): PollResult =
    when (val result = client.members(fdUrl, token)) {
        is FetchResult.Success -> {
            val previous = monitorStore.snapshot()
            val transitions = diffFleet(previous, result.data)
            monitorStore.saveSnapshot(FleetSnapshot.of(result.data))
            PollResult.Changed(transitions)
        }
        FetchResult.Unauthorized -> PollResult.Unauthorized
        is FetchResult.Failure -> PollResult.Failed
    }

/**
 * runBackstop is the guarded dispatch [FleetPollWorker.doWork] performs each run,
 * extracted from the worker runtime so its short-circuits are unit-testable (the
 * same reason [pollFleet] is a free function). It bails to success in the steady
 * states the foreground UI already handles — monitoring turned off, notifications
 * blocked, the device unlinked, or the token unreadable — and otherwise polls,
 * notifies on any health edges, and maps the poll outcome onto a worker [Result].
 */
suspend fun runBackstop(
    monitorStore: MonitorStore,
    linkStore: LinkStore,
    client: FrontDeskClient,
    canNotify: Boolean,
    notify: (MemberTransition) -> Unit,
): Result {
    // Disabled or already unscheduled: nothing to do. A stale run can outlive the
    // toggle being turned off, so re-check rather than trust scheduling.
    if (!monitorStore.enabled.first()) return Result.success()
    // Can't post? Don't poll. Advancing the baseline while alerts are silently
    // dropped would swallow the very down->up change the operator needs to see
    // once they grant the permission, so freeze until then (Settings flags it).
    if (!canNotify) return Result.success()

    val link = linkStore.state.first()
    if (link !is LinkState.Linked) return Result.success()
    // No readable token (unlinked mid-run, or the Keystore key is gone): the
    // foreground UI surfaces the revoke; the backstop just stops quietly.
    val token = linkStore.token() ?: return Result.success()

    return when (val result = pollFleet(client, link.fdUrl, token, monitorStore)) {
        is PollResult.Changed -> {
            result.transitions.forEach(notify)
            Result.success()
        }
        // A revoked token can never succeed again; retrying would just hammer
        // Front Desk. The foreground UI flags the revoke, so end quietly.
        PollResult.Unauthorized -> Result.success()
        PollResult.Failed -> Result.retry()
    }
}

/**
 * FleetPollWorker is the Layer-2 background backstop (plan section 5.2): a periodic
 * poll that, while Bellhop is backgrounded or killed, diffs fleet health against
 * the last poll and posts a local notification on a member going down or
 * recovering. It needs no push infrastructure and no Google dependency; the
 * trade-off is the 15-minute WorkManager floor, so a change is learned up to a
 * poll late. The run logic lives in [runBackstop]; this shell only supplies the
 * real stores, client, and notifier.
 */
class FleetPollWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val context = applicationContext
        return runBackstop(
            monitorStore = MonitorStore.create(context),
            linkStore = LinkStore.create(context),
            client = FrontDeskClient(),
            canNotify = FleetNotifier.canPost(context),
            notify = { FleetNotifier.notify(context, it) },
        )
    }

    companion object {
        private const val UNIQUE_NAME = "fleet-poll"

        // The 15-minute WorkManager floor is the shortest periodic interval Android
        // allows; the backstop is explicitly not real-time (plan section 5.2).
        private const val INTERVAL_MINUTES = 15L

        /**
         * schedule enqueues the periodic poll, keeping any existing schedule so a
         * re-schedule on app open (self-heal) doesn't reset the interval clock.
         * Requires network so a poll doesn't wake only to fail offline.
         */
        fun schedule(context: Context) {
            val request =
                PeriodicWorkRequestBuilder<FleetPollWorker>(INTERVAL_MINUTES, TimeUnit.MINUTES)
                    .setConstraints(
                        Constraints
                            .Builder()
                            .setRequiredNetworkType(NetworkType.CONNECTED)
                            .build(),
                    ).build()
            WorkManager
                .getInstance(context)
                .enqueueUniquePeriodicWork(UNIQUE_NAME, ExistingPeriodicWorkPolicy.KEEP, request)
        }

        fun cancel(context: Context) {
            WorkManager.getInstance(context).cancelUniqueWork(UNIQUE_NAME)
        }
    }
}
