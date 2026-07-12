package com.hugalafutro.bellhop.ui.pairing

import android.os.Build
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.PairPayload
import com.hugalafutro.bellhop.data.PairResult
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json

/**
 * PairingError is what the pairing screen renders: an unreadable string, a
 * bad/expired code, a scan that could not open the camera (all fixed localized
 * strings), or a transport failure that carries the upstream message.
 */
sealed interface PairingError {
    data object BadString : PairingError

    data object InvalidCode : PairingError

    data object ScanUnavailable : PairingError

    data class Message(
        val text: String,
    ) : PairingError
}

/**
 * PairingUiState is driven entirely by the one pairing string: pasting it (or, a
 * later slice, scanning its QR) yields the Front Desk URL, code, and name, which
 * is everything needed to pair. The only other input is this device's display
 * name, which is pre-filled and optional.
 */
data class PairingUiState(
    val pasteText: String = "",
    val fdUrl: String = "",
    val code: String = "",
    val fdName: String = "",
    val label: String = defaultLabel(),
    val parsed: Boolean = false,
    val busy: Boolean = false,
    val error: PairingError? = null,
) {
    val canSubmit: Boolean get() = parsed && fdUrl.isNotBlank() && code.isNotBlank() && !busy
}

private fun defaultLabel(): String =
    listOf(Build.MANUFACTURER, Build.MODEL)
        .filter { it.isNotBlank() }
        .joinToString(" ")
        .ifBlank { "Bellhop device" }

/**
 * PairingViewModel drives the linking flow from the single pairing string: parse
 * it, POST the code to its Front Desk, and on success persist the returned token
 * via [LinkStore] (which flips the app gate to the dashboard). Nothing here
 * holds the token beyond handing it to the store.
 */
class PairingViewModel(
    private val client: FrontDeskClient,
    private val linkStore: LinkStore,
    private val json: Json = Json { ignoreUnknownKeys = true },
) : ViewModel() {
    private val _state = MutableStateFlow(PairingUiState())
    val state: StateFlow<PairingUiState> = _state.asStateFlow()

    /**
     * onPastePayload is the sole entry point: the operator pastes (or scans) the
     * pairing string. A blank field resets; a readable string fills the derived
     * Front Desk fields; anything else surfaces a "not a pairing string" hint.
     */
    fun onPastePayload(raw: String) {
        _state.update { s ->
            when {
                raw.isBlank() ->
                    s.copy(pasteText = raw, fdUrl = "", code = "", fdName = "", parsed = false, error = null)
                else ->
                    when (val payload = parsePayload(raw)) {
                        null -> s.copy(pasteText = raw, parsed = false, error = PairingError.BadString)
                        else ->
                            s.copy(
                                pasteText = raw,
                                fdUrl = payload.fdUrl,
                                code = payload.pairingCode,
                                fdName = payload.fdName.ifBlank { payload.fdUrl },
                                parsed = true,
                                error = null,
                            )
                    }
            }
        }
    }

    fun onLabelChange(value: String) = _state.update { it.copy(label = value) }

    /**
     * onScanUnavailable is invoked when ZXing finishes the scan without a decoded
     * value because the CAMERA permission was denied. It leaves any already-parsed
     * fields intact and just posts a hint so the failure does not look like a
     * deliberate cancel and the paste fallback is offered.
     */
    fun onScanUnavailable() = _state.update { it.copy(error = PairingError.ScanUnavailable) }

    /**
     * reset returns the form to a clean slate. This ViewModel is Activity-scoped
     * and outlives a link, so it must be reset whenever the pairing screen is
     * re-entered (e.g. after an unlink) or a stale pasted string and a frozen
     * busy spinner from the previous session would linger.
     */
    fun reset() {
        _state.value = PairingUiState()
    }

    // A valid pairing string is JSON carrying both a URL and a code; require both
    // so a half-formed paste doesn't enable Pair.
    private fun parsePayload(raw: String): PairPayload? {
        if (!raw.trimStart().startsWith("{")) return null
        return runCatching { json.decodeFromString<PairPayload>(raw) }
            .getOrNull()
            ?.takeIf { it.pairingCode.isNotBlank() && it.fdUrl.isNotBlank() }
    }

    fun pair() {
        val s = _state.value
        if (!s.canSubmit) return
        _state.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            when (val result = client.pair(s.fdUrl, s.code.trim(), s.label.trim())) {
                is PairResult.Success -> {
                    linkStore.save(
                        fdUrl = s.fdUrl.trim().trimEnd('/'),
                        fdName = s.fdName.ifBlank { s.fdUrl },
                        token = result.response.token,
                        device = result.response.device,
                    )
                    // The app gate observes LinkStore.state and swaps to the
                    // dashboard; leave busy true so the form stays disabled
                    // through the transition.
                }
                PairResult.InvalidCode ->
                    _state.update { it.copy(busy = false, error = PairingError.InvalidCode) }
                is PairResult.Failure ->
                    _state.update {
                        it.copy(busy = false, error = PairingError.Message(result.message))
                    }
            }
        }
    }

    class Factory(
        private val client: FrontDeskClient,
        private val linkStore: LinkStore,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = PairingViewModel(client, linkStore) as T
    }
}
