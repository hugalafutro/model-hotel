package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp

/**
 * NavChevron is the one affordance that marks a card or row whose tap navigates
 * to another screen: a trailing chevron tinted with the brass accent. Static
 * cards omit it, and a card whose tap is an inline action (a toggle, a copy)
 * keeps its own control instead — so a brass chevron always reads as "tap to go
 * there", never "tap to do something here". [contentDescription] should name the
 * destination for screen readers.
 */
@Composable
fun NavChevron(
    contentDescription: String,
    modifier: Modifier = Modifier,
    tag: String = "nav-chevron",
) {
    Icon(
        imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
        contentDescription = contentDescription,
        tint = MaterialTheme.colorScheme.primary,
        modifier = modifier.size(24.dp).testTag(tag),
    )
}
