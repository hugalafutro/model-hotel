package com.hugalafutro.bellhop.ui.member

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.ActionResult
import com.hugalafutro.bellhop.data.EventQuery
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.ui.common.CustomDateRange
import com.hugalafutro.bellhop.ui.common.EventRange
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.time.Instant

/**
 * MemberDetailUiState is what the member-detail traffic card renders. A failed
 * refresh keeps the last good series on screen (stale beats blank on a phone)
 * and raises [error]; [revoked] means the device token itself no longer
 * authenticates, same remedy as the dashboard's flag: unlink and re-pair.
 */
data class MemberDetailUiState(
    val loading: Boolean = true,
    val traffic: MemberTraffic? = null,
    // The member's own recent events (newest first), from GET /api/events filtered
    // to this member. Time-filterable (preset or calendar range) and paged:
    // bottoming out the list grows the window ([MemberDetailViewModel.loadMore]).
    val events: List<FdEvent> = emptyList(),
    // Total matching rows server-side; drives [canLoadMore].
    val eventsTotal: Int = 0,
    val range: EventRange = EventRange.ALL,
    // Absolute calendar range from the picker; non-null overrides [range].
    val custom: CustomDateRange? = null,
    val loadingMore: Boolean = false,
    // When a sync last actually wrote config to any member (from
    // GET /api/fleet/autosync); "" until one has. Shown under the fleet-sync
    // action on the primary.
    val lastFleetSyncAt: String = "",
    val error: String? = null,
    val revoked: Boolean = false,
    // Operator-action overlay (drain/activate, config sync). Orthogonal to the
    // read state above, which keeps polling regardless.
    val action: ActionUiState = ActionUiState(),
) {
    // More rows exist server-side and the drift-proof window can still grow.
    val canLoadMore: Boolean
        get() = events.size < eventsTotal && events.size < MemberDetailViewModel.MAX_EVENTS_WINDOW
}

/**
 * ActionUiState is the operator-action overlay on the detail screen. [inProgress]
 * disables the controls and shows a spinner while a mutation is in flight;
 * [pendingState] is the optimistic drain/activate target shown once Front Desk
 * has accepted it (the ack), until the dashboard's live state reconciles it (see
 * [MemberDetailViewModel.reconcile]); [forbidden] is Front Desk's 403 — the
 * device's role may not mutate, the authoritative guard behind the hidden UI;
 * [error] is the last action failure; [syncSummary] is a completed config sync's
 * per-member tally; [busy] flags a tap that arrived while another mutation was
 * still in flight (dropped, not queued) so the screen can say so rather than the
 * tap vanishing silently.
 */
data class ActionUiState(
    val inProgress: Boolean = false,
    val pendingState: String? = null,
    val forbidden: Boolean = false,
    val error: String? = null,
    val syncSummary: SyncSummary? = null,
    val busy: Boolean = false,
)

/** SyncSummary is a finished config sync's tally: how many members, how many failed. */
data class SyncSummary(
    val total: Int,
    val failed: Int,
)

/**
 * MemberDetailViewModel keeps one member's traffic series fresh while its
 * detail screen is on screen. The series is hourly 5-minute buckets, so a slow
 * poll is enough; no SSE here (traffic moves every request, the member's
 * health/badges on this screen ride the dashboard's live state instead).
 * The loop is gated on active collectors, so a backgrounded app goes quiet.
 */
class MemberDetailViewModel(
    private val client: FrontDeskClient,
    private val linkStore: LinkStore,
    private val fdUrl: String,
    private val memberId: String,
    private val pollIntervalMs: Long = POLL_INTERVAL_MS,
    // Injectable clock so tests can pin the preset-range "since" bound.
    private val now: () -> Long = System::currentTimeMillis,
) : ViewModel() {
    private val _state = MutableStateFlow(MemberDetailUiState())
    val state: StateFlow<MemberDetailUiState> = _state.asStateFlow()

    // The last live member state seen from the dashboard (via [reconcile]). An
    // accepted action whose target already matches this needs no optimistic
    // pending hint: the dashboard's SSE refetch beat our ack, so there is nothing
    // left to reconcile and a pending hint would strand (member.state won't change
    // again to re-fire the reconcile effect).
    private var lastLiveState: String? = null

    // Serializes the poll refresh, filter-change reloads and loadMore so two
    // window fetches can't interleave and fold out of order.
    private val fetchMutex = Mutex()

    init {
        viewModelScope.launch {
            _state.subscriptionCount
                .map { it > 0 }
                .distinctUntilChanged()
                .collectLatest { active ->
                    if (active) {
                        // A revoked (or unreadable) token can never
                        // authenticate again; only unlinking fixes it, so stop
                        // the radio instead of retrying forever. The entry
                        // check keeps a collector restart (backgrounding and
                        // reopening the screen) from firing one more doomed
                        // request per restart.
                        while (!_state.value.revoked) {
                            refreshOnce()
                            if (_state.value.revoked) break
                            delay(pollIntervalMs)
                        }
                    }
                }
        }
    }

    /**
     * refreshOnce fetches this member's traffic, its recent events and the
     * fleet's autosync status (for the last-actual-sync stamp) together (none
     * depend on each other) and folds them in. The events fetch reloads the
     * whole already-paged window at offset 0 (the same drift-proof shape the
     * Events screen uses) so a poll can't shear a grown list. A 401 on the
     * member reads means the token is dead, so revoked wins; a failure keeps
     * the last-good slice and raises the error. The autosync read is
     * best-effort garnish: its failure never degrades the screen.
     */
    suspend fun refreshOnce() {
        val token = linkStore.token()
        if (token == null) {
            // Still linked but the token can't be read back (e.g. the Keystore
            // key is gone): no request can ever succeed, same operator remedy
            // as a remote revoke, so surface it through the same flag.
            _state.update { it.copy(loading = false, revoked = true) }
            return
        }
        fetchMutex.withLock {
            val before = _state.value
            val query = eventsQuery(before, limit = before.events.size.coerceIn(EVENTS_PAGE, MAX_EVENTS_WINDOW))
            val (traffic, events, autoSync) =
                coroutineScope {
                    val t = async { client.memberTraffic(fdUrl, token, memberId) }
                    val e = async { client.events(fdUrl, token, query) }
                    val a = async { client.autoSync(fdUrl, token) }
                    Triple(t.await(), e.await(), a.await())
                }
            if (traffic is FetchResult.Unauthorized || events is FetchResult.Unauthorized) {
                _state.update { it.copy(loading = false, revoked = true) }
                return
            }
            _state.update { st ->
                val failure =
                    (traffic as? FetchResult.Failure)?.message
                        ?: (events as? FetchResult.Failure)?.message
                // A filter that changed while this fetch was in flight makes the
                // events slice stale; keep whatever the newer reload produced.
                val filtersLive = st.range == before.range && st.custom == before.custom
                val eventsPage = (events as? FetchResult.Success)?.data
                st.copy(
                    loading = false,
                    traffic = (traffic as? FetchResult.Success)?.data ?: st.traffic,
                    events =
                        if (filtersLive && eventsPage != null) {
                            eventsPage.events.orEmpty()
                        } else {
                            st.events
                        },
                    eventsTotal =
                        if (filtersLive && eventsPage != null) eventsPage.total else st.eventsTotal,
                    lastFleetSyncAt =
                        (autoSync as? FetchResult.Success)?.data?.lastSyncAt ?: st.lastFleetSyncAt,
                    error = failure,
                    revoked = false,
                )
            }
        }
    }

    /** setRange swaps the time-range preset (clearing any calendar range) and reloads. */
    fun setRange(range: EventRange) {
        val s = _state.value
        if (range == s.range && s.custom == null) return
        _state.update {
            it.copy(range = range, custom = null, loading = true, events = emptyList(), eventsTotal = 0)
        }
        viewModelScope.launch { refreshOnce() }
    }

    /** setCustomRange swaps the calendar range (null falls back to the preset) and reloads. */
    fun setCustomRange(range: CustomDateRange?) {
        if (range == _state.value.custom) return
        _state.update {
            it.copy(custom = range, loading = true, events = emptyList(), eventsTotal = 0)
        }
        viewModelScope.launch { refreshOnce() }
    }

    /**
     * loadMore grows the events window by one page when the list bottoms out
     * (the screen's infinite scroll). Same drift-proof shape as the Events
     * screen: re-request offset 0 with a bigger limit instead of paging by
     * offset, so a new event arriving mid-scroll can't duplicate or skip rows.
     */
    fun loadMore() {
        val s = _state.value
        if (s.loadingMore || s.loading || s.revoked || !s.canLoadMore) return
        _state.update { it.copy(loadingMore = true) }
        viewModelScope.launch {
            val token = linkStore.token()
            if (token == null) {
                _state.update { it.copy(loadingMore = false, revoked = true) }
                return@launch
            }
            fetchMutex.withLock {
                val before = _state.value
                val limit = (before.events.size + EVENTS_PAGE).coerceAtMost(MAX_EVENTS_WINDOW)
                val result = client.events(fdUrl, token, eventsQuery(before, limit))
                _state.update { st ->
                    if (st.range != before.range || st.custom != before.custom) {
                        return@update st.copy(loadingMore = false)
                    }
                    when (result) {
                        is FetchResult.Success ->
                            st.copy(
                                loadingMore = false,
                                events = result.data.events.orEmpty(),
                                eventsTotal = result.data.total,
                            )
                        FetchResult.Unauthorized -> st.copy(loadingMore = false, revoked = true)
                        is FetchResult.Failure -> st.copy(loadingMore = false, error = result.message)
                    }
                }
            }
        }
    }

    // eventsQuery builds this member's events query for the current filters:
    // the calendar range wins over a preset; ALL means no time bounds.
    private fun eventsQuery(
        st: MemberDetailUiState,
        limit: Int,
    ): EventQuery =
        EventQuery(
            memberId = memberId,
            since =
                when {
                    st.custom != null -> st.custom.sinceRfc3339()
                    st.range.ms > 0 -> Instant.ofEpochMilli(now() - st.range.ms).toString()
                    else -> ""
                },
            until = st.custom?.untilRfc3339() ?: "",
            limit = limit,
        )

    /**
     * setMemberState drains or activates this member. Pessimistic-accept,
     * optimistic-reconcile: it waits for Front Desk's 200 (the recorded state is
     * the ack) and then flips [ActionUiState.pendingState] so the screen shows the
     * target optimistically; the dashboard's live state reconciles it through
     * [reconcile], never blocking on physical fleet convergence. Set-state, not
     * toggle, so a double-tap or retry is a safe no-op. A 403 means the device's
     * role may not mutate (the real guard, surfaced as [ActionUiState.forbidden]);
     * a 401 is a dead token, the same revoked remedy as the reads.
     */
    fun setMemberState(target: String) {
        if (rejectWhileInFlight()) return
        viewModelScope.launch {
            _state.update {
                it.copy(action = it.action.copy(inProgress = true, error = null, syncSummary = null, busy = false))
            }
            val token = linkStore.token()
            if (token == null) {
                _state.update { it.copy(revoked = true, action = it.action.copy(inProgress = false)) }
                return@launch
            }
            applyActionResult(client.setMemberState(fdUrl, token, memberId, target)) { st, ok ->
                // If the live state already shows the accepted target (an SSE
                // refetch landed it before our 200), skip the optimistic hint so
                // it can't strand: there is nothing left for [reconcile] to clear.
                val pending = ok.state.takeUnless { it == lastLiveState }
                st.copy(action = st.action.copy(pendingState = pending, forbidden = false))
            }
        }
    }

    /**
     * syncFleet propagates [primaryId]'s config to the rest of the fleet. Only the
     * designated primary is ever passed (choosing a primary is a later slice). The
     * ack is Front Desk's 200, which carries the per-member outcomes summarized
     * into [ActionUiState.syncSummary]; the dashboard reconciles the members'
     * "last config sync" afterwards.
     */
    fun syncFleet(primaryId: String) {
        if (rejectWhileInFlight()) return
        viewModelScope.launch {
            _state.update {
                it.copy(action = it.action.copy(inProgress = true, error = null, syncSummary = null, busy = false))
            }
            val token = linkStore.token()
            if (token == null) {
                _state.update { it.copy(revoked = true, action = it.action.copy(inProgress = false)) }
                return@launch
            }
            applyActionResult(client.syncFleet(fdUrl, token, primaryId)) { st, ok ->
                st.copy(
                    action =
                        st.action.copy(
                            forbidden = false,
                            syncSummary = SyncSummary(total = ok.results.size, failed = ok.results.count { !it.ok }),
                        ),
                )
            }
        }
    }

    // A single mutation runs at a time. A tap that lands while one is in flight is
    // dropped rather than queued (set-state is idempotent, so nothing is lost) but
    // flagged [ActionUiState.busy] so the screen can nudge the operator instead of
    // the tap vanishing silently. Returns true when the caller should bail out.
    private fun rejectWhileInFlight(): Boolean {
        if (!_state.value.action.inProgress) return false
        _state.update { it.copy(action = it.action.copy(busy = true)) }
        return true
    }

    // applyActionResult folds an operator ActionResult into the ui state with the
    // shared arm handling (clear inProgress/busy; 403 -> forbidden, 401 -> revoked,
    // failure -> error), delegating only the success shaping to [onSuccess] so
    // drain/activate and sync each stamp their own success field.
    private fun <T> applyActionResult(
        result: ActionResult<T>,
        onSuccess: (MemberDetailUiState, T) -> MemberDetailUiState,
    ) {
        _state.update { st ->
            val cleared = st.copy(action = st.action.copy(inProgress = false, busy = false))
            when (result) {
                is ActionResult.Success -> onSuccess(cleared, result.data)
                ActionResult.Forbidden -> cleared.copy(action = cleared.action.copy(forbidden = true))
                ActionResult.Unauthorized -> cleared.copy(revoked = true)
                is ActionResult.Failure -> cleared.copy(action = cleared.action.copy(error = result.message))
            }
        }
    }

    /**
     * reconcile clears the optimistic [ActionUiState.pendingState] once the live
     * member state (from the dashboard's SSE/poll refresher, passed down by the
     * screen) has caught up to the accepted target. Until then the screen shows
     * the pending target; afterwards it shows the live state directly, so a later
     * change made elsewhere isn't masked by a stale pending value.
     */
    fun reconcile(liveState: String) {
        lastLiveState = liveState
        _state.update { st ->
            if (st.action.pendingState != null && st.action.pendingState == liveState) {
                st.copy(action = st.action.copy(pendingState = null))
            } else {
                st
            }
        }
    }

    /** dismissActionError clears the last action failure banner. */
    fun dismissActionError() {
        _state.update { it.copy(action = it.action.copy(error = null)) }
    }

    class Factory(
        private val client: FrontDeskClient,
        private val linkStore: LinkStore,
        private val fdUrl: String,
        private val memberId: String,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            MemberDetailViewModel(client, linkStore, fdUrl, memberId) as T
    }

    companion object {
        // The chart's buckets are 5 minutes wide; a minute-ish poll keeps the
        // current bucket moving without hammering the member's stats API.
        const val POLL_INTERVAL_MS = 60_000L

        // One page of recent events under the graph; infinite scroll grows the
        // window a page at a time.
        const val EVENTS_PAGE = 25

        // Ceiling on the drift-proof reload window, mirroring the Events
        // screen (and the server's own clamp).
        const val MAX_EVENTS_WINDOW = 500
    }
}
