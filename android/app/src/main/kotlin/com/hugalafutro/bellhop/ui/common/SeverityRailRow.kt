package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp

// Width of the coloured severity rail down the left edge of each log line.
private val RAIL_WIDTH = 3.dp

/**
 * SeverityRailRow is the shared "log line" chrome for both event lists: a
 * severity-coloured rail down the left edge plus a faint tint of the same
 * colour, wrapping caller [content].
 *
 * The rail is painted with drawBehind on a matchParentSize overlay instead of
 * being a fillMaxHeight sibling. That removes the per-row height(IntrinsicSize.Min)
 * measurement pass the old two-child Row needed — the double-measure that made
 * long event lists stutter while flinging on device. The overlay still fills the
 * row and carries [railTag], so the tests that assert / tap the rail keep working.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun SeverityRailRow(
    severity: String,
    rowTag: String,
    railTag: String,
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    // Long-press handler, used when copy is gated behind a hold gesture so a
    // stray tap while scrolling the log doesn't copy. When set, the row wires
    // combinedClickable; a plain [onClick] alone still uses clickable.
    onLongClick: (() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val (accent, _) = severityColors(severity)
    Box(
        modifier =
            modifier
                .fillMaxWidth()
                .then(
                    when {
                        onLongClick != null ->
                            Modifier.combinedClickable(
                                onClick = onClick ?: {},
                                onLongClick = onLongClick,
                            )
                        onClick != null -> Modifier.clickable(onClick = onClick)
                        else -> Modifier
                    },
                )
                .background(accent.copy(alpha = 0.06f))
                .testTag(rowTag),
    ) {
        // Full-row overlay that only paints the left rail. matchParentSize keeps it
        // out of the Box's own sizing (so no intrinsic pass), yet gives the tests a
        // displayed, tappable node at railTag. Taps fall through to the Box's
        // clickable, exactly as the old sibling rail did.
        Box(
            modifier =
                Modifier
                    .matchParentSize()
                    .drawBehind {
                        drawRect(color = accent, size = Size(RAIL_WIDTH.toPx(), size.height))
                    }
                    .testTag(railTag),
        )
        // start clears the rail (was a 3.dp rail + 12.dp content padding = 15.dp).
        Column(
            modifier = Modifier.fillMaxWidth().padding(start = 15.dp, end = 12.dp, top = 10.dp, bottom = 10.dp),
            verticalArrangement = Arrangement.spacedBy(3.dp),
        ) {
            content()
        }
    }
}
