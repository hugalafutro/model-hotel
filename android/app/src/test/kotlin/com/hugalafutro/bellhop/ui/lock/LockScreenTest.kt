package com.hugalafutro.bellhop.ui.lock

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * Lock screen: covers the fleet UI and asks the host to prompt. It prompts once
 * automatically on show and again from the retry button; it never unlocks itself.
 */
@RunWith(RobolectricTestRunner::class)
class LockScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    @Test
    fun promptsOnceAutomaticallyOnShow() {
        var prompts = 0
        composeTestRule.setContent {
            BellhopTheme { LockScreen(onUnlock = { prompts++ }) }
        }
        composeTestRule.onNodeWithTag("lock-screen").assertIsDisplayed()
        assertEquals(1, prompts)
    }

    @Test
    fun retryButtonPromptsAgain() {
        var prompts = 0
        composeTestRule.setContent {
            BellhopTheme { LockScreen(onUnlock = { prompts++ }) }
        }
        // One auto-prompt on show, then the button is the retry after a cancel.
        composeTestRule.onNodeWithTag("lock-unlock").performClick()
        assertEquals(2, prompts)
    }
}
