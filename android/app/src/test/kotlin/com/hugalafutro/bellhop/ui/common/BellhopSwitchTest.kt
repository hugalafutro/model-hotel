package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * BellhopSwitch replaces Material's `Switch` at every call site, so what these
 * assert is that it still *is* a switch: [assertIsOn] / [assertIsOff] and
 * `performClick` only work on a node carrying toggleable semantics with
 * Role.Switch, which is also what a screen reader reads. Nothing here asserts
 * on the shape -- the corners are the reason it exists, but the semantics are
 * what a regression would silently take away.
 */
@RunWith(RobolectricTestRunner::class)
class BellhopSwitchTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    @Test
    fun reportsItsCheckedState() {
        composeTestRule.setContent {
            BellhopTheme {
                Column {
                    BellhopSwitch(checked = true, onCheckedChange = {}, modifier = Modifier.testTag("on"))
                    BellhopSwitch(checked = false, onCheckedChange = {}, modifier = Modifier.testTag("off"))
                }
            }
        }

        composeTestRule.onNodeWithTag("on").assertIsDisplayed().assertIsOn()
        composeTestRule.onNodeWithTag("off").assertIsDisplayed().assertIsOff()
    }

    @Test
    fun tapFiresTheOppositeValue() {
        var got: Boolean? = null
        composeTestRule.setContent {
            BellhopTheme {
                BellhopSwitch(checked = false, onCheckedChange = { got = it }, modifier = Modifier.testTag("t"))
            }
        }

        composeTestRule.onNodeWithTag("t").performClick()
        assertEquals(true, got)
    }

    @Test
    fun disabledSwitchSwallowsTheTap() {
        // Several call sites disable themselves mid-flight (a lock with no
        // hardware enrolled, an auto-sync flip already in progress); a tap that
        // still fired would send a request the screen has already ruled out.
        var got: Boolean? = null
        composeTestRule.setContent {
            BellhopTheme {
                BellhopSwitch(
                    checked = false,
                    onCheckedChange = { got = it },
                    enabled = false,
                    modifier = Modifier.testTag("d"),
                )
            }
        }

        composeTestRule.onNodeWithTag("d").assertIsNotEnabled().performClick()
        assertNull(got)
    }
}
