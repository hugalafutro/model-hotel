package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.ui.theme.BellhopTheme

/**
 * DashboardScreen is the linked-state home. For the A2 pairing slice it just
 * confirms the link (Front Desk name + role) and offers Unlink; the read-only
 * member list, events, and alerts land in the next slice.
 */
@Composable
fun DashboardScreen(
    link: LinkState.Linked,
    onUnlink: () -> Unit,
    unlinking: Boolean,
    modifier: Modifier = Modifier,
    unlinkFailed: Boolean = false,
    onDismissUnlinkError: () -> Unit = {},
    onForceUnlink: () -> Unit = {},
) {
    var confirmUnlink by remember { mutableStateOf(false) }

    // The remote revoke couldn't reach Front Desk (or the token can't be read to
    // revoke at all). The device is still linked and nothing was cleared, so
    // offer a retry AND an "unlink anyway" escape: with a dead/unreachable token a
    // retry can loop forever, so the operator needs a way to clear locally (and is
    // told to revoke on Front Desk) rather than being stranded on this screen.
    if (unlinkFailed) {
        AlertDialog(
            onDismissRequest = onDismissUnlinkError,
            title = { Text(stringResource(R.string.dashboard_unlink_failed_title)) },
            text = { Text(stringResource(R.string.dashboard_unlink_failed_body)) },
            confirmButton = {
                TextButton(
                    enabled = !unlinking,
                    onClick = {
                        onDismissUnlinkError()
                        onUnlink()
                    },
                    modifier = Modifier.testTag("dashboard-unlink-retry"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink_retry))
                }
            },
            dismissButton = {
                TextButton(
                    enabled = !unlinking,
                    onClick = {
                        onDismissUnlinkError()
                        onForceUnlink()
                    },
                    modifier = Modifier.testTag("dashboard-unlink-force"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink_force))
                }
            },
        )
    }

    if (confirmUnlink) {
        AlertDialog(
            onDismissRequest = { confirmUnlink = false },
            title = { Text(stringResource(R.string.dashboard_unlink_confirm_title)) },
            text = {
                Text(
                    stringResource(
                        R.string.dashboard_unlink_confirm_body,
                        link.fdName.ifBlank { link.fdUrl },
                    ),
                )
            },
            confirmButton = {
                TextButton(
                    enabled = !unlinking,
                    onClick = {
                        confirmUnlink = false
                        onUnlink()
                    },
                    modifier = Modifier.testTag("dashboard-unlink-confirm"),
                ) {
                    Text(stringResource(R.string.dashboard_unlink))
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmUnlink = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    Scaffold(modifier = modifier.fillMaxSize()) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(24.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = stringResource(R.string.app_name),
                style = MaterialTheme.typography.displayMedium,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.testTag("dashboard-title"),
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.dashboard_linked, link.fdName.ifBlank { link.fdUrl }),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.testTag("dashboard-linked"),
            )
            Text(
                text = stringResource(R.string.dashboard_role, link.role),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.height(24.dp))
            OutlinedButton(
                onClick = { confirmUnlink = true },
                enabled = !unlinking,
                modifier = Modifier.testTag("dashboard-unlink"),
            ) {
                Text(stringResource(R.string.dashboard_unlink))
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun DashboardScreenPreview() {
    BellhopTheme {
        DashboardScreen(
            link =
                LinkState.Linked(
                    fdUrl = "http://10.0.2.2:8080",
                    fdName = "Home Front Desk",
                    role = "operator",
                    deviceId = "dev-1",
                    label = "Pixel 8",
                ),
            onUnlink = {},
            unlinking = false,
        )
    }
}
