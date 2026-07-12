package com.hugalafutro.bellhop.ui.settings

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LockConfig
import com.hugalafutro.bellhop.data.LockTimeout
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
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
        lockConfig: LockConfig = LockConfig(enabled = false, timeoutMs = LockTimeout.THIRTY_MINUTES.millis),
        lockAvailable: Boolean = true,
        monitorEnabled: Boolean = false,
        unlinkFailed: Boolean = false,
        onToggleLock: (Boolean) -> Unit = {},
        onSelectTimeout: (LockTimeout) -> Unit = {},
        onToggleMonitor: (Boolean) -> Unit = {},
        onAlertsClick: () -> Unit = {},
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
                    onBack = {},
                    onToggleLock = onToggleLock,
                    onSelectTimeout = onSelectTimeout,
                    onToggleMonitor = onToggleMonitor,
                    onAlertsClick = onAlertsClick,
                    onUnlink = onUnlink,
                    unlinkFailed = unlinkFailed,
                    onDismissUnlinkError = onDismissUnlinkError,
                    onForceUnlink = onForceUnlink,
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
    fun togglingLockFiresCallback() {
        var toggledTo: Boolean? = null
        content(onToggleLock = { toggledTo = it })
        composeTestRule.onNodeWithTag("settings-lock-toggle").performClick()
        assertEquals(true, toggledTo)
    }

    @Test
    fun unavailableLockShowsHint() {
        content(lockAvailable = false)
        composeTestRule.onNodeWithTag("settings-lock-unavailable").assertIsDisplayed()
    }

    @Test
    fun timeoutPillsShowWhenEnabledAndFireSelection() {
        var picked: LockTimeout? = null
        content(
            lockConfig = LockConfig(enabled = true, timeoutMs = LockTimeout.THIRTY_MINUTES.millis),
            onSelectTimeout = { picked = it },
        )
        composeTestRule.onNodeWithTag("settings-lock-timeout-FIVE_MINUTES").performClick()
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
    fun alertsRowFiresCallback() {
        var opened = 0
        content(onAlertsClick = { opened++ })
        composeTestRule.onNodeWithTag("settings-alerts").performScrollTo().performClick()
        assertTrue(opened == 1)
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
}
