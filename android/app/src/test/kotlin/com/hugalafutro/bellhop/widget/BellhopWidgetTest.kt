package com.hugalafutro.bellhop.widget

import android.app.Application
import androidx.glance.GlanceId
import androidx.glance.action.actionParametersOf
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
 * A widget quota badge only has room for a short provider code, so its tap
 * names the provider in a toast. That callback is the one piece of the tap
 * plain enough to unit test; the render itself is covered by an on-device
 * smoke test, not here.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class BellhopWidgetTest {
    private val app: Application = RuntimeEnvironment.getApplication()

    // The callback never dereferences the id, so a marker instance is enough to
    // call it outside a Glance session.
    private object FakeGlanceId : GlanceId

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
