package com.hugalafutro.bellhop.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Shapes
import androidx.compose.ui.unit.dp

/**
 * BellhopShapes tightens every corner Material 3 would otherwise round.
 * The stock scale (4/8/12/16/28dp) turns small surfaces — chips, pills, the
 * badge strip — into ovals, which is the opposite of the squared-off,
 * lightly-softened look the rest of the app aims for. Halving it keeps the
 * corners legible as corners at every size.
 *
 * Material reads these tokens for Card, Surface, the FABs and the modal
 * sheet, so setting them here is enough for those. Button and the pills pick
 * their own "full" corner regardless of the theme, so those pass a shape
 * from this scale explicitly.
 */
val BellhopShapes =
    Shapes(
        extraSmall = RoundedCornerShape(2.dp),
        small = RoundedCornerShape(4.dp),
        medium = RoundedCornerShape(6.dp),
        large = RoundedCornerShape(8.dp),
        extraLarge = RoundedCornerShape(12.dp),
    )
