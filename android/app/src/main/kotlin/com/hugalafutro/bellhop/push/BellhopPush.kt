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
     * Bellhop with it; the resulting endpoint arrives asynchronously in
     * [BellhopPushService.onNewEndpoint]. Needs an Activity because choosing a
     * distributor may surface a picker. A no-op when no distributor can be chosen.
     */
    fun register(activity: Activity) {
        UnifiedPush.tryUseCurrentOrDefaultDistributor(activity) { chosen ->
            if (chosen) UnifiedPush.register(activity.applicationContext)
        }
    }

    /** unregister tears down the registration; the distributor stops waking us. */
    fun unregister(context: Context) {
        UnifiedPush.unregister(context.applicationContext)
    }
}
