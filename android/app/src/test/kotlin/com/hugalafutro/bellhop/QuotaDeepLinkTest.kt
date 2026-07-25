package com.hugalafutro.bellhop

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The widget quota deep-link must ride the app lock: [consumePendingQuotaTarget]
 * only surfaces the target once the app is unlocked, so a locked device holds it
 * pending instead of flashing a detail sheet over the lock screen.
 */
class QuotaDeepLinkTest {
    @Test
    fun heldWhileLocked() {
        // Locked: nothing opens, the target stays pending.
        assertEquals(null to "openrouter", consumePendingQuotaTarget("openrouter", unlocked = false))
    }

    @Test
    fun openedAfterUnlock() {
        // Unlocked: the target is consumed into the sheet and cleared.
        assertEquals("openrouter" to null, consumePendingQuotaTarget("openrouter", unlocked = true))
    }

    @Test
    fun nothingPendingIsNoOp() {
        assertEquals(null to null, consumePendingQuotaTarget(null, unlocked = true))
    }
}
