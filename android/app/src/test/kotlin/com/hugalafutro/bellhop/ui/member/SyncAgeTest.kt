package com.hugalafutro.bellhop.ui.member

import androidx.compose.ui.graphics.Color
import com.hugalafutro.bellhop.ui.common.Ago
import com.hugalafutro.bellhop.ui.common.agoBucket
import org.junit.Assert.assertEquals
import org.junit.Test

class SyncAgeTest {
    private val minute = 60_000L
    private val hour = 3_600_000L
    private val day = 86_400_000L

    @Test
    fun agoBucketPicksTheRightUnitAtEachScale() {
        // Pure bucketing (no resources): the localized wording is a thin wrapper in
        // relativeAgo, and locale parity is enforced by lint.
        assertEquals(Ago.JustNow, agoBucket(30_000L))
        assertEquals(Ago.Minutes(5), agoBucket(5 * minute))
        assertEquals(Ago.Hours(3), agoBucket(3 * hour))
        assertEquals(Ago.Days(1), agoBucket(day))
        assertEquals(Ago.Days(3), agoBucket(3 * day))
    }

    @Test
    fun freshTickUnderAnHourKeepsTheBaseColour() {
        // While the heartbeat is still ticking (under an hour) the suffix stays muted.
        val base = Color(0xFF102030)
        assertEquals(base, syncAgeColor(0L, base))
        assertEquals(base, syncAgeColor(59 * minute, base))
    }

    @Test
    fun pastAnHourTurnsYellow() {
        val base = Color(0xFF102030)
        val yellow = Color(0xFFFBC02D)
        assertEquals(yellow, syncAgeColor(hour, base))
        assertEquals(yellow, syncAgeColor(25 * hour, base)) // still under 2 days
    }

    @Test
    fun secondDayTurnsOrange() {
        val base = Color(0xFF102030)
        val orange = Color(0xFFF57C00)
        assertEquals(orange, syncAgeColor(2 * day, base))
        assertEquals(orange, syncAgeColor((2.5 * day).toLong(), base))
    }

    @Test
    fun thirdDayOrOlderTurnsRed() {
        val base = Color(0xFF102030)
        val red = Color(0xFFC62828)
        assertEquals(red, syncAgeColor(3 * day, base))
        assertEquals(red, syncAgeColor(30 * day, base))
    }
}
