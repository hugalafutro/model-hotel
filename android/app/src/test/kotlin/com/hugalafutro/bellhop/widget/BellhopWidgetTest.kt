package com.hugalafutro.bellhop.widget

import android.app.Application
import androidx.glance.GlanceId
import androidx.glance.action.actionParametersOf
import com.hugalafutro.bellhop.MainActivity
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.shadows.ShadowToast

/**
 * A widget quota badge taps one of two ways: into the app for a provider with a
 * detail view, or into a toast naming the provider for one whose badge is
 * already the whole reading. Both destinations are plain enough to unit test --
 * an intent builder and an action callback; which of the two a given badge gets
 * is [com.hugalafutro.bellhop.data.quotaHasDetail]'s call, tested with it. The
 * render itself is covered by an on-device smoke test, not here.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class BellhopWidgetTest {
    private val app: Application = RuntimeEnvironment.getApplication()

    // The callback never dereferences the id, so a marker instance is enough to
    // call it outside a Glance session.
    private object FakeGlanceId : GlanceId

    @Test
    fun detailBearingBadgeIntentTargetsMainActivityWithDeepLinkExtras() {
        val intent = quotaBadgeIntent(app, "or-1")

        assertEquals(ACTION_OPEN_QUOTA, intent.action)
        assertEquals("or-1", intent.getStringExtra(EXTRA_BADGE_PROVIDER_NAME))
        assertEquals(MainActivity::class.java.name, intent.component?.className)
    }

    @Test
    fun badgeTapToastsTheFullProviderName() =
        runBlocking {
            QuotaBadgeNameAction().onAction(
                app,
                FakeGlanceId,
                actionParametersOf(BADGE_PROVIDER_NAME to "openrouter-personal"),
            )

            assertEquals("openrouter-personal", ShadowToast.getTextOfLatestToast())
        }

    @Test
    fun aTapWithoutAProviderNameShowsNothing() =
        runBlocking {
            // Can't happen from the widget (every badge carries its name), but a
            // missing parameter must stay a no-op rather than toast a blank.
            QuotaBadgeNameAction().onAction(app, FakeGlanceId, actionParametersOf())

            assertNull(ShadowToast.getTextOfLatestToast())
        }
}
