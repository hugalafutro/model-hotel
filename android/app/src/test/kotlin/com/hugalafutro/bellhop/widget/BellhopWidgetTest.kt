package com.hugalafutro.bellhop.widget

import android.app.Application
import com.hugalafutro.bellhop.MainActivity
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * quotaBadgeIntent is the only piece of the quota-badge tap that is plain
 * enough to unit test: it's a pure function over a Context, with no Glance
 * composition involved. The render itself is covered by an on-device smoke
 * test, not here.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class BellhopWidgetTest {
    private val app: Application = RuntimeEnvironment.getApplication()

    @Test
    fun quotaBadgeIntentTargetsMainActivityWithDeepLinkExtras() {
        val intent = quotaBadgeIntent(app, "or-1")

        assertEquals(ACTION_OPEN_QUOTA, intent.action)
        assertEquals("or-1", intent.getStringExtra(EXTRA_BADGE_PROVIDER_NAME))
        assertEquals(MainActivity::class.java.name, intent.component?.className)
    }
}
