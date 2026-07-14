package com.hugalafutro.bellhop.ui.member

import androidx.compose.ui.graphics.Color
import com.hugalafutro.bellhop.ui.common.relativeAgo
import org.junit.Assert.assertEquals
import org.junit.Test

class SyncAgeTest {
    private val minute = 60_000L
    private val hour = 3_600_000L
    private val day = 86_400_000L

    @Test
    fun relativeAgoReadsHumanAtEachScale() {
        assertEquals("just now", relativeAgo(30_000L))
        assertEquals("5 min ago", relativeAgo(5 * minute))
        assertEquals("3 hr ago", relativeAgo(3 * hour))
        assertEquals("1 day ago", relativeAgo(day))
        assertEquals("3 days ago", relativeAgo(3 * day))
    }

    @Test
    fun freshSyncKeepsTheBaseColour() {
        val base = Color(0xFF102030)
        assertEquals(base, syncAgeColor(0L, base))
    }

    @Test
    fun weekOldOrOlderTurnsRed() {
        val base = Color(0xFF102030)
        val red = Color(0xFFC62828)
        assertEquals(red, syncAgeColor(7 * day, base))
        assertEquals(red, syncAgeColor(30 * day, base))
    }

    @Test
    fun halfwayHitsTheAmberStop() {
        // At 3.5 days (half of the 7-day window) the grade reaches the amber stop.
        val base = Color(0xFF102030)
        assertEquals(Color(0xFFFBC02D), syncAgeColor((3.5 * day).toLong(), base))
    }
}
