package com.hugalafutro.bellhop.ui.settings

import android.app.Activity
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.AppLocale
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LockConfig
import com.hugalafutro.bellhop.data.LockTimeout
import com.hugalafutro.bellhop.data.PrefsStore
import com.hugalafutro.bellhop.data.QuotaBadgeAlign
import com.hugalafutro.bellhop.data.TimeFormat
import com.hugalafutro.bellhop.ui.alerts.ALERT_SEVERITIES
import com.hugalafutro.bellhop.ui.common.BellhopSwitch
import com.hugalafutro.bellhop.ui.common.FilterPill
import com.hugalafutro.bellhop.ui.common.NavChevron
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.rememberCopyText
import com.hugalafutro.bellhop.ui.common.severityColors
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * SettingsScreen is where the link's status and the two things the dashboard
 * shouldn't clutter itself with live: the app lock and Unlink. It renders the
 * linked Front Desk (name, address, this device's label and role), the app-lock
 * policy (toggle + idle window), a shortcut into Alerts, and the Unlink flow —
 * confirm, plus the failure/force-unlink escape moved here from the dashboard.
 * It holds only the confirm-dialog visibility locally; the unlink work and its
 * failure state are the host's, so the same revoke-first guarantees apply.
 */
@Composable
fun SettingsScreen(
    link: LinkState.Linked,
    lockConfig: LockConfig,
    lockAvailable: Boolean,
    monitorEnabled: Boolean,
    onBack: () -> Unit,
    onToggleLock: (Boolean) -> Unit,
    onSelectTimeout: (LockTimeout) -> Unit,
    onToggleMonitor: (Boolean) -> Unit,
    onTogglePush: (Boolean) -> Unit,
    onAlertsClick: () -> Unit,
    onQuotaBadgesClick: () -> Unit,
    onUnlink: () -> Unit,
    modifier: Modifier = Modifier,
    notificationsBlocked: Boolean = false,
    pushEnabled: Boolean = false,
    pushEndpoint: String? = null,
    pushDistributorAvailable: Boolean = false,
    pushNotificationsBlocked: Boolean = false,
    batteryUnrestricted: Boolean = true,
    onRequestBatteryExemption: () -> Unit = {},
    unlinking: Boolean = false,
    unlinkFailed: Boolean = false,
    onDismissUnlinkError: () -> Unit = {},
    onForceUnlink: () -> Unit = {},
    holdToCopy: Boolean = false,
    onToggleHoldToCopy: (Boolean) -> Unit = {},
    graphRangeMinutes: Int = PrefsStore.DEFAULT_GRAPH_RANGE_MINUTES,
    onSetGraphRange: (Int) -> Unit = {},
    widgetGraphs: Boolean = false,
    onToggleWidgetGraphs: (Boolean) -> Unit = {},
    // Defaults on, like the preference itself: the widget carried the badge
    // strip before there was a switch for it.
    widgetQuota: Boolean = true,
    onToggleWidgetQuota: (Boolean) -> Unit = {},
    // Defaults to LEFT like the preference itself: that is what the widget did
    // before the picker existed.
    widgetQuotaAlign: QuotaBadgeAlign = QuotaBadgeAlign.LEFT,
    onSetWidgetQuotaAlign: (QuotaBadgeAlign) -> Unit = {},
    timeFormat: TimeFormat = TimeFormat.SYSTEM,
    onSetTimeFormat: (TimeFormat) -> Unit = {},
    // Enabled-alert counts per severity (error/warning/info/success), sourced from
    // Front Desk's live selection. Always rendered as badges on the Alerts pill,
    // even at 0, so the pill reads as a live, tappable destination.
    alertCounts: Map<String, Int> = emptyMap(),
) {
    var confirmUnlink by remember { mutableStateOf(false) }
    var confirmCopyAddress by remember { mutableStateOf(false) }
    var showLanguagePicker by remember { mutableStateOf(false) }
    val copyText = rememberCopyText()
    val context = LocalContext.current
    // Read once: choosing a different language recreates the activity, so this
    // never needs to react to a live change.
    val currentLanguage = remember { AppLocale.stored(context) }
    val pushCopied = stringResource(R.string.settings_push_copied)
    val addressCopied = stringResource(R.string.settings_fd_copied)

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
                    modifier = Modifier.testTag("settings-unlink-retry"),
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
                    modifier = Modifier.testTag("settings-unlink-force"),
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
                    modifier = Modifier.testTag("settings-unlink-confirm"),
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

    if (confirmCopyAddress) {
        val address = link.fdUrl
        AlertDialog(
            onDismissRequest = { confirmCopyAddress = false },
            title = { Text(stringResource(R.string.settings_fd_copy_title)) },
            text = { Text(stringResource(R.string.settings_fd_copy_body, address)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmCopyAddress = false
                        copyText(address, addressCopied)
                    },
                    modifier = Modifier.testTag("settings-fd-copy-confirm"),
                ) {
                    Text(stringResource(R.string.common_copy))
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmCopyAddress = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    if (showLanguagePicker) {
        LanguagePickerDialog(
            current = currentLanguage,
            onSelect = { tag ->
                showLanguagePicker = false
                // A no-op pick avoids a pointless recreate; a real change persists
                // then recreates the activity, which re-reads the tag in the new locale.
                if (tag != currentLanguage) {
                    AppLocale.store(context, tag)
                    (context as? Activity)?.recreate()
                }
            },
            onDismiss = { showLanguagePicker = false },
        )
    }

    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 16.dp)
                    .verticalScroll(rememberScrollState()),
        ) {
            Spacer(modifier = Modifier.height(8.dp))
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            ) {
                IconButton(onClick = onBack, modifier = Modifier.testTag("settings-back")) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.settings_back),
                    )
                }
                Text(
                    text = stringResource(R.string.settings_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.weight(1f).testTag("settings-title"),
                )
                // Language lives up here, not on the dashboard: the settings cog is a
                // universal-enough entry point that the picker needn't sit on the main
                // screen. The globe/translate glyph reads across languages.
                IconButton(
                    onClick = { showLanguagePicker = true },
                    modifier = Modifier.testTag("settings-language"),
                ) {
                    Icon(
                        painter = painterResource(R.drawable.ic_translate),
                        contentDescription = stringResource(R.string.settings_language),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
            }

            // Front Desk link status (moved off the dashboard toolbar).
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(
                    modifier = Modifier.padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Text(
                        text = stringResource(R.string.settings_fd_title),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    // A Front Desk has no separate name — the pairing string only
                    // carries its address — so the bold line is its URL, and there
                    // is no redundant subtext beneath it. Tapping it offers to copy
                    // the address (confirmed, since a stray tap here is easy).
                    Text(
                        text = link.fdName.ifBlank { link.fdUrl },
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier =
                            Modifier
                                .clickable { confirmCopyAddress = true }
                                .testTag("settings-fd-name"),
                    )
                    linkedOnLabel(link.linkedAt)?.let { date ->
                        Text(
                            text = stringResource(R.string.settings_fd_linked_on, date),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.testTag("settings-fd-linked-on"),
                        )
                    }
                    Text(
                        text = stringResource(R.string.dashboard_linked_as, link.label, link.role),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("settings-linked"),
                    )
                }
            }

            Spacer(modifier = Modifier.height(16.dp))

            // Copy gesture: whether a tap or a long-press copies a log/member cell.
            // Hold (the default) guards against stray copies while scrolling a list.
            //
            // The 12dp arrangement on this row and the switch rows below it is
            // load-bearing: the label Column takes every pixel the switch leaves,
            // so without a gap a subtitle that fills its line ends up touching
            // the toggle. Same 12dp the nav cards further down put between their
            // text and chevron.
            SettingsSection {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.settings_hold_copy_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(R.string.settings_hold_copy_subtitle),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    BellhopSwitch(
                        checked = holdToCopy,
                        onCheckedChange = onToggleHoldToCopy,
                        modifier = Modifier.testTag("settings-hold-copy-toggle"),
                    )
                }
            }

            // Home-screen widget: what the widget carries, one switch per block.
            // The section title used to sit on the traffic-graph switch itself,
            // which read as if the switch turned the whole widget off; it is a
            // heading now, and each row names the block it governs. Both are
            // opt-in-shaped for the same reason: each block the widget shows
            // costs the background check its own extra read.
            SettingsSection {
                Text(
                    text = stringResource(R.string.settings_widget_title),
                    style = MaterialTheme.typography.titleMedium,
                )
                WidgetToggleRow(
                    title = stringResource(R.string.settings_widget_graphs_title),
                    subtitle = stringResource(R.string.settings_widget_graphs_subtitle),
                    checked = widgetGraphs,
                    onCheckedChange = onToggleWidgetGraphs,
                    tag = "settings-widget-graphs-toggle",
                )
                WidgetToggleRow(
                    title = stringResource(R.string.settings_widget_quota_title),
                    subtitle = stringResource(R.string.settings_widget_quota_subtitle),
                    checked = widgetQuota,
                    onCheckedChange = onToggleWidgetQuota,
                    tag = "settings-widget-quota-toggle",
                )
                // Not a WidgetToggleRow: alignment is a three-way choice, and
                // unlike the two switches above it changes nothing about what the
                // background poll reads -- only where the same badges sit.
                Text(
                    text = stringResource(R.string.settings_widget_align_title),
                    style = MaterialTheme.typography.bodyMedium,
                )
                Text(
                    text = stringResource(R.string.settings_widget_align_subtitle),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    QuotaBadgeAlign.entries.forEach { option ->
                        FilterPill(
                            text = stringResource(alignLabel(option)),
                            selected = option == widgetQuotaAlign,
                            onClick = { onSetWidgetQuotaAlign(option) },
                            tag = "settings-widget-align-${option.name}",
                            modifier = Modifier.weight(1f),
                            borderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                            enabled = widgetQuota,
                        )
                    }
                }
            }

            // Clock: which face every wall-clock time in the app is drawn on.
            // System follows the device, the other two override it -- the app is
            // the only clock some people read on this screen.
            SettingsSection {
                Text(
                    text = stringResource(R.string.settings_time_format_title),
                    style = MaterialTheme.typography.titleMedium,
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    TimeFormat.entries.forEach { option ->
                        FilterPill(
                            text = stringResource(timeFormatLabel(option)),
                            selected = option == timeFormat,
                            onClick = { onSetTimeFormat(option) },
                            tag = "settings-time-format-${option.name}",
                            modifier = Modifier.weight(1f),
                            borderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }

            // Traffic graph range: how far back the request charts (the dashboard
            // sparklines and the member-detail graph) reach. Coarse presets only.
            SettingsSection {
                Text(
                    text = stringResource(R.string.settings_graph_range_title),
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    text = stringResource(R.string.settings_graph_range_subtitle),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                // The same FilterPill row as the app-lock window pills, so the two
                // pickers read identically. weight(1f) shares the width evenly, so
                // the five ranges fit without overflowing the card.
                Row(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    PrefsStore.GRAPH_RANGE_OPTIONS.forEach { minutes ->
                        FilterPill(
                            text = stringResource(R.string.settings_graph_range_hours, minutes / 60),
                            selected = graphRangeMinutes == minutes,
                            onClick = { onSetGraphRange(minutes) },
                            tag = "settings-graph-range-$minutes",
                            modifier = Modifier.weight(1f),
                            // In a Card the default outline nearly vanishes; match the
                            // lock pills and use the higher-contrast onSurfaceVariant.
                            borderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }

            // App lock: on/off plus the idle window it measures from foreground exit.
            SettingsSection {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.settings_lock_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(R.string.settings_lock_subtitle),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    BellhopSwitch(
                        checked = lockConfig.enabled,
                        onCheckedChange = onToggleLock,
                        enabled = lockAvailable,
                        modifier = Modifier.testTag("settings-lock-toggle"),
                    )
                }
                if (!lockAvailable) {
                    // No biometric or device credential enrolled: the gate can't
                    // engage, so say why the toggle is inert rather than failing
                    // silently when it's flipped on. Muted, not red: this is
                    // guidance about a precondition, unlike the monitor/push
                    // "delivery is blocked" notes where something is broken.
                    Text(
                        text = stringResource(R.string.settings_lock_unavailable),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("settings-lock-unavailable"),
                    )
                }
                if (lockConfig.enabled && lockAvailable) {
                    Text(
                        text = stringResource(R.string.settings_lock_window),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    val selected = LockTimeout.fromMillis(lockConfig.timeoutMs)
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.fillMaxWidth()) {
                        LockTimeout.entries.forEach { option ->
                            FilterPill(
                                text = stringResource(timeoutLabel(option)),
                                selected = option == selected,
                                onClick = { onSelectTimeout(option) },
                                tag = "settings-lock-timeout-${option.name}",
                                modifier = Modifier.weight(1f),
                                // These pills sit inside a Card, where the default
                                // `outline` border nearly vanishes; use the higher-
                                // contrast onSurfaceVariant so the unselected pills read.
                                borderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }

            // Background monitoring: the Layer-2 backstop (plan section 5.2). Off
            // by default; turning it on schedules the periodic poll and prompts
            // for the notification permission.
            SettingsSection {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.settings_monitor_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(R.string.settings_monitor_subtitle),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    BellhopSwitch(
                        checked = monitorEnabled,
                        onCheckedChange = onToggleMonitor,
                        modifier = Modifier.testTag("settings-monitor-toggle"),
                    )
                }
                if (monitorEnabled) {
                    if (notificationsBlocked) {
                        // Monitoring polls regardless, but with
                        // POST_NOTIFICATIONS denied nothing reaches the
                        // operator, so say so rather than let the switch
                        // imply working alerts.
                        Text(
                            text = stringResource(R.string.settings_monitor_blocked),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                            modifier = Modifier.testTag("settings-monitor-blocked"),
                        )
                    }
                    Text(
                        text = stringResource(R.string.settings_monitor_note),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.testTag("settings-monitor-note"),
                    )
                }
            }

            // Real-time push: Layer-3 opt-in (plan section 5.2). Off by default like
            // monitoring; turning it on registers with a UnifiedPush distributor and
            // (on API 33+) prompts for the notification permission.
            SettingsSection {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.settings_push_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(R.string.settings_push_subtitle),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    BellhopSwitch(
                        checked = pushEnabled,
                        onCheckedChange = onTogglePush,
                        modifier = Modifier.testTag("settings-push-toggle"),
                    )
                }
                if (!pushDistributorAvailable) {
                    // No distributor app installed: registration can't complete,
                    // so nothing will ever wake Bellhop. Say so rather than let
                    // an enabled switch imply working push.
                    Text(
                        text = stringResource(R.string.settings_push_no_distributor),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.testTag("settings-push-no-distributor"),
                    )
                }
                if (pushEnabled) {
                    if (pushNotificationsBlocked) {
                        Text(
                            text = stringResource(R.string.settings_push_blocked),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                            modifier = Modifier.testTag("settings-push-blocked"),
                        )
                    }
                    if (pushDistributorAvailable) {
                        Text(
                            text = stringResource(R.string.settings_push_endpoint_label),
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        val endpoint = pushEndpoint
                        if (endpoint != null) {
                            Row(
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                Text(
                                    text = endpoint,
                                    style = MaterialTheme.typography.bodySmall,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f).testTag("settings-push-endpoint"),
                                )
                                TextButton(
                                    onClick = {
                                        copyText(endpoint, pushCopied)
                                    },
                                    modifier = Modifier.testTag("settings-push-copy"),
                                ) {
                                    Text(stringResource(R.string.settings_push_copy))
                                }
                            }
                            Text(
                                text = stringResource(R.string.settings_push_endpoint_note),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        } else {
                            Text(
                                text = stringResource(R.string.settings_push_endpoint_waiting),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.testTag("settings-push-waiting"),
                            )
                        }
                    }
                }
            }

            // Battery: background alert delivery (the poll + push wake) is only
            // reliable if Bellhop is exempt from battery optimisation. Shown once
            // monitoring or push is on, since it's irrelevant otherwise.
            if (monitorEnabled || pushEnabled) {
                SettingsSection(modifier = Modifier.testTag("settings-battery")) {
                    Text(
                        text = stringResource(R.string.settings_battery_title),
                        style = MaterialTheme.typography.titleMedium,
                    )
                    Text(
                        text = stringResource(R.string.settings_battery_subtitle),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    if (batteryUnrestricted) {
                        Text(
                            text = stringResource(R.string.settings_battery_unrestricted),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.testTag("settings-battery-ok"),
                        )
                    } else {
                        Text(
                            text = stringResource(R.string.settings_battery_optimized),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                        )
                        OutlinedButton(
                            onClick = onRequestBatteryExemption,
                            // The default outline colour vanishes on the card; use
                            // primary to match the button's own text and read as an
                            // action, like the pills' higher-contrast borders.
                            border = BorderStroke(1.dp, MaterialTheme.colorScheme.primary),
                            // Buttons take their corner from a Material token, not
                            // the theme's shape scale, so each names the radius.
                            shape = MaterialTheme.shapes.small,
                            modifier = Modifier.testTag("settings-battery-request"),
                        ) {
                            Text(stringResource(R.string.settings_battery_action))
                        }
                    }
                }
            }

            // Alerts stays reachable here even when all is green (the dashboard bell
            // only appears when a member is down). The severity badges tally what
            // Front Desk currently alerts on; the brass chevron marks the tap as a
            // jump to the Alerts screen (where an operator can flip them).
            SettingsSection(
                modifier = Modifier.clickable(onClick = onQuotaBadgesClick).testTag("settings-quota-badges"),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(
                            text = stringResource(R.string.settings_quota_badges_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(R.string.settings_quota_badges_subtitle),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    NavChevron(
                        contentDescription = stringResource(R.string.settings_quota_badges_title),
                        tag = "nav-chevron-quota",
                    )
                }
            }

            SettingsSection(modifier = Modifier.clickable(onClick = onAlertsClick).testTag("settings-alerts")) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(
                            text = stringResource(R.string.settings_alerts_title),
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            text = stringResource(R.string.settings_alerts_subtitle),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        AlertSeverityBadges(counts = alertCounts)
                    }
                    NavChevron(contentDescription = stringResource(R.string.settings_alerts_title))
                }
            }

            Spacer(modifier = Modifier.height(24.dp))

            OutlinedButton(
                onClick = { confirmUnlink = true },
                enabled = !unlinking,
                shape = MaterialTheme.shapes.small,
                modifier = Modifier.fillMaxWidth().testTag("settings-unlink"),
            ) {
                Text(stringResource(R.string.dashboard_unlink))
            }
            Spacer(modifier = Modifier.height(24.dp))
        }
    }
}

/**
 * WidgetToggleRow is one switch under the widget heading: the block's name, what
 * it costs, and its switch. A private row rather than repeated Row/Column
 * scaffolding, since the two are identical but for their strings and tag.
 */
@Composable
private fun WidgetToggleRow(
    title: String,
    subtitle: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    tag: String,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(text = title, style = MaterialTheme.typography.bodyMedium)
            Text(
                text = subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        BellhopSwitch(
            checked = checked,
            onCheckedChange = onCheckedChange,
            modifier = Modifier.testTag(tag),
        )
    }
}

/** timeFormatLabel maps a clock choice to its picker label. */
private fun timeFormatLabel(format: TimeFormat): Int =
    when (format) {
        TimeFormat.SYSTEM -> R.string.settings_time_format_system
        TimeFormat.H24 -> R.string.settings_time_format_24h
        TimeFormat.H12 -> R.string.settings_time_format_12h
    }

/** alignLabel maps a widget badge alignment to its picker label. */
private fun alignLabel(align: QuotaBadgeAlign): Int =
    when (align) {
        QuotaBadgeAlign.LEFT -> R.string.settings_widget_align_left
        QuotaBadgeAlign.CENTER -> R.string.settings_widget_align_center
        QuotaBadgeAlign.RIGHT -> R.string.settings_widget_align_right
    }

/**
 * SettingsSection is one entry in the settings list: its content across the
 * full width, on a tight vertical inset, closed by a rule.
 *
 * It replaces the card each setting used to sit in. A screen of stacked cards
 * is mostly card -- 16dp of inset and 16dp of gap around every switch -- and a
 * one-pixel rule separates two settings just as well while fitting several
 * more on the screen. The link summary at the top keeps its card, because that
 * one is a status panel rather than a setting.
 *
 * [modifier] lands on the content, not the rule, so a tappable section's
 * clickable and test tag cover exactly the rows the user sees.
 */
@Composable
private fun SettingsSection(
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = modifier.fillMaxWidth().padding(vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
            content = content,
        )
        HorizontalDivider()
    }
}

/**
 * LanguagePickerDialog lists the system-default option plus every supported
 * language by its own name (endonym), with the active one selected. Choosing one
 * bubbles the tag up; the caller persists it and recreates the activity. The list
 * scrolls within a capped height so it fits on a short screen.
 */
@Composable
private fun LanguagePickerDialog(
    current: String,
    onSelect: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(onClick = onDismiss, modifier = Modifier.testTag("language-cancel")) {
                Text(text = stringResource(R.string.common_cancel))
            }
        },
        title = { Text(text = stringResource(R.string.settings_language)) },
        text = {
            Column(modifier = Modifier.fillMaxWidth().heightIn(max = 360.dp).verticalScroll(rememberScrollState())) {
                LanguageRow(
                    label = stringResource(R.string.language_system_default),
                    selected = current == AppLocale.SYSTEM,
                    onClick = { onSelect(AppLocale.SYSTEM) },
                    tag = "language-option-system",
                )
                AppLocale.SUPPORTED.forEach { tag ->
                    LanguageRow(
                        label = AppLocale.endonyms[tag] ?: tag,
                        selected = current == tag,
                        onClick = { onSelect(tag) },
                        tag = "language-option-$tag",
                    )
                }
            }
        },
    )
}

@Composable
private fun LanguageRow(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
    tag: String,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier =
            Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                .padding(vertical = 8.dp)
                .testTag(tag),
    ) {
        RadioButton(selected = selected, onClick = onClick)
        Text(
            text = label,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.padding(start = 8.dp),
        )
    }
}

/**
 * AlertSeverityBadges renders one small coloured badge per severity (error,
 * warning, info, success), each carrying the count of currently-enabled events of
 * that severity. All four are always shown, even at 0, so the pill reads as a live
 * summary and a tappable destination rather than an inert label. Colour encodes
 * severity (matching the log-rail palette); the count and a per-badge content
 * description carry the meaning for screen readers.
 */
@Composable
private fun AlertSeverityBadges(
    counts: Map<String, Int>,
    modifier: Modifier = Modifier,
) {
    Row(modifier = modifier.testTag("settings-alert-badges"), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        ALERT_SEVERITIES.forEach { severity ->
            val (container, content) = severityColors(severity)
            val count = counts[severity] ?: 0
            val label = severityBadgeLabel(severity)
            // Resolve the description here: the semantics lambda is not composable.
            val desc = stringResource(R.string.settings_alerts_badge_desc, count, label)
            Pill(
                text = count.toString(),
                container = container,
                content = content,
                tag = "settings-alert-badge-$severity",
                modifier = Modifier.semantics { contentDescription = desc },
            )
        }
    }
}

// severityBadgeLabel names a severity for the badge's accessibility description,
// reusing the shared event-severity strings.
@Composable
private fun severityBadgeLabel(severity: String): String =
    when (severity) {
        "success" -> stringResource(R.string.events_sev_success)
        "warning" -> stringResource(R.string.events_sev_warning)
        "error" -> stringResource(R.string.events_sev_error)
        else -> stringResource(R.string.events_sev_info)
    }

// linkedOnLabel is a localized "Jul 14, 2026"-style date for when the link was
// paired, or null when the stamp is absent (links saved before linkedAt existed)
// so the row hides. The formatter is built per call from the current default
// locale (kept in step with the in-app language by AppLocale), so the date follows
// a language switch instead of freezing at process start.
private fun linkedOnLabel(linkedAt: Long): String? {
    if (linkedAt <= 0L) return null
    val formatter =
        DateTimeFormatter
            .ofLocalizedDate(FormatStyle.MEDIUM)
            .withLocale(Locale.getDefault())
            .withZone(ZoneId.systemDefault())
    return formatter.format(Instant.ofEpochMilli(linkedAt))
}

private fun timeoutLabel(option: LockTimeout): Int =
    when (option) {
        LockTimeout.IMMEDIATELY -> R.string.settings_lock_now
        LockTimeout.ONE_MINUTE -> R.string.settings_lock_1m
        LockTimeout.FIVE_MINUTES -> R.string.settings_lock_5m
        LockTimeout.FIFTEEN_MINUTES -> R.string.settings_lock_15m
        LockTimeout.THIRTY_MINUTES -> R.string.settings_lock_30m
        LockTimeout.ONE_HOUR -> R.string.settings_lock_1h
    }

@Preview(showBackground = true)
@Composable
private fun SettingsScreenPreview() {
    BellhopTheme {
        SettingsScreen(
            link =
                LinkState.Linked(
                    fdUrl = "http://10.0.2.2:8080",
                    fdName = "Home Front Desk",
                    role = "operator",
                    deviceId = "dev-1",
                    label = "Pixel 8",
                ),
            lockConfig = LockConfig(enabled = true, timeoutMs = LockTimeout.THIRTY_MINUTES.millis),
            lockAvailable = true,
            monitorEnabled = true,
            pushEnabled = true,
            pushEndpoint = "https://ntfy.sh/upExample123",
            pushDistributorAvailable = true,
            onBack = {},
            onToggleLock = {},
            onSelectTimeout = {},
            onToggleMonitor = {},
            onTogglePush = {},
            onAlertsClick = {},
            onQuotaBadgesClick = {},
            onUnlink = {},
        )
    }
}
