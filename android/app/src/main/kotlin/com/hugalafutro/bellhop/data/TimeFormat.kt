package com.hugalafutro.bellhop.data

import android.content.Context
import android.text.format.DateFormat

/**
 * TimeFormat is the clock every wall-clock time in Bellhop is drawn on: the
 * device's own setting, or an override in either direction. It exists because
 * the app is the only clock some people read on that screen, and the system
 * setting isn't always the one they want Bellhop to follow.
 */
enum class TimeFormat {
    /** Follow the device's 24-hour setting. The default. */
    SYSTEM,

    /** 24-hour ("18:05"), whatever the device says. */
    H24,

    /** 12-hour with a meridiem ("6:05 PM"), whatever the device says. */
    H12,
}

/**
 * timePattern is the `java.time` pattern for the hour-and-minute part of a
 * timestamp under [format]. It's a pattern rather than a formatter so callers
 * can splice it into a longer one (the events list dates its times) and keep
 * building per call, which is what lets an in-app language switch land without
 * a process restart.
 *
 * [SYSTEM][TimeFormat.SYSTEM] reads the device setting through
 * [DateFormat.is24HourFormat], the same source the widget's own stamps use.
 */
fun timePattern(
    format: TimeFormat,
    context: Context,
): String =
    when (format) {
        TimeFormat.H24 -> "HH:mm"
        TimeFormat.H12 -> "h:mm a"
        TimeFormat.SYSTEM -> if (DateFormat.is24HourFormat(context)) "HH:mm" else "h:mm a"
    }
