package com.hugalafutro.bellhop.ui.member

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
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
import com.hugalafutro.bellhop.ui.common.ConfirmOpenUrlDialog
import com.hugalafutro.bellhop.ui.common.CustomDateRange
import com.hugalafutro.bellhop.ui.common.EventRange
import com.hugalafutro.bellhop.ui.common.EventRangeRow
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
 * The graph is the only card on the page; everything under it is flat ledger
 * sections so the screen doesn't read as a stack of pills. [member] arrives
 * live from the dashboard's poll; [ui] carries the graph + events from
 * [MemberDetailViewModel].
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
    onRange: (EventRange) -> Unit = {},
    onCustomRange: (CustomDateRange?) -> Unit = {},
    onLoadMoreEvents: () -> Unit = {},
) {
    val health = member.status.health
    // Clear the optimistic pending state once the dashboard's live state (member
    // arrives live from its poll/SSE) has caught up to the accepted target.
    LaunchedEffect(member.state) { onReconcile(member.state) }
    var showUrlDialog by remember { mutableStateOf(false) }
    if (showUrlDialog) {
        ConfirmOpenUrlDialog(url = member.url, onDismiss = { showUrlDialog = false })
    }
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
                contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            ) {
                item { TrafficCard(ui = ui, modifier = Modifier.padding(bottom = 20.dp)) }
                item {
                    MetaLedger(
                        member = member,
                        onOpenUrl = { showUrlDialog = true },
                        modifier = Modifier.padding(bottom = 20.dp),
                    )
                }
                if (canOperate) {
                    item {
                        OperatorControls(
                            member = member,
                            isPrimary = isPrimary,
                            action = ui.action,
                            lastFleetSyncAt = ui.lastFleetSyncAt,
                            onSetState = onSetState,
                            onSyncFleet = onSyncFleet,
                            onDismissActionError = onDismissActionError,
                            modifier = Modifier.padding(bottom = 20.dp),
                        )
                    }
                }
                item {
                    SectionHeader(
                        text = stringResource(R.string.member_detail_events_title),
                        tag = "member-detail-events-title",
                        modifier = Modifier.padding(bottom = 6.dp),
                    )
                }
                item {
                    EventRangeRow(
                        selected = ui.range,
                        custom = ui.custom,
                        onRange = onRange,
                        onCustomRange = onCustomRange,
                        tagPrefix = "member-events-range",
                        modifier = Modifier.padding(bottom = 6.dp),
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
                    else ->
                        itemsIndexed(ui.events) { index, event ->
                            MemberEventRow(event = event)
                            if (index < ui.events.lastIndex) {
                                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                            }
                        }
                }
                if (ui.loadingMore) {
                    item {
                        Box(
                            modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            CircularProgressIndicator(
                                strokeWidth = 2.dp,
                                modifier = Modifier.size(20.dp).testTag("member-events-loading-more"),
                            )
                        }
                    }
                } else if (ui.canLoadMore) {
                    // Infinite scroll: lazy items only compose near the
                    // viewport, so this sentinel composing at all means the
                    // user bottomed out — ask for the next page. The key
                    // changes with the window size, so a fresh sentinel arms
                    // after each page (and a short first page auto-fills the
                    // screen). The VM still no-ops when nothing more exists.
                    // The 1dp spacer gives it a semantics node (tests scroll
                    // to it); visually it's nothing.
                    item(key = "member-events-load-more-${ui.events.size}") {
                        LaunchedEffect(Unit) { onLoadMoreEvents() }
                        Spacer(
                            modifier =
                                Modifier
                                    .height(1.dp)
                                    .testTag("member-events-load-more-sentinel"),
                        )
                    }
                }
            }
        }
    }
}

/**
 * SectionHeader is the flat replacement for a card title: a small brass
 * overline with a hairline rule running out to the edge, so sections read as
 * ledger dividers instead of yet more pills.
 */
@Composable
private fun SectionHeader(
    text: String,
    modifier: Modifier = Modifier,
    tag: String? = null,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = modifier.fillMaxWidth().then(if (tag != null) Modifier.testTag(tag) else Modifier),
    ) {
        Text(
            text = text.uppercase(),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.primary,
        )
        HorizontalDivider(modifier = Modifier.weight(1f), color = MaterialTheme.colorScheme.outlineVariant)
    }
}

/**
 * MetaLedger is the info the dashboard card has no room for: the full probe
 * error when down, config-sync provenance, the in-sync heartbeat, and when the
 * member was added. Flat label/value register lines under the graph — not a
 * card — with the values in mono so timestamps and the address align. Each
 * line only shows when it has a value, so a healthy never-synced member stays
 * terse.
 */
@Composable
private fun MetaLedger(
    member: FleetMember,
    onOpenUrl: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val health = member.status.health
    Column(
        modifier = modifier.fillMaxWidth().testTag("member-detail-meta"),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        // Brass + tappable, same affordance as the dashboard card's address:
        // the tap only raises the confirm dialog, never fires an intent itself.
        LedgerRow(
            label = stringResource(R.string.member_detail_label_address),
            value = member.url,
            valueColor = MaterialTheme.colorScheme.primary,
            onClick = onOpenUrl,
            tag = "member-detail-url",
        )
        if (health.known && !health.healthy && health.error.isNotBlank()) {
            LedgerRow(
                label = stringResource(R.string.member_health_down),
                value = health.error,
                valueColor = MaterialTheme.colorScheme.error,
                tag = "member-detail-down-reason",
            )
        }
        if (member.lastConfigSyncAt.isNotBlank()) {
            LedgerRow(
                label = stringResource(R.string.member_detail_label_synced),
                value = formatEventTime(member.lastConfigSyncAt),
                tag = "member-detail-synced",
            )
            if (member.lastConfigSyncReason.isNotBlank()) {
                // Why the last sync ran, indented to the value column (label
                // width + gap) so it hangs off the SYNCED entry.
                Text(
                    text = member.lastConfigSyncReason,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 88.dp),
                )
            }
        }
        if (member.status.autoSyncVerifiedAt.isNotBlank()) {
            LedgerRow(
                label = stringResource(R.string.member_detail_label_verified),
                value = formatEventTime(member.status.autoSyncVerifiedAt),
                tag = "member-detail-verified",
            )
        }
        if (member.createdAt.isNotBlank()) {
            LedgerRow(
                label = stringResource(R.string.member_detail_label_added),
                value = formatEventTime(member.createdAt),
                tag = "member-detail-created",
            )
        }
    }
}

/**
 * LedgerRow is one register line of [MetaLedger]: an uppercase muted label in
 * a fixed column and a mono value, so the block reads as one aligned ledger
 * instead of a pile of sentences.
 */
@Composable
private fun LedgerRow(
    label: String,
    value: String,
    modifier: Modifier = Modifier,
    valueColor: Color = MaterialTheme.colorScheme.onSurface,
    tag: String? = null,
    onClick: (() -> Unit)? = null,
) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        modifier =
            modifier
                .fillMaxWidth()
                .then(if (tag != null) Modifier.testTag(tag) else Modifier)
                .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier),
    ) {
        Text(
            text = label.uppercase(),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.width(76.dp),
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = MonoFamily,
            color = valueColor,
            modifier = Modifier.weight(1f),
        )
    }
}

/**
 * OperatorControls is the drain/activate (and, on the primary, fleet-sync)
 * surface, shown only to an operator device: a section rule and a compact
 * button row rather than a card with a full-width button. The button reflects
 * the effective state: Front Desk's live state, or the optimistic pending
 * target once an action was accepted and until the dashboard reconciles it.
 * A 403 from Front Desk collapses the section to the guard note — the
 * authoritative role check overriding the role-hint UI. Actions are set-state,
 * so a double-tap is a safe no-op; the biometric prompt gating them lives in
 * the host (MainActivity).
 */
@Composable
private fun OperatorControls(
    member: FleetMember,
    isPrimary: Boolean,
    action: ActionUiState,
    lastFleetSyncAt: String,
    onSetState: (String) -> Unit,
    onSyncFleet: () -> Unit,
    onDismissActionError: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxWidth().testTag("member-operator-card"),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        SectionHeader(text = stringResource(R.string.member_op_title))
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
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Button(
                onClick = { onSetState(if (drained) MemberState.ACTIVE else MemberState.DRAINED) },
                enabled = !action.inProgress,
                modifier = Modifier.testTag("member-op-state"),
            ) {
                if (action.inProgress) {
                    // Spinner joins the label instead of replacing it so the
                    // button keeps its width and stays legible in flight.
                    CircularProgressIndicator(
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(16.dp),
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                }
                Text(
                    text =
                        stringResource(
                            if (drained) R.string.member_op_activate else R.string.member_op_drain,
                        ),
                )
            }
            if (isPrimary) {
                OutlinedButton(
                    onClick = onSyncFleet,
                    enabled = !action.inProgress,
                    modifier = Modifier.testTag("member-op-sync"),
                ) {
                    Text(text = stringResource(R.string.member_op_sync))
                }
            }
        }
        if (isPrimary && lastFleetSyncAt.isNotBlank()) {
            // When a sync last actually wrote config somewhere, not when the
            // button was last pressed; a run that changed nothing doesn't move it.
            Text(
                text = stringResource(R.string.member_op_last_sync, formatEventTime(lastFleetSyncAt)),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.testTag("member-op-last-sync"),
            )
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
        if (action.busy) {
            // A tap arrived mid-flight and was dropped: say so rather than
            // leaving the operator wondering why nothing happened.
            Text(
                text = stringResource(R.string.member_op_busy),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.testTag("member-op-busy"),
            )
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
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = stringResource(R.string.member_op_error, message),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.weight(1f).testTag("member-op-error"),
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
                        fontFamily = MonoFamily,
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
 * Events screen: a flat line — no card — with a severity-coloured rail down the
 * left edge and a faint tint of the same colour, the type as the heading with a
 * mono timestamp, then the message.
 */
@Composable
private fun MemberEventRow(
    event: FdEvent,
    modifier: Modifier = Modifier,
) {
    val accent = severityColors(event.severity).first
    Row(
        modifier =
            modifier
                .fillMaxWidth()
                .background(accent.copy(alpha = 0.06f))
                .height(IntrinsicSize.Min)
                .testTag("member-event-row"),
    ) {
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
