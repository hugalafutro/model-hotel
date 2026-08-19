package com.hugalafutro.bellhop.ui.common

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalContext
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.TimeFormat
import com.hugalafutro.bellhop.data.timePattern
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * LocalTimePattern carries the hour-and-minute pattern every screen draws its
 * clock times with, resolved once from the Settings preference. A composition
 * local rather than a parameter because the setting is read in one place and
 * used in a dozen leaf rows, none of which have anything else to say about it.
 *
 * The default is 24-hour so a composable rendered outside
 * [ProvideTimePattern] -- a preview, a test that composes a single row -- still
 * shows a time.
 */
val LocalTimePattern = staticCompositionLocalOf { "HH:mm" }

/**
 * timeAndDate stamps a moment with both halves of a wall-clock reading: the time
 * on the face Settings chose ([timePattern]), then the date in the short form
 * this device's region writes it in (`ofLocalizedDate(SHORT)`, so 22/07/26,
 * 7/22/26 or 22.07.26 as the locale has it -- Bellhop never picks a field order).
 *
 * The two are joined by a string resource rather than a literal so a locale that
 * leads with the date can say so. Not a composable and not tied to
 * [LocalTimePattern]: the home-screen widget is a Glance composition and can't
 * read Compose UI locals, and it is the caller that needs both halves.
 */
fun timeAndDate(
    context: Context,
    format: TimeFormat,
    millis: Long,
): String {
    val zone = ZoneId.systemDefault()
    val locale = Locale.getDefault()
    val moment = Instant.ofEpochMilli(millis)
    val time = DateTimeFormatter.ofPattern(timePattern(format, context), locale).withZone(zone).format(moment)
    val date =
        DateTimeFormatter
            .ofLocalizedDate(FormatStyle.SHORT)
            .withLocale(locale)
            .withZone(zone)
            .format(moment)
    return context.getString(R.string.widget_event_stamp, time, date)
}

/**
 * ProvideTimePattern resolves [format] against the device (which
 * [TimeFormat.SYSTEM] defers to) and publishes the result to [content].
 */
@Composable
fun ProvideTimePattern(
    format: TimeFormat,
    content: @Composable () -> Unit,
) {
    val context = LocalContext.current
    val pattern = remember(format, context) { timePattern(format, context) }
    CompositionLocalProvider(LocalTimePattern provides pattern, content = content)
}
