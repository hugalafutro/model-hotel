package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.height
import com.hugalafutro.bellhop.data.ProviderQuota
import com.hugalafutro.bellhop.data.QuotaBarMode
import com.hugalafutro.bellhop.data.QuotaData
import com.hugalafutro.bellhop.data.QuotaType
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.GraphicsMode

/**
 * The badge strip's footprint, measured rather than argued about.
 *
 * These need [GraphicsMode.Mode.NATIVE]: under Robolectric's default legacy
 * graphics, text is measured by a stub font that reports roughly twice the
 * real height, which makes every dp assertion here meaningless -- a chip that
 * really draws at 17dp measures 35dp, and a wrong theory about where the
 * strip's dead space comes from survives a green test run. NATIVE measures
 * with the app's bundled fonts, so the numbers below are the numbers on the
 * device. That is also why this lives in its own class: the mode applies to
 * every test in a class, and the rest of [QuotaBadgesTest] has no reason to
 * pay for real text layout.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class QuotaBadgeGeometryTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private val names = listOf("openrouter-personal", "nanogpt", "zai-coding", "neuralwatt", "kimi", "minimax")

    private fun renderStrip() {
        val quota =
            names.mapIndexed { i, name ->
                ProviderQuota(
                    providerName = name,
                    type = QuotaType.OPENROUTER,
                    data = QuotaData.OpenRouter(creditsRemaining = 12.5 + i),
                    fetchedAt = "2026-07-26T00:00:00Z",
                    available = true,
                )
            }
        composeTestRule.setContent {
            BellhopTheme {
                // A narrow-phone content width, so the strip actually wraps and
                // there is more than one row to measure the gap between.
                Column(modifier = Modifier.width(360.dp)) {
                    QuotaBadgeRow(quota = quota, mode = QuotaBarMode.REMAINING, onBadgeClick = {})
                }
            }
        }
    }

    private fun boundsOf(name: String) = composeTestRule.onNodeWithTag("quota-badge-$name").getUnclippedBoundsInRoot()

    @Test
    fun chipsAreOnlyAsTallAsTheirText() {
        renderStrip()

        val height = boundsOf(names.first()).height
        // A chip is one line of labelMedium plus 1dp of padding and a 1dp
        // border on each edge. The bound is loose enough to survive a type
        // tweak and tight enough to catch a minimum-size rule reappearing.
        assertTrue("chip measured $height", height < 24.dp)
    }

    @Test
    fun wrappedRowsSitOneDpApart() {
        renderStrip()

        val first = boundsOf(names.first())
        // The strip wraps at 360dp: this badge is on the second row, and the
        // gap between the rows is the whole point of the arrangement.
        val second = boundsOf("zai-coding")
        assertTrue("expected a wrapped second row, both tops were ${first.top}", second.top > first.top)
        assertEquals(first.height + 1.dp, second.top - first.top)
    }
}
