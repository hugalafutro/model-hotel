package com.hugalafutro.bellhop.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
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
    fun theSenderIsReadFromTheTestBody() {
        val body = "Test notification from Front Desk: if you can read this, alerting is wired up correctly."
        assertEquals("Front Desk", BellhopPush.testPushSender(body.toByteArray()))
        // The gateway names itself, so a second Model Hotel testing the same phone
        // is distinguishable from the Front Desk it is paired to.
        assertEquals(
            "Model Hotel",
            BellhopPush.testPushSender("Test notification from Model Hotel: hello".toByteArray()),
        )
    }

    @Test
    fun aSenderThatIsMissingOrUnusableYieldsNull() {
        // Not a test push at all.
        assertNull(BellhopPush.testPushSender("mh-2 is down".toByteArray()))
        // A test body with nothing after the marker has no ":" to close the name.
        assertNull(BellhopPush.testPushSender("Test notification from Front Desk".toByteArray()))
        // Blanks are not a name, so the caller falls back rather than rendering an
        // empty-looking title.
        assertNull(BellhopPush.testPushSender("Test notification from   : hi".toByteArray()))
        // Nor is a name with nothing allowed left in it once it is stripped.
        assertNull(BellhopPush.testPushSender("Test notification from \u202E\u0007: hi".toByteArray()))
    }

    @Test
    fun theSenderIsStrippedToWhatATitleMayShow() {
        // The payload is unauthenticated text from whoever can post to the push
        // topic, so a newline, a control character or a right-to-left override
        // (which could dress a hostile push up as something else) is dropped
        // rather than rendered into the title.
        assertEquals(
            "Front Desk",
            BellhopPush.testPushSender("Test notification from Front Desk\u202E \u0007: hi".toByteArray()),
        )
        // Dots, underscores and hyphens are how gateways are actually named, so
        // they survive.
        assertEquals(
            "mh-1.prod_eu",
            BellhopPush.testPushSender("Test notification from mh-1.prod_eu: hi".toByteArray()),
        )
    }

    @Test
    fun anOverlongSenderIsCapped() {
        // The sender is operator-supplied text off the network: uncapped, it would
        // push the rest of the title out of view.
        val long = "x".repeat(80)
        val sender = BellhopPush.testPushSender("Test notification from $long: hi".toByteArray())
        assertEquals("x".repeat(40), sender)
    }

    @Test
    fun emptyOrUndecodableBytesAreNotTests() {
        assertFalse(BellhopPush.isTestPush(ByteArray(0)))
        // Bytes that are not valid UTF-8 decode to replacement characters rather
        // than throwing, so a mangled payload degrades to a plain wake.
        assertFalse(BellhopPush.isTestPush(byteArrayOf(-1, -2, -3, 0x54, 0x65)))
    }
}
