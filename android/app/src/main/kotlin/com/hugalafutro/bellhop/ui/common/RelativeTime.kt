package com.hugalafutro.bellhop.ui.common

// relativeAgo turns an elapsed span (millis) into a terse human string: "just now"
// under a minute, then minutes, hours, and days. Shared by the member-detail
// SYNCED row and the dashboard cards' recent-event pills.
fun relativeAgo(elapsedMs: Long): String {
    val minutes = elapsedMs / 60_000L
    val hours = elapsedMs / 3_600_000L
    val days = elapsedMs / 86_400_000L
    return when {
        minutes < 1 -> "just now"
        minutes < 60 -> "$minutes min ago"
        hours < 24 -> "$hours hr ago"
        days == 1L -> "1 day ago"
        else -> "$days days ago"
    }
}
