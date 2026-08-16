package com.hugalafutro.bellhop.push

import android.app.Activity
import android.content.Context
import org.unifiedpush.android.connector.UnifiedPush

/**
 * BellhopPush wraps the UnifiedPush registration dance for Layer 3 (plan section
 * 5.2) so MainActivity doesn't reach into the connector directly. Registration
 * needs a *distributor* app installed (ntfy is the recommended, Google-free one);
 * with none present [hasDistributor] is false and Settings tells the user to
 * install one instead of silently failing.
 */
object BellhopPush {
    /**
     * TEST_BODY_PREFIX opens the body of Front Desk's "Send test" notification. It
     * mirrors `TestBodyPrefix` in the gateway's Go `internal/alert` package, and the
     * two must stay identical: it is the only thing that tells a test push apart
     * from a real alert wake, which is why a test gets its own notification while
     * everything else stays a silent wake.
     */
    const val TEST_BODY_PREFIX = "Test notification from "

    /**
     * isTestPush reports whether a push payload is Front Desk's test notification.
     * The UnifiedPush payload ntfy hands over is the message body as UTF-8 bytes;
     * bytes that are not valid UTF-8 decode to replacement characters rather than
     * throwing, so a malformed or empty payload simply reads as "not a test".
     */
    fun isTestPush(content: ByteArray): Boolean = content.toString(Charsets.UTF_8).startsWith(TEST_BODY_PREFIX)

    /**
     * SENDER_MAX_LENGTH caps how much of the payload can reach the notification
     * title. The sender is whatever the gateway was configured to call itself, so
     * it is operator-supplied text arriving over the network: a long one would
     * push the rest of the title out of view, and the title is the only part of a
     * test notification that says who sent it.
     */
    private const val SENDER_MAX_LENGTH = 40

    /**
     * testPushSender returns the name the sending gateway gave itself, taken from
     * the test body's `Test notification from <prefix>: ...` opening, or null when
     * the payload is not a test push at all. A phone can be pointed at more than
     * one Front Desk, so the title carries this rather than a fixed name.
     *
     * The name is trimmed and capped at [SENDER_MAX_LENGTH]; a test body with no
     * ":" after the marker, or nothing but blanks in front of it, yields null, so
     * the caller falls back to nothing rather than an empty-looking title.
     */
    fun testPushSender(content: ByteArray): String? {
        val body = content.toString(Charsets.UTF_8)
        if (!body.startsWith(TEST_BODY_PREFIX)) return null
        val rest = body.substring(TEST_BODY_PREFIX.length)
        val end = rest.indexOf(':')
        if (end < 0) return null
        val sender = rest.substring(0, end).trim()
        if (sender.isEmpty()) return null
        return sender.take(SENDER_MAX_LENGTH)
    }

    /** hasDistributor reports whether any UnifiedPush distributor is installed. */
    fun hasDistributor(context: Context): Boolean = UnifiedPush.getDistributors(context).isNotEmpty()

    /**
     * register picks the saved distributor (or the sole/default one) and registers
     * Bellhop with it under [instance] (a per-registration id minted by MonitorStore
     * so endpoint callbacks can be attributed to the registration that produced
     * them); the resulting endpoint arrives asynchronously in
     * [BellhopPushService.onNewEndpoint]. Needs an Activity because choosing a
     * distributor may surface a picker. A no-op when no distributor can be chosen.
     */
    fun register(
        activity: Activity,
        instance: String,
    ) {
        UnifiedPush.tryUseCurrentOrDefaultDistributor(activity) { chosen ->
            if (chosen) UnifiedPush.register(activity.applicationContext, instance)
        }
    }

    /**
     * unregister tears down the registration for [instance] so the distributor stops
     * waking us. A null instance means nothing was ever registered (push never
     * enabled), so there is nothing to tear down.
     */
    fun unregister(
        context: Context,
        instance: String?,
    ) {
        if (instance != null) UnifiedPush.unregister(context.applicationContext, instance)
    }
}
