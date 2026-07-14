package com.hugalafutro.bellhop.ui.settings

import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LockConfig
import com.hugalafutro.bellhop.data.LockTimeout
import com.hugalafutro.bellhop.ui.common.FilterPill
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

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
    onUnlink: () -> Unit,
    modifier: Modifier = Modifier,
    notificationsBlocked: Boolean = false,
    pushEnabled: Boolean = false,
    pushEndpoint: String? = null,
    pushDistributorAvailable: Boolean = false,
    pushNotificationsBlocked: Boolean = false,
    unlinking: Boolean = false,
    unlinkFailed: Boolean = false,
    onDismissUnlinkError: () -> Unit = {},
    onForceUnlink: () -> Unit = {},
    holdToCopy: Boolean = false,
    onToggleHoldToCopy: (Boolean) -> Unit = {},
) {
    var confirmUnlink by remember { mutableStateOf(false) }
    var confirmCopyAddress by remember { mutableStateOf(false) }
    val clipboard = LocalClipboardManager.current
    val context = LocalContext.current
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
                        clipboard.setText(AnnotatedString(address))
                        Toast.makeText(context, addressCopied, Toast.LENGTH_SHORT).show()
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
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
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
                        Switch(
                            checked = holdToCopy,
                            onCheckedChange = onToggleHoldToCopy,
                            // Same off-state colours as the other switches so an off
                            // toggle stays legible on the card.
                            colors =
                                SwitchDefaults.colors(
                                    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                    uncheckedTrackColor = MaterialTheme.colorScheme.surface,
                                    uncheckedBorderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                ),
                            modifier = Modifier.testTag("settings-hold-copy-toggle"),
                        )
                    }
                }
            }

            Spacer(modifier = Modifier.height(16.dp))

            // App lock: on/off plus the idle window it measures from foreground exit.
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
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
                        Switch(
                            checked = lockConfig.enabled,
                            onCheckedChange = onToggleLock,
                            enabled = lockAvailable,
                            // The default unchecked track is surfaceContainerHighest
                            // (the Card's own colour) with an outline thumb/border, so
                            // an off switch blends into the card. Give the off state a
                            // light thumb + border over a surface track so it stays
                            // legible on both the ink and paper schemes.
                            colors =
                                SwitchDefaults.colors(
                                    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                    uncheckedTrackColor = MaterialTheme.colorScheme.surface,
                                    uncheckedBorderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                ),
                            modifier = Modifier.testTag("settings-lock-toggle"),
                        )
                    }
                    if (!lockAvailable) {
                        // No biometric or device credential enrolled: the gate can't
                        // engage, so say why the toggle is inert rather than failing
                        // silently when it's flipped on.
                        Text(
                            text = stringResource(R.string.settings_lock_unavailable),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
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
            }

            Spacer(modifier = Modifier.height(16.dp))

            // Background monitoring: the Layer-2 backstop (plan section 5.2). Off
            // by default; turning it on schedules the periodic poll and prompts
            // for the notification permission.
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
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
                        Switch(
                            checked = monitorEnabled,
                            onCheckedChange = onToggleMonitor,
                            // Same off-state colours as the lock switch so an off
                            // toggle stays legible on the card (see note above).
                            colors =
                                SwitchDefaults.colors(
                                    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                    uncheckedTrackColor = MaterialTheme.colorScheme.surface,
                                    uncheckedBorderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                ),
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
            }

            Spacer(modifier = Modifier.height(16.dp))

            // Real-time push: Layer-3 opt-in (plan section 5.2). Off by default like
            // monitoring; turning it on registers with a UnifiedPush distributor and
            // (on API 33+) prompts for the notification permission.
            Card(modifier = Modifier.fillMaxWidth()) {
                Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
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
                        Switch(
                            checked = pushEnabled,
                            onCheckedChange = onTogglePush,
                            // Same off-state colours as the other switches so an off
                            // toggle stays legible on the card (see note above).
                            colors =
                                SwitchDefaults.colors(
                                    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                    uncheckedTrackColor = MaterialTheme.colorScheme.surface,
                                    uncheckedBorderColor = MaterialTheme.colorScheme.onSurfaceVariant,
                                ),
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
                                            clipboard.setText(AnnotatedString(endpoint))
                                            Toast.makeText(context, pushCopied, Toast.LENGTH_SHORT).show()
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
            }

            Spacer(modifier = Modifier.height(16.dp))

            // Alerts stays reachable here even when all is green (the dashboard bell
            // only appears when a member is down).
            Card(modifier = Modifier.fillMaxWidth().clickable(onClick = onAlertsClick).testTag("settings-alerts")) {
                Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(
                        text = stringResource(R.string.settings_alerts_title),
                        style = MaterialTheme.typography.titleMedium,
                    )
                    Text(
                        text = stringResource(R.string.settings_alerts_subtitle),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            Spacer(modifier = Modifier.height(24.dp))

            OutlinedButton(
                onClick = { confirmUnlink = true },
                enabled = !unlinking,
                modifier = Modifier.fillMaxWidth().testTag("settings-unlink"),
            ) {
                Text(stringResource(R.string.dashboard_unlink))
            }
            Spacer(modifier = Modifier.height(24.dp))
        }
    }
}

// A localized "Jul 14, 2026"-style date for when the link was paired, or null
// when the stamp is absent (links saved before linkedAt existed) so the row hides.
private val linkedOnFormatter: DateTimeFormatter =
    DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withZone(ZoneId.systemDefault())

private fun linkedOnLabel(linkedAt: Long): String? =
    if (linkedAt <= 0L) null else linkedOnFormatter.format(Instant.ofEpochMilli(linkedAt))

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
            onUnlink = {},
        )
    }
}
