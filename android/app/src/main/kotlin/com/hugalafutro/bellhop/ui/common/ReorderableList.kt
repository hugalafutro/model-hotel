package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp

/**
 * Returns a new list with the item at [from] moved to [to], leaving all
 * other items in their relative order. Pure and side-effect free so it can
 * be unit tested without Compose.
 *
 * Out-of-range [from] is a no-op (returns [list] unchanged). Out-of-range
 * [to] is clamped into `0..list.lastIndex`. `from == to` is a no-op.
 */
fun <T> moveItem(
    list: List<T>,
    from: Int,
    to: Int,
): List<T> {
    if (list.isEmpty() || from !in list.indices) return list
    val target = to.coerceIn(0, list.lastIndex)
    if (from == target) return list
    val mutable = list.toMutableList()
    val item = mutable.removeAt(from)
    mutable.add(target, item)
    return mutable
}

/**
 * A minimal, hand-rolled drag-to-reorder [LazyColumn] for short string lists
 * (no drag-and-drop library dependency; see task brief). Each row is handed a
 * `dragHandle` modifier to hang on its grip; dragging that grip up/down
 * live-shifts the row, and [onMove] is invoked once with the final
 * `(from, to)` on release, so the caller owns the source of truth and is
 * expected to persist the reordered list.
 *
 * The drag lives on the handle rather than the whole row, and so starts on
 * touch instead of after a long press: a row that carries its own controls (a
 * switch, a tappable label) can't also be a drag surface without one gesture
 * stealing from the other, and a visible grip says the list reorders at all,
 * which a long-press-anywhere list never does.
 *
 * Row heights are assumed roughly uniform (the common case for short
 * configurator lists like provider name order) since the drop target is
 * derived from accumulated drag distance divided by the first measured row
 * height.
 */
@Composable
fun ReorderableColumn(
    items: List<String>,
    onMove: (from: Int, to: Int) -> Unit,
    modifier: Modifier = Modifier,
    key: (String) -> Any = { it },
    lazyListState: LazyListState = rememberLazyListState(),
    itemContent: @Composable (item: String, dragHandle: Modifier) -> Unit,
) {
    var draggedIndex by remember { mutableStateOf<Int?>(null) }
    var dragOffsetY by remember { mutableStateOf(0f) }
    var rowHeightPx by remember { mutableIntStateOf(0) }

    LazyColumn(modifier = modifier.testTag("reorderable-column"), state = lazyListState) {
        itemsIndexed(items, key = { _, item -> key(item) }) { index, item ->
            val isDragged = draggedIndex == index
            Surface(
                tonalElevation = if (isDragged) 4.dp else 0.dp,
                modifier =
                    Modifier
                        .testTag("reorderable-row-$index")
                        .graphicsLayer {
                            translationY = if (isDragged) dragOffsetY else 0f
                        }.onGloballyPositioned { coordinates ->
                            if (rowHeightPx == 0) rowHeightPx = coordinates.size.height
                        },
            ) {
                itemContent(
                    item,
                    Modifier.pointerInput(items, index) {
                        detectDragGestures(
                            onDragStart = {
                                draggedIndex = index
                                dragOffsetY = 0f
                            },
                            onDragCancel = {
                                draggedIndex = null
                                dragOffsetY = 0f
                            },
                            onDragEnd = {
                                val startIndex = draggedIndex
                                if (startIndex != null && rowHeightPx > 0) {
                                    val rowsMoved = (dragOffsetY / rowHeightPx).toInt()
                                    val targetIndex =
                                        (startIndex + rowsMoved)
                                            .coerceIn(0, items.lastIndex)
                                    if (targetIndex != startIndex) {
                                        onMove(startIndex, targetIndex)
                                    }
                                }
                                draggedIndex = null
                                dragOffsetY = 0f
                            },
                        ) { change, dragAmount ->
                            change.consume()
                            dragOffsetY += dragAmount.y
                        }
                    },
                )
            }
        }
    }
}
