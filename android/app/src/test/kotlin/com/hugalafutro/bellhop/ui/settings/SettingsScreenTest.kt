package com.hugalafutro.bellhop.ui.settings

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LockConfig
import com.hugalafutro.bellhop.data.LockTimeout
import com.hugalafutro.bellhop.data.QuotaBadgeAlign
import com.hugalafutro.bellhop.data.TimeFormat
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * Settings screen: link status, app-lock toggle/window, and the Unlink flow
 * moved off the dashboard. Asserts on test tags, not display text, so English
 * copy never breaks tests.
 */
@RunWith(RobolectricTestRunner::class)
class SettingsScreenTest {
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

    private fun content(
        link: LinkState.Linked = this.link,
        lockConfig: LockConfig = LockConfig(enabled = false, timeoutMs = LockTimeout.THIRTY_MINUTES.millis),
        lockAvailable: Boolean = true,
        monitorEnabled: Boolean = false,
        notificationsBlocked: Boolean = false,
        pushEnabled: Boolean = false,
        pushEndpoint: String? = null,
        pushDistributorAvailable: Boolean = true,
        pushNotificationsBlocked: Boolean = false,
        unlinkFailed: Boolean = false,
        holdToCopy: Boolean = false,
        alertCounts: Map<String, Int> = emptyMap(),
        onToggleLock: (Boolean) -> Unit = {},
        onSelectTimeout: (LockTimeout) -> Unit = {},
        onToggleMonitor: (Boolean) -> Unit = {},
        onTogglePush: (Boolean) -> Unit = {},
        onToggleHoldToCopy: (Boolean) -> Unit = {},
        graphRangeMinutes: Int = 60,
        onSetGraphRange: (Int) -> Unit = {},
        widgetGraphs: Boolean = false,
        onToggleWidgetGraphs: (Boolean) -> Unit = {},
        widgetQuota: Boolean = true,
        onToggleWidgetQuota: (Boolean) -> Unit = {},
        widgetQuotaAlign: QuotaBadgeAlign = QuotaBadgeAlign.LEFT,
        onSetWidgetQuotaAlign: (QuotaBadgeAlign) -> Unit = {},
        timeFormat: TimeFormat = TimeFormat.SYSTEM,
        onSetTimeFormat: (TimeFormat) -> Unit = {},
        batteryUnrestricted: Boolean = true,
        onRequestBatteryExemption: () -> Unit = {},
        onAlertsClick: () -> Unit = {},
        onQuotaBadgesClick: () -> Unit = {},
        onUnlink: () -> Unit = {},
        onForceUnlink: () -> Unit = {},
        onDismissUnlinkError: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            BellhopTheme {
                SettingsScreen(
                    link = link,
                    lockConfig = lockConfig,
                    lockAvailable = lockAvailable,
                    monitorEnabled = monitorEnabled,
                    notificationsBlocked = notificationsBlocked,
                    pushEnabled = pushEnabled,
                    pushEndpoint = pushEndpoint,
                    pushDistributorAvailable = pushDistributorAvailable,
                    pushNotificationsBlocked = pushNotificationsBlocked,
                    batteryUnrestricted = batteryUnrestricted,
                    onRequestBatteryExemption = onRequestBatteryExemption,
                    onBack = {},
                    onToggleLock = onToggleLock,
                    onSelectTimeout = onSelectTimeout,
                    onToggleMonitor = onToggleMonitor,
                    onTogglePush = onTogglePush,
                    onAlertsClick = onAlertsClick,
                    onQuotaBadgesClick = onQuotaBadgesClick,
                    onUnlink = onUnlink,
                    unlinkFailed = unlinkFailed,
                    onDismissUnlinkError = onDismissUnlinkError,
                    onForceUnlink = onForceUnlink,
                    holdToCopy = holdToCopy,
                    onToggleHoldToCopy = onToggleHoldToCopy,
                    graphRangeMinutes = graphRangeMinutes,
                    onSetGraphRange = onSetGraphRange,
                    widgetGraphs = widgetGraphs,
                    onToggleWidgetGraphs = onToggleWidgetGraphs,
                    widgetQuota = widgetQuota,
                    onToggleWidgetQuota = onToggleWidgetQuota,
                    widgetQuotaAlign = widgetQuotaAlign,
                    onSetWidgetQuotaAlign = onSetWidgetQuotaAlign,
                    timeFormat = timeFormat,
                    onSetTimeFormat = onSetTimeFormat,
                    alertCounts = alertCounts,
                )
            }
        }
    }

    @Test
    fun showsLinkStatus() {
        content()
        composeTestRule.onNodeWithTag("settings-title").assertIsDisplayed()
        composeTestRule.onNodeWithTag("settings-fd-name").assertIsDisplayed()
        composeTestRule.onNodeWithTag("settings-linked").assertIsDisplayed()
    }

    @Test
    fun linkedOnRowHiddenWithoutStamp() {
        // A link saved before linkedAt existed carries no stamp, so the date row hides.
        content()
        composeTestRule.onNodeWithTag("settings-fd-linked-on").assertDoesNotExist()
    }

    @Test
    fun linkedOnRowShownWithStamp() {
        content(link = link.copy(linkedAt = 1_700_000_000_000L))
        composeTestRule.onNodeWithTag("settings-fd-linked-on").assertIsDisplayed()
    }

    @Test
    fun tappingFdNameAsksBeforeCopyingAddress() {
        content()
        // The name only opens the confirm dialog on tap; the copy is the second step.
        composeTestRule.onNodeWithTag("settings-fd-copy-confirm").assertDoesNotExist()
        composeTestRule.onNodeWithTag("settings-fd-name").performClick()
        composeTestRule.onNodeWithTag("settings-fd-copy-confirm").assertIsDisplayed()
    }

    @Test
    fun togglingHoldToCopyFiresCallback() {
        var toggledTo: Boolean? = null
        content(holdToCopy = false, onToggleHoldToCopy = { toggledTo = it })
        composeTestRule.onNodeWithTag("settings-hold-copy-toggle").performScrollTo().performClick()
        assertEquals(true, toggledTo)
    }

    @Test
    fun pickingGraphRangeFiresCallback() {
        var picked: Int? = null
        content(graphRangeMinutes = 60, onSetGraphRange = { picked = it })
        composeTestRule.onNodeWithTag("settings-graph-range-360").performScrollTo().performClick()
        assertEquals(360, picked)
    }

    @Test
    fun theWidgetSectionSwitchesItsTwoBlocksIndependently() {
        // The traffic bars and the badge strip are separate blocks of the widget
        // under one heading: each switch must reach only its own preference, and
        // neither can be mistaken for an off switch for the widget itself.
        var graphsTo: Boolean? = null
        var quotaTo: Boolean? = null
        content(
            widgetGraphs = false,
            onToggleWidgetGraphs = { graphsTo = it },
            widgetQuota = true,
            onToggleWidgetQuota = { quotaTo = it },
        )

        composeTestRule.onNodeWithTag("settings-widget-graphs-toggle").performScrollTo().performClick()
        assertEquals(true, graphsTo)
        assertNull(quotaTo)

        composeTestRule.onNodeWithTag("settings-widget-quota-toggle").performScrollTo().performClick()
        assertEquals(false, quotaTo)
        assertEquals(true, graphsTo)
    }

    @Test
    fun pickingTimeFormatFiresCallback() {
        var picked: TimeFormat? = null
        content(timeFormat = TimeFormat.SYSTEM, onSetTimeFormat = { picked = it })
        composeTestRule.onNodeWithTag("settings-time-format-H12").performScrollTo().performClick()
        assertEquals(TimeFormat.H12, picked)
    }

    @Test
    fun batteryRowHiddenWhenBackgroundOff() {
        content(monitorEnabled = false, pushEnabled = false)
        composeTestRule.onNodeWithTag("settings-battery").assertDoesNotExist()
    }

    @Test
    fun optimizedBatteryOffersRequestThatFires() {
        var requested = false
        content(
            monitorEnabled = true,
            batteryUnrestricted = false,
            onRequestBatteryExemption = { requested = true },
        )
        composeTestRule.onNodeWithTag("settings-battery-request").performScrollTo().performClick()
        assertTrue(requested)
    }

    @Test
    fun togglingLockFiresCallback() {
        var toggledTo: Boolean? = null
        content(onToggleLock = { toggledTo = it })
        composeTestRule.onNodeWithTag("settings-lock-toggle").performScrollTo().performClick()
        assertEquals(true, toggledTo)
    }

    @Test
    fun unavailableLockShowsHint() {
        content(lockAvailable = false)
        composeTestRule.onNodeWithTag("settings-lock-unavailable").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun timeoutPillsShowWhenEnabledAndFireSelection() {
        var picked: LockTimeout? = null
        content(
            lockConfig = LockConfig(enabled = true, timeoutMs = LockTimeout.THIRTY_MINUTES.millis),
            onSelectTimeout = { picked = it },
        )
        composeTestRule.onNodeWithTag("settings-lock-timeout-FIVE_MINUTES").performScrollTo().performClick()
        assertEquals(LockTimeout.FIVE_MINUTES, picked)
    }

    @Test
    fun timeoutPillsHiddenWhenLockDisabled() {
        content(lockConfig = LockConfig(enabled = false, timeoutMs = LockTimeout.THIRTY_MINUTES.millis))
        composeTestRule.onNodeWithTag("settings-lock-timeout-THIRTY_MINUTES").assertDoesNotExist()
    }

    @Test
    fun togglingMonitorFiresCallback() {
        var toggledTo: Boolean? = null
        content(onToggleMonitor = { toggledTo = it })
        composeTestRule.onNodeWithTag("settings-monitor-toggle").performScrollTo().performClick()
        assertEquals(true, toggledTo)
    }

    @Test
    fun monitorNoteHiddenWhenDisabled() {
        content(monitorEnabled = false)
        composeTestRule.onNodeWithTag("settings-monitor-note").assertDoesNotExist()
    }

    @Test
    fun monitorNoteShownWhenEnabled() {
        content(monitorEnabled = true)
        composeTestRule.onNodeWithTag("settings-monitor-note").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun monitorBlockedHintShownWhenEnabledButNotificationsDenied() {
        content(monitorEnabled = true, notificationsBlocked = true)
        composeTestRule.onNodeWithTag("settings-monitor-blocked").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun monitorBlockedHintHiddenWhenNotificationsAllowed() {
        content(monitorEnabled = true, notificationsBlocked = false)
        composeTestRule.onNodeWithTag("settings-monitor-blocked").assertDoesNotExist()
    }

    @Test
    fun togglingPushFiresCallback() {
        var toggledTo: Boolean? = null
        content(onTogglePush = { toggledTo = it })
        composeTestRule.onNodeWithTag("settings-push-toggle").performScrollTo().performClick()
        assertEquals(true, toggledTo)
    }

    @Test
    fun pushEndpointShownWhenEnabledAndAssigned() {
        content(pushEnabled = true, pushEndpoint = "https://ntfy.sh/upABC123")
        composeTestRule.onNodeWithTag("settings-push-endpoint").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun pushWaitingHintShownWhenEnabledButNoEndpointYet() {
        content(pushEnabled = true, pushEndpoint = null)
        composeTestRule.onNodeWithTag("settings-push-waiting").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun pushEndpointHiddenWhenDisabled() {
        content(pushEnabled = false, pushEndpoint = "https://ntfy.sh/upABC123")
        composeTestRule.onNodeWithTag("settings-push-endpoint").assertDoesNotExist()
    }

    @Test
    fun noDistributorHintShownWhenNoneInstalled() {
        content(pushDistributorAvailable = false)
        composeTestRule.onNodeWithTag("settings-push-no-distributor").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun pushBlockedHintShownWhenEnabledButNotificationsDenied() {
        content(pushEnabled = true, pushNotificationsBlocked = true)
        composeTestRule.onNodeWithTag("settings-push-blocked").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun alertsRowFiresCallback() {
        var opened = 0
        content(onAlertsClick = { opened++ })
        composeTestRule.onNodeWithTag("settings-alerts").performScrollTo().performClick()
        assertTrue(opened == 1)
    }

    @Test
    fun quotaBadgesRowFiresCallback() {
        var opened = 0
        content(onQuotaBadgesClick = { opened++ })
        composeTestRule.onNodeWithTag("settings-quota-badges").performScrollTo().performClick()
        assertTrue(opened == 1)
    }

    @Test
    fun languageIconOpensPickerWithSystemAndLanguageOptions() {
        content()
        composeTestRule.onNodeWithTag("settings-language").performScrollTo().performClick()
        // The dialog offers the system default plus every supported language.
        composeTestRule.onNodeWithTag("language-option-system").assertIsDisplayed()
        composeTestRule.onNodeWithTag("language-option-de").assertExists()
        composeTestRule.onNodeWithTag("language-option-ja").assertExists()
        composeTestRule.onNodeWithTag("language-cancel").assertExists()
    }

    @Test
    fun alertSeverityBadgesShowAllFourEvenAtZero() {
        // Only error and warning enabled; info and success still render (at 0) so the
        // pill reads as a live, tappable destination.
        content(alertCounts = mapOf("error" to 2, "warning" to 1, "info" to 0, "success" to 0))
        // The clickable Alerts card merges its descendants' semantics, so the badge
        // and chevron tags live in the unmerged tree; scroll the card into view first.
        composeTestRule.onNodeWithTag("settings-alerts").performScrollTo()
        for (sev in listOf("error", "warning", "info", "success")) {
            composeTestRule.onNodeWithTag("settings-alert-badge-$sev", useUnmergedTree = true).assertExists()
        }
        // The nav chevron marks the pill as a jump to another screen.
        composeTestRule.onNodeWithTag("nav-chevron", useUnmergedTree = true).assertExists()
    }

    @Test
    fun unlinkAsksForConfirmationBeforeFiring() {
        var unlinked = 0
        content(onUnlink = { unlinked++ })
        composeTestRule.onNodeWithTag("settings-unlink").performScrollTo().performClick()
        // The tap only opens the confirm dialog; nothing fires yet.
        assertTrue(unlinked == 0)
        composeTestRule.onNodeWithTag("settings-unlink-confirm").performClick()
        assertTrue(unlinked == 1)
    }

    @Test
    fun failedUnlinkOffersForceUnlinkEscapeHatch() {
        var forced = 0
        var dismissed = false
        content(unlinkFailed = true, onForceUnlink = { forced++ }, onDismissUnlinkError = { dismissed = true })
        // With a dead/unreachable token, "Unlink anyway" clears locally so the
        // operator is never stranded.
        composeTestRule.onNodeWithTag("settings-unlink-force").performClick()
        assertTrue(dismissed)
        assertTrue(forced == 1)
    }

    @Test
    fun widgetAlignmentPickerOffersAllThreeChoices() {
        content()
        composeTestRule.onNodeWithTag("settings-widget-align-LEFT").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithTag("settings-widget-align-CENTER").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithTag("settings-widget-align-RIGHT").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun pickingWidgetAlignmentFiresCallback() {
        var picked: QuotaBadgeAlign? = null
        content(widgetQuotaAlign = QuotaBadgeAlign.LEFT, onSetWidgetQuotaAlign = { picked = it })

        composeTestRule.onNodeWithTag("settings-widget-align-CENTER").performScrollTo().performClick()
        assertEquals(QuotaBadgeAlign.CENTER, picked)

        composeTestRule.onNodeWithTag("settings-widget-align-RIGHT").performScrollTo().performClick()
        assertEquals(QuotaBadgeAlign.RIGHT, picked)
    }

    @Test
    fun widgetAlignmentPickerMarksTheActiveChoiceSelected() {
        // Selection reaches the semantics tree, not just the fill colour, so a
        // screen reader can say which alignment is in effect. Asserting the two
        // unselected pills as well pins the mapping: a pill that reported itself
        // selected unconditionally would satisfy the first assertion alone.
        content(widgetQuotaAlign = QuotaBadgeAlign.CENTER)

        composeTestRule.onNodeWithTag("settings-widget-align-CENTER").performScrollTo().assertIsSelected()
        composeTestRule.onNodeWithTag("settings-widget-align-LEFT").assertIsNotSelected()
        composeTestRule.onNodeWithTag("settings-widget-align-RIGHT").assertIsNotSelected()
    }

    @Test
    fun widgetAlignmentPickerInertWhenQuotaStripOff() {
        // BellhopWidget gates the whole badge strip on widgetQuota, so with the
        // switch off the alignment choice has nothing left to arrange. The
        // pills stay visible (the set still says what it offers) but faded and
        // untappable, same as the WIDGET tab on the quota config screen.
        var picked: QuotaBadgeAlign? = null
        content(widgetQuota = false, onSetWidgetQuotaAlign = { picked = it })

        composeTestRule
            .onNodeWithTag("settings-widget-align-LEFT")
            .performScrollTo()
            .assertIsDisplayed()
            .assertIsNotEnabled()
        composeTestRule.onNodeWithTag("settings-widget-align-CENTER").performScrollTo().assertIsNotEnabled()
        composeTestRule.onNodeWithTag("settings-widget-align-RIGHT").performScrollTo().assertIsNotEnabled()

        composeTestRule.onNodeWithTag("settings-widget-align-CENTER").performClick()
        composeTestRule.waitForIdle()
        assertNull(picked)
    }
}
