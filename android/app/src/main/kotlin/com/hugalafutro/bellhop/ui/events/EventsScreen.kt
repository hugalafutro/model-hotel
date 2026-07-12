package com.hugalafutro.bellhop.ui.events

import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * EventsScreen renders the Front Desk control-plane event log: severity and
 * time-range filter chips, newest-first event cards, and a load-more tail
 * while the server holds more matching rows. Read-only; state comes from
 * [EventsViewModel].
 */
@Composable
fun EventsScreen(
    onBack: () -> Unit,
    ui: EventsUiState = EventsUiState(),
    memberNames: Map<String, String> = emptyMap(),
    onSeverity: (String) -> Unit = {},
    onRange: (EventRange) -> Unit = {},
    onLoadMore: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 16.dp),
        ) {
            Spacer(modifier = Modifier.height(8.dp))
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            ) {
                IconButton(onClick = onBack, modifier = Modifier.testTag("events-back")) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.events_back),
                    )
                }
                Text(
                    text = stringResource(R.string.events_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.weight(1f).testTag("events-title"),
                )
                if (!ui.loading) {
                    Text(
                        text = pluralStringResource(R.plurals.events_total, ui.total, ui.total),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("events-total"),
                    )
                }
            }

            SeverityChips(selected = ui.severity, onSeverity = onSeverity)
            RangeChips(selected = ui.range, onRange = onRange)
            Spacer(modifier = Modifier.height(8.dp))

            if (ui.revoked) {
                StatusBanner(text = stringResource(R.string.dashboard_revoked), tag = "events-revoked")
            } else if (ui.error != null) {
                StatusBanner(
                    text = stringResource(R.string.dashboard_refresh_failed, ui.error),
                    tag = "events-error",
                )
            }

            when {
                ui.loading ->
                    Box(
                        modifier = Modifier.fillMaxWidth().weight(1f),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(modifier = Modifier.testTag("events-loading"))
                    }
                ui.events.isEmpty() ->
                    Box(
                        modifier = Modifier.fillMaxWidth().weight(1f),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            text = stringResource(R.string.events_empty),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.testTag("events-empty"),
                        )
                    }
                else ->
                    LazyColumn(
                        modifier = Modifier.weight(1f).testTag("events-list"),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                        contentPadding = PaddingValues(bottom = 24.dp),
                    ) {
                        // Unkeyed on purpose, like the dashboard list: ids are
                        // primary keys server-side, but a buggy duplicate must
                        // degrade to a double row, not a crash.
                        items(ui.events) { event ->
                            EventCard(event = event, memberName = memberNames[event.memberId])
                        }
                        if (ui.events.size < ui.total) {
                            item {
                                Box(
                                    modifier = Modifier.fillMaxWidth(),
                                    contentAlignment = Alignment.Center,
                                ) {
                                    if (ui.loadingMore) {
                                        CircularProgressIndicator(
                                            modifier = Modifier.padding(8.dp).testTag("events-loading-more"),
                                        )
                                    } else {
                                        TextButton(
                                            onClick = onLoadMore,
                                            modifier = Modifier.testTag("events-load-more"),
                                        ) {
                                            Text(stringResource(R.string.events_load_more))
                                        }
                                    }
                                }
                            }
                        }
                    }
            }
        }
    }
}

// Severity filter values, "" meaning all. Matches the Front Desk web page's
// SEVERITIES list (frontdesk/web/src/pages/EventsPage.tsx).
private val SEVERITIES = listOf("", "info", "success", "warning", "error")

@Composable
private fun SeverityChips(
    selected: String,
    onSeverity: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
    ) {
        SEVERITIES.forEach { sev ->
            FilterChip(
                selected = selected == sev,
                onClick = { onSeverity(sev) },
                label = { Text(severityLabel(sev)) },
                modifier = Modifier.testTag("events-sev-${sev.ifEmpty { "all" }}"),
            )
        }
    }
}

@Composable
private fun RangeChips(
    selected: EventRange,
    onRange: (EventRange) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
    ) {
        EventRange.entries.forEach { range ->
            FilterChip(
                selected = selected == range,
                onClick = { onRange(range) },
                label = { Text(rangeLabel(range)) },
                modifier = Modifier.testTag("events-range-${range.name.lowercase()}"),
            )
        }
    }
}

@Composable
private fun EventCard(
    event: FdEvent,
    memberName: String?,
    modifier: Modifier = Modifier,
) {
    val (container, content) = severityColors(event.severity)
    Card(modifier = modifier.fillMaxWidth().testTag("event-card")) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Pill(
                    text = severityLabel(event.severity),
                    container = container,
                    content = content,
                    tag = "event-sev-${event.severity}",
                )
                Spacer(modifier = Modifier.weight(1f))
                Text(
                    text = formatEventTime(event.createdAt),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = event.message,
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                text =
                    listOfNotNull(
                        event.source.ifEmpty { null },
                        memberName ?: event.memberId.ifEmpty { null },
                    ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** severityLabel is the chip/badge text for a severity ("" = the All chip). */
@Composable
private fun severityLabel(severity: String): String =
    when (severity) {
        "" -> stringResource(R.string.events_sev_all)
        "info" -> stringResource(R.string.events_sev_info)
        "success" -> stringResource(R.string.events_sev_success)
        "warning" -> stringResource(R.string.events_sev_warning)
        "error" -> stringResource(R.string.events_sev_error)
        else -> severity
    }

@Composable
private fun rangeLabel(range: EventRange): String =
    when (range) {
        EventRange.ALL -> stringResource(R.string.events_range_all)
        EventRange.H1 -> stringResource(R.string.events_range_1h)
        EventRange.H24 -> stringResource(R.string.events_range_24h)
        EventRange.D7 -> stringResource(R.string.events_range_7d)
        EventRange.D30 -> stringResource(R.string.events_range_30d)
    }

/** severityColors maps a severity onto the badge palette FD web uses (ok/warn/danger/info). */
@Composable
private fun severityColors(severity: String): Pair<Color, Color> =
    when (severity) {
        "success" ->
            MaterialTheme.colorScheme.tertiaryContainer to
                MaterialTheme.colorScheme.onTertiaryContainer
        "warning" ->
            MaterialTheme.colorScheme.secondaryContainer to
                MaterialTheme.colorScheme.onSecondaryContainer
        "error" ->
            MaterialTheme.colorScheme.errorContainer to
                MaterialTheme.colorScheme.onErrorContainer
        else ->
            MaterialTheme.colorScheme.surfaceVariant to
                MaterialTheme.colorScheme.onSurfaceVariant
    }

private val EVENT_TIME_FORMAT =
    DateTimeFormatter.ofPattern("MMM d · HH:mm", Locale.getDefault())

// formatEventTime renders the stored RFC3339 timestamp in local time, falling
// back to the raw string on anything unparseable (garbage in, garbage shown —
// better than a crash or a blank cell).
internal fun formatEventTime(createdAt: String): String =
    try {
        Instant.parse(createdAt).atZone(ZoneId.systemDefault()).format(EVENT_TIME_FORMAT)
    } catch (e: Exception) {
        createdAt
    }

@Preview(showBackground = true)
@Composable
private fun EventsScreenPreview() {
    BellhopTheme {
        EventsScreen(
            onBack = {},
            ui =
                EventsUiState(
                    loading = false,
                    total = 40,
                    events =
                        listOf(
                            FdEvent(
                                id = "e1",
                                type = "health.down",
                                severity = "error",
                                source = "poller",
                                message = "hotel-2 is unreachable",
                                memberId = "m2",
                                createdAt = "2026-07-12T10:15:00Z",
                            ),
                            FdEvent(
                                id = "e2",
                                type = "config.synced",
                                severity = "success",
                                source = "autosync",
                                message = "Config synced to 3 members",
                                createdAt = "2026-07-12T10:00:00Z",
                            ),
                        ),
                ),
            memberNames = mapOf("m2" to "hotel-2"),
        )
    }
}
