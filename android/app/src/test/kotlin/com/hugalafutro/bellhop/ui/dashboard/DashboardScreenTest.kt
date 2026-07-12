package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.MemberStatus
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

    private val members =
        listOf(
            FleetMember(
                id = "m1",
                name = "alpha",
                url = "http://a:8080",
                status =
                    MemberStatus(
                        health = HealthStatus(known = true, healthy = true, latencyMs = 3),
                        traefikStatus = "UP",
                        version = "1.0.0",
                    ),
            ),
            FleetMember(
                id = "m2",
                name = "beta",
                url = "http://b:8080",
                state = "drained",
                status =
                    MemberStatus(
                        health = HealthStatus(known = true, healthy = false, error = "connection refused"),
                    ),
            ),
        )

    @Test
    fun rendersMemberCardsWithPrimaryAndDrainedBadges() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    ui = DashboardUiState(loading = false, members = members, primaryId = "m1"),
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-summary").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-card-alpha").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-card-beta").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-primary", useUnmergedTree = true).assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-drained", useUnmergedTree = true).assertIsDisplayed()
    }

    @Test
    fun clickingMemberCardFiresCallbackWithId() {
        var clicked: String? = null
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    ui = DashboardUiState(loading = false, members = members),
                    onMemberClick = { clicked = it },
                )
            }
        }
        composeTestRule.onNodeWithTag("member-card-beta").performClick()
        assertTrue(clicked == "m2")
    }

    @Test
    fun eventsButtonFiresCallback() {
        var opened = 0
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    ui = DashboardUiState(loading = false, members = members),
                    onEventsClick = { opened++ },
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-events").performClick()
        assertTrue(opened == 1)
    }

    @Test
    fun firstLoadShowsSpinnerThenEmptyStateWithoutMembers() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(link = link, onUnlink = {}, unlinking = false)
            }
        }
        composeTestRule.onNodeWithTag("dashboard-loading").assertIsDisplayed()
    }

    @Test
    fun emptyFleetShowsEmptyState() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    ui = DashboardUiState(loading = false),
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-empty").assertIsDisplayed()
    }

    @Test
    fun refreshErrorShowsBannerAndKeepsStaleList() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    ui = DashboardUiState(loading = false, members = members, error = "boom"),
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-error").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-card-alpha").assertIsDisplayed()
    }

    @Test
    fun revokedTokenShowsRevokedBanner() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    onUnlink = {},
                    unlinking = false,
                    ui = DashboardUiState(loading = false, members = members, revoked = true),
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-revoked").assertIsDisplayed()
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
