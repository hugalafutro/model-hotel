package com.hugalafutro.bellhop.ui.pairing

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class PairingScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private fun render(
        state: PairingUiState,
        onSubmit: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            BellhopTheme {
                PairingScreen(
                    state = state,
                    onPastePayload = {},
                    onLabelChange = {},
                    onSubmit = onSubmit,
                    onScanUnavailable = {},
                )
            }
        }
    }

    @Test
    fun disablesSubmitBeforeAStringIsParsed() {
        render(PairingUiState())
        composeTestRule.onNodeWithTag("pairing-title").assertIsDisplayed()
        composeTestRule.onNodeWithTag("pairing-paste").assertIsDisplayed()
        composeTestRule.onNodeWithTag("pairing-submit").assertIsNotEnabled()
    }

    @Test
    fun offersScanAlongsidePaste() {
        // The scan path is a first-class equal to paste (plan 3.2): its button
        // is present and usable from the clean unlinked state.
        render(PairingUiState())
        composeTestRule.onNodeWithTag("pairing-scan").assertIsDisplayed().assertIsEnabled()
    }

    @Test
    fun disablesScanWhilePairing() {
        // A scan mid-pair would race the in-flight POST, so it is disabled while
        // busy, matching the paste field's frozen-form behaviour.
        render(
            PairingUiState(
                pasteText = "{...}",
                fdUrl = "https://h",
                code = "ABC",
                parsed = true,
                busy = true,
            ),
        )
        composeTestRule.onNodeWithTag("pairing-scan").assertIsNotEnabled()
    }

    @Test
    fun showsTargetAndEnablesSubmitWhenParsed() {
        var submitted = false
        render(
            PairingUiState(
                pasteText = "{...}",
                fdUrl = "https://front-desk.example",
                code = "ABC",
                fdName = "Home",
                label = "Pixel",
                parsed = true,
            ),
            onSubmit = { submitted = true },
        )
        composeTestRule.onNodeWithTag("pairing-target").assertIsDisplayed()
        composeTestRule
            .onNodeWithTag("pairing-submit")
            .performScrollTo()
            .assertIsEnabled()
            .performClick()
        assertTrue(submitted)
    }

    @Test
    fun showsInvalidCodeError() {
        render(
            PairingUiState(
                pasteText = "{...}",
                fdUrl = "https://h",
                code = "BAD",
                parsed = true,
                error = PairingError.InvalidCode,
            ),
        )
        composeTestRule.onNodeWithTag("pairing-error").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun showsBadStringError() {
        render(PairingUiState(pasteText = "junk", error = PairingError.BadString))
        composeTestRule.onNodeWithTag("pairing-error").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithTag("pairing-submit").assertIsNotEnabled()
    }

    @Test
    fun showsScanUnavailableError() {
        // A denied camera permission surfaces as an error hint rather than a
        // silent no-op, so the operator is pointed at the paste fallback.
        render(PairingUiState(error = PairingError.ScanUnavailable))
        composeTestRule.onNodeWithTag("pairing-error").performScrollTo().assertIsDisplayed()
    }
}
