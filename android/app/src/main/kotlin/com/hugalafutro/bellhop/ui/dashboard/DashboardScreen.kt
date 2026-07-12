package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.foundation.background
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.healthColor
import com.hugalafutro.bellhop.ui.common.healthLabel
import com.hugalafutro.bellhop.ui.theme.BellhopTheme

/**
 * DashboardScreen is the linked-state home: the fleet's members with their live
 * health, plus Unlink. Member data arrives via [DashboardViewModel]'s poll; a
 * refresh failure keeps the stale list visible under an error banner.
 */
@Composable
fun DashboardScreen(
    link: LinkState.Linked,
    onUnlink: () -> Unit,
    unlinking: Boolean,
    modifier: Modifier = Modifier,
    ui: DashboardUiState = DashboardUiState(),
    unlinkFailed: Boolean = false,
    onDismissUnlinkError: () -> Unit = {},
    onForceUnlink: () -> Unit = {},
    onMemberClick: (String) -> Unit = {},
    onEventsClick: () -> Unit = {},
) {
    var confirmUnlink by remember { mutableStateOf(false) }

    // The remote revoke couldn't reach Front Desk (or the token can't be read to
    // revoke at all). The device is still linked and nothing was cleared, so
    // offer a retry AND an "unlink anyway" escape: with a dead/unreachable token a
    // retry can loop forever, so the operator needs a way to clear locally (and is
    // told to revoke on Front Desk) rather than being stranded on this screen.
    if (unlinkFailed) {
        AlertDialog(
            onDismissRequest = onDismissUnlinkError,
            title = { Text(stringResource(R.string.dashboard_unlink_failed_title)) },
            text = { Text(stringResource(R.string.dashboard_unlink_failed_body)) },
            confirmButton = {
                TextButton(
                    enabled = !unlinking,
                    onClick = {
                        onDismissUnlinkError()
                        onUnlink()
                    },
                    modifier = Modifier.testTag("dashboard-unlink-retry"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink_retry))
                }
            },
            dismissButton = {
                TextButton(
                    enabled = !unlinking,
                    onClick = {
                        onDismissUnlinkError()
                        onForceUnlink()
                    },
                    modifier = Modifier.testTag("dashboard-unlink-force"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink_force))
                }
            },
        )
    }

    if (confirmUnlink) {
        AlertDialog(
            onDismissRequest = { confirmUnlink = false },
            title = { Text(stringResource(R.string.dashboard_unlink_confirm_title)) },
            text = {
                Text(
                    stringResource(
                        R.string.dashboard_unlink_confirm_body,
                        link.fdName.ifBlank { link.fdUrl },
                    ),
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !unlinking,
                    onClick = {
                        confirmUnlink = false
                        onUnlink()
                    },
                    modifier = Modifier.testTag("dashboard-unlink-confirm"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink))
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmUnlink = false }) {
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
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = link.fdName.ifBlank { link.fdUrl },
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.primary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.testTag("dashboard-title"),
                    )
                    Text(
                        text = stringResource(R.string.dashboard_linked_as, link.label, link.role),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("dashboard-linked"),
                    )
                }
                IconButton(
                    onClick = onEventsClick,
                    modifier = Modifier.testTag("dashboard-events"),
                ) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.List,
                        contentDescription = stringResource(R.string.events_open),
                    )
                }
                TextButton(
                    onClick = { confirmUnlink = true },
                    enabled = !unlinking,
                    modifier = Modifier.testTag("dashboard-unlink"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink))
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
                    LazyColumn(
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
                                onClick = { onMemberClick(member.id) },
                            )
                        }
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
    onClick: () -> Unit,
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
            Text(
                text = member.url,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
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
            onUnlink = {},
            unlinking = false,
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
