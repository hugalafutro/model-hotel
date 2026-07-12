package com.hugalafutro.bellhop.ui.lock

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.ui.theme.BellhopTheme

/**
 * LockScreen is the local access gate shown when the idle window has lapsed. It
 * covers the linked UI entirely so no fleet data is visible until the user
 * authenticates. [onUnlock] fires the BiometricPrompt; it's invoked once
 * automatically on first show and again from the button, which is the retry path
 * after a cancelled or failed prompt. The screen never authenticates by itself —
 * it only ever asks the host to prompt, and stays put until the host clears the
 * lock on success.
 */
@Composable
fun LockScreen(
    onUnlock: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // Prompt straight away so an unlock is one glance/touch, not two taps. The
    // button below is the retry once the sheet has been dismissed.
    LaunchedEffect(Unit) { onUnlock() }
    Scaffold(modifier = modifier.fillMaxSize().testTag("lock-screen")) { innerPadding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(horizontal = 32.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(
                imageVector = Icons.Filled.Lock,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.size(48.dp),
            )
            Text(
                text = stringResource(R.string.lock_title),
                style = MaterialTheme.typography.titleLarge,
                color = MaterialTheme.colorScheme.primary,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(R.string.lock_subtitle),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
            Button(onClick = onUnlock, modifier = Modifier.testTag("lock-unlock")) {
                Text(stringResource(R.string.lock_unlock))
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun LockScreenPreview() {
    BellhopTheme {
        LockScreen(onUnlock = {})
    }
}
