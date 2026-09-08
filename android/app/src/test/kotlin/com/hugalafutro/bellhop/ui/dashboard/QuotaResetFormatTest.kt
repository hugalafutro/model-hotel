package com.hugalafutro.bellhop.ui.dashboard

import com.hugalafutro.bellhop.ui.events.formatEventTime
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.ZoneId
import java.util.Locale
import java.util.TimeZone

/**
 * Pins the reset-time rendering the OpenCode Go detail rows use. The gateway
 * sends UTC, so the row has to read at the device's zone and on the clock face
 * Settings chose, and a value that isn't RFC3339-shaped has to survive rather
 * than blank the sheet.
 */
class QuotaResetFormatTest {
    // A fixed moment in a fixed zone and locale so the expected reading is not
    // the machine's: 2026-09-08T18:25:46Z is 20:25 in Prague, two hours off the
    // UTC clock the row used to print.
    private val resetsAt = "2026-09-08T18:25:46.155Z"

    private fun inPragueEnUs(block: () -> Unit) {
        val zone = TimeZone.getDefault()
        val locale = Locale.getDefault()
        TimeZone.setDefault(TimeZone.getTimeZone(ZoneId.of("Europe/Prague")))
        Locale.setDefault(Locale.US)
        try {
            block()
        } finally {
            TimeZone.setDefault(zone)
            Locale.setDefault(locale)
        }
    }

    @Test
    fun readsTheResetAtTheDeviceZoneOnA24HourClock() =
        inPragueEnUs {
            assertEquals("Sep 8, 2026 · 20:25", formatEventTime(resetsAt, "HH:mm"))
        }

    @Test
    fun honoursThe12HourSetting() =
        inPragueEnUs {
            assertEquals("Sep 8, 2026 · 8:25 PM", formatEventTime(resetsAt, "h:mm a"))
        }

    @Test
    fun leavesAForeignFormatAlone() =
        inPragueEnUs {
            assertEquals("soon", formatEventTime("soon", "HH:mm"))
            assertEquals("2026-09-08", formatEventTime("2026-09-08", "HH:mm"))
        }
}
