package com.hugalafutro.bellhop.ui.common

import android.content.ClipData
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.ClipEntry
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.launch

/**
 * rememberCopyText returns a `(text, confirmation) -> Unit` that puts [text] on
 * the system clipboard as plain text and confirms the otherwise-silent act with
 * a short toast of [confirmation].
 *
 * The copy itself is fire-and-forget: [androidx.compose.ui.platform.Clipboard]
 * exposes a suspend write, so it is launched into the composition's scope
 * while the toast shows straight away from the caller's event handler. The
 * toast is what the user sees, and the write lands within the same frame.
 */
@Composable
fun rememberCopyText(): (text: String, confirmation: String) -> Unit {
    val clipboard = LocalClipboard.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    return remember(clipboard, context, scope) {
        { text, confirmation ->
            scope.launch { clipboard.setClipEntry(ClipEntry(ClipData.newPlainText(null, text))) }
            Toast.makeText(context, confirmation, Toast.LENGTH_SHORT).show()
        }
    }
}
