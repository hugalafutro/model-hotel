package com.hugalafutro.bellhop.widget

import android.app.Application
import androidx.glance.GlanceId
import androidx.glance.action.actionParametersOf
import androidx.glance.layout.Alignment
import com.hugalafutro.bellhop.MainActivity
import com.hugalafutro.bellhop.data.QuotaBadgeAlign
import com.hugalafutro.bellhop.data.WidgetQuotaBadge
import com.hugalafutro.bellhop.data.quotaBadgeRows
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
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

    @Test
    fun alignmentPreferenceMapsToTheMatchingGlanceAlignment() {
        // The render itself is an on-device smoke test (see this class' header:
        // Glance composables cannot be driven by createComposeRule), so the one
        // thing worth pinning here is the mapping, where a transposition would
        // silently send RIGHT to the left edge.
        assertEquals(Alignment.Start, quotaRowAlignment(QuotaBadgeAlign.LEFT))
        assertEquals(Alignment.CenterHorizontally, quotaRowAlignment(QuotaBadgeAlign.CENTER))
        assertEquals(Alignment.End, quotaRowAlignment(QuotaBadgeAlign.RIGHT))
    }

    @Test
    @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun overflowMarkerWidensWithTheFontScaleAndTheLastRowMakesRoomForIt() {
        // NATIVE graphics so the paint really measures; the legacy shadow
        // ignores text size. "+12" is 9sp text, so doubling the font setting
        // must widen it well past half again, and the packer must hand the
        // last row's second badge to the overflow once it does.
        val normal = overflowMarkerDp(app, 12)
        RuntimeEnvironment.setFontScale(2f)
        val large = overflowMarkerDp(RuntimeEnvironment.getApplication(), 12)
        assertTrue("$normal -> $large", large * 2 > normal * 3)

        // Two 81dp estimate-width badges per row; a budget midway between the
        // two markers keeps both badges beside the normal marker only.
        val badges = (1..12).map { WidgetQuotaBadge(providerName = "P$it", type = "NANOGPT", label = "NANO 1.9M/3M") }
        val budget = 162 + (normal + large) / 2
        assertEquals(2, quotaBadgeRows(badges, budget, markerDp = normal).last().size)
        assertEquals(1, quotaBadgeRows(badges, budget, markerDp = large).last().size)
    }

    @Test
    @GraphicsMode(GraphicsMode.Mode.NATIVE)
    fun theHeightBudgetFollowsTheFontScale() {
        // The smallest widget that holds four member rows at 100% font cannot
        // hold them at 200%: WidgetHeights.of must measure lines in sp, or a
        // large font clips the footer again. (Member rows are the decision that
        // only ever tightens with the font; collapsing them frees room for the
        // badges and the event block, so those can go either way.)
        val normal = WidgetHeights.of(app)
        val height = (1..4000).first { normal.memberForm(it, 4) == MemberForm.ROWS }
        RuntimeEnvironment.setFontScale(2f)
        val large = WidgetHeights.of(RuntimeEnvironment.getApplication())
        assertEquals(MemberForm.ONE_LINE, large.memberForm(height, 4))
    }
}
