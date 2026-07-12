package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.HealthStatus

// Small fleet-rendering pieces shared by the dashboard cards and the member
// detail screen, so both spell a member's health and badges the same way.

/** StatusBanner is the stale-data warning strip: refresh failed or token dead. */
@Composable
internal fun StatusBanner(
    text: String,
    tag: String,
    modifier: Modifier = Modifier,
) {
    Surface(
        color = MaterialTheme.colorScheme.errorContainer,
        contentColor = MaterialTheme.colorScheme.onErrorContainer,
        shape = MaterialTheme.shapes.medium,
        modifier = modifier.fillMaxWidth().padding(bottom = 12.dp).testTag(tag),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(12.dp),
        )
    }
}

/** Pill is a compact rounded badge (Primary, Drained). */
@Composable
internal fun Pill(
    text: String,
    container: Color,
    content: Color,
    tag: String,
    modifier: Modifier = Modifier,
) {
    Surface(
        color = container,
        contentColor = content,
        shape = RoundedCornerShape(999.dp),
        modifier = modifier.testTag(tag),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
        )
    }
}

/** healthColor maps the poller's view to the shared up/down/unknown color. */
@Composable
internal fun healthColor(health: HealthStatus): Color =
    when {
        !health.known -> MaterialTheme.colorScheme.outline
        health.healthy -> MaterialTheme.colorScheme.tertiary
        else -> MaterialTheme.colorScheme.error
    }

/** healthLabel is the one-line human reading of the poller's view. */
@Composable
internal fun healthLabel(health: HealthStatus): String =
    when {
        !health.known -> stringResource(R.string.member_health_unknown)
        health.healthy -> stringResource(R.string.member_health_up, health.latencyMs)
        else -> stringResource(R.string.member_health_down)
    }
