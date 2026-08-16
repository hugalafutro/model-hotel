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
