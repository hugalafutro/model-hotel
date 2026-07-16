package com.hugalafutro.bellhop.ui.dashboard

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.ActionResult
import com.hugalafutro.bellhop.data.EventQuery
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.PrefsStore
import com.hugalafutro.bellhop.data.SseMessage
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * DashboardUiState is what the linked home renders. A failed refresh keeps the
 * last good member list on screen (stale beats blank on a phone) and raises
 * [error]; [revoked] means the device token itself no longer authenticates
 * (revoked on Front Desk, or unreadable locally), which only unlinking fixes.
 */
data class DashboardUiState(
    val loading: Boolean = true,
    val members: List<FleetMember> = emptyList(),
    val primaryId: String = "",
    // Auto-sync master toggle, from GET /api/fleet/autosync. Drives the pause/
    // resume operator control, which is only shown once a primary is configured.
    val autoSyncEnabled: Boolean = false,
    // Per-member traffic for the inline card sparkline, keyed by member id and
    // filled lazily for whichever members are currently on screen (see
    // [DashboardViewModel.setVisibleMembers]). A member absent from the map just
    // renders without a sparkline yet.
    val traffic: Map<String, MemberTraffic> = emptyMap(),
    // Each member's single most recent event, keyed by member id, for the card's
    // recent-event pill. Taken from the members read's inline newest_event; against
    // a Front Desk too old to send it, filled by a per-member GET /api/events read
    // instead. A member with no events is simply absent from the map.
    val recentEvents: Map<String, FdEvent> = emptyMap(),
    val error: String? = null,
    val revoked: Boolean = false,
    // Pause/resume operator action state (same shape as the member-detail card).
    val autoSync: AutoSyncAction = AutoSyncAction(),
)

/**
 * AutoSyncAction is the pause/resume operator control's UI state, the fleet-wide
 * twin of the member-detail operator card: pessimistic-accept Front Desk's 200,
 * show [pendingEnabled] optimistically until a refresh reconciles the live value,
 * collapse to a guard note on a 403 ([forbidden]), and surface a failure in
 * [error]. [busy] flags a tap dropped because one is already in flight.
 */
data class AutoSyncAction(
    val inProgress: Boolean = false,
    val pendingEnabled: Boolean? = null,
    val forbidden: Boolean = false,
    val error: String? = null,
    val busy: Boolean = false,
)

/**
 * DashboardViewModel keeps the Front Desk read tier fresh while the dashboard is
 * on screen, on two loops gated on active collectors (a backgrounded app stops
 * both and goes quiet):
 *
 *  - a live GET /api/sse subscription that pushes a refresh the moment a member
 *    goes up/down or the fleet config changes, so state is near-instant; and
 *  - a slow [pollIntervalMs] fallback poll that also catches anything missed
 *    across an SSE reconnect gap and keeps relative times honest.
 *
 * Both nudge a single conflated refresher rather than fetching directly, so a
 * burst of events (or an event landing mid-poll) collapses into one refresh and
 * the two loops can never race to overwrite the list with a staler snapshot.
 */
class DashboardViewModel(
    private val client: FrontDeskClient,
    private val linkStore: LinkStore,
    private val fdUrl: String,
    private val pollIntervalMs: Long = POLL_INTERVAL_MS,
    private val sseHealthyPollIntervalMs: Long = SSE_HEALTHY_POLL_INTERVAL_MS,
    private val trafficPollMs: Long = TRAFFIC_POLL_MS,
    private val now: () -> Long = System::currentTimeMillis,
) : ViewModel() {
    private val _state = MutableStateFlow(DashboardUiState())
    val state: StateFlow<DashboardUiState> = _state.asStateFlow()

    // Conflated: rapid nudges from the poll and the event stream coalesce into at
    // most one queued refresh instead of stacking sequential fetches.
    private val refreshTrigger = Channel<Unit>(Channel.CONFLATED)

    // Whether the SSE stream is currently connected. It drives the fallback poll
    // cadence: while the stream is live it carries changes, so [pollLoop] slackens
    // to save the radio; while it's down the poll tightens to stay the sole
    // freshness source. A StateFlow so pollLoop reacts the instant it flips (via
    // collectLatest) instead of waiting out a slow delay. [streamLoop] owns it.
    private val sseConnected = MutableStateFlow(false)

    // The member ids currently visible in the list, reported by the screen. Only
    // these get their traffic sparkline fetched, so the per-member fan-out is
    // bounded by what's on screen (~a handful) rather than the fleet size — a
    // 100-member fleet never triggers 100 traffic calls. When Front Desk grows a
    // single /api/fleet/usage rollup, this loop swaps its source for that one call.
    private val visibleIds = MutableStateFlow<List<String>>(emptyList())

    // Whether the dashboard is hidden behind another screen (member detail,
    // settings, alerts, events). The dashboard ViewModel keeps running under those
    // overlays so its state stays warm, but the traffic sparklines aren't on screen
    // then, so [trafficLoop] pauses the per-member fan-out while covered and catches
    // up when shown again. Reported by the screen via [setCovered].
    private val covered = MutableStateFlow(false)

    // The traffic-graph range (minutes) the sparklines request, driven by the
    // Settings pref via [setGraphRange]. Changing it force-refetches so the
    // charts redraw at the new span.
    private val graphWindow = MutableStateFlow(PrefsStore.DEFAULT_GRAPH_RANGE_MINUTES)

    // When each member's traffic was last fetched, so a re-tick or a re-scroll
    // past an already-fresh member doesn't refetch within the TTL. Only touched
    // from viewModelScope coroutines (single-threaded Main), so a plain map is safe.
    private val trafficFetchedAt = mutableMapOf<String, Long>()

    // The in-flight traffic fetch per member, keyed by id. Doubles as the
    // "currently fetching" set (so the debounce path and the periodic tick can't
    // both request the same member at once; the TTL only dedupes sequential
    // fetches) and as cancellable handles: a graph-range change cancels these
    // old-window fetches and refetches the viewport at the new span, so a late
    // old-window response can't overwrite the chart and freeze it until the next
    // tick. Kept separate from trafficFetchedAt so a failed fetch still retries
    // immediately. Main-confined, so a plain map is safe.
    private val trafficJobs = mutableMapOf<String, Job>()

    init {
        viewModelScope.launch {
            _state.subscriptionCount
                .map { it > 0 }
                .distinctUntilChanged()
                .collectLatest { active ->
                    if (active) {
                        coroutineScope {
                            launch { runRefreshes() }
                            launch { pollLoop() }
                            launch { streamLoop() }
                            launch { trafficLoop() }
                        }
                    }
                }
        }
    }

    // runRefreshes is the sole refresher: one refresh on subscribe, then one per
    // nudge. Serial by construction, so the poll and the stream never overlap.
    private suspend fun runRefreshes() {
        // A revoked (or unreadable) token can never authenticate again; only
        // unlinking fixes it, and relinking rebuilds this ViewModel. Swallow
        // the initial refresh and the nudges instead of hitting Front Desk
        // forever; gating the initial one also keeps a collector restart
        // (backgrounding and reopening the app) from firing one more doomed
        // request per restart.
        if (!_state.value.revoked) refreshOnce()
        for (ignored in refreshTrigger) {
            if (_state.value.revoked) continue
            refreshOnce()
        }
    }

    // pollLoop is the fallback safety net: with the stream carrying live changes,
    // this only has to catch a missed event and keep the view from going stale. It
    // runs slack while the stream is healthy (the radio can idle between ticks) and
    // tight while the stream is down (the poll is then the only freshness source).
    // Keyed on [sseConnected] via collectLatest so a disconnect re-tightens the
    // cadence at once — the pending slack delay is cancelled, not waited out.
    private suspend fun pollLoop() {
        sseConnected.collectLatest { connected ->
            val interval = if (connected) sseHealthyPollIntervalMs else pollIntervalMs
            while (true) {
                delay(interval)
                refreshTrigger.trySend(Unit)
            }
        }
    }

    // streamLoop holds a live SSE subscription while the dashboard is shown,
    // reconnecting with capped exponential backoff. It nudges a refresh on any
    // event that changes what the list shows, resyncs on every reconnect (a gap
    // may have dropped events the slack poll hasn't caught yet), and stops for good
    // on a dead token.
    private suspend fun streamLoop() {
        // A fresh active-collector cycle starts disconnected until Open fires, so
        // pollLoop doesn't inherit a stale "connected" from a prior cycle.
        sseConnected.value = false
        // No readable token means no request can ever succeed; the poll's
        // refreshOnce already flags this as revoked, so just stay off the stream.
        val token = linkStore.token() ?: return
        var backoff = SSE_BACKOFF_MIN_MS
        // The first Open is the initial connect, which runRefreshes already
        // covered with its startup read; only subsequent Opens are reconnects that
        // need a resync. Tracks across reconnects within this loop, not per socket.
        var firstConnect = true
        while (true) {
            var revoked = false
            var connectedAt: Long? = null
            client.streamEvents(fdUrl, token).collect { msg ->
                when (msg) {
                    SseMessage.Open -> {
                        connectedAt = now()
                        sseConnected.value = true
                        if (!firstConnect) refreshTrigger.trySend(Unit)
                        firstConnect = false
                    }
                    is SseMessage.Event ->
                        if (triggersRefresh(msg.event.type)) refreshTrigger.trySend(Unit)
                    SseMessage.Unauthorized -> revoked = true
                }
            }
            sseConnected.value = false
            if (revoked) {
                _state.update { it.copy(revoked = true) }
                return
            }
            // Reset the backoff only when the connection proved durable. If Open
            // alone reset it, a server (or proxy) that accepts the stream then
            // drops it immediately would spin a ~1s reconnect loop forever; keying
            // the reset on how long the connection lasted keeps a flapping endpoint
            // backing off instead.
            val lasted = connectedAt?.let { now() - it } ?: 0L
            backoff = nextBackoff(backoff, lasted)
            delay(backoff)
        }
    }

    /**
     * setVisibleMembers is called by the screen with the member ids currently
     * scrolled into view. Only these have their traffic sparkline fetched, so the
     * per-member fan-out never exceeds a viewport regardless of fleet size.
     */
    fun setVisibleMembers(ids: List<String>) {
        visibleIds.value = ids
    }

    /**
     * setCovered tells the ViewModel whether the dashboard is hidden behind another
     * screen. While covered, [trafficLoop] stops the per-member sparkline fan-out
     * (those charts aren't on screen, so fetching them just wakes the radio for
     * nothing); it resumes with an immediate catch-up fetch when shown again. The
     * health poll and SSE keep running so the list is fresh the moment it reappears.
     */
    fun setCovered(value: Boolean) {
        covered.value = value
    }

    /**
     * setGraphRange updates the traffic-graph span (minutes) from the Settings
     * pref. The traffic loop watches [graphWindow] and force-refetches the
     * visible sparklines when it changes, so the charts follow the new range.
     */
    fun setGraphRange(minutes: Int) {
        graphWindow.value = minutes
    }

    // trafficLoop keeps the on-screen members' sparklines fresh on two triggers:
    // the visible set changing (scroll), debounced so a fast scroll doesn't fetch
    // every row it flies past; and a slow tick so the current view's sparklines
    // keep moving. Both go through fetchVisibleTraffic, which skips members still
    // within the TTL, so overlap is cheap.
    private suspend fun trafficLoop() =
        coroutineScope {
            launch {
                visibleIds.collectLatest { ids ->
                    // collectLatest cancels this delay if the visible set changes
                    // again, so only a settled viewport triggers a fetch. Skip while
                    // covered (the list is hidden, so nothing needs its sparkline):
                    // in practice the screen is torn down then and reports no
                    // changes, but this keeps the fetch honest regardless of caller.
                    delay(TRAFFIC_DEBOUNCE_MS)
                    if (!covered.value) fetchVisibleTraffic(ids, force = false)
                }
            }
            launch {
                // Pause the periodic fan-out while the dashboard is covered — its
                // sparklines aren't on screen, so fetching them is pure radio waste.
                // collectLatest cancels the loop the instant it's covered and, when
                // shown again, restarts with an immediate catch-up fetch before
                // resuming the tick. Uncovered from the start, the first fetch runs
                // against the still-empty viewport (a no-op) until the list reports
                // its visible ids, which the debounce launch above fetches.
                covered.collectLatest { isCovered ->
                    if (isCovered) return@collectLatest
                    while (true) {
                        // A dead token can't authenticate; stop ticking like the
                        // other loops rather than spinning on a no-op fetch.
                        if (_state.value.revoked) return@collectLatest
                        fetchVisibleTraffic(visibleIds.value, force = true)
                        delay(trafficPollMs)
                    }
                }
            }
            launch {
                // A range change invalidates every in-flight and cached sparkline
                // (they cover the old span). Cancel the old-window fetches so a late
                // response can't overwrite the chart and drop their stamps
                // regardless. Only refetch now if the dashboard is actually on
                // screen: while it's covered there is nothing to redraw, so the
                // refetch would just wake the radio — the catch-up fetch when it's
                // uncovered picks up the new span (fetchVisibleTraffic reads the
                // current window). drop(1) skips the initial value the first fetch
                // already used.
                graphWindow.drop(1).collectLatest {
                    if (_state.value.revoked) return@collectLatest
                    trafficJobs.values.forEach { it.cancel() }
                    trafficJobs.clear()
                    trafficFetchedAt.clear()
                    if (!covered.value) {
                        fetchVisibleTraffic(visibleIds.value, force = true)
                    }
                }
            }
        }

    // fetchVisibleTraffic fetches traffic for the given members whose cache is
    // stale (or all of them when [force]), concurrently — the set is viewport-
    // bounded, so this is a handful of calls at most. A per-member failure just
    // leaves that sparkline stale; only a 401 escalates to revoked. Runs on the
    // Main-confined viewModelScope, so the plain fetched-at map needs no lock.
    private suspend fun fetchVisibleTraffic(
        ids: List<String>,
        force: Boolean,
    ) {
        if (_state.value.revoked) return
        val token = linkStore.token() ?: return
        val at = now()
        val due =
            ids.filter {
                it !in trafficJobs && (force || at - (trafficFetchedAt[it] ?: 0L) >= TRAFFIC_TTL_MS)
            }
        if (due.isEmpty()) return
        // Capture the window for this batch so the request stays aimed at the span
        // that was current when it was decided, even if the range changes mid-flight
        // (the observer below cancels these jobs on such a change).
        val window = graphWindow.value
        coroutineScope {
            due.forEach { id ->
                val job =
                    launch {
                        try {
                            when (val result = client.memberTraffic(fdUrl, token, id, window)) {
                                is FetchResult.Success -> {
                                    trafficFetchedAt[id] = at
                                    _state.update { it.copy(traffic = it.traffic + (id to result.data)) }
                                }
                                FetchResult.Unauthorized -> _state.update { it.copy(revoked = true) }
                                // A stale sparkline is fine; the card's health still shows.
                                is FetchResult.Failure -> Unit
                            }
                        } finally {
                            // Runs on success, failure, or cancellation, so a member
                            // is never stuck marked-in-flight after its fetch ends.
                            trafficJobs -= id
                        }
                    }
                // Registered right after launch (still synchronous on Main; the body
                // is dispatched, not run inline) so a concurrent call sees it in
                // flight and a range change can cancel it.
                trafficJobs[id] = job
            }
        }
    }

    /**
     * refreshOnce performs one members fetch. The auto-sync fetch is best-effort:
     * the Primary badge going stale must not fail an otherwise good refresh.
     */
    suspend fun refreshOnce() {
        val token = linkStore.token()
        if (token == null) {
            // Still linked but the token can't be read back (e.g. the Keystore
            // key is gone): no request can ever succeed, same operator remedy as
            // a remote revoke, so surface it through the same flag.
            _state.update { it.copy(loading = false, revoked = true) }
            return
        }
        when (val result = client.members(fdUrl, token)) {
            is FetchResult.Success -> {
                val autoSync = client.autoSync(fdUrl, token) as? FetchResult.Success
                val liveIds = result.data.mapTo(HashSet()) { it.id }
                // Each card's pill shows that member's newest event. Front Desk
                // attaches it inline on the members read (newestEvent), so the common
                // path needs no per-member request at all. A member with no events
                // simply carries none and keeps any previous pill via the merge below
                // rather than blanking.
                val inline = result.data.mapNotNull { m -> m.newestEvent?.let { m.id to it } }.toMap()
                val recentMap: Map<String, FdEvent> =
                    if (inline.isNotEmpty()) {
                        inline
                    } else {
                        // No inline events across the whole fleet means either a
                        // genuinely event-free fleet or a Front Desk that predates the
                        // inline field. Fall back to the per-member fetch (one
                        // member_id-filtered read, limit 1) so pills still work against
                        // an un-upgraded Front Desk; an event-free fleet just pays a few
                        // empty reads until it has some. This works uniformly for the
                        // primary too (its events are older than the fleet-wide feed's
                        // top, so a single unfiltered read would miss them).
                        val fallback = mutableMapOf<String, FdEvent>()
                        for (m in result.data) {
                            when (val res = client.events(fdUrl, token, EventQuery(memberId = m.id, limit = 1))) {
                                is FetchResult.Success ->
                                    res.data.events?.firstOrNull()?.let { fallback[m.id] = it }
                                // A token revoked mid-refresh (members succeeded, then
                                // this authenticated call is rejected) must land in the
                                // same revoked-unlink recovery state as a members 401,
                                // not be swallowed into a "healthy" refresh that keeps
                                // polling a dead token.
                                FetchResult.Unauthorized -> {
                                    _state.update { it.copy(loading = false, revoked = true) }
                                    return
                                }
                                // A stale pill is fine; the card's health still shows.
                                is FetchResult.Failure -> Unit
                            }
                        }
                        fallback
                    }
                _state.update {
                    val pending = it.autoSync.pendingEnabled
                    // When the auto-sync read fails but the rest of the refresh
                    // succeeds, fall back to the optimistic pending value (already
                    // applied per the PUT's 200 echo) rather than the stale baseline,
                    // so clearing the hint below doesn't revert the toggle on screen.
                    val liveEnabled = autoSync?.data?.enabled ?: pending ?: it.autoSyncEnabled
                    it.copy(
                        loading = false,
                        members = result.data,
                        primaryId = autoSync?.data?.primaryId ?: it.primaryId,
                        autoSyncEnabled = liveEnabled,
                        // Drop cached traffic for members that left the fleet so
                        // the map can't grow without bound as members churn.
                        traffic = it.traffic.filterKeys { id -> id in liveIds },
                        // Churn-drop departed members, then overlay this refresh's
                        // fresh pills; members whose per-member read failed keep their
                        // prior entry (they're simply absent from recentMap).
                        recentEvents = it.recentEvents.filterKeys { id -> id in liveIds } + recentMap,
                        error = null,
                        revoked = false,
                        // Reconcile the pause/resume control: drop the optimistic hint
                        // once it's resolved. Either a live read shows the toggle
                        // caught up (so a change made elsewhere isn't masked by it), or
                        // the confirming endpoint is down and we've promoted the
                        // PUT-acked value to the baseline above instead of stranding a
                        // perpetual "pending" against a read that keeps failing.
                        autoSync =
                            if (pending != null && (autoSync == null || pending == liveEnabled)) {
                                it.autoSync.copy(pendingEnabled = null)
                            } else {
                                it.autoSync
                            },
                    )
                }
                trafficFetchedAt.keys.retainAll(liveIds)
            }
            FetchResult.Unauthorized ->
                _state.update { it.copy(loading = false, revoked = true) }
            is FetchResult.Failure ->
                _state.update { it.copy(loading = false, error = result.message) }
        }
    }

    /**
     * setAutoSync pauses or resumes auto-sync, the fleet-wide operator action. Same
     * pessimistic-accept/optimistic-reconcile shape as the member-detail card: wait
     * for Front Desk's 200 (its echo is the ack), show the applied value
     * optimistically via [AutoSyncAction.pendingEnabled] until [refreshOnce]
     * reconciles it, never blocking on physical convergence. A 403 means this
     * device's role may not mutate (surfaced as [AutoSyncAction.forbidden]); a 401
     * is a dead token, same revoked remedy as reads. It only ever toggles the
     * already-configured primary — choosing or repointing one stays a web action.
     */
    fun setAutoSync(enabled: Boolean) {
        // One toggle at a time: a tap while one is in flight is dropped (idempotent
        // set, so nothing is lost) but flagged busy so the screen can nudge instead
        // of the tap vanishing silently.
        if (_state.value.autoSync.inProgress) {
            _state.update { it.copy(autoSync = it.autoSync.copy(busy = true)) }
            return
        }
        // The control is only shown with a primary configured; guard anyway so a
        // stray call can't send an empty primary that Front Desk would reject.
        val primaryId = _state.value.primaryId
        if (primaryId.isEmpty()) return
        viewModelScope.launch {
            _state.update { it.copy(autoSync = it.autoSync.copy(inProgress = true, error = null, busy = false)) }
            val token = linkStore.token()
            if (token == null) {
                _state.update { it.copy(revoked = true, autoSync = it.autoSync.copy(inProgress = false)) }
                return@launch
            }
            val result = client.setAutoSync(fdUrl, token, enabled, primaryId)
            _state.update { st ->
                val cleared = st.copy(autoSync = st.autoSync.copy(inProgress = false, busy = false))
                when (result) {
                    is ActionResult.Success -> {
                        // If a live read already shows the applied state (it beat our
                        // 200), skip the optimistic hint so nothing is left for a
                        // reconcile to strand.
                        val applied = result.data.enabled
                        val pending = applied.takeUnless { it == cleared.autoSyncEnabled }
                        cleared.copy(autoSync = cleared.autoSync.copy(pendingEnabled = pending, forbidden = false))
                    }
                    ActionResult.Forbidden -> cleared.copy(autoSync = cleared.autoSync.copy(forbidden = true))
                    ActionResult.Unauthorized -> cleared.copy(revoked = true)
                    is ActionResult.Failure -> cleared.copy(autoSync = cleared.autoSync.copy(error = result.message))
                }
            }
        }
    }

    /** dismissAutoSyncError clears a failed pause/resume so its notice can be tapped away. */
    fun dismissAutoSyncError() {
        _state.update { it.copy(autoSync = it.autoSync.copy(error = null)) }
    }

    class Factory(
        private val client: FrontDeskClient,
        private val linkStore: LinkStore,
        private val fdUrl: String,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = DashboardViewModel(client, linkStore, fdUrl) as T
    }

    companion object {
        // Fallback cadence while the SSE stream is DOWN: the poll is then the only
        // freshness source, so it stays tight. Once the stream is live, pollLoop
        // switches to [SSE_HEALTHY_POLL_INTERVAL_MS] instead.
        const val POLL_INTERVAL_MS = 15_000L

        // Fallback cadence while the SSE stream is healthy: the stream carries live
        // changes, so the poll only has to backstop a rare missed event and keep
        // relative times honest. Four times slacker than the disconnected cadence,
        // which is what lets the radio idle between ticks instead of waking every
        // 15s for the whole time the dashboard is open.
        const val SSE_HEALTHY_POLL_INTERVAL_MS = 60_000L

        // Traffic sparklines refresh slower than the health poll: the series is
        // 5-minute buckets, so a per-minute tick keeps the current bucket moving
        // without a per-member fan-out on every health refresh.
        const val TRAFFIC_POLL_MS = 60_000L

        // Don't refetch a member's traffic more than once per this window when the
        // visible set changes (scroll back and forth); the tick force-refreshes.
        const val TRAFFIC_TTL_MS = 30_000L

        // Settle time before fetching a changed viewport, so a fast scroll doesn't
        // fetch every row it passes — only where it comes to rest.
        const val TRAFFIC_DEBOUNCE_MS = 300L

        // SSE reconnect backoff, mirroring the web dashboard's 1s..30s range.
        const val SSE_BACKOFF_MIN_MS = 1_000L
        const val SSE_BACKOFF_MAX_MS = 30_000L

        // A stream must stay connected at least this long to count as durable and
        // earn a fast reconnect (see [nextBackoff]). Above the 25s server heartbeat
        // so a genuinely live stream qualifies, while an accept-then-drop (which
        // fails in well under a second) never does.
        const val SSE_STABLE_MS = 30_000L

        // nextBackoff picks the wait before the next SSE reconnect. A connection
        // that lasted at least [SSE_STABLE_MS] is healthy and resets to the floor
        // for a prompt reconnect; a shorter-lived one (including one that never
        // opened, or a server that accepts then drops at once) doubles the backoff
        // up to the cap, so a flapping endpoint can't spin a tight reconnect loop.
        // internal so the decision is unit-testable without driving the stream in
        // real time.
        internal fun nextBackoff(
            current: Long,
            connectedMs: Long,
        ): Long =
            if (connectedMs >= SSE_STABLE_MS) {
                SSE_BACKOFF_MIN_MS
            } else {
                (current * 2).coerceAtMost(SSE_BACKOFF_MAX_MS)
            }

        // triggersRefresh mirrors the Front Desk web dashboard: only membership,
        // config, health, and version events change what a member card shows, so
        // only those warrant a refetch. Other events (alerts, traefik notices) ride
        // the same stream but the dashboard ignores them. internal so the filter can
        // be unit-tested directly without driving the whole stream.
        internal fun triggersRefresh(type: String): Boolean =
            type.startsWith("member.") ||
                type.startsWith("config.") ||
                type.startsWith("health.") ||
                type.startsWith("version.")
    }
}
