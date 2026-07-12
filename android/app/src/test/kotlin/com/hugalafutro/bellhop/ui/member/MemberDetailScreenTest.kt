package com.hugalafutro.bellhop.ui.member

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.TrafficPoint
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * Member detail: identity header, traffic states, and the back affordance.
 * Asserts on test tags, not display text, so English copy never breaks tests.
 */
@RunWith(RobolectricTestRunner::class)
class MemberDetailScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private val member =
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
        )

    private val reachableTraffic =
        MemberTraffic(
            memberId = "m1",
            reachable = true,
            totalRequests = 12,
            totalErrors = 2,
            points = listOf(TrafficPoint(bucket = "b0", requests = 12, errors = 2)),
        )

    @Test
    fun rendersHeaderMetadataAndChart() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = true,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-detail-title").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-detail-primary", useUnmergedTree = true).assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-detail-meta").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-traffic-totals").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-traffic-chart").assertIsDisplayed()
    }

    @Test
    fun drainedMemberShowsDrainedPill() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member.copy(state = "drained"),
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-detail-drained", useUnmergedTree = true).assertIsDisplayed()
    }

    @Test
    fun backFiresCallback() {
        var backs = 0
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = { backs++ },
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-detail-back").performClick()
        assertTrue(backs == 1)
    }

    @Test
    fun firstLoadShowsSpinnerInsideTrafficCard() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(member = member, isPrimary = false, onBack = {})
            }
        }
        composeTestRule.onNodeWithTag("member-traffic-loading").assertIsDisplayed()
    }

    @Test
    fun unreachableTrafficShowsExplainerNotError() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui =
                        MemberDetailUiState(
                            loading = false,
                            traffic = MemberTraffic(memberId = "m1", reachable = false),
                        ),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-traffic-unreachable").assertIsDisplayed()
    }

    @Test
    fun reachableButIdleShowsEmptyState() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui =
                        MemberDetailUiState(
                            loading = false,
                            traffic = MemberTraffic(memberId = "m1", reachable = true),
                        ),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-traffic-empty").assertIsDisplayed()
    }

    @Test
    fun refreshErrorShowsBannerAndKeepsStaleChart() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui =
                        MemberDetailUiState(
                            loading = false,
                            traffic = reachableTraffic,
                            error = "boom",
                        ),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-detail-error").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-traffic-chart").assertIsDisplayed()
    }

    @Test
    fun revokedTokenShowsRevokedBanner() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, revoked = true),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-detail-revoked").assertIsDisplayed()
    }
}
