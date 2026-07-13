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
