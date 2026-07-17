package com.hugalafutro.bellhop.ui.common

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import com.hugalafutro.bellhop.R

/**
 * eventTypeLabel is the human name for a control-plane event type, shared by
 * the alert catalog and both event logs so the same event reads identically
 * everywhere. The raw dotted code stays available in the log rows' mono meta
 * line (and in the long-press copy text); a brand-new server-side type this
 * build doesn't know yet falls back to that raw code as the title.
 */
@Composable
internal fun eventTypeLabel(type: String): String =
    when (type) {
        "health.down" -> stringResource(R.string.alerts_event_health_down)
        "health.up" -> stringResource(R.string.alerts_event_health_up)
        "config.sync_failed" -> stringResource(R.string.alerts_event_config_sync_failed)
        "config.synced" -> stringResource(R.string.alerts_event_config_synced)
        "config.auto_synced" -> stringResource(R.string.alerts_event_config_auto_synced)
        "config.autosync_stale" -> stringResource(R.string.alerts_event_config_autosync_stale)
        "config.sync_held" -> stringResource(R.string.alerts_event_config_sync_held)
        "config.regenerated" -> stringResource(R.string.event_config_regenerated)
        "version.fetch_failed" -> stringResource(R.string.alerts_event_version_fetch_failed)
        "version.fetch_recovered" -> stringResource(R.string.alerts_event_version_fetch_recovered)
        "traefik.stale" -> stringResource(R.string.alerts_event_traefik_stale)
        "member.added" -> stringResource(R.string.alerts_event_member_added)
        "member.removed" -> stringResource(R.string.alerts_event_member_removed)
        "member.state_changed" -> stringResource(R.string.alerts_event_member_state_changed)
        "device.paired" -> stringResource(R.string.event_device_paired)
        "device.revoked" -> stringResource(R.string.event_device_revoked)
        "settings.changed" -> stringResource(R.string.event_settings_changed)
        else -> type
    }
