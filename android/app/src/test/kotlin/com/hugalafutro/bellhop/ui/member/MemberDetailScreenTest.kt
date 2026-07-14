package com.hugalafutro.bellhop.ui.member

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.TrafficPoint
import com.hugalafutro.bellhop.ui.common.EventRange
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertFalse
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
    fun downMemberShowsProbeErrorAndSyncMetadata() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member =
                        member.copy(
                            createdAt = "2026-06-28T17:53:27Z",
                            lastConfigSyncAt = "2026-07-10T20:26:40Z",
                            lastConfigSyncReason = "the primary's config changed",
                            status =
                                member.status.copy(
                                    health =
                                        HealthStatus(known = true, healthy = false, error = "connection refused"),
                                    autoSyncVerifiedAt = "2026-07-12T13:42:17Z",
                                ),
                        ),
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                )
            }
        }
        // The detail surfaces the info the card can't: full probe error + sync
        // provenance + in-sync heartbeat + when it was added. All four sit in the
        // meta card, a LazyColumn item that may start below the fold — scroll it in.
        for (
        tag in
        listOf(
            "member-detail-down-reason",
            "member-detail-synced",
            "member-detail-verified",
            "member-detail-created",
        )
        ) {
            composeTestRule.onNodeWithTag("member-detail-list").performScrollToNode(hasTestTag(tag))
            composeTestRule.onNodeWithTag(tag).assertIsDisplayed()
        }
    }

    @Test
    fun rendersMemberEventRows() {
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
                            events =
                                listOf(
                                    FdEvent(id = "e1", severity = "error", message = "down", memberId = "m1"),
                                    FdEvent(id = "e2", severity = "success", message = "up", memberId = "m1"),
                                ),
                        ),
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-event-row"))
        assertTrue(
            composeTestRule.onAllNodesWithTag("member-event-row").fetchSemanticsNodes().isNotEmpty(),
        )
    }

    @Test
    fun emptyEventsShowsEmptyState() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic, events = emptyList()),
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-detail-no-events"))
        composeTestRule.onNodeWithTag("member-detail-no-events").assertIsDisplayed()
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

    @Test
    fun operatorCardHiddenForMonitorDevice() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                    canOperate = false,
                )
            }
        }
        assertTrue(
            composeTestRule.onAllNodesWithTag("member-operator-card").fetchSemanticsNodes().isEmpty(),
        )
    }

    @Test
    fun operatorDrainButtonFiresTargetForActiveMember() {
        var target: String? = null
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                    canOperate = true,
                    onSetState = { target = it },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-state"))
        composeTestRule.onNodeWithTag("member-op-state").performClick()
        assertTrue(target == "drained")
    }

    @Test
    fun operatorButtonFiresActivateForDrainedMember() {
        var target: String? = null
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member.copy(state = "drained"),
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                    canOperate = true,
                    onSetState = { target = it },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-state"))
        composeTestRule.onNodeWithTag("member-op-state").performClick()
        assertTrue(target == "active")
    }

    @Test
    fun pendingStateShowsPendingHint() {
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
                            action = ActionUiState(pendingState = "drained"),
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-pending"))
        composeTestRule.onNodeWithTag("member-op-pending").assertIsDisplayed()
    }

    @Test
    fun forbiddenActionCollapsesToGuardNote() {
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
                            action = ActionUiState(forbidden = true),
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-forbidden"))
        composeTestRule.onNodeWithTag("member-op-forbidden").assertIsDisplayed()
        // The guard overrides the role-hint UI: the mutate button is gone.
        assertTrue(
            composeTestRule.onAllNodesWithTag("member-op-state").fetchSemanticsNodes().isEmpty(),
        )
    }

    @Test
    fun syncButtonShownOnPrimaryAndFires() {
        var synced = false
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = true,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                    canOperate = true,
                    onSyncFleet = { synced = true },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-sync"))
        composeTestRule.onNodeWithTag("member-op-sync").performClick()
        assertTrue(synced)
    }

    @Test
    fun syncButtonAbsentOnNonPrimary() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                    canOperate = true,
                )
            }
        }
        assertTrue(
            composeTestRule.onAllNodesWithTag("member-op-sync").fetchSemanticsNodes().isEmpty(),
        )
    }

    @Test
    fun inProgressActionDisablesTheStateButton() {
        var fired = false
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
                            action = ActionUiState(inProgress = true),
                        ),
                    canOperate = true,
                    onSetState = { fired = true },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-state"))
        // Disabled while in flight: a tap must not re-issue the action.
        composeTestRule.onNodeWithTag("member-op-state").performClick()
        assertFalse(fired)
    }

    @Test
    fun addressRowOpensConfirmDialog() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-detail-url"))
        composeTestRule.onNodeWithTag("member-detail-url").performClick()
        // Same confirm dialog as the dashboard card: tap never fires an intent
        // directly.
        composeTestRule.onNodeWithTag("member-url-dialog-text").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-url-open").assertIsDisplayed()
    }

    @Test
    fun lastFleetSyncLineRendersUnderTheSyncButton() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = true,
                    onBack = {},
                    ui =
                        MemberDetailUiState(
                            loading = false,
                            traffic = reachableTraffic,
                            lastFleetSyncAt = "2026-07-13T22:59:49Z",
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-last-sync"))
        composeTestRule.onNodeWithTag("member-op-last-sync").assertIsDisplayed()
    }

    @Test
    fun rangePillsRenderAndFireCallback() {
        var picked: EventRange? = null
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = false,
                    onBack = {},
                    ui = MemberDetailUiState(loading = false, traffic = reachableTraffic),
                    onRange = { picked = it },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-events-range-h24"))
        composeTestRule.onNodeWithTag("member-events-range-h24").performClick()
        assertTrue(picked == EventRange.H24)
    }

    @Test
    fun bottomingOutAsksForMoreEvents() {
        // Scrolling to the end composes the load-more sentinel (lazy items only
        // compose near the viewport), which asks for the next page; the
        // ViewModel is the one that no-ops when nothing more exists.
        var asked = false
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
                            events =
                                listOf(
                                    FdEvent(id = "e1", severity = "info", message = "up", memberId = "m1"),
                                ),
                            // More rows exist server-side, so the sentinel arms.
                            eventsTotal = 10,
                        ),
                    onLoadMoreEvents = { asked = true },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-events-load-more-sentinel"))
        composeTestRule.waitUntil(timeoutMillis = 5_000) { asked }
        assertTrue(asked)
    }

    @Test
    fun exhaustedWindowDoesNotAskForMore() {
        // events.size == eventsTotal: no sentinel, no phantom page requests.
        var asked = false
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
                            events =
                                listOf(
                                    FdEvent(id = "e1", severity = "info", message = "up", memberId = "m1"),
                                ),
                            eventsTotal = 1,
                        ),
                    onLoadMoreEvents = { asked = true },
                )
            }
        }
        composeTestRule.waitForIdle()
        composeTestRule.onNodeWithTag("member-events-load-more-sentinel").assertDoesNotExist()
        assertFalse(asked)
    }

    @Test
    fun loadingMoreShowsFooterSpinner() {
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
                            events =
                                listOf(
                                    FdEvent(id = "e1", severity = "info", message = "up", memberId = "m1"),
                                ),
                            loadingMore = true,
                        ),
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-events-loading-more"))
        composeTestRule.onNodeWithTag("member-events-loading-more").assertIsDisplayed()
    }

    @Test
    fun syncSummaryRendersTheTally() {
        composeTestRule.setContent {
            BellhopTheme {
                MemberDetailScreen(
                    member = member,
                    isPrimary = true,
                    onBack = {},
                    ui =
                        MemberDetailUiState(
                            loading = false,
                            traffic = reachableTraffic,
                            action = ActionUiState(syncSummary = SyncSummary(total = 3, failed = 1)),
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-sync-result"))
        composeTestRule.onNodeWithTag("member-op-sync-result").assertIsDisplayed()
    }

    @Test
    fun errorBannerRendersAndDismissFires() {
        var dismissed = false
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
                            action = ActionUiState(error = "boom"),
                        ),
                    canOperate = true,
                    onDismissActionError = { dismissed = true },
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-error"))
        composeTestRule.onNodeWithTag("member-op-error").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-op-dismiss").performClick()
        assertTrue(dismissed)
    }

    @Test
    fun busyNoticeRendersWhenATapIsDropped() {
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
                            action = ActionUiState(inProgress = true, busy = true),
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule
            .onNodeWithTag("member-detail-list")
            .performScrollToNode(hasTestTag("member-op-busy"))
        composeTestRule.onNodeWithTag("member-op-busy").assertIsDisplayed()
    }
}
