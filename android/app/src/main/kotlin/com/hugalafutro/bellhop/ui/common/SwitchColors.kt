package com.hugalafutro.bellhop.ui.common

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SwitchColors
import androidx.compose.material3.SwitchDefaults
import androidx.compose.runtime.Composable

/**
 * bellhopSwitchColors is the single source of truth for every Switch in the app
 * (Settings, Alerts, the dashboard auto-sync control), so a toggle reads the same
 * in every state instead of each call site re-deriving it, and any new switch is
 * consistent for free.
 *
 * Material's default unchecked track is the Card's own colour with an outline
 * thumb/border, so an off (and especially a disabled-off) switch nearly vanishes
 * against the card. This gives the off state a light thumb + border over a surface
 * track so it stays legible on both the ink and paper schemes. The
 * disabled-unchecked variants are set too (dimmed to 0.6 alpha so it still reads
 * as disabled) because several of these switches disable themselves mid-flight.
 */
@Composable
fun bellhopSwitchColors(): SwitchColors =
    SwitchDefaults.colors(
        uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
        uncheckedTrackColor = MaterialTheme.colorScheme.surface,
        uncheckedBorderColor = MaterialTheme.colorScheme.onSurfaceVariant,
        disabledUncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f),
        disabledUncheckedTrackColor = MaterialTheme.colorScheme.surface,
        disabledUncheckedBorderColor = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f),
    )
