package com.hugalafutro.bellhop.ui.dashboard

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.ProviderQuota
import com.hugalafutro.bellhop.data.QuotaBarMode
import com.hugalafutro.bellhop.data.QuotaData
import com.hugalafutro.bellhop.data.quotaBadgeLabel
import java.util.Locale
import kotlin.math.roundToInt

/**
 * QuotaBadgeRow is the dashboard's quota-badge strip: one tappable chip per
 * [quota] entry, showing the same short label [quotaBadgeLabel] computes for
 * the widget, so the two surfaces never drift on what a badge says. Scrolls
 * horizontally rather than wrapping, so a long provider list stays a single
 * line above the member list. Renders nothing when [quota] is empty (no
 * quota-capable providers configured) instead of an empty strip. A dead-key
 * entry ([ProviderQuota.available] == false) still renders and is still
 * tappable -- [onBadgeClick] carries [ProviderQuota.providerName] either
 * way -- because the detail sheet is what explains *why* it's unavailable;
 * swallowing the tap would strand the user looking at a bare "-".
 */
@Composable
fun QuotaBadgeRow(
    quota: List<ProviderQuota>,
    mode: QuotaBarMode,
    onBadgeClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (quota.isEmpty()) return
    Row(
        modifier = modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        quota.forEach { pq ->
            QuotaBadgeChip(pq = pq, mode = mode, onClick = { onBadgeClick(pq.providerName) })
        }
    }
}

/**
 * QuotaBadgeChip is one badge: the provider's own name (badge identity is
 * always the wire's provider *name*, never its type or a list index -- see
 * global-constraints) plus [quotaBadgeLabel]'s short value. Availability
 * only changes the chip's tint (dimmed, matching a disabled-looking control)
 * -- it stays a live tap target either way.
 */
@Composable
private fun QuotaBadgeChip(
    pq: ProviderQuota,
    mode: QuotaBarMode,
    onClick: () -> Unit,
) {
    val available = pq.available
    val bg = if (available) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surfaceVariant
    val fg =
        if (available) MaterialTheme.colorScheme.onSecondaryContainer else MaterialTheme.colorScheme.onSurfaceVariant
    Surface(
        onClick = onClick,
        shape = MaterialTheme.shapes.large,
        color = bg,
        contentColor = fg,
        modifier = Modifier.testTag("quota-badge-${pq.providerName}"),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
        ) {
            Text(
                text = pq.providerName,
                style = MaterialTheme.typography.labelSmall,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = quotaBadgeLabel(pq, mode),
                style = MaterialTheme.typography.labelMedium,
            )
        }
    }
}

/**
 * QuotaDetailSheet is the badge's tap target: [pq]'s full name, the same
 * headline [quotaBadgeLabel] the chip shows (so the sheet never contradicts
 * the badge that opened it), and a handful of per-type supporting rows --
 * data/shape parity with the web dashboard's quota/balance modals
 * (web/src/components/QuotaBadge.tsx + the web quota/balance modals), not a
 * line-for-line port. An unavailable/payload-less quota ([ProviderQuota.data]
 * null or [ProviderQuota.available] false) collapses to a short "unavailable"
 * line instead of the supporting rows, mirroring the badge's own "-"
 * fallback.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QuotaDetailSheet(
    pq: ProviderQuota,
    mode: QuotaBarMode,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState()
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        modifier = Modifier.testTag("quota-detail-sheet"),
    ) {
        Column(
            modifier =
                Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 20.dp)
                    .padding(bottom = 24.dp),
        ) {
            Text(text = pq.providerName, style = MaterialTheme.typography.titleLarge)
            Spacer(modifier = Modifier.height(8.dp))
            val data = pq.data
            if (!pq.available || data == null) {
                Text(
                    text = stringResource(R.string.quota_unavailable),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag("quota-detail-unavailable"),
                )
            } else {
                Text(
                    text = quotaBadgeLabel(pq, mode),
                    style = MaterialTheme.typography.headlineSmall,
                    color = MaterialTheme.colorScheme.primary,
                )
                Spacer(modifier = Modifier.height(12.dp))
                QuotaDetailRows(data)
            }
        }
    }
}

/** QuotaDetailRow is one label/value line in the detail sheet. */
@Composable
private fun QuotaDetailRow(
    label: String,
    value: String,
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(text = value, style = MaterialTheme.typography.bodyMedium)
    }
}

/**
 * QuotaDetailRows dispatches on [data]'s variant to the per-type rows below.
 * Each shows only the handful of fields the matching web modal (or, for the
 * two types with no dedicated modal -- DeepSeek, Ollama Cloud -- the badge's
 * own tooltip) actually surfaces; the headline above already carries the
 * fraction/amount every type's badge shows, so these add context rather than
 * repeating it.
 */
@Composable
private fun QuotaDetailRows(data: QuotaData) {
    val yes = stringResource(R.string.quota_yes)
    val no = stringResource(R.string.quota_no)
    when (data) {
        is QuotaData.NanoGpt -> {
            QuotaDetailRow(
                stringResource(R.string.quota_field_status),
                stringResource(if (data.active) R.string.quota_status_active else R.string.quota_status_inactive),
            )
            if (data.period.currentPeriodEnd.isNotBlank()) {
                QuotaDetailRow(
                    stringResource(R.string.quota_field_period_end),
                    isoDatePart(data.period.currentPeriodEnd),
                )
            }
            QuotaDetailRow(stringResource(R.string.quota_field_allow_overage), if (data.allowOverage) yes else no)
            data.dailyInputTokens?.let { info ->
                QuotaDetailRow(
                    stringResource(R.string.quota_field_daily_input_tokens),
                    "${tokenAmount(info.used)}/${tokenAmount(data.limits.dailyInputTokens)}",
                )
            }
            data.dailyImages?.let { info ->
                QuotaDetailRow(
                    stringResource(R.string.quota_field_daily_images),
                    "${tokenAmount(info.used)}/${tokenAmount(data.limits.dailyImages)}",
                )
            }
        }
        is QuotaData.ZaiCoding -> {
            if (data.data.level.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_plan), data.data.level)
            }
            val mcpLimit = data.data.limits.find { it.type == "TIME_LIMIT" && it.unit == 5 }
            mcpLimit?.let {
                QuotaDetailRow(
                    stringResource(R.string.quota_field_mcp_quota),
                    "${it.percentage.roundToInt()}%",
                )
            }
        }
        is QuotaData.KimiCode -> {
            val level = data.user.membership.level
            if (level.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_membership), level)
            }
            val parallel = data.parallel.limit
            if (parallel.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_parallel_limit), parallel)
            }
            val total = data.totalQuota
            if (total.remaining.isNotBlank() || total.limit.isNotBlank()) {
                QuotaDetailRow(
                    stringResource(R.string.quota_field_total_quota),
                    "${total.remaining.ifBlank { "-" }}/${total.limit.ifBlank { "-" }}",
                )
            }
        }
        is QuotaData.MiniMax -> {
            data.modelRemains.forEach { entry ->
                QuotaDetailRow(
                    entry.modelName,
                    stringResource(
                        if (entry.currentIntervalStatus == 3) {
                            R.string.quota_status_not_in_plan
                        } else {
                            R.string.quota_status_in_plan
                        },
                    ),
                )
            }
        }
        // DeepSeek has no dedicated web modal -- only the badge tooltip, which
        // is exactly the headline above -- so no supporting rows.
        is QuotaData.DeepSeek -> Unit
        is QuotaData.OpenRouter -> {
            if (data.creditsTotal > 0) {
                QuotaDetailRow(stringResource(R.string.quota_field_credits_total), usd(data.creditsTotal))
            }
            QuotaDetailRow(stringResource(R.string.quota_field_credits_used), usd(data.creditsUsed))
            QuotaDetailRow(stringResource(R.string.quota_field_usage_today), usd(data.usageDaily))
            QuotaDetailRow(stringResource(R.string.quota_field_usage_month), usd(data.usageMonthly))
            QuotaDetailRow(stringResource(R.string.quota_field_free_tier), if (data.isFreeTier) yes else no)
        }
        is QuotaData.OllamaCloud -> {
            if (data.subscriptionPeriodEnd.valid) {
                QuotaDetailRow(
                    stringResource(R.string.quota_field_subscription_end),
                    isoDatePart(data.subscriptionPeriodEnd.time),
                )
            }
            if (data.suspendedAt.valid) {
                QuotaDetailRow(stringResource(R.string.quota_field_suspended), isoDatePart(data.suspendedAt.time))
            }
        }
        is QuotaData.NeuralWatt -> {
            QuotaDetailRow(
                stringResource(R.string.quota_field_balance_remaining),
                usd(data.balance.creditsRemainingUsd),
            )
            if (data.balance.totalCreditsUsd > 0) {
                QuotaDetailRow(stringResource(R.string.quota_field_balance_total), usd(data.balance.totalCreditsUsd))
            }
            if (data.subscription.plan.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_plan), data.subscription.plan)
            }
            if (data.subscription.status.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_status), data.subscription.status)
            }
        }
    }
}

/** isoDatePart trims an RFC3339 timestamp to its date portion; a value that
 * isn't RFC3339-shaped (no "T") is returned as-is rather than dropped, so a
 * foreign format still shows something instead of going blank. */
private fun isoDatePart(iso: String): String {
    val t = iso.indexOf('T')
    return if (t > 0) iso.substring(0, t) else iso
}

/** usd formats a dollar amount with a fixed Locale, mirroring
 * [com.hugalafutro.bellhop.data]'s widget-facing formatters, so the sheet
 * reads the same on every device regardless of the user's locale. */
private fun usd(v: Double): String = "$" + String.format(Locale.US, "%.2f", v)

/** tokenAmount formats a token/image count with locale-aware digit grouping;
 * a null limit (no cap set) renders as "∞", mirroring the web modal's
 * fallback for an unset per-period limit. */
private fun tokenAmount(n: Long?): String = if (n == null) "∞" else String.format(Locale.US, "%,d", n)
