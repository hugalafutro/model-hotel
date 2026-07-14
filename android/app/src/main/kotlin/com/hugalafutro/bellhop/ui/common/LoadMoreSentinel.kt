package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp

/**
 * loadMoreSentinel is the shared infinite-scroll tail for a paged [LazyListScope]
 * feed (the events log and the member-detail event log). While a page is in
 * flight it shows a centered spinner; otherwise, when more pages exist, it drops a
 * zero-height sentinel item whose mere composition means the user bottomed out
 * (lazy items only compose near the viewport), so it fires [onLoadMore]. The
 * sentinel's key changes with [itemCount], so a fresh one arms after each page —
 * and a first page too short to fill the screen keeps arming until it does. The
 * caller's ViewModel is expected to no-op [onLoadMore] while a page is in flight
 * or nothing more remains, and to back off failed pages (see [loadMoreBackoffMillis]).
 *
 * [loadingTag]/[sentinelTag] name the spinner and sentinel nodes so each screen's
 * tests can target its own; the 1dp sentinel exists only to carry that semantics node.
 */
fun LazyListScope.loadMoreSentinel(
    canLoadMore: Boolean,
    loadingMore: Boolean,
    itemCount: Int,
    onLoadMore: () -> Unit,
    loadingTag: String,
    sentinelTag: String,
) {
    if (loadingMore) {
        item(key = loadingTag) {
            Box(
                modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator(
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(20.dp).testTag(loadingTag),
                )
            }
        }
    } else if (canLoadMore) {
        item(key = "$sentinelTag-$itemCount") {
            LaunchedEffect(Unit) { onLoadMore() }
            Spacer(modifier = Modifier.height(1.dp).testTag(sentinelTag))
        }
    }
}
