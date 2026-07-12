package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AppLockTest {
    private val thirtyMin = LockTimeout.THIRTY_MINUTES.millis

    @Test
    fun disabledNeverLocksEvenAfterAges() {
        val config = LockConfig(enabled = false, timeoutMs = thirtyMin)
        // A year in the background still doesn't lock when the policy is off.
        assertFalse(shouldLock(config, lastForegroundExit = 0L, now = 365L * 24 * 3600_000))
    }

    @Test
    fun enabledLocksOncePastTheWindow() {
        val config = LockConfig(enabled = true, timeoutMs = thirtyMin)
        val exit = 1_000_000L
        assertTrue(shouldLock(config, lastForegroundExit = exit, now = exit + thirtyMin + 1))
    }

    @Test
    fun enabledStaysOpenWithinTheWindow() {
        val config = LockConfig(enabled = true, timeoutMs = thirtyMin)
        val exit = 1_000_000L
        // Exactly at the boundary is not yet past it, so no lock.
        assertFalse(shouldLock(config, lastForegroundExit = exit, now = exit + thirtyMin))
        assertFalse(shouldLock(config, lastForegroundExit = exit, now = exit + 1))
    }

    @Test
    fun immediatelyLocksOnAnyElapsedTime() {
        val config = LockConfig(enabled = true, timeoutMs = LockTimeout.IMMEDIATELY.millis)
        val exit = 1_000_000L
        assertTrue(shouldLock(config, lastForegroundExit = exit, now = exit + 1))
        // No elapsed time (same instant) is still open.
        assertFalse(shouldLock(config, lastForegroundExit = exit, now = exit))
    }

    @Test
    fun fromMillisRoundTripsKnownAndFallsBackForUnknown() {
        assertEquals(LockTimeout.FIVE_MINUTES, LockTimeout.fromMillis(LockTimeout.FIVE_MINUTES.millis))
        // A value no option matches (e.g. a future build's window) falls back.
        assertEquals(LockTimeout.DEFAULT, LockTimeout.fromMillis(123_456L))
    }
}
