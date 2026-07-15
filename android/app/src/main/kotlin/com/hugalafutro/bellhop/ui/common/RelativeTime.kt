package com.hugalafutro.bellhop.ui.common

import android.content.Context
import com.hugalafutro.bellhop.R

// relativeAgo turns an elapsed span (millis) into a terse, localized human string:
// "just now" under a minute, then minutes, hours, and days. It resolves strings
// through the given context, so the wording follows the in-app language (and the
// day count pluralizes correctly per locale). Shared by the member-detail SYNCED /
// VERIFIED / ADDED rows and the dashboard cards' recent-event pills.
fun relativeAgo(
    context: Context,
    elapsedMs: Long,
): String {
    val minutes = (elapsedMs / 60_000L).toInt()
    val hours = (elapsedMs / 3_600_000L).toInt()
    val days = (elapsedMs / 86_400_000L).toInt()
    val res = context.resources
    return when {
        minutes < 1 -> res.getString(R.string.time_just_now)
        minutes < 60 -> res.getQuantityString(R.plurals.time_min_ago, minutes, minutes)
        hours < 24 -> res.getQuantityString(R.plurals.time_hr_ago, hours, hours)
        else -> res.getQuantityString(R.plurals.time_day_ago, days, days)
    }
}
