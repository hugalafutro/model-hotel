package com.hugalafutro.bellhop.ui.member

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.TrafficPoint
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.healthColor
import com.hugalafutro.bellhop.ui.common.healthLabel
import com.hugalafutro.bellhop.ui.theme.BellhopTheme

/**
 * MemberDetailScreen is one member up close: the same identity and health the
 * dashboard card shows (the [member] arrives live from the dashboard's poll,
 * so badges keep moving here too) plus the last hour of request/error traffic
 * from [MemberDetailViewModel]. Read-only; operator actions are a later phase.
 */
@Composable
fun MemberDetailScreen(
    member: FleetMember,
    isPrimary: Boolean,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    ui: MemberDetailUiState = MemberDetailUiState(),
) {
    val health = member.status.health
    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 16.dp)
                    .verticalScroll(rememberScrollState()),
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
                if (member.drained) {
                    Pill(
                        text = stringResource(R.string.member_state_drained),
                        container = MaterialTheme.colorScheme.errorContainer,
                        content = MaterialTheme.colorScheme.onErrorContainer,
                        tag = "member-detail-drained",
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

            Card(modifier = Modifier.fillMaxWidth().testTag("member-detail-meta")) {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Text(
                        text = member.url,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text(
                            text = healthLabel(health),
                            style = MaterialTheme.typography.bodySmall,
                            color = healthColor(health),
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
                    // The card truncates this; up close the operator wants the
                    // whole probe error.
                    if (health.known && !health.healthy && health.error.isNotBlank()) {
                        Text(
                            text = health.error,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                        )
                    }
                }
            }

            Spacer(modifier = Modifier.height(12.dp))
            TrafficCard(ui = ui)
            Spacer(modifier = Modifier.height(16.dp))
        }
    }
}

/**
 * TrafficCard renders the last-hour series. Unreachable is a normal, explained
 * state, not an error: Front Desk may hold no admin token for this member, or
 * the member didn't answer its stats API.
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
                    TrafficChart(points = traffic.points)
                }
            }
        }
    }
}

/**
 * TrafficChart is a plain Canvas bar chart, one bar per 5-minute bucket oldest
 * to newest: bar height is that bucket's requests against the window maximum,
 * with the error subset overlaid from the baseline in the error color. No
 * chart library by design (plan section 5.4).
 */
@Composable
private fun TrafficChart(
    points: List<TrafficPoint>,
    modifier: Modifier = Modifier,
) {
    val barColor = MaterialTheme.colorScheme.primary
    val errorColor = MaterialTheme.colorScheme.error
    val idleColor = MaterialTheme.colorScheme.outlineVariant
    Canvas(
        modifier =
            modifier
                .fillMaxWidth()
                .height(96.dp)
                .testTag("member-traffic-chart"),
    ) {
        val max = points.maxOf { it.requests }.coerceAtLeast(1)
        val gap = 3.dp.toPx()
        val barWidth = (size.width - gap * (points.size - 1)) / points.size
        val stub = 2.dp.toPx()
        points.forEachIndexed { i, p ->
            val x = i * (barWidth + gap)
            if (p.requests <= 0) {
                // Zero bucket: a faint stub so the timeline stays readable
                // instead of leaving holes.
                drawRect(idleColor, Offset(x, size.height - stub), Size(barWidth, stub))
                return@forEachIndexed
            }
            val h = (size.height * p.requests / max).coerceAtLeast(stub)
            drawRect(barColor, Offset(x, size.height - h), Size(barWidth, h))
            if (p.errors > 0) {
                // Errors are a subset of requests, so the overlay shares the
                // scale and can never outgrow its bar.
                val eh = (size.height * p.errors / max).coerceAtLeast(stub)
                drawRect(errorColor, Offset(x, size.height - eh), Size(barWidth, eh))
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
                    status =
                        MemberStatus(
                            health = HealthStatus(known = true, healthy = true, latencyMs = 12),
                            traefikStatus = "UP",
                            version = "0.33.0",
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
                ),
        )
    }
}
