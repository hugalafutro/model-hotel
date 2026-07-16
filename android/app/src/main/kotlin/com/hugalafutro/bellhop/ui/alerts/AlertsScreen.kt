package com.hugalafutro.bellhop.ui.alerts

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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.AlertEventDef
import com.hugalafutro.bellhop.data.AlertStatus
import com.hugalafutro.bellhop.ui.common.Pill
import com.hugalafutro.bellhop.ui.common.StatusBanner
import com.hugalafutro.bellhop.ui.common.bellhopSwitchColors
import com.hugalafutro.bellhop.ui.common.severityColors
import com.hugalafutro.bellhop.ui.theme.BellhopTheme

/**
 * AlertsScreen renders Front Desk's outbound-alert subsystem: a delivery-health
 * pill (is the apprise-api reachable and delivering?) and the events Front Desk
 * can notify on, grouped by category with a severity dot and a per-event switch
 * showing its live enabled state. An operator flips a switch (routed through the
 * biometric operator gate); a monitor sees the switches read-only. State and the
 * flip action come from [AlertsViewModel].
 */
@Composable
fun AlertsScreen(
    onBack: () -> Unit,
    ui: AlertsUiState = AlertsUiState(),
    // Whether this device is paired as an operator. A monitor sees each event's
    // state read-only (a disabled switch); an operator can flip it, which routes
    // through the biometric operator gate in the host before [onToggleEvent] fires.
    canOperate: Boolean = false,
    onToggleEvent: (type: String, enabled: Boolean) -> Unit = { _, _ -> },
    onDismissActionError: () -> Unit = {},
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
                IconButton(onClick = onBack, modifier = Modifier.testTag("alerts-back")) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = stringResource(R.string.alerts_back),
                    )
                }
                Text(
                    text = stringResource(R.string.alerts_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.weight(1f).testTag("alerts-title"),
                )
            }

            if (ui.revoked) {
                StatusBanner(text = stringResource(R.string.dashboard_revoked), tag = "alerts-revoked")
            } else if (ui.error != null) {
                StatusBanner(
                    text = stringResource(R.string.dashboard_refresh_failed, ui.error),
                    tag = "alerts-error",
                )
            }
            // Operator-flip feedback: a 403 means this monitor device may not flip
            // (the switch is disabled, but a mid-session role change can still land
            // one here), and a failed flip surfaces so a tap never vanishes silently.
            if (ui.action.forbidden) {
                StatusBanner(text = stringResource(R.string.alerts_flip_forbidden), tag = "alerts-flip-forbidden")
            }
            ui.action.error?.let { message ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier.padding(bottom = 8.dp),
                ) {
                    Text(
                        text = stringResource(R.string.alerts_flip_failed, message),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.weight(1f).testTag("alerts-flip-error"),
                    )
                    TextButton(onClick = onDismissActionError, modifier = Modifier.testTag("alerts-flip-dismiss")) {
                        Text(text = stringResource(R.string.member_op_dismiss))
                    }
                }
            }

            if (ui.loading) {
                Box(
                    modifier = Modifier.fillMaxWidth().weight(1f),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(modifier = Modifier.testTag("alerts-loading"))
                }
            } else {
                LazyColumn(
                    modifier = Modifier.weight(1f).testTag("alerts-list"),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                    contentPadding = PaddingValues(bottom = 24.dp),
                ) {
                    item { DeliveryStatusCard(status = ui.status) }
                    item {
                        Text(
                            text = stringResource(R.string.alerts_catalog_title),
                            style = MaterialTheme.typography.titleMedium,
                            modifier = Modifier.padding(top = 8.dp).testTag("alerts-catalog-title"),
                        )
                    }
                    item {
                        Text(
                            text = stringResource(R.string.alerts_catalog_note),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    // Group by category, preserving the server's order (a
                    // LinkedHashMap keeps first-seen category order, mirroring the
                    // Front Desk web picker).
                    ui.catalog
                        .groupByTo(LinkedHashMap()) { it.category }
                        .forEach { (category, defs) ->
                            item {
                                Text(
                                    text = category,
                                    style = MaterialTheme.typography.labelLarge,
                                    color = MaterialTheme.colorScheme.primary,
                                    modifier = Modifier.padding(top = 8.dp),
                                )
                            }
                            items(defs) { def ->
                                AlertRow(
                                    def = def,
                                    canOperate = canOperate,
                                    toggling = ui.action.togglingType == def.type,
                                    onToggle = { enabled -> onToggleEvent(def.type, enabled) },
                                )
                            }
                        }
                }
            }
        }
    }
}

@Composable
private fun DeliveryStatusCard(
    status: AlertStatus?,
    modifier: Modifier = Modifier,
) {
    val (severity, labelRes) = statusSeverityAndLabel(status)
    val (container, content) = severityColors(severity)
    // The probe detail is only meaningful (and only carries a reason) once the
    // notifier is configured but not fully healthy; a green "delivering" pill and
    // an unconfigured notifier both need no note (mirrors Front Desk web).
    val showDetail =
        status?.configured == true &&
            status.detail.isNotBlank() &&
            (!status.reachable || !status.healthy)
    Card(modifier = modifier.fillMaxWidth().testTag("alerts-status-card")) {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                text = stringResource(R.string.alerts_delivery_title),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Pill(
                text = stringResource(labelRes),
                container = container,
                content = content,
                tag = "alerts-status-pill",
            )
            if (showDetail) {
                Text(
                    text = status.detail,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag("alerts-status-detail"),
                )
            }
        }
    }
}

@Composable
private fun AlertRow(
    def: AlertEventDef,
    canOperate: Boolean,
    toggling: Boolean,
    onToggle: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    val (container, content) = severityColors(def.severity)
    Card(modifier = modifier.fillMaxWidth().testTag("alert-row")) {
        Row(
            modifier = Modifier.padding(14.dp).fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Pill(
                text = severityLabel(def.severity),
                container = container,
                content = content,
                tag = "alert-sev-${def.severity}",
            )
            Text(
                text = eventTypeLabel(def.type),
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            // A flip in flight shows a spinner in the switch's place so a double-tap
            // can't fire a second request. A monitor device gets a disabled switch that
            // still reflects the true on/off state (read-only); an operator can flip it.
            if (toggling) {
                CircularProgressIndicator(
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(24.dp).testTag("alert-toggle-spinner-${def.type}"),
                )
            } else {
                // Keep onCheckedChange non-null even for a monitor so the switch keeps
                // its toggleable semantics (and reads as disabled, not merely inert);
                // enabled=false blocks the tap, and Front Desk's 403 is the real guard.
                Switch(
                    checked = def.enabled,
                    onCheckedChange = onToggle,
                    enabled = canOperate,
                    colors = bellhopSwitchColors(),
                    modifier = Modifier.testTag("alert-toggle-${def.type}"),
                )
            }
        }
    }
}

// statusSeverityAndLabel narrows the AlertStatus into a (severity-key, label)
// pair, mirroring Front Desk web's StatusPill: unconfigured is neutral info,
// then unreachable (danger) and unhealthy (warning) before a green "delivering".
private fun statusSeverityAndLabel(status: AlertStatus?): Pair<String, Int> =
    when {
        status == null || !status.configured -> "info" to R.string.alerts_status_not_configured
        !status.reachable -> "error" to R.string.alerts_status_unreachable
        !status.healthy -> "warning" to R.string.alerts_status_unhealthy
        else -> "success" to R.string.alerts_status_delivering
    }

/** severityLabel is the badge text for a display severity, falling back to the raw value. */
@Composable
private fun severityLabel(severity: String): String =
    when (severity) {
        "info" -> stringResource(R.string.events_sev_info)
        "success" -> stringResource(R.string.events_sev_success)
        "warning" -> stringResource(R.string.events_sev_warning)
        "error" -> stringResource(R.string.events_sev_error)
        else -> severity
    }

// eventTypeLabel gives a readable name for a catalog event type, falling back to
// the raw type so a brand-new server-side event still renders before a string is
// added (matches the Front Desk web picker's defaultValue behaviour).
@Composable
private fun eventTypeLabel(type: String): String =
    when (type) {
        "health.down" -> stringResource(R.string.alerts_event_health_down)
        "health.up" -> stringResource(R.string.alerts_event_health_up)
        "config.sync_failed" -> stringResource(R.string.alerts_event_config_sync_failed)
        "config.synced" -> stringResource(R.string.alerts_event_config_synced)
        "config.auto_synced" -> stringResource(R.string.alerts_event_config_auto_synced)
        "config.autosync_stale" -> stringResource(R.string.alerts_event_config_autosync_stale)
        "version.fetch_failed" -> stringResource(R.string.alerts_event_version_fetch_failed)
        "version.fetch_recovered" -> stringResource(R.string.alerts_event_version_fetch_recovered)
        "traefik.stale" -> stringResource(R.string.alerts_event_traefik_stale)
        "member.added" -> stringResource(R.string.alerts_event_member_added)
        "member.removed" -> stringResource(R.string.alerts_event_member_removed)
        "member.state_changed" -> stringResource(R.string.alerts_event_member_state_changed)
        else -> type
    }

@Preview(showBackground = true)
@Composable
private fun AlertsScreenPreview() {
    BellhopTheme {
        AlertsScreen(
            onBack = {},
            ui =
                AlertsUiState(
                    loading = false,
                    status = AlertStatus(configured = true, reachable = true, healthy = true),
                    catalog =
                        listOf(
                            AlertEventDef("health.down", "Health", "error", defaultOn = true),
                            AlertEventDef("health.up", "Health", "success", defaultOn = true),
                            AlertEventDef("config.sync_failed", "Config Sync", "warning", defaultOn = true),
                            AlertEventDef("config.synced", "Config Sync", "info", defaultOn = false),
                        ),
                ),
        )
    }
}
