package com.hugalafutro.bellhop.ui.dashboard

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.SseMessage
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
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
    val error: String? = null,
    val revoked: Boolean = false,
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
) : ViewModel() {
    private val _state = MutableStateFlow(DashboardUiState())
    val state: StateFlow<DashboardUiState> = _state.asStateFlow()

    // Conflated: rapid nudges from the poll and the event stream coalesce into at
    // most one queued refresh instead of stacking sequential fetches.
    private val refreshTrigger = Channel<Unit>(Channel.CONFLATED)

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
    // this only has to catch a missed event and keep the view from going stale.
    private suspend fun pollLoop() {
        while (true) {
            delay(pollIntervalMs)
            refreshTrigger.trySend(Unit)
        }
    }

    // streamLoop holds a live SSE subscription while the dashboard is shown,
    // reconnecting with capped exponential backoff. It nudges a refresh on any
    // event that changes what the list shows and stops for good on a dead token.
    private suspend fun streamLoop() {
        // No readable token means no request can ever succeed; the poll's
        // refreshOnce already flags this as revoked, so just stay off the stream.
        val token = linkStore.token() ?: return
        var backoff = SSE_BACKOFF_MIN_MS
        while (true) {
            var revoked = false
            client.streamEvents(fdUrl, token).collect { msg ->
                when (msg) {
                    // A fresh connection: reset backoff so the next drop reconnects
                    // fast even after a long, quiet, healthy stream.
                    SseMessage.Open -> backoff = SSE_BACKOFF_MIN_MS
                    is SseMessage.Event ->
                        if (triggersRefresh(msg.event.type)) refreshTrigger.trySend(Unit)
                    SseMessage.Unauthorized -> revoked = true
                }
            }
            if (revoked) {
                _state.update { it.copy(revoked = true) }
                return
            }
            delay(backoff)
            backoff = (backoff * 2).coerceAtMost(SSE_BACKOFF_MAX_MS)
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
                _state.update {
                    it.copy(
                        loading = false,
                        members = result.data,
                        primaryId = autoSync?.data?.primaryId ?: it.primaryId,
                        error = null,
                        revoked = false,
                    )
                }
            }
            FetchResult.Unauthorized ->
                _state.update { it.copy(loading = false, revoked = true) }
            is FetchResult.Failure ->
                _state.update { it.copy(loading = false, error = result.message) }
        }
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
        // Fallback cadence now that the SSE stream carries live changes: slow
        // enough to be polite from a phone, fast enough to backstop a missed event.
        const val POLL_INTERVAL_MS = 15_000L

        // SSE reconnect backoff, mirroring the web dashboard's 1s..30s range.
        const val SSE_BACKOFF_MIN_MS = 1_000L
        const val SSE_BACKOFF_MAX_MS = 30_000L

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
