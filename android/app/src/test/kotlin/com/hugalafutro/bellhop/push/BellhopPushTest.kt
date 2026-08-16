package com.hugalafutro.bellhop.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The test-push marker is the only thing separating Front Desk's "Send test" from
 * a real alert wake, so it is pinned here: the prefix must match the gateway's Go
 * constant, and anything else (including junk bytes) must stay a bare wake.
 */
class BellhopPushTest {
    @Test
    fun frontDeskTestBodyIsRecognised() {
        val body = "Test notification from Front Desk: if you can read this, alerting is wired up correctly."
        assertTrue(BellhopPush.isTestPush(body.toByteArray()))
    }

    @Test
    fun theMarkerMatchesTheGatewayConstant() {
        // internal/alert/dispatcher.go: const TestBodyPrefix = "Test notification from "
        assertEquals("Test notification from ", BellhopPush.TEST_BODY_PREFIX)
    }

    @Test
    fun realAlertBodiesAreNotTests() {
        assertFalse(BellhopPush.isTestPush("mh-2 is down".toByteArray()))
        // The marker only counts at the start: a body that merely mentions it is a
        // real alert, so a quoted marker must not silently swallow one.
        assertFalse(BellhopPush.isTestPush("Alert: Test notification from Front Desk".toByteArray()))
    }

    @Test
    fun emptyOrUndecodableBytesAreNotTests() {
        assertFalse(BellhopPush.isTestPush(ByteArray(0)))
        // Bytes that are not valid UTF-8 decode to replacement characters rather
        // than throwing, so a mangled payload degrades to a plain wake.
        assertFalse(BellhopPush.isTestPush(byteArrayOf(-1, -2, -3, 0x54, 0x65)))
    }
}
