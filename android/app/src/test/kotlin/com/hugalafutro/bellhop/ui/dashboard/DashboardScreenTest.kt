package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * Linked-state dashboard: confirms the link summary renders and Unlink fires.
 * Asserts on test tags, not display text, so English copy never breaks tests.
 */
@RunWith(RobolectricTestRunner::class)
class DashboardScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private val link =
        LinkState.Linked(
            fdUrl = "http://10.0.2.2:8080",
            fdName = "Home Front Desk",
            role = "operator",
            deviceId = "dev-1",
            label = "Pixel 8",
        )

    @Test
    fun showsLinkSummary() {
        composeTestRule.setContent {
            BellhopTheme { DashboardScreen(link = link, onUnlink = {}, unlinking = false) }
        }
        composeTestRule.onNodeWithTag("dashboard-title").assertIsDisplayed()
        composeTestRule.onNodeWithTag("dashboard-linked").assertIsDisplayed()
        composeTestRule.onNodeWithTag("dashboard-unlink").assertIsDisplayed()
    }

    @Test
    fun unlinkAsksForConfirmationBeforeFiring() {
        var clicked = false
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(link = link, onUnlink = { clicked = true }, unlinking = false)
            }
        }
        // Tapping Unlink only opens the confirm dialog; the callback must not
        // fire until the dialog is confirmed.
        composeTestRule.onNodeWithTag("dashboard-unlink").performClick()
        assertFalse(clicked)
        composeTestRule.onNodeWithTag("dashboard-unlink-confirm").performClick()
        assertTrue(clicked)
    }

    @Test
    fun failedUnlinkOffersRetryThatRefiresUnlink() {
        var retries = 0
        var dismissed = false
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = { retries++ },
                    unlinking = false,
                    unlinkFailed = true,
                    onDismissUnlinkError = { dismissed = true },
                )
            }
        }
        // A failed remote revoke surfaces the error dialog; "Try again" dismisses
        // it and re-fires the unlink so the orphaned row can still be cleared.
        composeTestRule.onNodeWithTag("dashboard-unlink-retry").performClick()
        assertTrue(dismissed)
        assertTrue(retries == 1)
    }

    @Test
    fun failedUnlinkOffersForceUnlinkEscapeHatch() {
        var forced = 0
        var dismissed = false
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    unlinkFailed = true,
                    onDismissUnlinkError = { dismissed = true },
                    onForceUnlink = { forced++ },
                )
            }
        }
        // When a revoke is impossible (dead/unreadable token), "Unlink anyway"
        // clears locally so the operator is never stranded on the dashboard.
        composeTestRule.onNodeWithTag("dashboard-unlink-force").performClick()
        assertTrue(dismissed)
        assertTrue(forced == 1)
    }
}
