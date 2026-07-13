package com.hugalafutro.bellhop.ui.member

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.MemberState
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.TrafficPoint
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.TrafficChart
import com.hugalafutro.bellhop.ui.common.healthColor
import com.hugalafutro.bellhop.ui.common.severityColors
import com.hugalafutro.bellhop.ui.events.formatEventTime
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import com.hugalafutro.bellhop.ui.theme.MonoFamily

/**
 * MemberDetailScreen is one member up close — deliberately *not* a repeat of the
 * dashboard card. A minimal header (name + live health dot) sits above the last
 * hour of traffic (the full graph the card only sparklines) and the info the
 * card has no room for: the probe error when down, config-sync provenance, the
 * in-sync heartbeat, when it was added, and the member's recent event log.
 * [member] arrives live from the dashboard's poll; [ui] carries the graph +
 * events from [MemberDetailViewModel].
 */
@Composable
fun MemberDetailScreen(
    member: FleetMember,
    isPrimary: Boolean,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    ui: MemberDetailUiState = MemberDetailUiState(),
    // Whether this paired device holds the operator role (from the link). UX only:
    // Front Desk's 403 is the real guard (surfaced via [ui.action.forbidden]), but
    // hiding the controls on a monitor device avoids a pointless denied tap.
    canOperate: Boolean = false,
    onSetState: (String) -> Unit = {},
    onSyncFleet: () -> Unit = {},
    onReconcile: (String) -> Unit = {},
    onDismissActionError: () -> Unit = {},
) {
    val health = member.status.health
    // Clear the optimistic pending state once the dashboard's live state (member
    // arrives live from its poll/SSE) has caught up to the accepted target.
    LaunchedEffect(member.state) { onReconcile(member.state) }
    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 16.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            ) {
                IconButton(onClick = onBack, modifier = Modifier.testTag("member-detail-back")) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.member_detail_back),
                    )
                }
                Box(
                    modifier =
                        Modifier
                            .size(10.dp)
                            .clip(CircleShape)
                            .background(healthColor(health)),
                )
                Text(
                    text = member.name,
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.primary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f).testTag("member-detail-title"),
                )
                if (isPrimary) {
                    Pill(
                        text = stringResource(R.string.member_primary),
                        container = MaterialTheme.colorScheme.secondaryContainer,
                        content = MaterialTheme.colorScheme.onSecondaryContainer,
                        tag = "member-detail-primary",
                    )
                }
            }

            if (ui.revoked) {
                StatusBanner(text = stringResource(R.string.dashboard_revoked), tag = "member-detail-revoked")
            } else if (ui.error != null) {
                StatusBanner(
                    text = stringResource(R.string.dashboard_refresh_failed, ui.error),
                    tag = "member-detail-error",
                )
            }

            LazyColumn(
                modifier = Modifier.weight(1f).testTag("member-detail-list"),
                verticalArrangement = Arrangement.spacedBy(12.dp),
                contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            ) {
                item { TrafficCard(ui = ui) }
                item { MetaCard(member = member) }
                if (canOperate) {
                    item {
                        OperatorCard(
                            member = member,
                            isPrimary = isPrimary,
                            action = ui.action,
                            onSetState = onSetState,
                            onSyncFleet = onSyncFleet,
                            onDismissActionError = onDismissActionError,
                        )
                    }
                }
                item {
                    Text(
                        text = stringResource(R.string.member_detail_events_title),
                        style = MaterialTheme.typography.titleMedium,
                        modifier = Modifier.testTag("member-detail-events-title"),
                    )
                }
                when {
                    ui.loading && ui.events.isEmpty() ->
                        item {
                            Box(
                                modifier = Modifier.fillMaxWidth().padding(vertical = 16.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                CircularProgressIndicator(modifier = Modifier.testTag("member-detail-events-loading"))
                            }
                        }
                    ui.events.isEmpty() ->
                        item {
                            Text(
                                text = stringResource(R.string.member_detail_no_events),
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.testTag("member-detail-no-events"),
                            )
                        }
                    else -> items(ui.events) { event -> MemberEventRow(event = event) }
                }
            }
        }
    }
}

/**
 * MetaCard is the info the dashboard card has no room for: the full probe error
 * when down, config-sync provenance, the in-sync heartbeat, and when the member
 * was added. Each line only shows when it has a value, so a healthy never-synced
 * member stays terse.
 */
@Composable
private fun MetaCard(
    member: FleetMember,
    modifier: Modifier = Modifier,
) {
    val health = member.status.health
    Card(modifier = modifier.fillMaxWidth().testTag("member-detail-meta")) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                text = member.url,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (health.known && !health.healthy && health.error.isNotBlank()) {
                Text(
                    text = health.error,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.testTag("member-detail-down-reason"),
                )
            }
            if (member.lastConfigSyncAt.isNotBlank()) {
                MetaLine(
                    text = stringResource(R.string.member_detail_synced, formatEventTime(member.lastConfigSyncAt)),
                    tag = "member-detail-synced",
                )
                if (member.lastConfigSyncReason.isNotBlank()) {
                    Text(
                        text = member.lastConfigSyncReason,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            if (member.status.autoSyncVerifiedAt.isNotBlank()) {
                MetaLine(
                    text =
                        stringResource(
                            R.string.member_detail_verified,
                            formatEventTime(member.status.autoSyncVerifiedAt),
                        ),
                    tag = "member-detail-verified",
                )
            }
            if (member.createdAt.isNotBlank()) {
                MetaLine(
                    text = stringResource(R.string.member_detail_created, formatEventTime(member.createdAt)),
                    tag = "member-detail-created",
                )
            }
        }
    }
}

@Composable
private fun MetaLine(
    text: String,
    tag: String,
) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.testTag(tag),
    )
}

/**
 * OperatorCard is the drain/activate (and, on the primary, fleet-sync) surface,
 * shown only to an operator device. The button reflects the effective state:
 * Front Desk's live state, or the optimistic pending target once an action was
 * accepted and until the dashboard reconciles it. A 403 from Front Desk collapses
 * the card to the guard note — the authoritative role check overriding the
 * role-hint UI. Actions are set-state, so a double-tap is a safe no-op; the
 * biometric prompt gating them lives in the host (MainActivity).
 */
@Composable
private fun OperatorCard(
    member: FleetMember,
    isPrimary: Boolean,
    action: ActionUiState,
    onSetState: (String) -> Unit,
    onSyncFleet: () -> Unit,
    onDismissActionError: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(modifier = modifier.fillMaxWidth().testTag("member-operator-card")) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = stringResource(R.string.member_op_title),
                style = MaterialTheme.typography.titleSmall,
            )
            if (action.forbidden) {
                // Front Desk refused the device's role: the real guard. Drop the
                // controls entirely so the note is the only thing left.
                Text(
                    text = stringResource(R.string.member_op_forbidden),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.testTag("member-op-forbidden"),
                )
                return@Column
            }

            // Optimistic pending target beats the live state until reconciled, so
            // a fresh tap flips the button immediately even before the fleet moves.
            val effectiveState = action.pendingState ?: member.state
            val drained = effectiveState == MemberState.DRAINED
            Button(
                onClick = { onSetState(if (drained) MemberState.ACTIVE else MemberState.DRAINED) },
                enabled = !action.inProgress,
                modifier = Modifier.fillMaxWidth().testTag("member-op-state"),
            ) {
                if (action.inProgress) {
                    CircularProgressIndicator(
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(16.dp),
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                } else {
                    Text(
                        text =
                            stringResource(
                                if (drained) R.string.member_op_activate else R.string.member_op_drain,
                            ),
                    )
                }
            }
            if (action.pendingState != null) {
                Text(
                    text =
                        stringResource(
                            if (drained) R.string.member_op_draining else R.string.member_op_activating,
                        ),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag("member-op-pending"),
                )
            }

            if (isPrimary) {
                OutlinedButton(
                    onClick = onSyncFleet,
                    enabled = !action.inProgress,
                    modifier = Modifier.fillMaxWidth().testTag("member-op-sync"),
                ) {
                    Text(text = stringResource(R.string.member_op_sync))
                }
            }
            action.syncSummary?.let { summary ->
                Text(
                    text =
                        if (summary.failed == 0) {
                            stringResource(R.string.member_op_synced, summary.total)
                        } else {
                            stringResource(R.string.member_op_sync_failed, summary.failed, summary.total)
                        },
                    style = MaterialTheme.typography.bodySmall,
                    color =
                        if (summary.failed == 0) {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        } else {
                            MaterialTheme.colorScheme.error
                        },
                    modifier = Modifier.testTag("member-op-sync-result"),
                )
            }
            action.error?.let { message ->
                Text(
                    text = stringResource(R.string.member_op_error, message),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.testTag("member-op-error"),
                )
                TextButton(
                    onClick = onDismissActionError,
                    modifier = Modifier.testTag("member-op-dismiss"),
                ) {
                    Text(text = stringResource(R.string.member_op_dismiss))
                }
            }
        }
    }
}

/**
 * TrafficCard renders the last-hour series bigger than the card sparkline.
 * Unreachable is a normal, explained state, not an error: Front Desk may hold no
 * admin token for this member, or the member didn't answer its stats API.
 */
@Composable
private fun TrafficCard(
    ui: MemberDetailUiState,
    modifier: Modifier = Modifier,
) {
    val traffic = ui.traffic
    Card(modifier = modifier.fillMaxWidth().testTag("member-traffic-card")) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = stringResource(R.string.member_detail_traffic_title, traffic?.windowMinutes ?: 60),
                style = MaterialTheme.typography.titleMedium,
            )
            when {
                traffic == null && ui.loading ->
                    Box(
                        modifier = Modifier.fillMaxWidth().padding(vertical = 16.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(modifier = Modifier.testTag("member-traffic-loading"))
                    }
                traffic == null -> Unit // fetch never landed; the banner above explains why
                !traffic.reachable ->
                    Text(
                        text = stringResource(R.string.member_detail_traffic_unreachable),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("member-traffic-unreachable"),
                    )
                traffic.points.isEmpty() ->
                    Text(
                        text = stringResource(R.string.member_detail_traffic_empty),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("member-traffic-empty"),
                    )
                else -> {
                    Text(
                        text =
                            stringResource(
                                R.string.member_detail_traffic_totals,
                                traffic.totalRequests,
                                traffic.totalErrors,
                            ),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("member-traffic-totals"),
                    )
                    TrafficChart(
                        points = traffic.points,
                        modifier = Modifier.height(140.dp).testTag("member-traffic-chart"),
                    )
                }
            }
        }
    }
}

/**
 * MemberEventRow is one event under the graph, in the same log language as the
 * Events screen: a severity-coloured rail down the card's left edge (no pill) and
 * the type as the heading with a mono timestamp, then the message.
 */
@Composable
private fun MemberEventRow(
    event: FdEvent,
    modifier: Modifier = Modifier,
) {
    val accent = severityColors(event.severity).first
    Card(modifier = modifier.fillMaxWidth().testTag("member-event-row")) {
        Row(modifier = Modifier.height(IntrinsicSize.Min)) {
            Box(
                modifier =
                    Modifier
                        .width(3.dp)
                        .fillMaxHeight()
                        .background(accent)
                        .testTag("member-event-sev-${event.severity}"),
            )
            Column(
                modifier = Modifier.weight(1f).padding(horizontal = 12.dp, vertical = 10.dp),
                verticalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        text = event.type,
                        style = MaterialTheme.typography.titleSmall,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        text = formatEventTime(event.createdAt),
                        style = MaterialTheme.typography.labelSmall,
                        fontFamily = MonoFamily,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    text = event.message,
                    style = MaterialTheme.typography.bodyMedium,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun MemberDetailScreenPreview() {
    BellhopTheme {
        MemberDetailScreen(
            member =
                FleetMember(
                    id = "m1",
                    name = "hotel-prime",
                    url = "http://192.168.1.10:8080",
                    createdAt = "2026-06-28T17:53:27Z",
                    lastConfigSyncAt = "2026-07-10T20:26:40Z",
                    lastConfigSyncReason = "the primary's config changed",
                    status =
                        MemberStatus(
                            health = HealthStatus(known = true, healthy = true, latencyMs = 12),
                            traefikStatus = "UP",
                            version = "0.33.0",
                            autoSyncVerifiedAt = "2026-07-12T13:42:17Z",
                        ),
                ),
            isPrimary = true,
            onBack = {},
            ui =
                MemberDetailUiState(
                    loading = false,
                    traffic =
                        MemberTraffic(
                            memberId = "m1",
                            reachable = true,
                            totalRequests = 420,
                            totalErrors = 7,
                            points =
                                (0 until 12).map {
                                    TrafficPoint(
                                        bucket = "b$it",
                                        requests = (it * 13) % 40,
                                        errors = if (it % 5 == 0) 2 else 0,
                                    )
                                },
                        ),
                    events =
                        listOf(
                            FdEvent(
                                id = "e1",
                                type = "health.down",
                                severity = "error",
                                source = "frontdesk-poller",
                                message = "hotel-prime is unreachable after 3 checks",
                                memberId = "m1",
                                createdAt = "2026-07-12T10:15:00Z",
                            ),
                        ),
                ),
        )
    }
}
