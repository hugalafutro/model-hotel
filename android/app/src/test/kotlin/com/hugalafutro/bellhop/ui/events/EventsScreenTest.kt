package com.hugalafutro.bellhop.ui.events

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTouchInput
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.ui.common.EventRange
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.shadows.ShadowToast

/**
 * Event log: filter chips, event cards, load-more tail, banners.
 * Asserts on test tags, not display text, so English copy never breaks tests.
 */
@RunWith(RobolectricTestRunner::class)
class EventsScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private fun ev(
        id: String,
        severity: String = "info",
        memberId: String = "",
    ) = FdEvent(
        id = id,
        type = "health.down",
        severity = severity,
        source = "poller",
        message = "event $id",
        memberId = memberId,
        createdAt = "2026-07-12T10:00:00Z",
    )

    private val loaded =
        EventsUiState(
            loading = false,
            events = listOf(ev("e1", severity = "error", memberId = "m1"), ev("e2", severity = "success")),
            total = 2,
        )

    @Test
    fun rendersCardsWithSeverityPills() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded, memberNames = mapOf("m1" to "alpha"))
            }
        }
        assertEquals(2, composeTestRule.onAllNodesWithTag("event-card").fetchSemanticsNodes().size)
        composeTestRule.onNodeWithTag("event-sev-error", useUnmergedTree = true).assertIsDisplayed()
        composeTestRule.onNodeWithTag("event-sev-success", useUnmergedTree = true).assertIsDisplayed()
        composeTestRule.onNodeWithTag("events-total").assertIsDisplayed()
    }

    @Test
    fun backArrowFiresCallback() {
        var backs = 0
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = { backs++ }, ui = loaded)
            }
        }
        composeTestRule.onNodeWithTag("events-back").performClick()
        assertEquals(1, backs)
    }

    @Test
    fun clipboardTextJoinsHeaderMessageAndWho() {
        val text =
            eventClipboardText(
                ev("e1", severity = "error", memberId = "m1").copy(message = "boom"),
                memberName = "alpha",
            )
        val lines = text.split("\n")
        assertEquals(3, lines.size)
        assertTrue(lines[0].contains("[error]"))
        assertTrue(lines[0].contains("health.down"))
        assertEquals("boom", lines[1])
        assertTrue(lines[2].contains("alpha"))
    }

    @Test
    fun clipboardTextDropsBlankMemberLineTail() {
        // A system event with no source and no member must not trail a dangling
        // separator: only the header and message remain.
        val bare =
            ev("e2").copy(source = "", memberId = "", message = "just a message")
        val lines = eventClipboardText(bare, memberName = null).split("\n")
        assertEquals(2, lines.size)
        assertEquals("just a message", lines[1])
    }

    @Test
    fun copyPillTapDoesNotCrash() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded)
            }
        }
        // The severity pill is the copy affordance; tapping it must stay on the
        // event log (clipboard + toast are side effects we don't assert here).
        composeTestRule.onNodeWithTag("event-sev-error", useUnmergedTree = true).performClick()
        composeTestRule.onNodeWithTag("events-list").assertIsDisplayed()
    }

    @Test
    fun copyDisabledNeitherTapNorHoldCopies() {
        // With hold-to-copy off (the default here), the row is inert: neither a
        // tap nor a long-press copies, so nothing lands on the clipboard.
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded, holdToCopy = false)
            }
        }
        val pill = composeTestRule.onNodeWithTag("event-sev-error", useUnmergedTree = true)
        pill.performClick()
        pill.performTouchInput { longClick() }
        composeTestRule.waitForIdle()
        assertEquals(0, ShadowToast.shownToastCount())
    }

    @Test
    fun holdToCopyRequiresLongPressNotTap() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded, holdToCopy = true)
            }
        }
        val pill = composeTestRule.onNodeWithTag("event-sev-error", useUnmergedTree = true)
        // With the hold gate on, a stray tap is inert: nothing copies, no toast.
        pill.performClick()
        composeTestRule.waitForIdle()
        assertEquals(0, ShadowToast.shownToastCount())
        // The long-press is what copies, confirmed by the toast.
        pill.performTouchInput { longClick() }
        composeTestRule.waitForIdle()
        assertEquals(1, ShadowToast.shownToastCount())
    }

    @Test
    fun severityChipFiresCallback() {
        var picked = ""
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded, onSeverity = { picked = it })
            }
        }
        composeTestRule.onNodeWithTag("events-sev-warning").performClick()
        assertEquals("warning", picked)
    }

    @Test
    fun rangeChipFiresCallback() {
        var picked = EventRange.ALL
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded, onRange = { picked = it })
            }
        }
        composeTestRule.onNodeWithTag("events-range-h24").performClick()
        assertEquals(EventRange.H24, picked)
    }

    @Test
    fun scrollingToTheEndAutoLoadsMore() {
        // Infinite scroll: composing the sentinel (which only happens at the end of
        // the list) fires onLoadMore, no button tap.
        var loads = 0
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded.copy(total = 5), onLoadMore = { loads++ })
            }
        }
        composeTestRule
            .onNodeWithTag("events-list")
            .performScrollToNode(hasTestTag("events-load-more-sentinel"))
        assertTrue(loads >= 1)
    }

    @Test
    fun scrollToTopButtonAppearsWhenScrolledAndReturnsToTop() {
        val many = loaded.copy(events = List(40) { ev("e$it") }, total = 40)
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = many)
            }
        }
        // At the top the control is absent.
        composeTestRule.onNodeWithTag("scroll-to-top").assertDoesNotExist()
        // Scrolling down reveals it.
        composeTestRule.onNodeWithTag("events-list").performScrollToIndex(30)
        composeTestRule.onNodeWithTag("scroll-to-top").assertIsDisplayed()
        // Tapping it returns to the top, and the control hides itself again.
        composeTestRule.onNodeWithTag("scroll-to-top").performClick()
        composeTestRule.waitForIdle()
        composeTestRule.onNodeWithTag("scroll-to-top").assertDoesNotExist()
    }

    @Test
    fun noSentinelWhenAllRowsLoaded() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded)
            }
        }
        assertTrue(
            composeTestRule.onAllNodesWithTag("events-load-more-sentinel").fetchSemanticsNodes().isEmpty(),
        )
    }

    @Test
    fun loadingMoreShowsSpinnerNotSentinel() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded.copy(total = 5, loadingMore = true))
            }
        }
        composeTestRule
            .onNodeWithTag("events-list")
            .performScrollToNode(hasTestTag("events-loading-more"))
        composeTestRule.onNodeWithTag("events-loading-more").assertIsDisplayed()
        assertTrue(
            composeTestRule.onAllNodesWithTag("events-load-more-sentinel").fetchSemanticsNodes().isEmpty(),
        )
    }

    @Test
    fun firstLoadShowsSpinner() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = EventsUiState())
            }
        }
        composeTestRule.onNodeWithTag("events-loading").assertIsDisplayed()
    }

    @Test
    fun emptyLogShowsEmptyState() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = EventsUiState(loading = false))
            }
        }
        composeTestRule.onNodeWithTag("events-empty").assertIsDisplayed()
    }

    @Test
    fun refreshErrorShowsBannerAndKeepsStaleList() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded.copy(error = "boom"))
            }
        }
        composeTestRule.onNodeWithTag("events-error").assertIsDisplayed()
        assertEquals(2, composeTestRule.onAllNodesWithTag("event-card").fetchSemanticsNodes().size)
    }

    @Test
    fun revokedTokenShowsRevokedBanner() {
        composeTestRule.setContent {
            BellhopTheme {
                EventsScreen(onBack = {}, ui = loaded.copy(revoked = true))
            }
        }
        composeTestRule.onNodeWithTag("events-revoked").assertIsDisplayed()
    }

    @Test
    fun eventTimeFallsBackToRawOnGarbage() {
        assertEquals("not-a-time", formatEventTime("not-a-time"))
    }
}
