package com.hugalafutro.bellhop.data

import android.app.Application
import android.provider.Settings
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

/**
 * timePattern is the one place the clock preference turns into something a
 * formatter understands, so these pin both overrides and the deference to the
 * device that [TimeFormat.SYSTEM] means.
 */
@RunWith(RobolectricTestRunner::class)
class TimeFormatTest {
    private val app: Application = RuntimeEnvironment.getApplication()

    private fun deviceIs24Hour(on: Boolean) {
        Settings.System.putString(app.contentResolver, Settings.System.TIME_12_24, if (on) "24" else "12")
    }

    @Test
    fun overridesIgnoreTheDevice() {
        deviceIs24Hour(false)
        assertEquals("HH:mm", timePattern(TimeFormat.H24, app))

        deviceIs24Hour(true)
        assertEquals("h:mm a", timePattern(TimeFormat.H12, app))
    }

    @Test
    fun systemFollowsTheDevice() {
        deviceIs24Hour(true)
        assertEquals("HH:mm", timePattern(TimeFormat.SYSTEM, app))

        deviceIs24Hour(false)
        assertEquals("h:mm a", timePattern(TimeFormat.SYSTEM, app))
    }
}
