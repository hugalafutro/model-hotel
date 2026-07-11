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
                val token = linkStore.token()
                val revoked =
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
                if (revoked) {
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
        is LinkState.Linked ->
            DashboardScreen(
                link = state,
                unlinking = unlinking,
                unlinkFailed = unlinkFailed,
                onUnlink = { runUnlink(state.fdUrl) },
                onDismissUnlinkError = { unlinkFailed = false },
            )
    }
}
