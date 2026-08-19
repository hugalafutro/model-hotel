package com.hugalafutro.bellhop.ui.settings

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.data.InMemoryPreferencesDataStore
import com.hugalafutro.bellhop.data.PrefsStore
import com.hugalafutro.bellhop.data.QuotaBadgeConfigStore
import com.hugalafutro.bellhop.data.QuotaBarMode
import com.hugalafutro.bellhop.data.QuotaSurface
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * QuotaBadgesConfigScreen owns its store writes directly (no ViewModel), so
 * these tests drive real in-memory-backed [QuotaBadgeConfigStore] /
 * [PrefsStore] instances and assert on the store's resulting state after a
 * tap, not on a callback capture. Assertions are all by test tag or store
 * enum value, never translated text (see global-constraints).
 */
@RunWith(RobolectricTestRunner::class)
class QuotaBadgesConfigScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private fun newConfigStore(): QuotaBadgeConfigStore = QuotaBadgeConfigStore(InMemoryPreferencesDataStore())

    private fun newPrefsStore(): PrefsStore = PrefsStore(InMemoryPreferencesDataStore())

    @Test
    fun bothSurfaceTabsAreDisplayed() {
        val configStore = newConfigStore()
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = configStore, prefsStore = newPrefsStore(), onBack = {})
            }
        }
        composeTestRule.onNodeWithTag("quota-config-tab-main").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-config-tab-widget").assertIsDisplayed()
    }

    @Test
    fun togglingRowSwitchHidesBadgeInStore() {
        val configStore = newConfigStore()
        runBlocking { configStore.reconcile(QuotaSurface.MAIN, listOf("OR", "NG")) }

        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = configStore, prefsStore = newPrefsStore(), onBack = {})
            }
        }

        // MAIN is the default tab and OR starts visible (unhidden) by default.
        composeTestRule.onNodeWithTag("quota-config-visible-OR").performClick()
        composeTestRule.waitForIdle()

        val hidden = runBlocking { configStore.config(QuotaSurface.MAIN).first().hidden }
        assertTrue("OR" in hidden)
    }

    @Test
    fun switchingToWidgetTabShowsWidgetConfig() {
        val configStore = newConfigStore()
        runBlocking {
            configStore.reconcile(QuotaSurface.MAIN, listOf("OR"))
            configStore.reconcile(QuotaSurface.WIDGET, listOf("NG"))
        }

        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = configStore, prefsStore = newPrefsStore(), onBack = {})
            }
        }

        composeTestRule.onNodeWithTag("quota-config-tab-widget").performClick()
        composeTestRule.waitForIdle()

        // WIDGET reconciled with one name, newly-seen names default hidden on
        // that surface, so the row toggling it back on lands in the store.
        composeTestRule.onNodeWithTag("quota-config-visible-NG").performClick()
        composeTestRule.waitForIdle()

        val visible = runBlocking { configStore.config(QuotaSurface.WIDGET).first().hidden }
        assertTrue("NG" !in visible)
    }

    @Test
    fun widgetTabGoesInertWhenTheWidgetIsntCarryingTheStrip() {
        val configStore = newConfigStore()
        val prefsStore = newPrefsStore()
        runBlocking {
            configStore.reconcile(QuotaSurface.MAIN, listOf("OR"))
            configStore.reconcile(QuotaSurface.WIDGET, listOf("NG"))
            prefsStore.setWidgetQuota(false)
        }

        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = configStore, prefsStore = prefsStore, onBack = {})
            }
        }

        // Still on screen (the two surfaces are what this screen is about) but
        // not selectable: with the strip switched off in Settings there is
        // nothing on the widget for an order to arrange. The tap does nothing,
        // so the MAIN row is the one that stays reachable.
        composeTestRule.onNodeWithTag("quota-config-tab-widget").assertIsDisplayed().assertIsNotEnabled()
        composeTestRule.onNodeWithTag("quota-config-tab-widget").performClick()
        composeTestRule.waitForIdle()
        composeTestRule.onNodeWithTag("quota-config-row-OR").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-config-row-NG").assertDoesNotExist()

        // The stored widget order is untouched, waiting for the switch to
        // come back rather than reset by the strip being off.
        assertEquals(listOf("NG"), runBlocking { configStore.config(QuotaSurface.WIDGET).first().order })
    }

    @Test
    fun bothTabsCountWhatTheyShow() {
        val configStore = newConfigStore()
        runBlocking {
            configStore.reconcile(QuotaSurface.MAIN, listOf("OR", "NG"))
            configStore.reconcile(QuotaSurface.WIDGET, listOf("OR", "NG"))
        }
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = configStore, prefsStore = newPrefsStore(), onBack = {})
            }
        }

        // The count used to be a widget-only line, so the dashboard tab is the
        // one worth asserting: it answers "how many of these am I showing?" too.
        composeTestRule.onNodeWithTag("quota-config-count").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-config-tab-widget").performClick()
        composeTestRule.waitForIdle()
        composeTestRule.onNodeWithTag("quota-config-count").assertIsDisplayed()
    }

    @Test
    fun everyRowCarriesADragHandle() {
        val configStore = newConfigStore()
        runBlocking { configStore.reconcile(QuotaSurface.MAIN, listOf("OR", "NG")) }
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = configStore, prefsStore = newPrefsStore(), onBack = {})
            }
        }

        // The order is dragged by the grip, not by long-pressing the row, so the
        // grip has to be on every row for the list to be reorderable at all.
        composeTestRule.onNodeWithTag("quota-config-drag-OR").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-config-drag-NG").assertIsDisplayed()
    }

    @Test
    fun emptyOrderShowsEmptyState() {
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = newConfigStore(), prefsStore = newPrefsStore(), onBack = {})
            }
        }
        composeTestRule.onNodeWithTag("quota-config-empty").assertIsDisplayed()
    }

    @Test
    fun modeToggleFiresSetQuotaBarMode() {
        val prefsStore = newPrefsStore()
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(configStore = newConfigStore(), prefsStore = prefsStore, onBack = {})
            }
        }

        composeTestRule.onNodeWithTag("quota-mode-toggle").assertIsDisplayed()
        composeTestRule.onNodeWithTag("quota-mode-used").performClick()
        composeTestRule.waitForIdle()

        assertEquals(QuotaBarMode.USED, runBlocking { prefsStore.quotaBarMode.first() })
    }

    @Test
    fun backArrowFiresCallback() {
        var backs = 0
        composeTestRule.setContent {
            BellhopTheme {
                QuotaBadgesConfigScreen(
                    configStore = newConfigStore(),
                    prefsStore = newPrefsStore(),
                    onBack = { backs++ },
                )
            }
        }
        composeTestRule.onNodeWithTag("quota-config-back").performClick()
        assertEquals(1, backs)
    }
}
