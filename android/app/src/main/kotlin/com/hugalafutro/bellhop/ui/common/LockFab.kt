package com.hugalafutro.bellhop.ui.common

import android.widget.Toast
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material3.FloatingActionButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R

/**
 * LockFab is the bottom-right "lock now" control on the dashboard, shown only
 * when the app lock is enabled in Settings. It is styled to match the
 * [ScrollToTopButton] small FAB, but a plain tap must not lock: that would fire
 * on any stray brush against the corner. So a tap only shows a "hold to lock"
 * toast and a long-press actually locks, via [onLock].
 *
 * It is a hand-rolled small-FAB lookalike rather than a
 * [androidx.compose.material3.SmallFloatingActionButton] because that composable
 * exposes only onClick, and the tap/long-press split is the whole point; the
 * shape, colour and elevation come from [FloatingActionButtonDefaults] so it
 * still reads as the same control.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun LockFab(
    onLock: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val holdHint = stringResource(R.string.lock_fab_hold_hint)
    val label = stringResource(R.string.lock_fab_label)
    Surface(
        shape = FloatingActionButtonDefaults.smallShape,
        color = MaterialTheme.colorScheme.primaryContainer,
        contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
        shadowElevation = 6.dp,
        tonalElevation = 6.dp,
        modifier = modifier.size(40.dp).testTag("lock-fab"),
    ) {
        Box(
            contentAlignment = Alignment.Center,
            modifier =
                Modifier.combinedClickable(
                    onClick = { Toast.makeText(context, holdHint, Toast.LENGTH_SHORT).show() },
                    onLongClickLabel = label,
                    onLongClick = onLock,
                ),
        ) {
            Icon(
                imageVector = Icons.Filled.Lock,
                contentDescription = label,
            )
        }
    }
}
