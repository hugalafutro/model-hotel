package com.hugalafutro.bellhop.ui.common

import android.content.Intent
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf

/**
 * The URLs this dialog opens come from a paired Front Desk (member addresses),
 * so the "Open" tap must only ever hand a web URL to the system: a fleet that
 * served an `intent:` or `javascript:` address must not get to launch anything
 * else on the phone through Bellhop.
 */
@RunWith(RobolectricTestRunner::class)
class OpenUrlDialogTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private fun nextStartedActivity(): Intent? = shadowOf(RuntimeEnvironment.getApplication()).nextStartedActivity

    @Test
    fun isBrowserUrlAcceptsOnlyHttpAndHttps() {
        assertTrue(isBrowserUrl("https://mh.example.org"))
        assertTrue(isBrowserUrl("http://10.0.0.5:8080/"))
        assertTrue(isBrowserUrl("HTTPS://MH.EXAMPLE.ORG"))
        assertFalse(isBrowserUrl(" https://mh.example.org"))
        assertFalse(isBrowserUrl("intent://scan/#Intent;scheme=zxing;end"))
        assertFalse(isBrowserUrl("javascript:alert(1)"))
        assertFalse(isBrowserUrl("file:///etc/hosts"))
        assertFalse(isBrowserUrl("content://com.android.contacts/contacts"))
        assertFalse(isBrowserUrl("mh.example.org"))
        assertFalse(isBrowserUrl(""))
        assertFalse(isBrowserUrl("https:/mh.example.org"))
        assertFalse(isBrowserUrl("httpsx://mh.example.org"))
    }

    @Test
    fun openLaunchesViewIntentForWebUrl() {
        var dismissed = false
        composeTestRule.setContent {
            BellhopTheme {
                ConfirmOpenUrlDialog(url = "https://mh.example.org/", onDismiss = { dismissed = true })
            }
        }

        composeTestRule.onNodeWithTag("member-url-open").performClick()

        val started = nextStartedActivity()
        assertEquals(Intent.ACTION_VIEW, started?.action)
        assertEquals("https://mh.example.org/", started?.data?.toString())
        assertTrue(started?.hasCategory(Intent.CATEGORY_BROWSABLE) == true)
        assertTrue(dismissed)
    }

    @Test
    fun openRefusesNonWebScheme() {
        var dismissed = false
        composeTestRule.setContent {
            BellhopTheme {
                ConfirmOpenUrlDialog(url = "intent://scan/#Intent;scheme=zxing;end", onDismiss = { dismissed = true })
            }
        }

        composeTestRule.onNodeWithTag("member-url-open").performClick()

        assertNull(nextStartedActivity())
        // The dialog still closes: the tap was answered, nothing was launched.
        assertTrue(dismissed)
    }
}
