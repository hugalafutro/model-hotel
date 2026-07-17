package com.hugalafutro.bellhop.ui.dashboard

import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.BuildConfig
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.ui.common.ConfirmOpenUrlDialog
import com.hugalafutro.bellhop.ui.common.LockFab
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.ScrollToTopButton
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.TightTouchTarget
import com.hugalafutro.bellhop.ui.common.TrafficChart
import com.hugalafutro.bellhop.ui.common.bellhopSwitchColors
import com.hugalafutro.bellhop.ui.common.healthColor
import com.hugalafutro.bellhop.ui.common.healthLabel
import com.hugalafutro.bellhop.ui.common.relativeAgo
import com.hugalafutro.bellhop.ui.common.severityColors
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import java.time.Instant

/**
 * DashboardScreen is the linked-state home: the fleet's members with their live
 * health. Member data arrives via [DashboardViewModel]'s poll; a refresh failure
 * keeps the stale list visible under an error banner. Settings (link status, app
 * lock, Unlink) lives behind the gear; the bell into Alerts appears only when a
 * member is down.
 */
@Composable
fun DashboardScreen(
    link: LinkState.Linked,
    modifier: Modifier = Modifier,
    ui: DashboardUiState = DashboardUiState(),
    canOperate: Boolean = false,
    onMemberClick: (String) -> Unit = {},
    onEventsClick: () -> Unit = {},
    onAlertsClick: () -> Unit = {},
    onSettingsClick: () -> Unit = {},
    onSetAutoSync: (Boolean) -> Unit = {},
    onDismissAutoSyncError: () -> Unit = {},
    onVisibleMembers: (List<String>) -> Unit = {},
    // When true, a long-press on a member card copies it to the clipboard (tap
    // still opens the member). Off leaves the card tap-only (Settings > Hold to copy).
    holdToCopy: Boolean = false,
    // When the app lock is enabled in Settings, a bottom-right lock FAB appears;
    // long-pressing it fires [onLock] (a tap only hints, so a stray touch can't lock).
    lockEnabled: Boolean = false,
    onLock: () -> Unit = {},
) {
    // Long-press copies a member row as text, with a toast to confirm the
    // otherwise-silent act. Gated on [holdToCopy] so it never fires by accident.
    val clipboard = LocalClipboardManager.current
    val context = LocalContext.current
    val memberCopiedMsg = stringResource(R.string.dashboard_member_copied)

    // Which member's URL the "open externally" popup is showing, if any. Tapping
    // a card's address opens this confirm dialog rather than firing an intent on
    // the same tap that could also be a mis-tap on the card itself.
    var urlDialogFor by remember { mutableStateOf<FleetMember?>(null) }
    urlDialogFor?.let { member ->
        ConfirmOpenUrlDialog(url = member.url, onDismiss = { urlDialogFor = null })
    }

    // Build footer: tapping it confirms before leaving for GitHub. Stamped builds
    // deep-link the exact commit; an unstamped (source-tarball) build links the repo.
    var showBuildInfoUrl by remember { mutableStateOf(false) }
    if (showBuildInfoUrl) {
        ConfirmOpenUrlDialog(
            url = buildInfoUrl(),
            title = stringResource(R.string.open_url_title),
            onDismiss = { showBuildInfoUrl = false },
        )
    }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        // Lives in the Scaffold FAB slot so it stays bottom-right across every
        // dashboard state (loading, empty, populated), not just the member list.
        floatingActionButton = { if (lockEnabled) LockFab(onLock = onLock) },
    ) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 16.dp),
        ) {
            Spacer(modifier = Modifier.height(8.dp))
            // A member being down is the "something needs attention" signal, so the
            // bell into Alerts appears only then; the event log and the gear
            // (Settings, which also reaches Alerts when all is green) are always on.
            val hasAlert = ui.members.any { it.status.health.known && !it.status.health.healthy }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = link.fdName.ifBlank { link.fdUrl },
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.primary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f).testTag("dashboard-title"),
                )
                IconButton(
                    onClick = onEventsClick,
                    modifier = Modifier.testTag("dashboard-events"),
                ) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.List,
                        contentDescription = stringResource(R.string.events_open),
                    )
                }
                if (hasAlert) {
                    IconButton(
                        onClick = onAlertsClick,
                        modifier = Modifier.testTag("dashboard-alerts"),
                    ) {
                        Icon(
                            imageVector = Icons.Filled.Notifications,
                            contentDescription = stringResource(R.string.alerts_open),
                            tint = MaterialTheme.colorScheme.error,
                        )
                    }
                }
                IconButton(
                    onClick = onSettingsClick,
                    modifier = Modifier.testTag("dashboard-settings"),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Settings,
                        contentDescription = stringResource(R.string.settings_open),
                    )
                }
            }
            Spacer(modifier = Modifier.height(12.dp))

            if (ui.revoked) {
                StatusBanner(
                    text = stringResource(R.string.dashboard_revoked),
                    tag = "dashboard-revoked",
                )
            } else if (ui.error != null) {
                StatusBanner(
                    text = stringResource(R.string.dashboard_refresh_failed, ui.error),
                    tag = "dashboard-error",
                )
            }

            // Pause/resume auto-sync: an operator lever, shown only once a primary
            // is configured (choosing one stays a web action) and only to an
            // operator device. Front Desk's 403 is still the real guard, collapsing
            // the card to a note if the role hint is wrong.
            if (canOperate && ui.primaryId.isNotEmpty()) {
                AutoSyncControl(
                    action = ui.autoSync,
                    enabled = ui.autoSyncEnabled,
                    onSetAutoSync = onSetAutoSync,
                    onDismissError = onDismissAutoSyncError,
                )
                Spacer(modifier = Modifier.height(12.dp))
            }

            when {
                ui.loading ->
                    Box(
                        modifier = Modifier.fillMaxWidth().weight(1f),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(modifier = Modifier.testTag("dashboard-loading"))
                    }
                ui.members.isEmpty() ->
                    Box(
                        modifier = Modifier.fillMaxWidth().weight(1f),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            text = stringResource(R.string.dashboard_empty),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.testTag("dashboard-empty"),
                        )
                    }
                else -> {
                    FleetSummary(
                        members = ui.members,
                        fleetState = ui.fleetState,
                        fleetStateReasons = ui.fleetStateReasons,
                    )
                    val listState = rememberLazyListState()
                    // Report which members are on screen so the ViewModel fetches
                    // traffic only for them (viewport-bounded fan-out; a big fleet
                    // never triggers a call per member). Keyed on listState only so
                    // the collector isn't torn down every health poll; members are
                    // read live via rememberUpdatedState, and mapping index->id
                    // inside snapshotFlow means a roster change with the same
                    // visible indices still re-reports the (now different) ids.
                    val liveMembers = rememberUpdatedState(ui.members)
                    LaunchedEffect(listState) {
                        snapshotFlow {
                            listState.layoutInfo.visibleItemsInfo
                                .mapNotNull { liveMembers.value.getOrNull(it.index)?.id }
                        }
                            .distinctUntilChanged()
                            .collect(onVisibleMembers)
                    }
                    // Footer placement: when the list overflows the viewport the
                    // footer rides along as the last item and scrolls off; when it
                    // fits, the footer is overlaid pinned to the bottom of the screen
                    // instead. The bottom content padding reserves the footer's height
                    // so a nearly-full list never hides its last card under the overlay.
                    val footerScrolls by remember {
                        derivedStateOf { listState.canScrollForward || listState.canScrollBackward }
                    }
                    Box(modifier = Modifier.weight(1f).fillMaxWidth()) {
                        LazyColumn(
                            state = listState,
                            modifier = Modifier.fillMaxSize(),
                            verticalArrangement = Arrangement.spacedBy(8.dp),
                            contentPadding = PaddingValues(bottom = 56.dp),
                        ) {
                            // Deliberately unkeyed: member ids are FD database primary
                            // keys so duplicates shouldn't happen, but a buggy response
                            // with duplicate ids would crash a keyed LazyColumn outright.
                            // Positional identity is fine for a small stateless list.
                            items(ui.members) { member ->
                                MemberCard(
                                    member = member,
                                    isPrimary = member.id == ui.primaryId,
                                    traffic = ui.traffic[member.id],
                                    recentEvent = ui.recentEvents[member.id],
                                    onClick = { onMemberClick(member.id) },
                                    onUrlClick = { urlDialogFor = member },
                                    onLongClick =
                                        if (holdToCopy) {
                                            {
                                                clipboard.setText(AnnotatedString(memberClipboardText(member)))
                                                Toast.makeText(context, memberCopiedMsg, Toast.LENGTH_SHORT).show()
                                            }
                                        } else {
                                            null
                                        },
                                )
                            }
                            // Version/build footer, mimicking Front Desk's: a divider then
                            // a centered, tappable "Bellhop <version> · <commit>" that opens
                            // GitHub (the exact commit when stamped) behind the confirm modal.
                            // It rides the list (and scrolls off) only when the list scrolls.
                            if (footerScrolls) {
                                item {
                                    DashboardFooter(onClick = { showBuildInfoUrl = true })
                                }
                            }
                        }
                        // Otherwise it's pinned to the bottom of the screen, over the
                        // empty space the reserved bottom padding leaves below the list.
                        if (!footerScrolls) {
                            DashboardFooter(
                                onClick = { showBuildInfoUrl = true },
                                modifier = Modifier.align(Alignment.BottomCenter),
                            )
                        }
                        ScrollToTopButton(
                            listState = listState,
                            // Lifted clear of the lock FAB when it's present, so the
                            // two never stack on the same bottom-right corner.
                            modifier = if (lockEnabled) Modifier.padding(bottom = 52.dp) else Modifier,
                        )
                    }
                }
            }
        }
    }
}

/**
 * AutoSyncControl is the pause/resume operator lever. It shows the effective state
 * optimistically ([AutoSyncAction.pendingEnabled] over the live value) so the
 * toggle reflects a just-sent change while it reconciles, collapses to a guard
 * note when Front Desk returns 403, and surfaces a failure with a dismiss.
 */
@Composable
private fun AutoSyncControl(
    action: AutoSyncAction,
    enabled: Boolean,
    onSetAutoSync: (Boolean) -> Unit,
    onDismissError: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val effective = action.pendingEnabled ?: enabled
    Card(modifier = modifier.fillMaxWidth().testTag("autosync-card")) {
        Column(
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            if (action.forbidden) {
                // Front Desk's 403 is the authoritative guard: drop the toggle and
                // show why, the same collapse the member-detail operator card does.
                Text(
                    text = stringResource(R.string.autosync_forbidden),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag("autosync-forbidden"),
                )
            } else {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.autosync_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(if (effective) R.string.autosync_on else R.string.autosync_off),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.testTag("autosync-status"),
                        )
                    }
                    Switch(
                        checked = effective,
                        onCheckedChange = onSetAutoSync,
                        enabled = !action.inProgress,
                        colors = bellhopSwitchColors(),
                        modifier = Modifier.testTag("autosync-toggle"),
                    )
                }
                if (action.pendingEnabled != null) {
                    Text(
                        text =
                            stringResource(
                                if (action.pendingEnabled) R.string.autosync_resuming else R.string.autosync_pausing,
                            ),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("autosync-pending"),
                    )
                }
                if (action.busy) {
                    Text(
                        text = stringResource(R.string.autosync_busy),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("autosync-busy"),
                    )
                }
            }
            if (action.error != null) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = stringResource(R.string.autosync_error, action.error),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.weight(1f).testTag("autosync-error"),
                    )
                    TextButton(onClick = onDismissError) {
                        Text(stringResource(R.string.member_op_dismiss))
                    }
                }
            }
        }
    }
}

/** FleetSummary is the one-line rollup above the list. It prefers the server's
 * fleet-state verdict (state + translated reason codes) and falls back to the
 * local all-up/down count when Front Desk predates the field. While every
 * member is still unprobed the local "checking" line wins even over a server
 * "ok": FD reports ok before its first probes, and "checking" is honester. */
@Composable
private fun FleetSummary(
    members: List<FleetMember>,
    fleetState: String,
    fleetStateReasons: List<String>,
    modifier: Modifier = Modifier,
) {
    val allUnknown = members.isNotEmpty() && members.all { !it.status.health.known }
    val server = if (allUnknown) null else serverFleetSummary(fleetState, fleetStateReasons)
    val (text, color) = server ?: localFleetSummary(members)
    Text(
        text = text,
        style = MaterialTheme.typography.labelLarge,
        color = color,
        modifier = modifier.padding(bottom = 8.dp).testTag("dashboard-summary"),
    )
}

/** serverFleetSummary renders the server verdict, or null for unknown/absent
 * states so the caller falls back to the local rollup. */
@Composable
private fun serverFleetSummary(
    state: String,
    reasons: List<String>,
): Pair<String, Color>? {
    val stateText =
        when (state) {
            "ok" -> stringResource(R.string.dashboard_summary_all_up)
            "degraded" -> stringResource(R.string.dashboard_state_degraded)
            "faulty" -> stringResource(R.string.dashboard_state_faulty)
            else -> return null
        }
    val color =
        when (state) {
            "ok" -> MaterialTheme.colorScheme.tertiary
            // Brand accent (brass on dark, copper on light) reads on the dashboard
            // surface in both themes; SeverityWarnFg is near-black and only legible
            // as the foreground on the orange warn badge, not as standalone text.
            "degraded" -> MaterialTheme.colorScheme.primary
            else -> MaterialTheme.colorScheme.error
        }
    val suffix = reasons.map { fleetReasonLabel(it) }.joinToString(" · ")
    return (if (suffix.isEmpty()) stateText else "$stateText · $suffix") to color
}

/** localFleetSummary is the pre-fleet-state rollup, kept as the fallback. */
@Composable
private fun localFleetSummary(members: List<FleetMember>): Pair<String, Color> {
    val down = members.count { it.status.health.known && !it.status.health.healthy }
    val unknown = members.count { !it.status.health.known }
    return when {
        down > 0 ->
            stringResource(R.string.dashboard_summary_down, down, members.size) to
                MaterialTheme.colorScheme.error
        unknown == members.size ->
            stringResource(R.string.dashboard_summary_checking) to
                MaterialTheme.colorScheme.onSurfaceVariant
        else ->
            stringResource(R.string.dashboard_summary_all_up) to
                MaterialTheme.colorScheme.tertiary
    }
}

/** fleetReasonLabel translates a wire reason code; unknown codes (a newer
 * server) fall back to the raw code, mirroring eventTypeLabel. */
@Composable
private fun fleetReasonLabel(code: String): String =
    when (code) {
        "member_down" -> stringResource(R.string.fleet_reason_member_down)
        "all_members_down" -> stringResource(R.string.fleet_reason_all_members_down)
        "member_drained" -> stringResource(R.string.fleet_reason_member_drained)
        "drained_to_single" -> stringResource(R.string.fleet_reason_drained_to_single)
        "sync_held" -> stringResource(R.string.fleet_reason_sync_held)
        "all_sync_held" -> stringResource(R.string.fleet_reason_all_sync_held)
        "autosync_stale" -> stringResource(R.string.fleet_reason_autosync_stale)
        "autosync_stale_long" -> stringResource(R.string.fleet_reason_autosync_stale_long)
        "traefik_config_stale" -> stringResource(R.string.fleet_reason_traefik_config_stale)
        else -> code
    }

// The plain-text a long-press copies for a member: its name and, when known, its
// URL on the next line, so it pastes cleanly into a note or bug report.
private fun memberClipboardText(member: FleetMember): String =
    buildString {
        append(member.name)
        if (member.url.isNotBlank()) {
            append('\n')
            append(member.url)
        }
    }

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun MemberCard(
    member: FleetMember,
    isPrimary: Boolean,
    traffic: MemberTraffic?,
    onClick: () -> Unit,
    onUrlClick: () -> Unit,
    modifier: Modifier = Modifier,
    // This member's most recent event, shown as a severity-tinted pill under the
    // sparkline; null hides it. Tapping the pill opens the member (its detail log
    // is newest-first, so this event sits at the top).
    recentEvent: FdEvent? = null,
    // When set, a long-press copies the member; a tap still opens it. Uses
    // combinedClickable on the Card's own modifier (Card's onClick overload has
    // no long-press hook).
    onLongClick: (() -> Unit)? = null,
) {
    val health = member.status.health
    val healthColor = healthColor(health)
    Card(
        modifier =
            modifier
                .fillMaxWidth()
                .testTag("member-card-${member.name}")
                .combinedClickable(
                    role = Role.Button,
                    onClick = onClick,
                    onLongClick = onLongClick,
                ),
    ) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Box(
                    modifier =
                        Modifier
                            .size(10.dp)
                            .clip(CircleShape)
                            .background(healthColor),
                )
                Text(
                    text = member.name,
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (isPrimary) {
                    Pill(
                        text = stringResource(R.string.member_primary),
                        container = MaterialTheme.colorScheme.secondaryContainer,
                        content = MaterialTheme.colorScheme.onSecondaryContainer,
                        tag = "member-primary",
                    )
                }
                if (member.drained) {
                    Pill(
                        text = stringResource(R.string.member_state_drained),
                        container = MaterialTheme.colorScheme.errorContainer,
                        content = MaterialTheme.colorScheme.onErrorContainer,
                        tag = "member-drained",
                    )
                }
            }
            // The URL is its own tap target (opens the "open externally" popup),
            // separate from the card tap that drills into the member. Its touch
            // target is capped to the text bounds: the default 48dp minimum
            // spills over the name row above and the health row below, so taps
            // meant to open the member land on the link instead. The card around
            // it stays the big target.
            TightTouchTarget {
                Text(
                    text = member.url,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier =
                        Modifier
                            .clickable(onClick = onUrlClick)
                            .testTag("member-url-${member.name}"),
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = healthLabel(health),
                    style = MaterialTheme.typography.bodySmall,
                    color = healthColor,
                )
                if (health.known && member.status.traefikStatus.isNotBlank()) {
                    Text(
                        text = stringResource(R.string.member_traefik, member.status.traefikStatus),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (member.status.version.isNotBlank()) {
                    Text(
                        text = member.status.version,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            if (health.known && !health.healthy && health.error.isNotBlank()) {
                Text(
                    text = health.error,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            // Inline last-hour sparkline: shown once its (viewport-lazy) traffic
            // has arrived and is reachable. A quiet window (buckets present but no
            // requests) draws the idle sparkline with the same "No requests in this
            // window." label the detail screen uses centred over it, so the flat
            // line is explained here rather than looking broken. Absent/unreachable
            // still leaves the card compact. Tapping the card (including here) opens
            // the detail with the full graph + event log.
            traffic?.takeIf { it.reachable && it.points.isNotEmpty() }?.let { t ->
                Spacer(modifier = Modifier.height(2.dp))
                Box(modifier = Modifier.fillMaxWidth()) {
                    TrafficChart(
                        points = t.points,
                        modifier = Modifier.height(28.dp).testTag("member-sparkline-${member.name}"),
                    )
                    if (t.totalRequests == 0) {
                        Text(
                            text = stringResource(R.string.member_detail_traffic_empty),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier =
                                Modifier
                                    .align(Alignment.Center)
                                    .testTag("member-sparkline-empty-${member.name}"),
                        )
                    }
                }
            }
            // Recent-event line: a quiet, one-line preview of this member's
            // latest event with its age right-aligned, tappable straight into
            // the member. A small dot plus a faint tint carry severity (the
            // saturated palette is badges-only); the line itself stays
            // surface-toned so it reads as a log line, not a button.
            recentEvent?.let { ev ->
                val (evAccent, _) = severityColors(ev.severity)
                // Tick a local clock so the "3 min ago" age recomputes on its own.
                // Without it the age is frozen: eventAgo is memoized on the event's
                // timestamp, which doesn't change between refreshes, so a stationary
                // event's label would never advance. Scoped to cards that actually
                // show an event, and disposed with the dashboard when it's covered.
                val now by produceState(System.currentTimeMillis()) {
                    while (true) {
                        delay(RELATIVE_TIME_TICK_MS)
                        value = System.currentTimeMillis()
                    }
                }
                Spacer(modifier = Modifier.height(4.dp))
                Surface(
                    onClick = onClick,
                    color = evAccent.copy(alpha = 0.06f),
                    contentColor = MaterialTheme.colorScheme.onSurface,
                    shape = RoundedCornerShape(8.dp),
                    modifier = Modifier.fillMaxWidth().testTag("member-recent-event-${member.name}"),
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                    ) {
                        Box(
                            modifier =
                                Modifier
                                    .size(8.dp)
                                    .clip(CircleShape)
                                    .background(evAccent),
                        )
                        Text(
                            text = ev.message.ifBlank { ev.type },
                            style = MaterialTheme.typography.bodySmall,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        val context = LocalContext.current
                        remember(ev.createdAt, now, context) { eventAgo(context, ev.createdAt, now) }?.let { ago ->
                            Text(
                                text = ago,
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1,
                            )
                        }
                    }
                }
            }
        }
    }
}

// DashboardFooter mimics Front Desk's footer: a divider, then a centered, tappable
// "Bellhop <version> · <commit>" build stamp. The tap is handled by the caller
// (a confirm-before-leaving dialog); this only renders the label.
@Composable
private fun DashboardFooter(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
        // Only the version/commit is the link, so brass it; the "Bellhop" label
        // stays the standard body colour.
        val label = buildInfoLabel()
        val full = stringResource(R.string.dashboard_footer, label)
        val text =
            buildAnnotatedString {
                append(full)
                val start = full.indexOf(label)
                if (start >= 0) {
                    addStyle(SpanStyle(color = MaterialTheme.colorScheme.primary), start, start + label.length)
                }
            }
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurface,
            modifier =
                Modifier
                    .clickable(onClick = onClick)
                    .padding(bottom = 8.dp)
                    .testTag("dashboard-footer"),
        )
    }
}

// eventAgo turns an event's RFC3339 timestamp into a terse "3 days ago"-style age
// for the recent-event pill, or null when it can't be parsed (the pill then just
// omits the age rather than showing a raw string).
private fun eventAgo(
    context: Context,
    createdAt: String,
    now: Long = System.currentTimeMillis(),
): String? =
    try {
        relativeAgo(context, (now - Instant.parse(createdAt).toEpochMilli()).coerceAtLeast(0L))
    } catch (e: Exception) {
        null
    }

// How often the recent-event pill re-evaluates its relative age. 30s keeps the
// "N min ago" label honest to within half a minute without waking often; it is a
// CPU-only recomposition of a couple of Text nodes, no network.
private const val RELATIVE_TIME_TICK_MS = 30_000L

private const val REPO_URL = "https://github.com/hugalafutro/model-hotel"

// hasCommit is true for a stamped build (any real short sha), false for a source
// build where GIT_COMMIT fell back to "unknown".
private fun hasCommit(): Boolean = BuildConfig.GIT_COMMIT.isNotBlank() && BuildConfig.GIT_COMMIT != "unknown"

// buildInfoLabel is the version span: "v0.1.0" plus the commit when stamped.
private fun buildInfoLabel(): String =
    buildString {
        append('v')
        append(BuildConfig.VERSION_NAME)
        if (hasCommit()) {
            append(" · ")
            append(BuildConfig.GIT_COMMIT)
        }
    }

// buildInfoUrl deep-links the exact commit on a stamped build (dropping any
// "-dirty" marker, which isn't part of a real sha), else the repo root.
private fun buildInfoUrl(): String =
    if (hasCommit()) {
        "$REPO_URL/commit/${BuildConfig.GIT_COMMIT.removeSuffix("-dirty")}"
    } else {
        REPO_URL
    }

@Preview(showBackground = true)
@Composable
private fun DashboardScreenPreview() {
    BellhopTheme {
        DashboardScreen(
            link =
                LinkState.Linked(
                    fdUrl = "http://10.0.2.2:8080",
                    fdName = "Home Front Desk",
                    role = "operator",
                    deviceId = "dev-1",
                    label = "Pixel 8",
                ),
            ui =
                DashboardUiState(
                    loading = false,
                    primaryId = "m1",
                    members =
                        listOf(
                            FleetMember(
                                id = "m1",
                                name = "hotel-1",
                                url = "http://10.0.0.10:8080",
                                status =
                                    MemberStatus(
                                        health = HealthStatus(known = true, healthy = true, latencyMs = 12),
                                        traefikStatus = "UP",
                                        version = "0.31.0",
                                    ),
                            ),
                            FleetMember(
                                id = "m2",
                                name = "hotel-2",
                                url = "http://10.0.0.11:8080",
                                state = "drained",
                                status =
                                    MemberStatus(
                                        health =
                                            HealthStatus(
                                                known = true,
                                                healthy = false,
                                                error = "connection refused",
                                            ),
                                        traefikStatus = "DOWN",
                                    ),
                            ),
                        ),
                ),
        )
    }
}
