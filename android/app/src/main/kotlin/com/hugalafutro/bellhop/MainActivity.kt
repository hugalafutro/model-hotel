package com.hugalafutro.bellhop

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.ui.dashboard.DashboardScreen
import com.hugalafutro.bellhop.ui.dashboard.DashboardViewModel
import com.hugalafutro.bellhop.ui.pairing.PairingScreen
import com.hugalafutro.bellhop.ui.pairing.PairingViewModel
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            BellhopTheme {
                BellhopApp()
            }
        }
    }
}

/**
 * BellhopApp is the top-level gate: it observes the persisted link and shows the
 * pairing screen when unlinked or the dashboard when linked. Loading renders
 * nothing so neither screen flashes before the link is read back from disk.
 */
@Composable
fun BellhopApp() {
    val context = LocalContext.current
    val linkStore = remember { LinkStore.create(context) }
    val client = remember { FrontDeskClient() }
    val linkState by linkStore.state.collectAsStateWithLifecycle(initialValue = LinkState.Loading)
    val scope = rememberCoroutineScope()
    var unlinking by remember { mutableStateOf(false) }
    var unlinkFailed by remember { mutableStateOf(false) }
    // Fence: the remote revoke for this link already succeeded and only the local
    // clear is still pending (a rare clear() failure). A retry must then SKIP
    // Front Desk (the token is now revoked, so a second DELETE would 401 and fail
    // forever) and just re-attempt the clear. Reset on return to Unlinked.
    var revokedRemotely by remember { mutableStateOf(false) }

    // Revoke the device token on Front Desk, THEN clear the local link. Only a
    // confirmed remote revoke clears locally: if the DELETE can't reach Front
    // Desk (or is refused) we keep the link and surface a retry, so a dropped
    // request can't silently orphan the device row on Front Desk.
    fun runUnlink(fdUrl: String) {
        if (unlinking) return
        unlinking = true
        unlinkFailed = false
        scope.launch {
            try {
                val revoked =
                    if (revokedRemotely) {
                        // Already gone from Front Desk on a prior attempt; don't
                        // re-hit it with the dead token, just fall through to clear.
                        true
                    } else {
                        val token = linkStore.token()
                        if (token == null) {
                            // Still Linked but the stored token can't be read (e.g. the
                            // Keystore key is gone): the Front Desk row is still live and
                            // we have no way to revoke it, so treat this as a failed
                            // unlink and surface the retry path rather than clearing
                            // locally and orphaning the row.
                            false
                        } else {
                            try {
                                client.unlink(fdUrl, token)
                            } catch (e: CancellationException) {
                                throw e
                            } catch (e: Throwable) {
                                false
                            }
                        }
                    }
                if (revoked) {
                    // Fence the remote side before clearing: if clear() throws, the
                    // retry skips the now-dead Front Desk call and only re-clears.
                    revokedRemotely = true
                    linkStore.clear()
                } else {
                    unlinkFailed = true
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // A clear() failure after a confirmed revoke must not strand the
                // dashboard mid-unlink; surface the retry path (the finally below
                // always re-enables the controls).
                unlinkFailed = true
            } finally {
                unlinking = false
            }
        }
    }

    // Operator escape hatch from the failure dialog: when the token can't be read
    // or Front Desk can't be reached, a revoke is impossible and a retry can loop
    // forever, so clear the link locally on request. The dialog already warns that
    // Front Desk may still list the device and to revoke it there, so this is an
    // informed choice, not a silent orphan.
    fun forceUnlink() {
        if (unlinking) return
        unlinking = true
        unlinkFailed = false
        scope.launch {
            try {
                linkStore.clear()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // Even the local clear failed (broken DataStore); re-surface the
                // dialog so the operator can try once more.
                unlinkFailed = true
            } finally {
                unlinking = false
            }
        }
    }

    when (val state = linkState) {
        LinkState.Loading -> Unit
        LinkState.Unlinked -> {
            val vm: PairingViewModel =
                viewModel(factory = PairingViewModel.Factory(client, linkStore))
            // The Activity-scoped ViewModel outlives a link; clear the unlink
            // flag and any stale form state each time we land back here.
            LaunchedEffect(Unit) {
                unlinking = false
                unlinkFailed = false
                revokedRemotely = false
                vm.reset()
            }
            val ui by vm.state.collectAsStateWithLifecycle()
            PairingScreen(
                state = ui,
                onPastePayload = vm::onPastePayload,
                onLabelChange = vm::onLabelChange,
                onSubmit = vm::pair,
            )
        }
        is LinkState.Linked -> {
            // Keyed by the full pairing (FD URL + deviceId): the Activity-scoped
            // ViewModel would otherwise survive an unlink and keep polling the OLD
            // Front Desk after a relink (same trap PairingViewModel.reset fixes).
            // deviceId alone is a UUID minted by the FD, but including the URL
            // costs nothing and holds even if some FD echoes a chosen id back.
            val dashVm: DashboardViewModel =
                viewModel(
                    key = "dashboard-${state.fdUrl}|${state.deviceId}",
                    factory = DashboardViewModel.Factory(client, linkStore, state.fdUrl),
                )
            val ui by dashVm.state.collectAsStateWithLifecycle()
            DashboardScreen(
                link = state,
                ui = ui,
                unlinking = unlinking,
                unlinkFailed = unlinkFailed,
                onUnlink = { runUnlink(state.fdUrl) },
                onDismissUnlinkError = { unlinkFailed = false },
                onForceUnlink = { forceUnlink() },
            )
        }
    }
}
