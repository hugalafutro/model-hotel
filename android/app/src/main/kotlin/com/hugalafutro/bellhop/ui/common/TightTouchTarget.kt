package com.hugalafutro.bellhop.ui.common

import androidx.compose.material3.LocalMinimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.platform.ViewConfiguration
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpSize

/**
 * TightTouchTarget keeps [content]'s tap targets at their drawn bounds by
 * turning off both of the 48dp minimums Compose puts on a clickable. They are
 * separate rules with separate switches, and turning off only the first is
 * what left the quota strip's rows 49dp apart while its chips drew at 17dp:
 *
 * - ViewConfiguration's `minimumTouchTargetSize` grows the clickable's *hit
 *   area* past its bounds. Meant for isolated controls; on links that sit
 *   closer than 48dp to other tap targets it steals their taps instead.
 * - Material 3's [LocalMinimumInteractiveComponentSize] grows the clickable's
 *   *measured size*, which is layout rather than input: an interactive
 *   `Surface` reserves a 48dp box however small it draws, and the row it sits
 *   in is that tall whatever arrangement the parent asked for.
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
    CompositionLocalProvider(
        LocalViewConfiguration provides tight,
        // Dp.Unspecified is Material's own "no minimum" sentinel: the enforcing
        // layout node skips the coercion rather than clamping to zero.
        LocalMinimumInteractiveComponentSize provides Dp.Unspecified,
        content = content,
    )
}
