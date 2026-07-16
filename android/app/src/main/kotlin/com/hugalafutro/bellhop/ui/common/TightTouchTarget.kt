package com.hugalafutro.bellhop.ui.common

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.platform.ViewConfiguration
import androidx.compose.ui.unit.DpSize

/**
 * TightTouchTarget keeps [content]'s tap targets at their drawn bounds by
 * turning off the 48dp minimum touch-target expansion (ViewConfiguration's
 * minimumTouchTargetSize, applied by every clickable). The expansion is meant
 * for isolated controls; on links that sit closer than 48dp to other tap
 * targets it silently steals their taps instead.
 */
@Composable
internal fun TightTouchTarget(content: @Composable () -> Unit) {
    val base = LocalViewConfiguration.current
    val tight =
        remember(base) {
            object : ViewConfiguration by base {
                override val minimumTouchTargetSize: DpSize get() = DpSize.Zero
            }
        }
    CompositionLocalProvider(LocalViewConfiguration provides tight, content = content)
}
