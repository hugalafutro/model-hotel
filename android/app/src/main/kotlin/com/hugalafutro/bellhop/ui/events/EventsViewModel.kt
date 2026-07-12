package com.hugalafutro.bellhop.ui.events

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.EventQuery
import com.hugalafutro.bellhop.data.EventsResponse
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import kotlinx.coroutines.channels.Channel
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
 * EventRange is the relative "since" presets offered as time filters,
 * mirroring the Front Desk web Events page (0 = no lower bound).
 */
enum class EventRange(val ms: Long) {
    ALL(0),
    H1(3_600_000),
    H24(86_400_000),
    D7(604_800_000),
    D30(2_592_000_000),
}

/**
 * EventsUiState is what the event-log screen renders. A failed refresh keeps
 * the last good page on screen (stale beats blank on a phone) and raises
 * [error]; [revoked] means the device token itself no longer authenticates,
 * same remedy as the dashboard's flag: unlink and re-pair. [total] counts all
 * rows matching the filters, so `events.size < total` means more to load.
 */
data class EventsUiState(
    val loading: Boolean = true,
    val events: List<FdEvent> = emptyList(),
    val total: Int = 0,
    // "" = all severities.
    val severity: String = "",
    val range: EventRange = EventRange.ALL,
    val loadingMore: Boolean = false,
    val error: String? = null,
    val revoked: Boolean = false,
)

/**
 * EventsViewModel keeps the Front Desk event log fresh while its screen is on
 * screen. Same conflated-refresh + poll shape as the dashboard, minus the SSE
 * stream: the dashboard already owns the app's one live stream, and a control-
 * plane log that trails reality by a poll interval is fine on a phone.
 *
 * Both refresh and load-more reload the whole loaded window from offset 0
 * (refresh at the current size, load-more one page larger) rather than paging
 * by offset. On a newest-first list any event that lands between fetches shifts
 * every offset by one, so an offset-based next-page fetch would skip a row;
 * re-reading the window from the top is drift-proof. New events prepend without
 * truncating a scrolled-back list. The server caps a page at 500 rows, which is
 * more log than anyone scrolls through on a phone.
 */
class EventsViewModel(
    private val client: FrontDeskClient,
    private val linkStore: LinkStore,
    private val fdUrl: String,
    private val pollIntervalMs: Long = POLL_INTERVAL_MS,
    private val now: () -> Long = System::currentTimeMillis,
) : ViewModel() {
    private val _state = MutableStateFlow(EventsUiState())
    val state: StateFlow<EventsUiState> = _state.asStateFlow()

    // Conflated: rapid nudges (poll tick + filter change) coalesce into at
    // most one queued refresh instead of stacking sequential fetches.
    private val refreshTrigger = Channel<Unit>(Channel.CONFLATED)

    // Serializes refreshes against load-mores: a refresh that captured the
    // window size before a concurrent append landed would reload too few rows
    // and truncate the list the user just paged in.
    private val fetchMutex = Mutex()

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
                        }
                    }
                }
        }
    }

    // runRefreshes is the sole refresher: one refresh on subscribe, then one
    // per nudge, serial by construction. Revoked gating matches the dashboard:
    // a dead token can never authenticate again, so stop the radio.
    private suspend fun runRefreshes() {
        if (!_state.value.revoked) refreshOnce()
        for (ignored in refreshTrigger) {
            if (_state.value.revoked) continue
            refreshOnce()
        }
    }

    private suspend fun pollLoop() {
        while (true) {
            delay(pollIntervalMs)
            refreshTrigger.trySend(Unit)
        }
    }

    /** setSeverity swaps the severity filter ("" = all) and reloads from scratch. */
    fun setSeverity(severity: String) {
        if (severity == _state.value.severity) return
        _state.update {
            it.copy(severity = severity, loading = true, events = emptyList(), total = 0)
        }
        refreshTrigger.trySend(Unit)
    }

    /** setRange swaps the time-range filter and reloads from scratch. */
    fun setRange(range: EventRange) {
        if (range == _state.value.range) return
        _state.update {
            it.copy(range = range, loading = true, events = emptyList(), total = 0)
        }
        refreshTrigger.trySend(Unit)
    }

    /** loadMore appends the next page; a no-op while one is in flight or at the end. */
    fun loadMore() {
        val s = _state.value
        if (s.loadingMore || s.loading || s.revoked || s.events.size >= s.total) return
        _state.update { it.copy(loadingMore = true) }
        viewModelScope.launch { loadMoreOnce() }
    }

    /** refreshOnce reloads the loaded window (or first page) under current filters. */
    suspend fun refreshOnce() =
        fetchMutex.withLock {
            val before = _state.value
            val limit = before.events.size.coerceIn(PAGE_SIZE, MAX_WINDOW)
            fetch(before, query(before, limit = limit, offset = 0)) { st, resp ->
                st.copy(
                    loading = false,
                    events = resp.events.orEmpty(),
                    total = resp.total,
                    error = null,
                    revoked = false,
                )
            }
        }

    private suspend fun loadMoreOnce() =
        fetchMutex.withLock {
            val before = _state.value
            // Grow the window by a page and reload it from the top rather than
            // fetching at offset = events.size: on a newest-first list an event
            // arriving between pages shifts every offset by one, so an offset
            // fetch would skip the row that slid past the old boundary. Reading
            // the whole window from offset 0 is drift-proof (and dedup-free).
            val limit = (before.events.size + PAGE_SIZE).coerceAtMost(MAX_WINDOW)
            fetch(before, query(before, limit = limit, offset = 0)) { st, resp ->
                st.copy(
                    events = resp.events.orEmpty(),
                    total = resp.total,
                    error = null,
                    revoked = false,
                )
            }
        }

    // fetch runs one events call and folds a success into the state via
    // [onSuccess] — unless the filters changed while it was in flight, in
    // which case the stale result is dropped (the filter change already queued
    // a fresh fetch). loadingMore is always cleared: whatever the outcome, no
    // page fetch is in flight anymore.
    private suspend fun fetch(
        before: EventsUiState,
        query: EventQuery,
        onSuccess: (EventsUiState, EventsResponse) -> EventsUiState,
    ) {
        val token = linkStore.token()
        if (token == null) {
            // Still linked but the token can't be read back (e.g. the Keystore
            // key is gone): no request can ever succeed, same operator remedy
            // as a remote revoke, so surface it through the same flag.
            _state.update { it.copy(loading = false, loadingMore = false, revoked = true) }
            return
        }
        val result = client.events(fdUrl, token, query)
        _state.update { st ->
            if (st.severity != before.severity || st.range != before.range) {
                return@update st.copy(loadingMore = false)
            }
            when (result) {
                is FetchResult.Success -> onSuccess(st, result.data).copy(loadingMore = false)
                FetchResult.Unauthorized ->
                    st.copy(loading = false, loadingMore = false, revoked = true)
                is FetchResult.Failure ->
                    st.copy(loading = false, loadingMore = false, error = result.message)
            }
        }
    }

    private fun query(
        st: EventsUiState,
        limit: Int,
        offset: Int,
    ): EventQuery =
        EventQuery(
            severity = st.severity,
            since =
                if (st.range.ms > 0) {
                    Instant.ofEpochMilli(now() - st.range.ms).toString()
                } else {
                    ""
                },
            limit = limit,
            offset = offset,
        )

    class Factory(
        private val client: FrontDeskClient,
        private val linkStore: LinkStore,
        private val fdUrl: String,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = EventsViewModel(client, linkStore, fdUrl) as T
    }

    companion object {
        // The log only grows at control-plane pace; a slow poll keeps the top
        // fresh without hammering Front Desk from a phone.
        const val POLL_INTERVAL_MS = 30_000L

        // One page per load-more tap, matching the Front Desk web page size.
        const val PAGE_SIZE = 25

        // Server-side clamp on a single page (maxEventsLimit in
        // internal/frontdesk/httputil.go); refreshes never ask for more.
        const val MAX_WINDOW = 500
    }
}
