package com.hugalafutro.bellhop.ui.pairing

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
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R

/**
 * PairingScreen is the unlinked-state entry point (plan A2). The pairing string
 * from the Front Desk "Paired devices" panel carries the URL, code, and name, so
 * it is the only thing asked for: paste it (QR scanning arrives in a later
 * slice) and the Front Desk it points at is shown for confirmation before Pair.
 */
@Composable
fun PairingScreen(
    state: PairingUiState,
    onPastePayload: (String) -> Unit,
    onLabelChange: (String) -> Unit,
    onSubmit: () -> Unit,
    modifier: Modifier = Modifier,
) {
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
