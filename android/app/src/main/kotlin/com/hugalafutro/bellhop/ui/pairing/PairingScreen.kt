package com.hugalafutro.bellhop.ui.pairing

import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.google.zxing.client.android.Intents
import com.hugalafutro.bellhop.R
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions

/**
 * PairingScreen is the unlinked-state entry point (plan A2). The single Front
 * Desk pairing string carries the URL, code, and name, so that is the only thing
 * asked for — supplied two equal ways (plan section 3.2): scan the QR shown in
 * the "Paired devices" panel, or paste the copyable string beside it. Both feed
 * the same parser, after which the Front Desk it points at is shown for
 * confirmation before Pair.
 */
@Composable
fun PairingScreen(
    state: PairingUiState,
    onPastePayload: (String) -> Unit,
    onLabelChange: (String) -> Unit,
    onSubmit: () -> Unit,
    onScanUnavailable: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // ZXing's CaptureActivity handles the camera preview and requests the CAMERA
    // permission itself, so the launch is inert until Scan is tapped. A decoded
    // QR is the same JSON string the user would paste, so it just feeds
    // onPastePayload. A null result is either a user cancel (no-op) or a denied
    // CAMERA permission: ZXing finishes the latter with a MISSING_CAMERA_PERMISSION
    // extra rather than a decoded value, so surface it as a hint toward the paste
    // fallback instead of letting the failure look like a deliberate cancel.
    val scanLauncher =
        rememberLauncherForActivityResult(ScanContract()) { result ->
            val contents = result.contents
            when {
                contents != null -> onPastePayload(contents)
                result.originalIntent?.getBooleanExtra(
                    Intents.Scan.MISSING_CAMERA_PERMISSION,
                    false,
                ) == true -> onScanUnavailable()
            }
        }
    // A device with no camera hardware can't be handled from the result: ZXing
    // just shows a framework dialog and finishes with an empty, cancel-shaped
    // result, so guard the launch itself and route straight to the paste hint.
    val context = LocalContext.current
    val hasCamera =
        remember(context) {
            context.packageManager.hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY)
        }
    val scanPrompt = stringResource(R.string.pairing_scan_prompt)
    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(innerPadding)
                    .padding(24.dp)
                    .testTag("pairing-screen"),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = stringResource(R.string.pairing_title),
                style = MaterialTheme.typography.headlineSmall,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.testTag("pairing-title"),
            )
            Text(
                text = stringResource(R.string.pairing_subtitle),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            OutlinedButton(
                onClick = {
                    if (!hasCamera) {
                        onScanUnavailable()
                        return@OutlinedButton
                    }
                    scanLauncher.launch(
                        ScanOptions().apply {
                            setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                            setPrompt(scanPrompt)
                            setBeepEnabled(false)
                            setOrientationLocked(false)
                        },
                    )
                },
                enabled = !state.busy,
                modifier = Modifier.fillMaxWidth().testTag("pairing-scan"),
            ) {
                Text(stringResource(R.string.pairing_scan))
            }

            Text(
                text = stringResource(R.string.pairing_or),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )

            OutlinedTextField(
                value = state.pasteText,
                onValueChange = onPastePayload,
                label = { Text(stringResource(R.string.pairing_paste_label)) },
                placeholder = { Text(stringResource(R.string.pairing_paste_hint)) },
                minLines = 2,
                isError = state.error == PairingError.BadString,
                modifier = Modifier.fillMaxWidth().testTag("pairing-paste"),
            )

            // Once the string is read, confirm which Front Desk it points at and
            // let the operator name this device before pairing.
            if (state.parsed) {
                Text(
                    text = stringResource(R.string.pairing_target, state.fdName, state.fdUrl),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.testTag("pairing-target"),
                )
                OutlinedTextField(
                    value = state.label,
                    onValueChange = onLabelChange,
                    label = { Text(stringResource(R.string.pairing_label_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth().testTag("pairing-label"),
                )
            }

            state.error?.let { err ->
                val message =
                    when (err) {
                        PairingError.BadString -> stringResource(R.string.pairing_error_bad_string)
                        PairingError.InvalidCode -> stringResource(R.string.pairing_error_invalid_code)
                        PairingError.ScanUnavailable -> stringResource(R.string.pairing_error_scan_unavailable)
                        is PairingError.Message -> err.text
                    }
                Text(
                    text = message,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.testTag("pairing-error"),
                )
            }

            Button(
                onClick = onSubmit,
                enabled = state.canSubmit,
                modifier = Modifier.fillMaxWidth().testTag("pairing-submit"),
            ) {
                if (state.busy) {
                    CircularProgressIndicator(
                        modifier = Modifier.testTag("pairing-busy"),
                        strokeWidth = 2.dp,
                    )
                } else {
                    Text(stringResource(R.string.pairing_submit))
                }
            }
        }
    }
}
