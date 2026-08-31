package com.hugalafutro.bellhop.ui.common

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import com.hugalafutro.bellhop.R

/**
 * eventTypeLabelRes is the string resource naming a control-plane event type,
 * or null for a type this build doesn't know. It is the non-Compose half of
 * [eventTypeLabel], so the background notifier can title a notification with
 * the same name the Alerts screen and both event logs use.
 */
@StringRes
internal fun eventTypeLabelRes(type: String): Int? =
    when (type) {
        "health.down" -> R.string.alerts_event_health_down
        "health.up" -> R.string.alerts_event_health_up
        "config.sync_failed" -> R.string.alerts_event_config_sync_failed
        "config.synced" -> R.string.alerts_event_config_synced
        "config.auto_synced" -> R.string.alerts_event_config_auto_synced
        "config.autosync_stale" -> R.string.alerts_event_config_autosync_stale
        "config.sync_held" -> R.string.alerts_event_config_sync_held
        "config.sync_incomplete" -> R.string.alerts_event_config_sync_incomplete
        "config.sync_recovered" -> R.string.alerts_event_config_sync_recovered
        "config.regenerated" -> R.string.event_config_regenerated
        "version.fetch_failed" -> R.string.alerts_event_version_fetch_failed
        "version.fetch_recovered" -> R.string.alerts_event_version_fetch_recovered
        "traefik.stale" -> R.string.alerts_event_traefik_stale
        "fleet.state_changed" -> R.string.alerts_event_fleet_state_changed
        "member.added" -> R.string.alerts_event_member_added
        "member.removed" -> R.string.alerts_event_member_removed
        "member.state_changed" -> R.string.alerts_event_member_state_changed
        "fleet.disbanded" -> R.string.alerts_event_fleet_disbanded
        "backup.stale" -> R.string.alerts_event_backup_stale
        "backup.recovered" -> R.string.alerts_event_backup_recovered
        "device.paired" -> R.string.event_device_paired
        "device.revoked" -> R.string.event_device_revoked
        "settings.changed" -> R.string.event_settings_changed
        else -> null
    }

/**
 * eventTypeLabel is the human name for a control-plane event type, shared by
 * the alert catalog, both event logs and the background notifier so the same
 * event reads identically everywhere. The raw dotted code stays available in
 * the log rows' mono meta line (and in the long-press copy text); a brand-new
 * server-side type this build doesn't know yet falls back to that raw code as
 * the title.
 */
@Composable
internal fun eventTypeLabel(type: String): String = eventTypeLabelRes(type)?.let { stringResource(it) } ?: type

/**
 * withoutMemberName drops the member's own name out of a Front Desk event
 * message on surfaces that already show that name directly above the line --
 * the dashboard card's recent-event pill and the member's own log -- so
 * "MH2 is healthy" reads as "is healthy" and a longer message has more room
 * before it truncates. A feed that mixes members keeps the full message: there
 * the name is the only thing saying which member the line is about.
 *
 * Messages are composed server-side in English, so two shapes cover them: the
 * name leading the sentence, and a " to <name>" sync target inside it. Anything
 * else, and anything that would strip down to nothing, is returned as it is.
 */
internal fun withoutMemberName(
    message: String,
    memberName: String,
): String {
    if (message.isBlank() || memberName.isBlank()) return message
    val stripped =
        stripLeadingName(message, memberName)
            ?: stripSyncTarget(message, memberName)
            ?: return message
    return stripped.ifBlank { message }
}

/**
 * NAME_BOUNDARY are the characters that may follow the name for it to count as
 * a whole word, which keeps "MH2" from matching inside "MH20".
 */
private const val NAME_BOUNDARY = " :,"

/** stripLeadingName removes a leading name plus its separator, or null if the message doesn't open with the name. */
private fun stripLeadingName(
    message: String,
    name: String,
): String? {
    if (!message.startsWith(name)) return null
    val rest = message.substring(name.length)
    if (rest.isNotEmpty() && rest.first() !in NAME_BOUNDARY) return null
    return rest.trimStart(' ').trimStart(':', ',', '-').trim()
}

/** stripSyncTarget removes a " to <name>" target from inside the message, or null if there isn't one. */
private fun stripSyncTarget(
    message: String,
    name: String,
): String? {
    val target = " to $name"
    val at = message.indexOf(target)
    if (at < 0) return null
    val after = at + target.length
    if (after < message.length && message[after] !in NAME_BOUNDARY) return null
    return (message.substring(0, at) + message.substring(after)).trim()
}
