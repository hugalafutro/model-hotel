package com.hugalafutro.bellhop

import android.content.Context
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricPrompt
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.LockConfig
import com.hugalafutro.bellhop.data.LockStore
import com.hugalafutro.bellhop.data.LockTimeout
import com.hugalafutro.bellhop.data.shouldLock
import com.hugalafutro.bellhop.ui.alerts.AlertsScreen
import com.hugalafutro.bellhop.ui.alerts.AlertsViewModel
import com.hugalafutro.bellhop.ui.dashboard.DashboardScreen
import com.hugalafutro.bellhop.ui.dashboard.DashboardViewModel
import com.hugalafutro.bellhop.ui.events.EventsScreen
import com.hugalafutro.bellhop.ui.events.EventsViewModel
import com.hugalafutro.bellhop.ui.lock.LockScreen
import com.hugalafutro.bellhop.ui.member.MemberDetailScreen
import com.hugalafutro.bellhop.ui.member.MemberDetailViewModel
import com.hugalafutro.bellhop.ui.pairing.PairingScreen
import com.hugalafutro.bellhop.ui.pairing.PairingViewModel
import com.hugalafutro.bellhop.ui.settings.SettingsScreen
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

// FragmentActivity (not plain ComponentActivity) because BiometricPrompt hosts
// its sheet on a FragmentManager; Compose still drives the whole UI.
class MainActivity : FragmentActivity() {
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

// appLockAuthenticators is the authenticator set the app lock prompts with. The
// STRONG|DEVICE_CREDENTIAL combination isn't supported below API 30, so fall back
// to WEAK|DEVICE_CREDENTIAL there (no CryptoObject is used, so Class 2 is fine).
private fun appLockAuthenticators(): Int =
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        BiometricManager.Authenticators.BIOMETRIC_STRONG or BiometricManager.Authenticators.DEVICE_CREDENTIAL
    } else {
        BiometricManager.Authenticators.BIOMETRIC_WEAK or BiometricManager.Authenticators.DEVICE_CREDENTIAL
    }

/** canAppLock reports whether a biometric or device credential can gate the app. */
private fun canAppLock(context: Context): Boolean =
    BiometricManager.from(context).canAuthenticate(appLockAuthenticators()) == BiometricManager.BIOMETRIC_SUCCESS

/**
 * promptAppUnlock shows the BiometricPrompt for the ambient app lock. It only
 * gates local access (the token at rest is Keystore-wrapped regardless), so if
 * no authenticator can be presented it degrades to success rather than stranding
 * the user behind an un-openable lock. A cancel or failure calls neither branch;
 * the caller stays locked and the lock screen offers a retry.
 */
private fun FragmentActivity.promptAppUnlock(
    title: String,
    subtitle: String,
    onSuccess: () -> Unit,
) {
    val authenticators = appLockAuthenticators()
    if (BiometricManager.from(this).canAuthenticate(authenticators) != BiometricManager.BIOMETRIC_SUCCESS) {
        onSuccess()
        return
    }
    val prompt =
        BiometricPrompt(
            this,
            ContextCompat.getMainExecutor(this),
            object : BiometricPrompt.AuthenticationCallback() {
                override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                    onSuccess()
                }
            },
        )
    prompt.authenticate(
        BiometricPrompt.PromptInfo
            .Builder()
            .setTitle(title)
            .setSubtitle(subtitle)
            .setAllowedAuthenticators(authenticators)
            .build(),
    )
}

/**
 * BellhopApp is the top-level gate: it observes the persisted link and shows the
 * pairing screen when unlinked or the dashboard when linked. Loading renders
 * nothing so neither screen flashes before the link is read back from disk. When
 * linked, an enabled app lock covers the fleet UI until the user authenticates.
 */
@Composable
fun BellhopApp() {
    val context = LocalContext.current
    val activity = context as? FragmentActivity
    val linkStore = remember { LinkStore.create(context) }
    val lockStore = remember { LockStore.create(context) }
    val client = remember { FrontDeskClient() }
    val linkState by linkStore.state.collectAsStateWithLifecycle(initialValue = LinkState.Loading)
    val lockConfig by
        lockStore.config.collectAsStateWithLifecycle(
            initialValue = LockConfig(enabled = false, timeoutMs = LockTimeout.DEFAULT.millis),
        )
    val lockAvailable = remember(activity) { activity != null && canAppLock(activity) }
    val scope = rememberCoroutineScope()
    var unlinking by remember { mutableStateOf(false) }
    var unlinkFailed by remember { mutableStateOf(false) }
    // Fence: the remote revoke for this link already succeeded and only the local
    // clear is still pending (a rare clear() failure). A retry must then SKIP
    // Front Desk (the token is now revoked, so a second DELETE would 401 and fail
    // forever) and just re-attempt the clear. Reset on return to Unlinked.
    var revokedRemotely by remember { mutableStateOf(false) }

    // App-lock gate. locked survives process death (rememberSaveable) so a killed
    // app comes back locked; lockEvaluated defers the linked UI on cold start
    // until the idle window has been checked, so no fleet data flashes first.
    var locked by rememberSaveable { mutableStateOf(false) }
    var lockEvaluated by rememberSaveable { mutableStateOf(false) }
    val unlockTitle = stringResource(R.string.lock_prompt_title)
    val unlockSubtitle = stringResource(R.string.lock_prompt_subtitle)

    fun requestUnlock() {
        val act = activity
        if (act == null) {
            locked = false
        } else {
            act.promptAppUnlock(unlockTitle, unlockSubtitle) { locked = false }
        }
    }

    // Cold-start decision: a freshly added lifecycle observer is not replayed the
    // current state, and the process is already foregrounded when we compose, so
    // the first gate check has to happen here rather than on ON_START.
    LaunchedEffect(Unit) {
        val snap = lockStore.snapshot()
        if (shouldLock(snap.config, snap.lastForegroundExit, System.currentTimeMillis())) locked = true
        lockEvaluated = true
    }

    // Foreground lifecycle: stamp the exit when Bellhop is backgrounded and
    // re-evaluate the gate when it returns. ProcessLifecycleOwner scopes this to
    // the whole process's foreground, not any one Activity (plan section 3.1).
    DisposableEffect(lockStore) {
        val owner = ProcessLifecycleOwner.get()
        val observer =
            LifecycleEventObserver { _, event ->
                when (event) {
                    Lifecycle.Event.ON_STOP -> scope.launch { lockStore.stampExit(System.currentTimeMillis()) }
                    Lifecycle.Event.ON_START ->
                        scope.launch {
                            val snap = lockStore.snapshot()
                            if (shouldLock(snap.config, snap.lastForegroundExit, System.currentTimeMillis())) {
                                locked = true
                            }
                        }
                    else -> Unit
                }
            }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer) }
    }

    // Revoke the device token on Front Desk, THEN clear the local link. Only a
    // confirmed remote revoke clears locally: if the DELETE can't reach Front
    // Desk (or is refused) we keep the link and surface a retry, so a dropped
    // request can't silently orphan the device row on Front Desk. On success the
    // lock policy is cleared too, so a re-pair starts from lock-off.
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
                    lockStore.clear()
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
                lockStore.clear()
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
                locked = false
                vm.reset()
            }
            val ui by vm.state.collectAsStateWithLifecycle()
            PairingScreen(
                state = ui,
                onPastePayload = vm::onPastePayload,
                onLabelChange = vm::onLabelChange,
                onSubmit = vm::pair,
                onScanUnavailable = vm::onScanUnavailable,
            )
        }
        is LinkState.Linked -> {
            when {
                // Deciding whether the idle window has lapsed; render nothing so no
                // fleet data flashes before the lock (if any) engages.
                !lockEvaluated -> Unit
                locked -> LockScreen(onUnlock = { requestUnlock() })
                else ->
                    LinkedContent(
                        state = state,
                        client = client,
                        linkStore = linkStore,
                        lockStore = lockStore,
                        lockConfig = lockConfig,
                        lockAvailable = lockAvailable,
                        scope = scope,
                        unlinking = unlinking,
                        unlinkFailed = unlinkFailed,
                        onDismissUnlinkError = { unlinkFailed = false },
                        onUnlink = { runUnlink(state.fdUrl) },
                        onForceUnlink = { forceUnlink() },
                    )
            }
        }
    }
}

/**
 * LinkedContent is the whole linked-state surface: dashboard plus the screens it
 * pushes (events, alerts, member detail, settings). It is only shown once the
 * app lock has cleared, so nothing here is visible while locked.
 */
@Composable
private fun LinkedContent(
    state: LinkState.Linked,
    client: FrontDeskClient,
    linkStore: LinkStore,
    lockStore: LockStore,
    lockConfig: LockConfig,
    lockAvailable: Boolean,
    scope: CoroutineScope,
    unlinking: Boolean,
    unlinkFailed: Boolean,
    onDismissUnlinkError: () -> Unit,
    onUnlink: () -> Unit,
    onForceUnlink: () -> Unit,
) {
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

    // Which member's detail is open, if any. Saveable so it survives
    // rotation/process death; keyed on the pairing so a relink lands
    // back on the new Front Desk's dashboard, not a stale detail.
    var selectedMemberId by rememberSaveable(state.fdUrl, state.deviceId) {
        mutableStateOf<String?>(null)
    }
    // Whether the event log is open. Same saveable/keying rationale.
    var showEvents by rememberSaveable(state.fdUrl, state.deviceId) {
        mutableStateOf(false)
    }
    // Whether the alerts screen is open. Same saveable/keying rationale.
    var showAlerts by rememberSaveable(state.fdUrl, state.deviceId) {
        mutableStateOf(false)
    }
    // Whether the settings screen is open. Same saveable/keying rationale.
    var showSettings by rememberSaveable(state.fdUrl, state.deviceId) {
        mutableStateOf(false)
    }
    val selected = ui.members.find { it.id == selectedMemberId }
    // The member left the fleet while its detail was open: drop the
    // selection (once the list has actually loaded) so the detail
    // doesn't silently reopen if the same id ever reappears.
    LaunchedEffect(selectedMemberId, selected, ui.loading) {
        if (selectedMemberId != null && selected == null && !ui.loading) {
            selectedMemberId = null
        }
    }

    if (showEvents) {
        BackHandler { showEvents = false }
        // Keyed like the dashboard VM so a relink gets a fresh log.
        val eventsVm: EventsViewModel =
            viewModel(
                key = "events-${state.fdUrl}|${state.deviceId}",
                factory = EventsViewModel.Factory(client, linkStore, state.fdUrl),
            )
        val eventsUi by eventsVm.state.collectAsStateWithLifecycle()
        EventsScreen(
            onBack = { showEvents = false },
            ui = eventsUi,
            // Member names ride the dashboard's live state; an unknown
            // id (member since removed) falls back to the raw id.
            memberNames = ui.members.associate { it.id to it.name },
            onSeverity = eventsVm::setSeverity,
            onRange = eventsVm::setRange,
            onLoadMore = eventsVm::loadMore,
        )
    } else if (showAlerts) {
        // Alerts can be reached from the dashboard bell or from Settings; back
        // returns to whichever is still open underneath (Settings if it was).
        BackHandler { showAlerts = false }
        // Keyed like the dashboard VM so a relink gets a fresh status.
        val alertsVm: AlertsViewModel =
            viewModel(
                key = "alerts-${state.fdUrl}|${state.deviceId}",
                factory = AlertsViewModel.Factory(client, linkStore, state.fdUrl),
            )
        val alertsUi by alertsVm.state.collectAsStateWithLifecycle()
        AlertsScreen(onBack = { showAlerts = false }, ui = alertsUi)
    } else if (showSettings) {
        BackHandler { showSettings = false }
        SettingsScreen(
            link = state,
            lockConfig = lockConfig,
            lockAvailable = lockAvailable,
            onBack = { showSettings = false },
            onToggleLock = { enabled -> scope.launch { lockStore.setEnabled(enabled) } },
            onSelectTimeout = { option -> scope.launch { lockStore.setTimeout(option.millis) } },
            onAlertsClick = { showAlerts = true },
            onUnlink = onUnlink,
            unlinking = unlinking,
            unlinkFailed = unlinkFailed,
            onDismissUnlinkError = onDismissUnlinkError,
            onForceUnlink = onForceUnlink,
        )
    } else if (selected != null) {
        BackHandler { selectedMemberId = null }
        // Keyed like the dashboard VM, plus the member id, so flipping
        // between members never shows another member's series.
        val detailVm: MemberDetailViewModel =
            viewModel(
                key = "member-${state.fdUrl}|${state.deviceId}|${selected.id}",
                factory = MemberDetailViewModel.Factory(client, linkStore, state.fdUrl, selected.id),
            )
        val detailUi by detailVm.state.collectAsStateWithLifecycle()
        MemberDetailScreen(
            member = selected,
            isPrimary = selected.id == ui.primaryId,
            ui = detailUi,
            onBack = { selectedMemberId = null },
        )
    } else {
        DashboardScreen(
            link = state,
            ui = ui,
            onMemberClick = { selectedMemberId = it },
            onEventsClick = { showEvents = true },
            onAlertsClick = { showAlerts = true },
            onSettingsClick = { showSettings = true },
            onVisibleMembers = dashVm::setVisibleMembers,
        )
    }
}
