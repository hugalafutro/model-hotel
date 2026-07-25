package com.hugalafutro.bellhop.ui.common

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalContext
import com.hugalafutro.bellhop.data.TimeFormat
import com.hugalafutro.bellhop.data.timePattern

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
