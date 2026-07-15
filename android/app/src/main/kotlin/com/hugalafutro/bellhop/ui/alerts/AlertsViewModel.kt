package com.hugalafutro.bellhop.ui.alerts

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.ActionResult
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
 * delivery health ([status]) and its alertable-event catalog enriched with the
 * live enabled state ([catalog], grouped by category in the screen). A failed
 * refresh keeps the last good data on screen (stale beats blank on a phone) and
 * raises [error]; [revoked] means the device token itself no longer authenticates,
 * same remedy as the dashboard: unlink and re-pair. [action] tracks an in-flight
 * operator flip.
 */
data class AlertsUiState(
    val loading: Boolean = true,
    val status: AlertStatus? = null,
    val catalog: List<AlertEventDef> = emptyList(),
    val error: String? = null,
    val revoked: Boolean = false,
    val action: AlertActionState = AlertActionState(),
)

/**
 * AlertActionState is the overlay for an operator flipping an event on or off.
 * [togglingType] is the event whose POST is in flight (its row shows a spinner
 * and is disabled); [forbidden] is Front Desk's 403 (this device paired as
 * monitor may not flip); [busy] flags a tap that arrived while another flip was
 * still in flight (dropped, not queued); [error] is the last flip failure.
 */
data class AlertActionState(
    val togglingType: String? = null,
    val forbidden: Boolean = false,
    val busy: Boolean = false,
    val error: String? = null,
)

/**
 * ALERT_SEVERITIES is the fixed pill order for the enabled-count badges, most
 * severe first, matching the severity-dot palette.
 */
val ALERT_SEVERITIES = listOf("error", "warning", "info", "success")

/**
 * enabledSeverityCounts tallies how many currently-enabled events fall under each
 * severity, always returning all four keys (0 when none) so the Settings pill can
 * render a full badge row regardless of the selection.
 */
fun enabledSeverityCounts(catalog: List<AlertEventDef>): Map<String, Int> =
    ALERT_SEVERITIES.associateWith { severity -> catalog.count { it.enabled && it.severity == severity } }

/**
 * AlertsViewModel keeps Front Desk's alert delivery status fresh while the
 * Alerts screen is on screen. Same subscription-gated refresh + slow poll shape
 * as the events screen, minus filters and paging: there is no feed to page, only
 * a status pill (which can flap when the apprise-api container stops) and a
 * catalog (static per Front Desk version). Both are read on every refresh for
 * simplicity; the catalog re-read is cheap and picks up a Front Desk upgrade.
 *
 * The catalog now carries each event's live enabled state (read from Front Desk's
 * /api/alert/selection), so an operator can flip events straight from the phone
 * ([toggleEvent]); a monitor device sees them read-only.
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
            val selectionDeferred = async { client.alertSelection(fdUrl, token) }
            fold(statusDeferred.await(), selectionDeferred.await())
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
            // A poll landing mid-flip must not clobber the catalog with a pre-flip
            // read; the toggle's own ack (which carries Front Desk's authoritative
            // selection) owns the catalog until it clears togglingType.
            val nextCatalog =
                if (st.action.togglingType == null) {
                    (catalog as? FetchResult.Success)?.data ?: st.catalog
                } else {
                    st.catalog
                }
            st.copy(
                loading = false,
                status = (status as? FetchResult.Success)?.data ?: st.status,
                catalog = nextCatalog,
                error = failure,
                revoked = false,
            )
        }
    }

    /**
     * toggleEvent flips one alert event on or off. It follows the same operator-action
     * idiom as member drain/activate: one flip at a time (a tap arriving mid-flight is
     * dropped and flagged [AlertActionState.busy], never queued), and rather than guess
     * optimistically it adopts the selection Front Desk echoes back — so if the phone
     * dies mid-request the next refresh simply re-reads Front Desk's truth and nothing is
     * left half-applied. A 403 surfaces as [AlertActionState.forbidden] (a monitor device
     * may not flip); a 401 is the revoked remedy.
     */
    fun toggleEvent(
        type: String,
        enabled: Boolean,
    ) {
        val current = _state.value
        if (current.action.togglingType != null) {
            _state.update { it.copy(action = it.action.copy(busy = true)) }
            return
        }
        if (current.revoked) return
        _state.update { it.copy(action = it.action.copy(togglingType = type, error = null, busy = false)) }
        viewModelScope.launch {
            val token = linkStore.token()
            if (token == null) {
                _state.update { it.copy(revoked = true, action = it.action.copy(togglingType = null)) }
                return@launch
            }
            val result = client.setAlertEvent(fdUrl, token, type, enabled)
            _state.update { st ->
                val cleared = st.copy(action = st.action.copy(togglingType = null))
                when (result) {
                    is ActionResult.Success ->
                        // Adopt Front Desk's echoed selection wholesale: the ack is the re-read.
                        cleared.copy(catalog = result.data, action = cleared.action.copy(forbidden = false))
                    ActionResult.Forbidden -> cleared.copy(action = cleared.action.copy(forbidden = true))
                    ActionResult.Unauthorized -> cleared.copy(revoked = true)
                    is ActionResult.Failure -> cleared.copy(action = cleared.action.copy(error = result.message))
                }
            }
        }
    }

    /** dismissActionError clears the last flip-failure banner. */
    fun dismissActionError() {
        _state.update { it.copy(action = it.action.copy(error = null)) }
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
