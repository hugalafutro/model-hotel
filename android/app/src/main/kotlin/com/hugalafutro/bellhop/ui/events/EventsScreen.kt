package com.hugalafutro.bellhop.ui.events

import android.widget.Toast
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.ui.common.CustomDateRange
import com.hugalafutro.bellhop.ui.common.EventRange
import com.hugalafutro.bellhop.ui.common.EventRangeRow
import com.hugalafutro.bellhop.ui.common.FilterPill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.severityColors
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import com.hugalafutro.bellhop.ui.theme.MonoFamily
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
    onCustomRange: (CustomDateRange?) -> Unit = {},
    onLoadMore: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Tapping an event's severity pill copies the whole event as text (handy for
    // pasting into a bug report), with a toast to confirm the otherwise-silent act.
    val clipboard = LocalClipboardManager.current
    val context = LocalContext.current
    val copiedMsg = stringResource(R.string.events_copied)
    val onCopy: (FdEvent) -> Unit = { event ->
        clipboard.setText(AnnotatedString(eventClipboardText(event, memberNames[event.memberId])))
        Toast.makeText(context, copiedMsg, Toast.LENGTH_SHORT).show()
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
            EventRangeRow(
                selected = ui.range,
                custom = ui.custom,
                onRange = onRange,
                onCustomRange = onCustomRange,
                tagPrefix = "events-range",
            )
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
                        contentPadding = PaddingValues(bottom = 24.dp),
                    ) {
                        // Unkeyed on purpose, like the dashboard list: ids are
                        // primary keys server-side, but a buggy duplicate must
                        // degrade to a double row, not a crash.
                        items(ui.events) { event ->
                            EventRow(
                                event = event,
                                memberName = memberNames[event.memberId],
                                onCopy = { onCopy(event) },
                            )
                            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                        }
                        if (ui.canLoadMore) {
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

// Both filter rows are equal-weight Rows, not wrapping FlowRows: a fixed small
// set of options reads as a segmented control that always fits one line on a
// phone (each pill takes 1/N of the width, label centered and ellipsized).
@Composable
private fun SeverityChips(
    selected: String,
    onSeverity: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        modifier = modifier.fillMaxWidth(),
    ) {
        SEVERITIES.forEach { sev ->
            FilterPill(
                text = severityLabel(sev),
                selected = selected == sev,
                onClick = { onSeverity(sev) },
                tag = "events-sev-${sev.ifEmpty { "all" }}",
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun EventRow(
    event: FdEvent,
    memberName: String?,
    onCopy: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // A log line, not a card: a colour-coded severity rail down the left edge and
    // a faint tint of the same colour, so severity reads at a glance without a
    // pill. The whole row taps to copy (the rail carries the severity test tag).
    val accent = severityColors(event.severity).first
    Row(
        modifier =
            modifier
                .fillMaxWidth()
                .clickable(onClick = onCopy)
                .background(accent.copy(alpha = 0.06f))
                .height(IntrinsicSize.Min)
                .testTag("event-card"),
    ) {
        Box(
            modifier =
                Modifier
                    .width(3.dp)
                    .fillMaxHeight()
                    .background(accent)
                    .testTag("event-sev-${event.severity}"),
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
                // Time in the brand mono so the column aligns and reads as a metric.
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
            )
            val who =
                listOfNotNull(
                    event.source.ifEmpty { null },
                    memberName ?: event.memberId.ifEmpty { null },
                ).joinToString(" · ")
            if (who.isNotEmpty()) {
                Text(
                    text = who,
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = MonoFamily,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
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

private val EVENT_TIME_FORMAT =
    DateTimeFormatter.ofPattern("MMM d, yyyy · HH:mm", Locale.getDefault())

// formatEventTime renders the stored RFC3339 timestamp in local time, falling
// back to the raw string on anything unparseable (garbage in, garbage shown —
// better than a crash or a blank cell).
internal fun formatEventTime(createdAt: String): String =
    try {
        Instant.parse(createdAt).atZone(ZoneId.systemDefault()).format(EVENT_TIME_FORMAT)
    } catch (e: Exception) {
        createdAt
    }

// eventClipboardText renders one event as a plain-text block for the clipboard:
// a header (time · severity · type), the message, then a source/member line —
// blank parts dropped so a memberless system event doesn't trail a dangling dot.
internal fun eventClipboardText(
    event: FdEvent,
    memberName: String?,
): String {
    val header = "${formatEventTime(event.createdAt)} · [${event.severity}] ${event.type}"
    val who =
        listOfNotNull(
            event.source.ifEmpty { null },
            memberName ?: event.memberId.ifEmpty { null },
        ).joinToString(" · ")
    return listOf(header, event.message, who).filter { it.isNotEmpty() }.joinToString("\n")
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
