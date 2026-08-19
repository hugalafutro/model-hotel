package com.hugalafutro.bellhop.ui.common

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

/**
 * BellhopSwitch is the app's on/off toggle: a rounded rectangle rather than
 * Material's stadium.
 *
 * Material 3's own `Switch` hard-codes its shape to a full-round track and a
 * circular thumb -- neither is exposed as a parameter and neither reads
 * [MaterialTheme.shapes] -- so it stayed a pill through the corner-radius pass
 * that squared off the rest of Bellhop. This draws the same control from a
 * track and a thumb that both take the theme's small radius, and keeps the
 * behaviour that matters: [Role.Switch] semantics (so screen readers and
 * `assertIsOn`/`performClick` see a switch), an animated thumb, a disabled
 * state, and the 48dp minimum touch target Material would have given it.
 *
 * The colours are the ones the old shared `bellhopSwitchColors()` helper
 * existed to supply. Material's default off state is the card's own colour with
 * an outline thumb, which nearly vanishes against a card, so off is drawn as a
 * light thumb and border over a surface track and stays legible on both the ink
 * and paper schemes.
 */
@Composable
fun BellhopSwitch(
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    // Dimmed rather than greyed: several of these switches disable themselves
    // mid-flight (a lock that needs hardware, an action already in progress),
    // and the reading has to survive that.
    val stateAlpha = if (enabled) 1f else 0.6f
    val track = if (checked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surface
    val edge =
        if (checked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant
    val knob =
        if (checked) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant

    val trackColor by animateColorAsState(track.copy(alpha = track.alpha * stateAlpha), label = "switchTrack")
    val edgeColor by animateColorAsState(edge.copy(alpha = edge.alpha * stateAlpha), label = "switchEdge")
    val knobColor by animateColorAsState(knob.copy(alpha = knob.alpha * stateAlpha), label = "switchKnob")
    val knobOffset by animateDpAsState(
        targetValue = if (checked) TrackWidth - (ThumbInset * 2) - ThumbSize else 0.dp,
        label = "switchKnobOffset",
    )

    Box(
        modifier =
            modifier
                .minimumInteractiveComponentSize()
                .toggleable(
                    value = checked,
                    enabled = enabled,
                    role = Role.Switch,
                    onValueChange = onCheckedChange,
                ).size(width = TrackWidth, height = TrackHeight)
                // Background first, border second: draw modifiers paint in chain
                // order, so the other way round buries the outline under the fill.
                .background(color = trackColor, shape = TrackShape)
                .border(width = 1.dp, color = edgeColor, shape = TrackShape)
                .padding(ThumbInset),
        contentAlignment = Alignment.CenterStart,
    ) {
        Box(
            modifier =
                Modifier
                    .offset(x = knobOffset)
                    .size(ThumbSize)
                    .background(color = knobColor, shape = ThumbShape),
        )
    }
}

// Narrower and shorter than Material's 52x32 stadium: a rectangle needs less
// width to read as a track, and the settings rows get the difference back.
private val TrackWidth = 44.dp
private val TrackHeight = 24.dp
private val ThumbSize = 14.dp
private val ThumbInset = 3.dp

// Half the theme's own small radius each, so the thumb stays visually squarer
// than the track it slides in rather than reading as a rounded blob inside a
// sharper box.
private val TrackShape = RoundedCornerShape(4.dp)
private val ThumbShape = RoundedCornerShape(2.dp)
