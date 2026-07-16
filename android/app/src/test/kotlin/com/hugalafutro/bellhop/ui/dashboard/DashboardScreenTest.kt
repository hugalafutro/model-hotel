package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.TrafficPoint
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.shadows.ShadowToast

/**
 * Linked-state dashboard, post toolbar-trim: the title plus the events and
 * settings icons are always present, the Alerts bell only when a member is down,
 * and Unlink/status now live in Settings. Asserts on test tags, not display
 * text, so English copy never breaks tests.
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

    private val members =
        listOf(
            FleetMember(
                id = "m1",
                name = "alpha",
                url = "http://a:8080",
                state = "active",
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

    private val allUp = listOf(members[0])

    @Test
    fun showsTitleAndToolbar() {
        composeTestRule.setContent {
            BellhopTheme { DashboardScreen(link = link) }
        }
        composeTestRule.onNodeWithTag("dashboard-title").assertIsDisplayed()
        composeTestRule.onNodeWithTag("dashboard-events").assertIsDisplayed()
        composeTestRule.onNodeWithTag("dashboard-settings").assertIsDisplayed()
    }

    @Test
    fun settingsButtonFiresCallback() {
        var opened = 0
        composeTestRule.setContent {
            BellhopTheme { DashboardScreen(link = link, onSettingsClick = { opened++ }) }
        }
        composeTestRule.onNodeWithTag("dashboard-settings").performClick()
        assertTrue(opened == 1)
    }

    @Test
    fun bellShownAndFiresWhenAMemberIsDown() {
        var opened = 0
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = members),
                    onAlertsClick = { opened++ },
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-alerts").assertIsDisplayed()
        composeTestRule.onNodeWithTag("dashboard-alerts").performClick()
        assertTrue(opened == 1)
    }

    @Test
    fun recentEventPillShowsAndOpensMember() {
        var opened = ""
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui =
                        DashboardUiState(
                            loading = false,
                            members = allUp,
                            recentEvents =
                                mapOf("m1" to FdEvent(id = "e1", severity = "error", message = "health check failed")),
                        ),
                    onMemberClick = { opened = it },
                )
            }
        }
        val pill = composeTestRule.onNodeWithTag("member-recent-event-alpha")
        pill.assertIsDisplayed()
        pill.performClick()
        assertEquals("m1", opened)
    }

    @Test
    fun quietTrafficWindowLabelsTheSparklineEmpty() {
        // Buckets present but no requests: the idle sparkline still draws, with the
        // same "No requests in this window." label the detail screen uses centred
        // over it, so the flat line reads as quiet rather than broken.
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui =
                        DashboardUiState(
                            loading = false,
                            members = allUp,
                            traffic =
                                mapOf(
                                    "m1" to
                                        MemberTraffic(
                                            memberId = "m1",
                                            reachable = true,
                                            totalRequests = 0,
                                            points = listOf(TrafficPoint(bucket = "t0", requests = 0)),
                                        ),
                                ),
                        ),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-sparkline-alpha", useUnmergedTree = true).assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-sparkline-empty-alpha", useUnmergedTree = true).assertIsDisplayed()
    }

    @Test
    fun busyTrafficWindowDrawsSparklineWithoutEmptyLabel() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui =
                        DashboardUiState(
                            loading = false,
                            members = allUp,
                            traffic =
                                mapOf(
                                    "m1" to
                                        MemberTraffic(
                                            memberId = "m1",
                                            reachable = true,
                                            totalRequests = 5,
                                            points = listOf(TrafficPoint(bucket = "t0", requests = 5, errors = 1)),
                                        ),
                                ),
                        ),
                )
            }
        }
        composeTestRule.onNodeWithTag("member-sparkline-alpha", useUnmergedTree = true).assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-sparkline-empty-alpha", useUnmergedTree = true).assertDoesNotExist()
    }

    @Test
    fun footerOpensGithubBehindConfirm() {
        // One member: the list doesn't scroll, so the footer is pinned to the
        // bottom of the screen (not a list item) and is reachable without scrolling.
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(link = link, ui = DashboardUiState(loading = false, members = allUp))
            }
        }
        composeTestRule.onNodeWithTag("dashboard-footer").performClick()
        // The confirm-before-leaving dialog appears (its URL text carries this tag).
        composeTestRule.onNodeWithTag("member-url-dialog-text").assertIsDisplayed()
    }

    @Test
    fun holdToCopyLongPressCopiesMember() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = allUp),
                    holdToCopy = true,
                )
            }
        }
        composeTestRule.onNodeWithTag("member-card-alpha").performTouchInput { longClick() }
        composeTestRule.waitForIdle()
        assertEquals(1, ShadowToast.shownToastCount())
    }

    @Test
    fun memberTapStillOpensMemberUnderHoldToCopy() {
        var opened = ""
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = allUp),
                    holdToCopy = true,
                    onMemberClick = { opened = it },
                )
            }
        }
        composeTestRule.onNodeWithTag("member-card-alpha").performClick()
        assertEquals("m1", opened)
    }

    @Test
    fun bellHiddenWhenAllMembersUp() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(link = link, ui = DashboardUiState(loading = false, members = allUp))
            }
        }
        composeTestRule.onNodeWithTag("dashboard-alerts").assertDoesNotExist()
    }

    @Test
    fun rendersMemberCardsWithPrimaryAndDrainedBadges() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
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
                    ui = DashboardUiState(loading = false, members = members),
                    onEventsClick = { opened++ },
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-events").performClick()
        assertTrue(opened == 1)
    }

    @Test
    fun lockFabHiddenWhenLockDisabled() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(link = link, ui = DashboardUiState(loading = false, members = members))
            }
        }
        composeTestRule.onNodeWithTag("lock-fab").assertDoesNotExist()
    }

    @Test
    fun lockFabTapHintsAndLongPressLocks() {
        var locked = false
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = members),
                    lockEnabled = true,
                    onLock = { locked = true },
                )
            }
        }
        val fab = composeTestRule.onNodeWithTag("lock-fab")
        fab.assertIsDisplayed()
        // A tap must not lock: it only hints via a toast, so a stray touch is safe.
        fab.performClick()
        assertEquals(1, ShadowToast.shownToastCount())
        assertFalse(locked)
        // A long-press is the deliberate gesture that actually locks.
        fab.performTouchInput { longClick() }
        assertTrue(locked)
    }

    @Test
    fun loadingShowsSpinner() {
        composeTestRule.setContent {
            BellhopTheme { DashboardScreen(link = link, ui = DashboardUiState(loading = true)) }
        }
        composeTestRule.onNodeWithTag("dashboard-loading").assertIsDisplayed()
    }

    @Test
    fun emptyShowsMessage() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(link = link, ui = DashboardUiState(loading = false, members = emptyList()))
            }
        }
        composeTestRule.onNodeWithTag("dashboard-empty").assertIsDisplayed()
    }

    @Test
    fun errorShowsBannerOverStaleList() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = members, error = "boom"),
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-error").assertIsDisplayed()
        composeTestRule.onNodeWithTag("member-card-alpha").assertIsDisplayed()
    }

    @Test
    fun autoSyncControlShownForOperatorAndToggleFires() {
        var requested: Boolean? = null
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = allUp, primaryId = "m1", autoSyncEnabled = true),
                    canOperate = true,
                    onSetAutoSync = { requested = it },
                )
            }
        }
        composeTestRule.onNodeWithTag("autosync-card").assertIsDisplayed()
        // Effective state is on; toggling asks to turn it off.
        composeTestRule.onNodeWithTag("autosync-toggle").performClick()
        assertTrue(requested == false)
    }

    @Test
    fun autoSyncControlHiddenForMonitor() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = allUp, primaryId = "m1"),
                    canOperate = false,
                )
            }
        }
        composeTestRule.onNodeWithTag("autosync-card").assertDoesNotExist()
    }

    @Test
    fun autoSyncControlHiddenWithoutAPrimary() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = allUp, primaryId = ""),
                    canOperate = true,
                )
            }
        }
        composeTestRule.onNodeWithTag("autosync-card").assertDoesNotExist()
    }

    @Test
    fun autoSyncForbiddenCollapsesToNoteWithoutToggle() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui =
                        DashboardUiState(
                            loading = false,
                            members = allUp,
                            primaryId = "m1",
                            autoSync = AutoSyncAction(forbidden = true),
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule.onNodeWithTag("autosync-forbidden").assertIsDisplayed()
        composeTestRule.onNodeWithTag("autosync-toggle").assertDoesNotExist()
    }

    @Test
    fun autoSyncPendingHintShownAndToggleDisabledWhileInFlight() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui =
                        DashboardUiState(
                            loading = false,
                            members = allUp,
                            primaryId = "m1",
                            autoSyncEnabled = true,
                            autoSync = AutoSyncAction(inProgress = true, pendingEnabled = false),
                        ),
                    canOperate = true,
                )
            }
        }
        composeTestRule.onNodeWithTag("autosync-pending").assertIsDisplayed()
        composeTestRule.onNodeWithTag("autosync-toggle").assertIsNotEnabled()
    }

    @Test
    fun revokedTokenShowsRevokedBanner() {
        composeTestRule.setContent {
            BellhopTheme {
                DashboardScreen(
                    link = link,
                    ui = DashboardUiState(loading = false, members = members, revoked = true),
                )
            }
        }
        composeTestRule.onNodeWithTag("dashboard-revoked").assertIsDisplayed()
    }
}
