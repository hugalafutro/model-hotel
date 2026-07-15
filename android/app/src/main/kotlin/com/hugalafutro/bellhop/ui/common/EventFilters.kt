package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DateRangePicker
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDateRangePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.ui.theme.MonoFamily
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Locale

// Shared event time filtering: the relative preset pills plus an absolute
// calendar range, used by both the Events screen and member detail's recent
// events. Presets and calendar are mutually exclusive; picking one clears the
// other.

/**
 * EventRange is the relative "since" presets offered as time filters,
 * mirroring the Front Desk web Events page (0 = no lower bound).
 */
enum class EventRange(val ms: Long) {
    ALL(0),
    H1(3_600_000),
    H24(86_400_000),
    D7(604_800_000),
    D30(2_592_000_000),
}

/**
 * CustomDateRange is an absolute calendar range from the date picker: UTC
 * midnights of the first and last selected day. [untilRfc3339] extends past the
 * end day's midnight so the whole final day is covered by the server's
 * inclusive (created_at <= ?) upper bound.
 */
data class CustomDateRange(
    val startMs: Long,
    val endMs: Long,
) {
    fun sinceRfc3339(): String = Instant.ofEpochMilli(startMs).toString()

    fun untilRfc3339(): String = Instant.ofEpochMilli(endMs + DAY_MS).toString()

    /** label renders "Jul 1 – Jul 14" for the active-filter line. */
    fun label(): String =
        dayFormat().let { fmt ->
            "${fmt.format(Instant.ofEpochMilli(startMs))} – ${fmt.format(Instant.ofEpochMilli(endMs))}"
        }

    private companion object {
        const val DAY_MS = 86_400_000L

        // dayFormat is built per call from the current default locale (kept in step
        // with the in-app language by AppLocale) so the month abbreviation follows a
        // language switch. The picker hands out UTC midnights, so format in UTC too:
        // a zoned format would shift the labelled day for anyone east of Greenwich.
        fun dayFormat(): DateTimeFormatter =
            DateTimeFormatter.ofPattern("MMM d", Locale.getDefault()).withZone(ZoneOffset.UTC)
    }
}

/** rangeLabel is the chip text for a preset (ALL reads "All"). */
@Composable
fun rangeLabel(range: EventRange): String =
    when (range) {
        EventRange.ALL -> stringResource(R.string.events_range_all)
        EventRange.H1 -> stringResource(R.string.events_range_1h)
        EventRange.H24 -> stringResource(R.string.events_range_24h)
        EventRange.D7 -> stringResource(R.string.events_range_7d)
        EventRange.D30 -> stringResource(R.string.events_range_30d)
    }

/**
 * EventRangeRow is the shared time filter: preset pills in an equal-weight row
 * (a segmented control that always fits one phone-width line) with a calendar
 * icon at the end opening a date-range picker. An active calendar range
 * deselects every preset, tints the icon brass, and shows a clearable
 * "Jul 1 – Jul 14" line under the row. Tags are "$tagPrefix-<preset>",
 * "$tagPrefix-calendar", "$tagPrefix-apply", "$tagPrefix-custom-label" and
 * "$tagPrefix-custom-clear", so the Events screen keeps its historical
 * "events-range-*" tags.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EventRangeRow(
    selected: EventRange,
    custom: CustomDateRange?,
    onRange: (EventRange) -> Unit,
    onCustomRange: (CustomDateRange?) -> Unit,
    tagPrefix: String,
    modifier: Modifier = Modifier,
) {
    var showPicker by remember { mutableStateOf(false) }
    Column(
        verticalArrangement = Arrangement.spacedBy(2.dp),
        modifier = modifier.fillMaxWidth(),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            EventRange.entries.forEach { range ->
                FilterPill(
                    text = rangeLabel(range),
                    selected = custom == null && selected == range,
                    onClick = { onRange(range) },
                    tag = "$tagPrefix-${range.name.lowercase()}",
                    modifier = Modifier.weight(1f),
                )
            }
            IconButton(
                onClick = { showPicker = true },
                modifier = Modifier.size(28.dp).testTag("$tagPrefix-calendar"),
            ) {
                Icon(
                    imageVector = Icons.Filled.DateRange,
                    contentDescription = stringResource(R.string.events_range_calendar),
                    tint =
                        if (custom != null) {
                            MaterialTheme.colorScheme.primary
                        } else {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        },
                    modifier = Modifier.size(18.dp),
                )
            }
        }
        if (custom != null) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(
                    text = custom.label(),
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = MonoFamily,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.testTag("$tagPrefix-custom-label"),
                )
                IconButton(
                    onClick = { onCustomRange(null) },
                    modifier = Modifier.size(20.dp).testTag("$tagPrefix-custom-clear"),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Close,
                        contentDescription = stringResource(R.string.events_range_clear),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.size(14.dp),
                    )
                }
            }
        }
    }
    if (showPicker) {
        val pickerState =
            rememberDateRangePickerState(
                initialSelectedStartDateMillis = custom?.startMs,
                initialSelectedEndDateMillis = custom?.endMs,
            )
        DatePickerDialog(
            onDismissRequest = { showPicker = false },
            confirmButton = {
                TextButton(
                    onClick = {
                        // A single tapped day is a valid one-day range.
                        pickerState.selectedStartDateMillis?.let { start ->
                            onCustomRange(
                                CustomDateRange(
                                    startMs = start,
                                    endMs = pickerState.selectedEndDateMillis ?: start,
                                ),
                            )
                        }
                        showPicker = false
                    },
                    modifier = Modifier.testTag("$tagPrefix-apply"),
                ) {
                    Text(stringResource(R.string.events_range_apply))
                }
            },
            dismissButton = {
                TextButton(onClick = { showPicker = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        ) {
            // weight keeps the picker from pushing the dialog's buttons
            // off-screen on small phones.
            DateRangePicker(
                state = pickerState,
                showModeToggle = false,
                modifier = Modifier.weight(1f),
            )
        }
    }
}
