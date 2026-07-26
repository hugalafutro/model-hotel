package com.hugalafutro.bellhop.ui.common

import android.app.Application
import android.provider.Settings
import com.hugalafutro.bellhop.data.TimeFormat
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.time.Instant
import java.time.ZoneId
import java.util.TimeZone

/**
 * timeAndDate is what the widget's newest-event line and anything else needing a
 * whole timestamp stamps with: the clock face Settings chose, plus the date in
 * the form the device's region writes it. These pin both halves, since the point
 * of the function is that neither is dropped and neither field order is ours.
 */
@RunWith(RobolectricTestRunner::class)
class TimeFormattingTest {
    private val app: Application = RuntimeEnvironment.getApplication()

    // A fixed moment, formatted in a fixed zone so the expected clock time is
    // not the machine's: 2026-07-22T19:45:00Z is 21:45 in Prague.
    private val moment = Instant.parse("2026-07-22T19:45:00Z")

    private fun inPrague(block: () -> Unit) {
        val previous = TimeZone.getDefault()
        TimeZone.setDefault(TimeZone.getTimeZone(ZoneId.of("Europe/Prague")))
        try {
            block()
        } finally {
            TimeZone.setDefault(previous)
        }
    }

    @Test
    @Config(qualifiers = "en-rGB")
    fun carriesBothTheChosenClockAndTheRegionsDate() =
        inPrague {
            val stamp = timeAndDate(app, TimeFormat.H24, moment.toEpochMilli())

            // en-GB writes the date day-first, and 24-hour was asked for outright.
            assertEquals("21:45, 22/07/2026", stamp)
        }

    @Test
    @Config(qualifiers = "en-rUS")
    fun theDateFollowsTheRegionRatherThanAFixedFieldOrder() =
        inPrague {
            val stamp = timeAndDate(app, TimeFormat.H24, moment.toEpochMilli())

            // Same moment, month-first: Bellhop never picks the field order, so a
            // US device reads 7/22/26 where a British one read 22/07/2026.
            assertEquals("21:45, 7/22/26", stamp)
        }

    @Test
    @Config(qualifiers = "en-rGB")
    fun theClockHalfHonoursTheTwelveHourOverride() =
        inPrague {
            val stamp = timeAndDate(app, TimeFormat.H12, moment.toEpochMilli())

            assertTrue("expected a 12-hour clock in <$stamp>", stamp.startsWith("9:45"))
            assertTrue("expected the date alongside it in <$stamp>", stamp.endsWith("22/07/2026"))
        }

    @Test
    @Config(qualifiers = "en-rGB")
    fun systemClockDefersToTheDevice() =
        inPrague {
            Settings.System.putString(app.contentResolver, Settings.System.TIME_12_24, "24")

            assertEquals("21:45, 22/07/2026", timeAndDate(app, TimeFormat.SYSTEM, moment.toEpochMilli()))
        }
}
