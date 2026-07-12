package com.hugalafutro.bellhop.ui.common

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.TrafficPoint
import com.hugalafutro.bellhop.ui.theme.SeverityErrorBg
import com.hugalafutro.bellhop.ui.theme.SeverityErrorFg
import com.hugalafutro.bellhop.ui.theme.SeverityInfoBg
import com.hugalafutro.bellhop.ui.theme.SeverityInfoFg
import com.hugalafutro.bellhop.ui.theme.SeveritySuccessBg
import com.hugalafutro.bellhop.ui.theme.SeveritySuccessFg
import com.hugalafutro.bellhop.ui.theme.SeverityWarnBg
import com.hugalafutro.bellhop.ui.theme.SeverityWarnFg

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

/**
 * Pill is a compact rounded badge (Primary, Drained). Pass [onClick] to make it
 * tappable (e.g. a severity pill that copies its event); left null it is inert.
 */
@Composable
internal fun Pill(
    text: String,
    container: Color,
    content: Color,
    tag: String,
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
) {
    Surface(
        color = container,
        contentColor = content,
        shape = RoundedCornerShape(999.dp),
        modifier =
            modifier
                .testTag(tag)
                .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
        )
    }
}

/**
 * FilterPill is a compact selectable filter chip: a rounded pill that fills with
 * the accent when [selected]. Deliberately lighter than Material's FilterChip
 * (smaller font, tighter padding). Meant to sit in an equal-weight Row so a set
 * of them reads as a segmented control that always fits one phone-width line
 * instead of wrapping or scrolling off-screen — the caller passes a weight
 * [modifier] and the label centers and ellipsizes within its share.
 */
@Composable
internal fun FilterPill(
    text: String,
    selected: Boolean,
    onClick: () -> Unit,
    tag: String,
    modifier: Modifier = Modifier,
) {
    val container =
        if (selected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceVariant
    val content =
        if (selected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant
    Surface(
        onClick = onClick,
        color = container,
        contentColor = content,
        shape = RoundedCornerShape(999.dp),
        modifier = modifier.testTag(tag),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            textAlign = TextAlign.Center,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 6.dp),
        )
    }
}

/**
 * severityColors maps an event/alert severity onto the (container, content)
 * badge palette using the conventional red/orange/blue/green severity colours
 * (see Color.kt), not the brand's warm roles — a status level must read
 * unambiguously. Shared so the events log and the alert catalog dot the same
 * severity the same colour. Unknown severities fall back to info (blue).
 */
internal fun severityColors(severity: String): Pair<Color, Color> =
    when (severity) {
        "success" -> SeveritySuccessBg to SeveritySuccessFg
        "warning" -> SeverityWarnBg to SeverityWarnFg
        "error" -> SeverityErrorBg to SeverityErrorFg
        else -> SeverityInfoBg to SeverityInfoFg
    }

/**
 * TrafficChart is a plain Canvas bar chart, one bar per bucket oldest to newest:
 * bar height is that bucket's requests against the window maximum, with the
 * error subset overlaid from the baseline in the error colour. Shared by the
 * dashboard card sparkline (short, via a height modifier) and the member-detail
 * graph (tall). No chart library by design (plan section 5.4). The caller sets
 * the height through [modifier].
 */
@Composable
internal fun TrafficChart(
    points: List<TrafficPoint>,
    modifier: Modifier = Modifier,
) {
    val barColor = MaterialTheme.colorScheme.primary
    val errorColor = MaterialTheme.colorScheme.error
    val idleColor = MaterialTheme.colorScheme.outlineVariant
    Canvas(modifier = modifier.fillMaxWidth()) {
        val max = points.maxOf { it.requests }.coerceAtLeast(1)
        val gap = 3.dp.toPx()
        val barWidth = (size.width - gap * (points.size - 1)) / points.size
        val stub = 2.dp.toPx()
        points.forEachIndexed { i, p ->
            val x = i * (barWidth + gap)
            if (p.requests <= 0) {
                // Zero bucket: a faint stub so the timeline stays readable
                // instead of leaving holes.
                drawRect(idleColor, Offset(x, size.height - stub), Size(barWidth, stub))
                return@forEachIndexed
            }
            val h = (size.height * p.requests / max).coerceAtLeast(stub)
            drawRect(barColor, Offset(x, size.height - h), Size(barWidth, h))
            if (p.errors > 0) {
                // Errors are a subset of requests, so the overlay shares the
                // scale and can never outgrow its bar.
                val eh = (size.height * p.errors / max).coerceAtLeast(stub)
                drawRect(errorColor, Offset(x, size.height - eh), Size(barWidth, eh))
            }
        }
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
