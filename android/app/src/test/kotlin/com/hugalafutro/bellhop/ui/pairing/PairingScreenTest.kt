package com.hugalafutro.bellhop.ui.pairing

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.hugalafutro.bellhop.ui.theme.BellhopTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class PairingScreenTest {
    @get:Rule
    val composeTestRule = createComposeRule()

    // A stand-in for the camera preview: two buttons that fire the same
    // callbacks the real QrScanner does, so the pairing wiring can be driven
    // deterministically without a camera.
    private val fakeScanner: @Composable (onScanned: (String) -> Unit, onCameraError: () -> Unit) -> Unit =
        { onScanned, onCameraError ->
            Column {
                Button(
                    onClick = { onScanned("PAIR_PAYLOAD") },
                    modifier = Modifier.testTag("fake-scan-ok"),
                ) { Text("scanned") }
                Button(
                    onClick = onCameraError,
                    modifier = Modifier.testTag("fake-scan-error"),
                ) { Text("camera error") }
            }
        }

    private fun render(
        state: PairingUiState,
        onSubmit: () -> Unit = {},
        onScanUnavailable: () -> Unit = {},
        onPastePayload: (String) -> Unit = {},
        scanner: @Composable (onScanned: (String) -> Unit, onCameraError: () -> Unit) -> Unit = { _, _ -> },
    ) {
        composeTestRule.setContent {
            BellhopTheme {
                PairingScreen(
                    state = state,
                    onPastePayload = onPastePayload,
                    onLabelChange = {},
                    onSubmit = onSubmit,
                    onScanUnavailable = onScanUnavailable,
                    scanner = scanner,
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
        // A camera that could not be opened surfaces as an error hint rather than
        // a silent no-op, so the operator is pointed at the paste fallback.
        render(PairingUiState(error = PairingError.ScanUnavailable))
        composeTestRule.onNodeWithTag("pairing-error").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun tappingScanOpensScanner() {
        render(PairingUiState(), scanner = fakeScanner)
        composeTestRule.onNodeWithTag("pairing-scan").performClick()
        composeTestRule.onNodeWithTag("fake-scan-ok").assertIsDisplayed()
    }

    @Test
    fun scannedQrFeedsTheSameParserAsPaste() {
        // A decoded QR is routed through onPastePayload, exactly as pasting the
        // string would be, and the form returns.
        var pasted: String? = null
        render(PairingUiState(), onPastePayload = { pasted = it }, scanner = fakeScanner)
        composeTestRule.onNodeWithTag("pairing-scan").performClick()
        composeTestRule.onNodeWithTag("fake-scan-ok").performClick()
        assertEquals("PAIR_PAYLOAD", pasted)
        composeTestRule.onNodeWithTag("pairing-scan").assertIsDisplayed()
    }

    @Test
    fun cameraErrorSurfacesScanUnavailable() {
        // A camera the preview could not open reports through onCameraError, which
        // the screen turns into the paste hint rather than a silent return.
        var scanUnavailable = false
        render(PairingUiState(), onScanUnavailable = { scanUnavailable = true }, scanner = fakeScanner)
        composeTestRule.onNodeWithTag("pairing-scan").performClick()
        composeTestRule.onNodeWithTag("fake-scan-error").performClick()
        assertTrue(scanUnavailable)
        composeTestRule.onNodeWithTag("pairing-scan").assertIsDisplayed()
    }
}
