package com.hugalafutro.bellhop.ui.common

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.Icon
import androidx.compose.material3.SmallFloatingActionButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import kotlinx.coroutines.launch

/**
 * ScrollToTopButton overlays a bottom-right "return to top" control on a scrolling
 * [LazyListState] list (the events log and the dashboard member list). It fades in
 * once the first item has scrolled off ([LazyListState.firstVisibleItemIndex] > 0)
 * and fades itself away again on return to the top, so it's absent until it's
 * useful. Tapping it animates the list back to the top.
 *
 * A [BoxScope] extension so the caller drops it straight into the Box that wraps
 * the list, where it aligns to the bottom-end corner over the content.
 */
@Composable
fun BoxScope.ScrollToTopButton(
    listState: LazyListState,
    modifier: Modifier = Modifier,
) {
    val scope = rememberCoroutineScope()
    val visible by remember { derivedStateOf { listState.firstVisibleItemIndex > 0 } }
    AnimatedVisibility(
        visible = visible,
        enter = fadeIn(),
        exit = fadeOut(),
        modifier = modifier.align(Alignment.BottomEnd).padding(16.dp),
    ) {
        SmallFloatingActionButton(
            onClick = { scope.launch { listState.animateScrollToItem(0) } },
            modifier = Modifier.testTag("scroll-to-top"),
        ) {
            Icon(
                imageVector = Icons.Filled.KeyboardArrowUp,
                contentDescription = stringResource(R.string.scroll_to_top),
            )
        }
    }
}
