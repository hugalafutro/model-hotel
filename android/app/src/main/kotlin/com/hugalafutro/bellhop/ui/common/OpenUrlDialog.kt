package com.hugalafutro.bellhop.ui.common

import android.content.Intent
import android.net.Uri
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import com.hugalafutro.bellhop.R

/**
 * ConfirmOpenUrlDialog is the "open this member's address?" confirmation shared
 * by the dashboard card and the member-detail ledger: leaving the app for a
 * browser must be a deliberate second tap, never the same tap that could also
 * be a mis-tap on the row itself.
 */
@Composable
fun ConfirmOpenUrlDialog(
    url: String,
    onDismiss: () -> Unit,
    // Dialog title; defaults to the member-address wording. The footer's GitHub
    // link passes a generic "Open link" instead.
    title: String = stringResource(R.string.member_url_title),
) {
    val context = LocalContext.current
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Text(
                text = url,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.testTag("member-url-dialog-text"),
            )
        },
        confirmButton = {
            TextButton(
                onClick = {
                    // ACTION_VIEW lets Android resolve the URL (browser or a
                    // matching app), showing its own chooser when several match.
                    // Only web URLs get that far: the address comes from the
                    // paired Front Desk, and an intent:/javascript:/file: value
                    // must not be able to launch anything else on the phone.
                    // runCatching: a device with nothing that can open it must
                    // not crash the app.
                    if (isBrowserUrl(url)) {
                        runCatching {
                            // BROWSABLE narrows the resolver to handlers that
                            // accept links from outside their own app.
                            val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
                            intent.addCategory(Intent.CATEGORY_BROWSABLE)
                            context.startActivity(intent)
                        }
                    }
                    onDismiss()
                },
                modifier = Modifier.testTag("member-url-open"),
            ) {
                Text(stringResource(R.string.member_url_open))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.common_cancel))
            }
        },
    )
}

/**
 * isBrowserUrl reports whether [url] is something a browser is meant to open:
 * an `http` or `https` URL and nothing else. Checked as a plain prefix on the
 * exact string that would be launched, rather than through `Uri.parse`, so what
 * is allowed cannot depend on how a parser reads an odd address.
 */
internal fun isBrowserUrl(url: String): Boolean =
    url.startsWith("http://", ignoreCase = true) || url.startsWith("https://", ignoreCase = true)
