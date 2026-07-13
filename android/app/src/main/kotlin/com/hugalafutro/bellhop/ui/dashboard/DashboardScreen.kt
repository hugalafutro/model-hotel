package com.hugalafutro.bellhop.ui.dashboard

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.TrafficChart
import com.hugalafutro.bellhop.ui.common.healthColor
import com.hugalafutro.bellhop.ui.common.healthLabel
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import kotlinx.coroutines.flow.distinctUntilChanged

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
) {
    // Which member's URL the "open externally" popup is showing, if any. Tapping
    // a card's address opens this confirm dialog rather than firing an intent on
    // the same tap that could also be a mis-tap on the card itself.
    var urlDialogFor by remember { mutableStateOf<FleetMember?>(null) }
    val context = LocalContext.current
    urlDialogFor?.let { member ->
        AlertDialog(
            onDismissRequest = { urlDialogFor = null },
            title = { Text(stringResource(R.string.member_url_title)) },
            text = {
                Text(
                    text = member.url,
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.testTag("member-url-dialog-text"),
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        // ACTION_VIEW lets Android resolve the URL (browser or a
                        // matching app), showing its own chooser when several match.
                        // runCatching: a device with nothing that can open it must
                        // not crash the app.
                        runCatching {
                            context.startActivity(
                                Intent(Intent.ACTION_VIEW, Uri.parse(member.url)),
                            )
                        }
                        urlDialogFor = null
                    },
                    modifier = Modifier.testTag("member-url-open"),
                ) {
                    Text(stringResource(R.string.member_url_open))
                }
            },
            dismissButton = {
                TextButton(onClick = { urlDialogFor = null }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
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
                    FleetSummary(members = ui.members)
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
                    LazyColumn(
                        state = listState,
                        modifier = Modifier.weight(1f),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                        contentPadding = PaddingValues(bottom = 24.dp),
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
                                onClick = { onMemberClick(member.id) },
                                onUrlClick = { urlDialogFor = member },
                            )
                        }
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

/** FleetSummary is the one-line rollup above the list: all up, or how many down. */
@Composable
private fun FleetSummary(
    members: List<FleetMember>,
    modifier: Modifier = Modifier,
) {
    val down = members.count { it.status.health.known && !it.status.health.healthy }
    val unknown = members.count { !it.status.health.known }
    val (text, color) =
        when {
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
    Text(
        text = text,
        style = MaterialTheme.typography.labelLarge,
        color = color,
        modifier = modifier.padding(bottom = 8.dp).testTag("dashboard-summary"),
    )
}

@Composable
private fun MemberCard(
    member: FleetMember,
    isPrimary: Boolean,
    traffic: MemberTraffic?,
    onClick: () -> Unit,
    onUrlClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val health = member.status.health
    val healthColor = healthColor(health)
    Card(onClick = onClick, modifier = modifier.fillMaxWidth().testTag("member-card-${member.name}")) {
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
            // separate from the card tap that drills into the member.
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
            // has arrived and there is something to draw. Absent/empty just leaves
            // the card compact rather than reserving blank space. Tapping the card
            // (including here) opens the detail with the full graph + event log.
            traffic?.points?.takeIf { traffic.reachable && it.isNotEmpty() }?.let { points ->
                Spacer(modifier = Modifier.height(2.dp))
                TrafficChart(
                    points = points,
                    modifier = Modifier.height(28.dp).testTag("member-sparkline-${member.name}"),
                )
            }
        }
    }
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
