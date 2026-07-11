package com.hugalafutro.bellhop.ui.member

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberTraffic
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
                        while (true) {
                            refreshOnce()
                            delay(pollIntervalMs)
                        }
                    }
                }
        }
    }

    /** refreshOnce performs one traffic fetch and folds it into the state. */
    suspend fun refreshOnce() {
        val token = linkStore.token()
        if (token == null) {
            // Still linked but the token can't be read back (e.g. the Keystore
            // key is gone): no request can ever succeed, same operator remedy
            // as a remote revoke, so surface it through the same flag.
            _state.update { it.copy(loading = false, revoked = true) }
            return
        }
        when (val result = client.memberTraffic(fdUrl, token, memberId)) {
            is FetchResult.Success ->
                _state.update {
                    it.copy(loading = false, traffic = result.data, error = null, revoked = false)
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
    }
}
