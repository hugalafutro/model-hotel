package com.hugalafutro.bellhop.ui.dashboard

import androidx.annotation.StringRes
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.ProviderQuota
import com.hugalafutro.bellhop.data.QuotaBarMode
import com.hugalafutro.bellhop.data.QuotaData
import com.hugalafutro.bellhop.data.QuotaMeter
import com.hugalafutro.bellhop.data.QuotaMeterKind
import com.hugalafutro.bellhop.data.quotaBadgeLabel
import com.hugalafutro.bellhop.data.quotaMeters
import com.hugalafutro.bellhop.ui.common.TightTouchTarget
import com.hugalafutro.bellhop.ui.theme.SeverityErrorBg
import com.hugalafutro.bellhop.ui.theme.SeverityWarnBg
import com.hugalafutro.bellhop.ui.theme.quotaBrandColor
import java.util.Locale
import kotlin.math.roundToInt

/**
 * QuotaBadgeRow is the dashboard's quota-badge strip: one tappable chip per
 * [quota] entry, showing the same short label [quotaBadgeLabel] computes for
 * the widget, so the two surfaces never drift on what a badge says. Wraps onto
 * as many lines as the selection needs rather than scrolling: everything the
 * operator ticked in the configurator has to be visible without a gesture that
 * nothing on screen advertises. Renders nothing when [quota] is empty (no
 * quota-capable providers configured) instead of an empty strip. A dead-key
 * entry ([ProviderQuota.available] == false) still renders and is still
 * tappable -- [onBadgeClick] carries [ProviderQuota.providerName] either
 * way -- because the detail sheet is what explains *why* it's unavailable;
 * swallowing the tap would strand the user looking at a bare "-".
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun QuotaBadgeRow(
    quota: List<ProviderQuota>,
    mode: QuotaBarMode,
    onBadgeClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (quota.isEmpty()) return
    // TightTouchTarget is what makes the strip's own gaps the real gaps: every
    // chip is a clickable Surface, which Material measures at no less than
    // 48dp square, so a 17dp chip would otherwise sit in a 48dp row and the
    // arrangement below would be invisible between three badges' worth of air.
    // The chips sit shoulder to shoulder, so nothing else is nearby to have
    // its taps stolen by the tighter bounds.
    TightTouchTarget {
        FlowRow(
            modifier = modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(1.dp),
            verticalArrangement = Arrangement.spacedBy(1.dp),
        ) {
            quota.forEach { pq ->
                QuotaBadgeChip(pq = pq, mode = mode, onClick = { onBadgeClick(pq.providerName) })
            }
        }
    }
}

/**
 * QuotaBadgeChip is one badge: the provider's own name (badge identity is
 * always the wire's provider *name*, never its type or a list index -- see
 * global-constraints) plus [quotaBadgeLabel]'s short value. The chip wears its
 * provider's brand colour ([quotaBrandColor]) in the Model Hotel dashboard's
 * own proportions -- tinted fill, brand text, brand outline -- so a strip of
 * badges is scannable by colour rather than eight identical pills. An
 * unavailable badge drops to the scheme's neutrals (matching a disabled-looking
 * control) but stays a live tap target either way: the detail sheet is what
 * explains the "-".
 */
@Composable
private fun QuotaBadgeChip(
    pq: ProviderQuota,
    mode: QuotaBarMode,
    onClick: () -> Unit,
) {
    val available = pq.available
    // Read the scheme rather than isSystemInDarkTheme(): BellhopTheme takes
    // darkTheme as a parameter, so the system flag can disagree with what is
    // actually rendering (previews, tests, a future in-app theme switch).
    val dark = MaterialTheme.colorScheme.surface.luminance() < 0.5f
    val brand = quotaBrandColor(pq.type, dark)
    // Alphas mirror the web pill (web/src/index.css .sidebar-quota-pill):
    // 15% fill, 40% border, full-strength text.
    val bg = if (available) brand.copy(alpha = 0.15f) else MaterialTheme.colorScheme.surfaceVariant
    val fg = if (available) brand else MaterialTheme.colorScheme.onSurfaceVariant
    val border =
        if (available) brand.copy(alpha = 0.4f) else MaterialTheme.colorScheme.outline
    Surface(
        onClick = onClick,
        shape = MaterialTheme.shapes.extraSmall,
        color = bg,
        contentColor = fg,
        border = BorderStroke(1.dp, border),
        modifier = Modifier.testTag("quota-badge-${pq.providerName}"),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(3.dp),
            // As near to nothing as the border allows: the strip is the densest
            // thing on the screen and every dp here is one the fleet list loses.
            // Horizontal keeps 3dp so the text isn't drawn onto the outline.
            modifier = Modifier.padding(horizontal = 3.dp, vertical = 1.dp),
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
            Spacer(modifier = Modifier.height(4.dp))
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
                val meters = quotaMeters(pq)
                if (meters.isNotEmpty()) {
                    Spacer(modifier = Modifier.height(8.dp))
                    var subject = ""
                    meters.forEach { meter ->
                        // MiniMax meters the same two windows per model, so the
                        // model name heads its own pair rather than being folded
                        // into every bar's label.
                        if (meter.subject != subject) {
                            subject = meter.subject
                            if (subject.isNotBlank()) {
                                Text(
                                    text = subject,
                                    style = MaterialTheme.typography.labelLarge,
                                    modifier = Modifier.padding(top = 6.dp),
                                )
                            }
                        }
                        QuotaMeterBar(meter = meter, mode = mode)
                    }
                }
                Spacer(modifier = Modifier.height(8.dp))
                QuotaDetailRows(data)
            }
        }
    }
}

/**
 * QuotaDetailRow is one label/value line in the detail sheet. The rule rides
 * above the row rather than below it, so a run of rows is separated without a
 * trailing rule hanging off the bottom of the sheet, and the first row is
 * fenced off from the bars above it.
 */
@Composable
private fun QuotaDetailRow(
    label: String,
    value: String,
) {
    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 5.dp),
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
            // Uncapped periods get a row instead of a bar: with no ceiling there
            // is nothing to fill, and "12,345/∞" is the whole reading.
            if (!isCapped(data.limits.dailyInputTokens)) {
                data.dailyInputTokens?.let { info ->
                    QuotaDetailRow(
                        stringResource(R.string.quota_field_daily_input_tokens),
                        "${tokenAmount(info.used)}/${tokenAmount(data.limits.dailyInputTokens)}",
                    )
                }
            }
            if (!isCapped(data.limits.dailyImages)) {
                data.dailyImages?.let { info ->
                    QuotaDetailRow(
                        stringResource(R.string.quota_field_daily_images),
                        "${tokenAmount(info.used)}/${tokenAmount(data.limits.dailyImages)}",
                    )
                }
            }
        }
        is QuotaData.ZaiCoding -> {
            if (data.data.level.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_plan), data.data.level)
            }
            // The MCP window is a bar now (see quotaMeters); a row repeating its
            // percentage underneath would say the same thing twice.
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
            // Models on the plan are drawn as bars above; the rest have no
            // windows to meter, so their status is all there is to say.
            data.modelRemains
                .filter { it.currentIntervalStatus != 1 }
                .forEach { entry ->
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
            // With a credit ceiling the bar above already reads used-of-total;
            // without one there is no bar, so the spend still needs a row.
            if (data.creditsTotal <= 0) {
                QuotaDetailRow(stringResource(R.string.quota_field_credits_used), usd(data.creditsUsed))
            }
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
            // Absent is not zero: rendering a missing balance as $0.00 would
            // read as "all credits depleted", so the row is skipped instead.
            data.balance.creditsRemainingUsd?.let { remaining ->
                QuotaDetailRow(
                    stringResource(R.string.quota_field_balance_remaining),
                    usd(remaining),
                )
            }
            if (data.subscription.inOverage) {
                QuotaDetailRow(stringResource(R.string.quota_field_in_overage), yes)
            }
            // The total is the right-hand end of the credits bar above whenever
            // there is one, and a bar is drawn for exactly the same condition.
            if (data.subscription.plan.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_plan), data.subscription.plan)
            }
            if (data.subscription.status.isNotBlank()) {
                QuotaDetailRow(stringResource(R.string.quota_field_status), data.subscription.status)
            }
        }
    }
}

/**
 * QuotaMeterBar draws one [QuotaMeter] as a labelled bar, in the polarity
 * [mode] selects -- the same polarity the badge that opened the sheet is using,
 * so a badge reading "63%" opens onto a bar that is 63% full, not 37%.
 *
 * The fill colour ramps healthy -> warning -> error as the reading gets worse,
 * on thresholds that differ by polarity because "half used" is calm and "half
 * left" is not. REMAINING mirrors the web modals' `remainingBarColor` (the
 * shared.tsx pair): error under 20% left, warning under 60%, accent above.
 * USED deliberately parts company with `usedBarColor` at the bottom of the
 * range: the web bar is amber from zero, this one starts in the theme's accent,
 * so a barely-touched quota reads as healthy whichever polarity you are in
 * rather than as a caution in one and fine in the other.
 */
@Composable
private fun QuotaMeterBar(
    meter: QuotaMeter,
    mode: QuotaBarMode,
) {
    val used = meter.usedPercent.coerceIn(0.0, 100.0)
    val shown = if (mode == QuotaBarMode.USED) used else 100.0 - used
    val tag =
        if (meter.subject.isBlank()) {
            "quota-detail-meter-${meter.kind.name}"
        } else {
            "quota-detail-meter-${meter.subject}-${meter.kind.name}"
        }
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp).testTag(tag)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = stringResource(meterLabel(meter.kind)),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                // A provider that reports only a percentage has nothing else to
                // put here, and the percentage is already the bar's own length.
                text = meter.value.ifBlank { "${shown.roundToInt()}%" },
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        Spacer(modifier = Modifier.height(2.dp))
        Box(
            modifier =
                Modifier
                    .fillMaxWidth()
                    .height(6.dp)
                    .clip(MaterialTheme.shapes.extraSmall)
                    .background(MaterialTheme.colorScheme.surfaceVariant),
        ) {
            Box(
                modifier =
                    Modifier
                        .fillMaxWidth((shown / 100.0).toFloat())
                        .fillMaxHeight()
                        .background(meterColor(used, mode)),
            )
        }
    }
}

@Composable
private fun meterColor(
    usedPercent: Double,
    mode: QuotaBarMode,
): Color =
    if (mode == QuotaBarMode.USED) {
        when {
            usedPercent < 50 -> MaterialTheme.colorScheme.primary
            usedPercent < 80 -> SeverityWarnBg
            else -> SeverityErrorBg
        }
    } else {
        when {
            100 - usedPercent < 20 -> SeverityErrorBg
            100 - usedPercent < 60 -> SeverityWarnBg
            else -> MaterialTheme.colorScheme.primary
        }
    }

/**
 * meterLabel names a bar. Three kinds reuse the row labels they replaced, so a
 * reading that moved from a row to a bar reads the same as it did before.
 */
@StringRes
private fun meterLabel(kind: QuotaMeterKind): Int =
    when (kind) {
        QuotaMeterKind.FIVE_HOUR -> R.string.quota_field_five_hour
        QuotaMeterKind.WEEKLY -> R.string.quota_field_weekly
        QuotaMeterKind.MCP -> R.string.quota_field_mcp_quota
        QuotaMeterKind.DAILY_INPUT_TOKENS -> R.string.quota_field_daily_input_tokens
        QuotaMeterKind.DAILY_IMAGES -> R.string.quota_field_daily_images
        QuotaMeterKind.CREDITS -> R.string.quota_field_credits_used
        QuotaMeterKind.ENERGY -> R.string.quota_field_energy
    }

/** isCapped mirrors [quotaMeters]'s own test for whether a period has a
 * ceiling worth drawing a bar against, so a row and a bar never both claim the
 * same reading. */
private fun isCapped(limit: Long?): Boolean = limit != null && limit > 0L

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
