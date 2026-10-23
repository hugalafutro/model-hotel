package com.hugalafutro.bellhop.ui.events

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId
import java.util.Locale
import java.util.TimeZone

/**
 * Pins the event-row stamp: the clock face Settings chose, extended to the
 * hundredth of a second with a fixed two-digit fraction and the locale's
 * decimal mark, read at the device zone. 2026-09-08T18:25:46Z is 20:25:46 in
 * Prague.
 */
class EventStampFormatTest {
    private fun inPrague(
        locale: Locale,
        block: () -> Unit,
    ) {
        val zone = TimeZone.getDefault()
        val previous = Locale.getDefault()
        TimeZone.setDefault(TimeZone.getTimeZone(ZoneId.of("Europe/Prague")))
        Locale.setDefault(locale)
        try {
            block()
        } finally {
            TimeZone.setDefault(zone)
            Locale.setDefault(previous)
        }
    }

    @Test
    fun addsSecondsAndHundredthsOnA24HourClock() =
        inPrague(Locale.US) {
            assertEquals("Sep 08, 2026 · 20:25:46.15", formatEventStamp("2026-09-08T18:25:46.159Z", "HH:mm"))
        }

    @Test
    fun padsTheHourAndKeepsTheMeridiemOnA12HourClock() =
        inPrague(Locale.US) {
            assertEquals("Sep 08, 2026 · 08:25:46.15 PM", formatEventStamp("2026-09-08T18:25:46.15Z", "h:mm a"))
        }

    // ".1" and ".10" are the same value; the row always shows two digits so the
    // mono column never changes width, and a whole second reads ".00".
    @Test
    fun alwaysShowsTwoFractionDigits() =
        inPrague(Locale.US) {
            assertEquals("Sep 08, 2026 · 20:25:46.10", formatEventStamp("2026-09-08T18:25:46.1Z", "HH:mm"))
            assertEquals("Sep 08, 2026 · 20:25:46.00", formatEventStamp("2026-09-08T18:25:46Z", "HH:mm"))
        }

    @Test
    fun usesTheLocaleDecimalMark() =
        inPrague(Locale.GERMANY) {
            assertTrue(formatEventStamp("2026-09-08T18:25:46.15Z", "HH:mm").endsWith("20:25:46,15"))
        }

    // Arabic's own decimal mark (U+066B) would sit between ASCII digits in a
    // mono font with no glyph for it, so the stamp falls back to ".".
    @Test
    fun fallsBackToADotForAnArabicDecimalMark() =
        inPrague(Locale.forLanguageTag("ar-EG")) {
            assertTrue(formatEventStamp("2026-09-08T18:25:46.15Z", "HH:mm").endsWith("20:25:46.15"))
        }

    @Test
    fun fallsBackToRawOnGarbage() {
        assertEquals("not-a-time", formatEventStamp("not-a-time", "HH:mm"))
    }
}
