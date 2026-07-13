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
    // to this member. Read-only and unpaged: the full log with filters lives on
    // the Events screen; here it's just "what happened to this box lately".
    val events: List<FdEvent> = emptyList(),
    val error: String? = null,
    val revoked: Boolean = false,
    // Operator-action overlay (drain/activate, config sync). Orthogonal to the
    // read state above, which keeps polling regardless.
    val action: ActionUiState = ActionUiState(),
)

/**
 * ActionUiState is the operator-action overlay on the detail screen. [inProgress]
 * disables the controls and shows a spinner while a mutation is in flight;
 * [pendingState] is the optimistic drain/activate target shown once Front Desk
 * has accepted it (the ack), until the dashboard's live state reconciles it (see
 * [MemberDetailViewModel.reconcile]); [forbidden] is Front Desk's 403 — the
 * device's role may not mutate, the authoritative guard behind the hidden UI;
 * [error] is the last action failure; [syncSummary] is a completed config sync's
 * per-member tally.
 */
data class ActionUiState(
    val inProgress: Boolean = false,
    val pendingState: String? = null,
    val forbidden: Boolean = false,
    val error: String? = null,
    val syncSummary: SyncSummary? = null,
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
) : ViewModel() {
    private val _state = MutableStateFlow(MemberDetailUiState())
    val state: StateFlow<MemberDetailUiState> = _state.asStateFlow()

    // The last live member state seen from the dashboard (via [reconcile]). An
    // accepted action whose target already matches this needs no optimistic
    // pending hint: the dashboard's SSE refetch beat our ack, so there is nothing
    // left to reconcile and a pending hint would strand (member.state won't change
    // again to re-fire the reconcile effect).
    private var lastLiveState: String? = null

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
     * refreshOnce fetches this member's traffic and its recent events together
     * (they don't depend on each other) and folds both in. Either 401 means the
     * token is dead, so revoked wins; a failure on either keeps the last-good
     * slice and raises the error; both succeeding clears it.
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
        val (traffic, events) =
            coroutineScope {
                val t = async { client.memberTraffic(fdUrl, token, memberId) }
                val e = async { client.events(fdUrl, token, EventQuery(memberId = memberId, limit = EVENTS_LIMIT)) }
                t.await() to e.await()
            }
        if (traffic is FetchResult.Unauthorized || events is FetchResult.Unauthorized) {
            _state.update { it.copy(loading = false, revoked = true) }
            return
        }
        _state.update { st ->
            val failure =
                (traffic as? FetchResult.Failure)?.message
                    ?: (events as? FetchResult.Failure)?.message
            st.copy(
                loading = false,
                traffic = (traffic as? FetchResult.Success)?.data ?: st.traffic,
                events =
                    (events as? FetchResult.Success)?.data?.events.orEmpty().ifEmpty {
                        // Keep the last-good list only when the events fetch itself
                        // failed; a genuine empty page (success) must clear it.
                        if (events is FetchResult.Success) emptyList() else st.events
                    },
                error = failure,
                revoked = false,
            )
        }
    }

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
        if (_state.value.action.inProgress) return
        viewModelScope.launch {
            _state.update {
                it.copy(action = it.action.copy(inProgress = true, error = null, syncSummary = null))
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
        if (_state.value.action.inProgress) return
        viewModelScope.launch {
            _state.update {
                it.copy(action = it.action.copy(inProgress = true, error = null, syncSummary = null))
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

    // applyActionResult folds an operator ActionResult into the ui state with the
    // shared arm handling (clear inProgress; 403 -> forbidden, 401 -> revoked,
    // failure -> error), delegating only the success shaping to [onSuccess] so
    // drain/activate and sync each stamp their own success field.
    private fun <T> applyActionResult(
        result: ActionResult<T>,
        onSuccess: (MemberDetailUiState, T) -> MemberDetailUiState,
    ) {
        _state.update { st ->
            val cleared = st.copy(action = st.action.copy(inProgress = false))
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

        // Recent events shown under the graph. Enough to see the member's recent
        // story; the Events screen owns the full, filterable, paged log.
        const val EVENTS_LIMIT = 25
    }
}
