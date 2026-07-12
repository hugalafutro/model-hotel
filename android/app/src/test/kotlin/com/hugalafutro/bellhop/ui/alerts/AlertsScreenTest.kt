package com.hugalafutro.bellhop.ui.alerts

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import com.hugalafutro.bellhop.data.AlertEventDef
import com.hugalafutro.bellhop.data.AlertStatus
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * Alerts screen: delivery-status pill, catalog rows, revoked banner, back arrow.
 * Asserts on test tags, not display text, so English copy never breaks tests.
 */
@RunWith(RobolectricTestRunner::class)
class AlertsScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private val catalog =
        listOf(
            AlertEventDef("health.down", "Health", "error", defaultOn = true),
            AlertEventDef("health.up", "Health", "success", defaultOn = true),
            AlertEventDef("config.synced", "Config Sync", "info", defaultOn = false),
        )

    private val loaded =
        AlertsUiState(
            loading = false,
            status = AlertStatus(configured = true, reachable = true, healthy = true),
            catalog = catalog,
        )

    @Test
    fun rendersStatusPillAndCatalogRows() {
        composeTestRule.setContent {
            BellhopTheme {
                AlertsScreen(onBack = {}, ui = loaded)
            }
        }
        composeTestRule.onNodeWithTag("alerts-status-pill", useUnmergedTree = true).assertIsDisplayed()
        // The catalog is a LazyColumn, so scroll each row into view before
        // asserting: off-screen rows are never composed.
        for (sev in listOf("alert-sev-error", "alert-sev-success", "alert-sev-info")) {
            composeTestRule.onNodeWithTag("alerts-list").performScrollToNode(hasTestTag(sev))
            composeTestRule.onNodeWithTag(sev, useUnmergedTree = true).assertIsDisplayed()
        }
    }

    @Test
    fun unhealthyStatusShowsDetail() {
        composeTestRule.setContent {
            BellhopTheme {
                AlertsScreen(
                    onBack = {},
                    ui =
                        loaded.copy(
                            status =
                                AlertStatus(
                                    configured = true,
                                    reachable = true,
                                    healthy = false,
                                    detail = "apprise-api returned status 417",
                                ),
                        ),
                )
            }
        }
        composeTestRule
            .onNodeWithTag("alerts-list")
            .performScrollToNode(hasTestTag("alerts-status-detail"))
        composeTestRule.onNodeWithTag("alerts-status-detail", useUnmergedTree = true).assertIsDisplayed()
    }

    @Test
    fun healthyStatusHidesDetail() {
        composeTestRule.setContent {
            BellhopTheme {
                AlertsScreen(
                    onBack = {},
                    // A healthy notifier carries no reason; the detail line stays hidden.
                    ui = loaded.copy(status = loaded.status?.copy(detail = "ignored while healthy")),
                )
            }
        }
        assertTrue(
            composeTestRule.onAllNodesWithTag("alerts-status-detail").fetchSemanticsNodes().isEmpty(),
        )
    }

    @Test
    fun firstLoadShowsSpinner() {
        composeTestRule.setContent {
            BellhopTheme {
                AlertsScreen(onBack = {}, ui = AlertsUiState())
            }
        }
        composeTestRule.onNodeWithTag("alerts-loading").assertIsDisplayed()
    }

    @Test
    fun revokedShowsBanner() {
        composeTestRule.setContent {
            BellhopTheme {
                AlertsScreen(onBack = {}, ui = AlertsUiState(loading = false, revoked = true))
            }
        }
        composeTestRule.onNodeWithTag("alerts-revoked").assertIsDisplayed()
    }

    @Test
    fun backArrowFiresCallback() {
        var backs = 0
        composeTestRule.setContent {
            BellhopTheme {
                AlertsScreen(onBack = { backs++ }, ui = loaded)
            }
        }
        composeTestRule.onNodeWithTag("alerts-back").performClick()
        assertEquals(1, backs)
    }
}
