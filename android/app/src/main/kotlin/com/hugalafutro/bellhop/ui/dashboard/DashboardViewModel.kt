package com.hugalafutro.bellhop.ui.dashboard

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
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
 * DashboardViewModel polls the Front Desk read tier while the dashboard is on
 * screen: GET /api/members every [pollIntervalMs], plus the auto-sync config for
 * the Primary badge. Polling is gated on active collectors so a backgrounded
 * app stops hitting the network; the SSE slice will replace this loop with a
 * push stream on the same state.
 */
class DashboardViewModel(
    private val client: FrontDeskClient,
    private val linkStore: LinkStore,
    private val fdUrl: String,
    private val pollIntervalMs: Long = POLL_INTERVAL_MS,
) : ViewModel() {
    private val _state = MutableStateFlow(DashboardUiState())
    val state: StateFlow<DashboardUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            _state.subscriptionCount
                .map { it > 0 }
                .distinctUntilChanged()
                .collectLatest { active ->
                    if (active) {
                        while (true) {
                            refreshOnce()
                            delay(pollIntervalMs)
                        }
                    }
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
        // Matches Front Desk's own paired-devices list poll cadence family: fast
        // enough that a member going down shows within seconds, slow enough to be
        // polite from a phone.
        const val POLL_INTERVAL_MS = 15_000L
    }
}
