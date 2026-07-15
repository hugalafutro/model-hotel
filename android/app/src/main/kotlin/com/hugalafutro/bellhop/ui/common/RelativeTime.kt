package com.hugalafutro.bellhop.ui.common

import android.content.Context
import com.hugalafutro.bellhop.R

// Ago is the bucketed elapsed span the UI shows as a relative age. It is a pure
// value so the bucketing thresholds stay unit-testable without Android resources;
// [relativeAgo] renders it to a localized string.
sealed interface Ago {
    data object JustNow : Ago

    data class Minutes(val n: Int) : Ago

    data class Hours(val n: Int) : Ago

    data class Days(val n: Int) : Ago
}

// agoBucket picks the coarse unit for an elapsed span (millis): "just now" under a
// minute, then whole minutes, hours, and days.
fun agoBucket(elapsedMs: Long): Ago {
    val minutes = (elapsedMs / 60_000L).toInt()
    val hours = (elapsedMs / 3_600_000L).toInt()
    val days = (elapsedMs / 86_400_000L).toInt()
    return when {
        minutes < 1 -> Ago.JustNow
        minutes < 60 -> Ago.Minutes(minutes)
        hours < 24 -> Ago.Hours(hours)
        else -> Ago.Days(days)
    }
}

// relativeAgo renders [agoBucket] as a terse, localized string through the given
// context, so the wording follows the in-app language and pluralizes per locale.
// Shared by the member-detail SYNCED / VERIFIED / ADDED rows and the dashboard
// cards' recent-event pills.
fun relativeAgo(
    context: Context,
    elapsedMs: Long,
): String {
    val res = context.resources
    return when (val ago = agoBucket(elapsedMs)) {
        Ago.JustNow -> res.getString(R.string.time_just_now)
        is Ago.Minutes -> res.getQuantityString(R.plurals.time_min_ago, ago.n, ago.n)
        is Ago.Hours -> res.getQuantityString(R.plurals.time_hr_ago, ago.n, ago.n)
        is Ago.Days -> res.getQuantityString(R.plurals.time_day_ago, ago.n, ago.n)
    }
}
