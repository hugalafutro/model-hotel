package com.hugalafutro.bellhop.ui.events

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Year
import java.time.ZoneId
import java.util.Locale
import java.util.TimeZone

/**
 * Pins the event-row stamp: the clock face Settings chose, extended to the
 * hundredth of a second with a fixed two-digit fraction and the locale's
 * decimal mark, read at the device zone, with the year only when it is not the
 * current one. 18:25:46Z on a September day is 20:25:46 in Prague.
 */
class EventStampFormatTest {
    private val prague = ZoneId.of("Europe/Prague")

    // A stamp in a year that is never the current one, so the year always shows.
    private val pastYear = "2025-09-08T18:25:46"

    private fun inPrague(
        locale: Locale,
        block: () -> Unit,
    ) {
        val zone = TimeZone.getDefault()
        val previous = Locale.getDefault()
        TimeZone.setDefault(TimeZone.getTimeZone(prague))
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
            assertEquals("Sep 08, 2025 · 20:25:46.15", formatEventStamp("$pastYear.159Z", "HH:mm"))
        }

    @Test
    fun padsTheHourAndKeepsTheMeridiemOnA12HourClock() =
        inPrague(Locale.US) {
            assertEquals("Sep 08, 2025 · 08:25:46.15 PM", formatEventStamp("$pastYear.15Z", "h:mm a"))
        }

    // ".1" and ".10" are the same value; the row always shows two digits so the
    // fraction never changes width between rows, and a whole second reads ".00".
    @Test
    fun alwaysShowsTwoFractionDigits() =
        inPrague(Locale.US) {
            assertEquals("Sep 08, 2025 · 20:25:46.10", formatEventStamp("$pastYear.1Z", "HH:mm"))
            assertEquals("Sep 08, 2025 · 20:25:46.00", formatEventStamp("${pastYear}Z", "HH:mm"))
        }

    // A row from this year drops the year to keep room for the event title;
    // the clipboard (fullDate) still names it.
    @Test
    fun namesTheYearOnlyOutsideTheCurrentOneUnlessAskedTo() =
        inPrague(Locale.US) {
            val year = Year.now(prague).value
            val thisYear = "$year-09-08T18:25:46.15Z"
            assertEquals("Sep 08 · 20:25:46.15", formatEventStamp(thisYear, "HH:mm"))
            assertEquals("Sep 08, $year · 20:25:46.15", formatEventStamp(thisYear, "HH:mm", fullDate = true))
        }

    @Test
    fun usesTheLocaleDecimalMark() =
        inPrague(Locale.GERMANY) {
            assertTrue(formatEventStamp("$pastYear.15Z", "HH:mm").endsWith("20:25:46,15"))
        }

    // The stamp prints ASCII digits, and CLDR's decimal mark for Arabic in
    // Latin digits is ".", not Arabic's own U+066B; the dashboard agrees.
    @Test
    fun fallsBackToADotForAnArabicDecimalMark() =
        inPrague(Locale.forLanguageTag("ar-EG")) {
            assertTrue(formatEventStamp("$pastYear.15Z", "HH:mm").endsWith("20:25:46.15"))
        }

    @Test
    fun fallsBackToRawOnGarbage() {
        assertEquals("not-a-time", formatEventStamp("not-a-time", "HH:mm"))
    }
}
