package com.hugalafutro.bellhop

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
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
import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.data.shouldLock
import com.hugalafutro.bellhop.notify.FleetNotifier
import com.hugalafutro.bellhop.push.BellhopPush
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
import com.hugalafutro.bellhop.work.FleetPollWorker
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

// The paired-device role that may mutate the fleet (Front Desk's RoleOperator).
// A monitor device hides the operator controls; the 403 is still the real guard.
private const val OPERATOR_ROLE = "operator"

// How long one operator biometric check authorizes further operator actions
// before the next one re-prompts. Deliberately short (a burst window) and tighter
// than the app-lock's default view window, and in-memory so it resets on a kill.
private const val OPERATOR_AUTH_WINDOW_MS = 60_000L

// FragmentActivity (not plain ComponentActivity) because BiometricPrompt hosts
// its sheet on a FragmentManager; Compose still drives the whole UI.
class MainActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Register the background-backstop notification channels up front so a
        // poll that fires while the app is dead has channels to post into.
        FleetNotifier.ensureChannels(this)
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

// hasPostNotificationPermission reports whether Bellhop may post notifications.
// POST_NOTIFICATIONS is a runtime permission from API 33; below that it is granted
// at install, so the backstop can always post.
private fun hasPostNotificationPermission(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
        PackageManager.PERMISSION_GRANTED

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
                    // No CryptoObject is bound to the result on purpose: this is a
                    // UI-only gate. The token at rest is Keystore-wrapped regardless
                    // and the gate degrades open when no credential is enrolled, so
                    // tying decryption to the biometric result would misstate the
                    // security model. (CodeQL java/android/insecure-local-authentication.)
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
    val monitorStore = remember { MonitorStore.create(context) }
    val client = remember { FrontDeskClient() }
    val linkState by linkStore.state.collectAsStateWithLifecycle(initialValue = LinkState.Loading)
    val lockConfig by
        lockStore.config.collectAsStateWithLifecycle(
            initialValue = LockConfig(enabled = false, timeoutMs = LockTimeout.DEFAULT.millis),
        )
    val lockAvailable = remember(activity) { activity != null && canAppLock(activity) }
    val monitorEnabled by monitorStore.enabled.collectAsStateWithLifecycle(initialValue = false)
    val pushEnabled by monitorStore.pushEnabled.collectAsStateWithLifecycle(initialValue = false)
    val pushEndpoint by monitorStore.endpoint.collectAsStateWithLifecycle(initialValue = null)
    val scope = rememberCoroutineScope()
    // Whether Bellhop may post notifications. Tracked so Settings can be honest
    // when monitoring is on but the permission was denied (or later revoked from
    // system settings); refreshed on the permission result and on every return to
    // the foreground below.
    var notificationsGranted by remember { mutableStateOf(hasPostNotificationPermission(context)) }
    // Whether any UnifiedPush distributor (ntfy) is installed, so Settings can point
    // the user at installing one rather than a push toggle that silently never
    // fires. Refreshed on every return to the foreground, since one may be installed
    // while Bellhop is away.
    var pushDistributorAvailable by remember { mutableStateOf(BellhopPush.hasDistributor(context)) }
    // Launcher for the POST_NOTIFICATIONS runtime permission (API 33+), fired when
    // the user turns background monitoring on. A denial is fine: the backstop still
    // polls, and Settings flags that the alerts won't reach them until it's granted.
    val notificationPermission =
        rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            notificationsGranted = granted
        }

    // toggleMonitor persists the opt-in and, on enable, asks for the notification
    // permission. Scheduling the actual poll is left to LinkedContent's effect so
    // the DataStore flag stays the single source of truth for whether it runs.
    fun toggleMonitor(enabled: Boolean) {
        scope.launch { monitorStore.setEnabled(enabled) }
        if (enabled && !hasPostNotificationPermission(context)) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    // togglePush persists the Layer-3 opt-in and, on enable, asks for the
    // notification permission (the push wake posts a notification too). The actual
    // UnifiedPush register/unregister is left to LinkedContent's effect so the
    // DataStore flag stays the single source of truth, mirroring toggleMonitor.
    fun togglePush(enabled: Boolean) {
        scope.launch { monitorStore.setPushEnabled(enabled) }
        if (enabled && !hasPostNotificationPermission(context)) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
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

    // Operator-action gate: a per-session biometric check on its own short timer,
    // tighter than the app-lock's view window and orthogonal to Front Desk's role
    // check (this proves the phone's owner is here; the token's role is what may
    // mutate — both are needed). One prompt authorizes a brief burst of operator
    // taps; after the window lapses the next action re-prompts. Degrades open with
    // no enrolled credential, like the app lock, since the token at rest is
    // Keystore-wrapped regardless and Front Desk's 403 is the authoritative guard.
    var operatorAuthorizedUntil by remember { mutableStateOf(0L) }
    val operatorTitle = stringResource(R.string.operator_prompt_title)
    val operatorSubtitle = stringResource(R.string.operator_prompt_subtitle)

    fun requireOperatorAuth(action: () -> Unit) {
        if (System.currentTimeMillis() < operatorAuthorizedUntil) {
            action()
            return
        }
        val act = activity
        if (act == null) {
            // No host activity to present the prompt (not expected during normal
            // composition): fall open, same rationale as the no-credential
            // degrade — Front Desk's 403 is the authoritative guard on the mutation.
            action()
            return
        }
        act.promptAppUnlock(operatorTitle, operatorSubtitle) {
            operatorAuthorizedUntil = System.currentTimeMillis() + OPERATOR_AUTH_WINDOW_MS
            action()
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
                    Lifecycle.Event.ON_START -> {
                        // Catch a grant/revoke, or a distributor install/removal,
                        // made in system settings while away.
                        notificationsGranted = hasPostNotificationPermission(context)
                        pushDistributorAvailable = BellhopPush.hasDistributor(context)
                        scope.launch {
                            val snap = lockStore.snapshot()
                            if (shouldLock(snap.config, snap.lastForegroundExit, System.currentTimeMillis())) {
                                locked = true
                            }
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
                    // Capture the push registration id before clear() wipes it, so
                    // the unregister below tears down the exact UnifiedPush instance.
                    val pushInstance = monitorStore.pushInstance()
                    linkStore.clear()
                    lockStore.clear()
                    // Stop both backstop layers and wipe the last-seen fleet so a
                    // re-pair (possibly to a different Front Desk) starts from a
                    // clean baseline rather than diffing against the old fleet.
                    // clear() drops the pushEnabled flag, but LinkedContent unmounts
                    // before its LaunchedEffect can unregister, so do it here too.
                    monitorStore.clear()
                    FleetPollWorker.cancelAll(context)
                    BellhopPush.unregister(context, pushInstance)
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
                // Capture the push registration id before clear() wipes it, so the
                // unregister below tears down the exact UnifiedPush instance.
                val pushInstance = monitorStore.pushInstance()
                linkStore.clear()
                lockStore.clear()
                monitorStore.clear()
                FleetPollWorker.cancelAll(context)
                BellhopPush.unregister(context, pushInstance)
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
                        monitorEnabled = monitorEnabled,
                        notificationsBlocked = monitorEnabled && !notificationsGranted,
                        pushEnabled = pushEnabled,
                        pushEndpoint = pushEndpoint,
                        pushDistributorAvailable = pushDistributorAvailable,
                        pushNotificationsBlocked = pushEnabled && !notificationsGranted,
                        scope = scope,
                        unlinking = unlinking,
                        unlinkFailed = unlinkFailed,
                        onDismissUnlinkError = { unlinkFailed = false },
                        onToggleMonitor = { toggleMonitor(it) },
                        onTogglePush = { togglePush(it) },
                        onUnlink = { runUnlink(state.fdUrl) },
                        onForceUnlink = { forceUnlink() },
                        requireOperatorAuth = { action -> requireOperatorAuth(action) },
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
    monitorEnabled: Boolean,
    notificationsBlocked: Boolean,
    pushEnabled: Boolean,
    pushEndpoint: String?,
    pushDistributorAvailable: Boolean,
    pushNotificationsBlocked: Boolean,
    scope: CoroutineScope,
    unlinking: Boolean,
    unlinkFailed: Boolean,
    onDismissUnlinkError: () -> Unit,
    onToggleMonitor: (Boolean) -> Unit,
    onTogglePush: (Boolean) -> Unit,
    onUnlink: () -> Unit,
    onForceUnlink: () -> Unit,
    requireOperatorAuth: (() -> Unit) -> Unit,
) {
    // Self-heal the Layer-2 poll: periodic work persists in WorkManager on its
    // own, but re-asserting the schedule here (KEEP policy, so no interval reset)
    // recovers it after a reinstall and stops it if monitoring was turned off.
    val monitorContext = LocalContext.current
    LaunchedEffect(monitorEnabled) {
        if (monitorEnabled) {
            FleetPollWorker.schedule(monitorContext)
        } else {
            FleetPollWorker.cancel(monitorContext)
        }
    }
    // Self-heal the Layer-3 push: UnifiedPush registration is persistent, but
    // re-registering here refreshes the endpoint and recovers it after a reinstall;
    // unregister if push was turned off. Also keyed on distributor availability so
    // installing a distributor while push is already on registers without a manual
    // off/on. Registration needs an Activity (choosing a distributor may show a
    // picker), so it's a no-op if we aren't hosted by one.
    val pushActivity = monitorContext as? FragmentActivity
    LaunchedEffect(pushEnabled, pushDistributorAvailable) {
        // Read the registration id fresh here rather than through a recomposed
        // param: setPushEnabled writes the flag and a new id in one edit, so by the
        // time this effect keys off pushEnabled the id is already the current one,
        // and register/unregister target the same instance the callbacks compare.
        val store = MonitorStore.create(monitorContext)
        if (pushEnabled) {
            val instance = store.pushInstance()
            if (pushActivity != null && instance != null) BellhopPush.register(pushActivity, instance)
        } else {
            BellhopPush.unregister(monitorContext, store.pushInstance())
        }
    }
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
            monitorEnabled = monitorEnabled,
            notificationsBlocked = notificationsBlocked,
            pushEnabled = pushEnabled,
            pushEndpoint = pushEndpoint,
            pushDistributorAvailable = pushDistributorAvailable,
            pushNotificationsBlocked = pushNotificationsBlocked,
            onBack = { showSettings = false },
            onToggleLock = { enabled -> scope.launch { lockStore.setEnabled(enabled) } },
            onSelectTimeout = { option -> scope.launch { lockStore.setTimeout(option.millis) } },
            onToggleMonitor = onToggleMonitor,
            onTogglePush = onTogglePush,
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
            // Role-hint UI: an operator device gets the controls, a monitor
            // doesn't. Front Desk's 403 is still the real guard (surfaced in the
            // card). Each action goes through the biometric operator gate.
            canOperate = state.role == OPERATOR_ROLE,
            onSetState = { target -> requireOperatorAuth { detailVm.setMemberState(target) } },
            onSyncFleet = { requireOperatorAuth { detailVm.syncFleet(ui.primaryId) } },
            onReconcile = { liveState -> detailVm.reconcile(liveState) },
            onDismissActionError = { detailVm.dismissActionError() },
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
