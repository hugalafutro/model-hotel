package com.hugalafutro.bellhop.ui.alerts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.AlertEventDef
import com.hugalafutro.bellhop.data.AlertStatus
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import kotlinx.coroutines.async
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

/**
 * AlertsUiState is what the Alerts screen renders: Front Desk's outbound-notifier
 * delivery health ([status]) and its alertable-event catalog ([catalog], grouped
 * by category in the screen). A failed refresh keeps the last good data on screen
 * (stale beats blank on a phone) and raises [error]; [revoked] means the device
 * token itself no longer authenticates, same remedy as the dashboard: unlink and
 * re-pair.
 */
data class AlertsUiState(
    val loading: Boolean = true,
    val status: AlertStatus? = null,
    val catalog: List<AlertEventDef> = emptyList(),
    val error: String? = null,
    val revoked: Boolean = false,
)

/**
 * AlertsViewModel keeps Front Desk's alert delivery status fresh while the
 * Alerts screen is on screen. Same subscription-gated refresh + slow poll shape
 * as the events screen, minus filters and paging: there is no feed to page, only
 * a status pill (which can flap when the apprise-api container stops) and a
 * catalog (static per Front Desk version). Both are read on every refresh for
 * simplicity; the catalog re-read is cheap and picks up a Front Desk upgrade.
 *
 * The catalog is a *reference* of what Front Desk can alert on, not which events
 * are currently enabled: the enabled subset lives in admin settings, which a
 * device token cannot read. The screen labels it accordingly.
 */
class AlertsViewModel(
    private val client: FrontDeskClient,
    private val linkStore: LinkStore,
    private val fdUrl: String,
    private val pollIntervalMs: Long = POLL_INTERVAL_MS,
) : ViewModel() {
    private val _state = MutableStateFlow(AlertsUiState())
    val state: StateFlow<AlertsUiState> = _state.asStateFlow()

    // Conflated: a poll tick landing while a refresh is queued coalesces into at
    // most one pending refresh instead of stacking fetches.
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
                        }
                    }
                }
        }
    }

    // runRefreshes is the sole refresher: one refresh on subscribe, then one per
    // nudge. A dead token can never authenticate again, so stop the radio once
    // revoked (matches the dashboard/events loops).
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

    /**
     * refreshOnce re-reads status and catalog together. A failure keeps the last
     * good data and raises [error]; an Unauthorized (or an unreadable token) sets
     * [revoked]. The two reads run concurrently since neither depends on the other.
     */
    suspend fun refreshOnce() {
        val token = linkStore.token()
        if (token == null) {
            // Still linked but the token can't be read back (e.g. the Keystore
            // key is gone): no request can ever succeed, same remedy as a remote
            // revoke, so surface it through the same flag.
            _state.update { it.copy(loading = false, revoked = true) }
            return
        }
        coroutineScope {
            val statusDeferred = async { client.alertStatus(fdUrl, token) }
            val catalogDeferred = async { client.alertCatalog(fdUrl, token) }
            fold(statusDeferred.await(), catalogDeferred.await())
        }
    }

    // fold merges the two reads into the state. Either 401 means the token is
    // dead, so revoked wins over a partial success. Otherwise each read updates
    // its slice, and a failure on either raises the error while keeping whatever
    // last loaded. Both succeeding clears the error.
    private fun fold(
        status: FetchResult<AlertStatus>,
        catalog: FetchResult<List<AlertEventDef>>,
    ) {
        if (status is FetchResult.Unauthorized || catalog is FetchResult.Unauthorized) {
            _state.update { it.copy(loading = false, revoked = true) }
            return
        }
        _state.update { st ->
            val failure =
                (status as? FetchResult.Failure)?.message
                    ?: (catalog as? FetchResult.Failure)?.message
            st.copy(
                loading = false,
                status = (status as? FetchResult.Success)?.data ?: st.status,
                catalog = (catalog as? FetchResult.Success)?.data ?: st.catalog,
                error = failure,
                revoked = false,
            )
        }
    }

    class Factory(
        private val client: FrontDeskClient,
        private val linkStore: LinkStore,
        private val fdUrl: String,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = AlertsViewModel(client, linkStore, fdUrl) as T
    }

    companion object {
        // Delivery health can flap when the apprise-api container stops; a slow
        // poll keeps the pill honest without hammering Front Desk from a phone.
        const val POLL_INTERVAL_MS = 30_000L
    }
}
