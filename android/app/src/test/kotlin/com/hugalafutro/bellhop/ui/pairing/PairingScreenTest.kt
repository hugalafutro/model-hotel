package com.hugalafutro.bellhop.ui.pairing

import android.content.pm.PackageManager
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf

@RunWith(RobolectricTestRunner::class)
class PairingScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    private fun render(
        state: PairingUiState,
        onSubmit: () -> Unit = {},
        onScanUnavailable: () -> Unit = {},
    ) {
        composeTestRule.setContent {
            BellhopTheme {
                PairingScreen(
                    state = state,
                    onPastePayload = {},
                    onLabelChange = {},
                    onSubmit = onSubmit,
                    onScanUnavailable = onScanUnavailable,
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

    @Test
    fun scanShortCircuitsWhenNoCamera() {
        // A device with no camera can't be handled from the scan result (ZXing
        // finishes cancel-shaped), so tapping Scan must route straight to the
        // paste hint instead of launching a scanner that can't open.
        shadowOf(RuntimeEnvironment.getApplication().packageManager)
            .setSystemFeature(PackageManager.FEATURE_CAMERA_ANY, false)
        var scanUnavailable = false
        render(PairingUiState(), onScanUnavailable = { scanUnavailable = true })
        composeTestRule.onNodeWithTag("pairing-scan").performClick()
        assertTrue(scanUnavailable)
    }

    @Test
    fun deniedPermissionEmptyScanIsFailure() {
        assertTrue(emptyScanIsFailure(missingPermission = true, elapsedSinceLaunchMs = 60_000))
    }

    @Test
    fun fastEmptyScanIsCameraOpenFailure() {
        // A camera that could not open returns almost immediately with no extra;
        // treat that as a failure so it surfaces the paste hint.
        assertTrue(emptyScanIsFailure(missingPermission = false, elapsedSinceLaunchMs = 200))
    }

    @Test
    fun slowEmptyScanIsCancelNotFailure() {
        // Time spent in the scanner UI before an empty result is a deliberate
        // cancel, which stays a silent no-op.
        assertFalse(
            emptyScanIsFailure(
                missingPermission = false,
                elapsedSinceLaunchMs = SCAN_OPEN_FAILURE_WINDOW_MS + 1,
            ),
        )
    }
}
