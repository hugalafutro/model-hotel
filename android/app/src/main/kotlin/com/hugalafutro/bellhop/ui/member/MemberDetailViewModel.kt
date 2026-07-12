package com.hugalafutro.bellhop.ui.member

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
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
