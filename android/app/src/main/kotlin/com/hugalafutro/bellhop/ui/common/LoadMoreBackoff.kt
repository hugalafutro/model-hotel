package com.hugalafutro.bellhop.ui.common

/**
 * loadMoreBackoffMillis is the delay before retrying a failed infinite-scroll
 * page. The list sentinel re-arms the instant `loadingMore` clears, so a
 * persistent fetch error would otherwise re-fire loadMore in a tight loop and
 * hammer Front Desk. Consecutive failures back off exponentially from 1s,
 * capped at 30s; the caller resets its failure count on the first success.
 */
internal fun loadMoreBackoffMillis(failures: Int): Long {
    if (failures <= 0) return 0L
    val steps = (failures - 1).coerceAtMost(5)
    return (1_000L shl steps).coerceAtMost(30_000L)
}
