package com.hugalafutro.bellhop.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.decodeFromJsonElement
import java.util.Locale
import kotlin.math.roundToInt

// Wire models for GET /api/quota (internal/frontdesk, monitor tier -- any
// paired device may read/refresh). The envelope is one entry per
// quota-supporting provider Front Desk has cached; `type` selects which
// per-provider payload shape to decode and which badge to render -- `kind`
// (usage|balance|account) is bookkeeping only, never a badge selector. Each
// [QuotaData] variant mirrors the matching Go struct in internal/provider/
// (discovery_types.go, openrouter_types.go, ollama_cloud_types.go) but only
// carries the fields the dashboard (web/src/components/QuotaBadge.tsx +
// web/src/components/modals/*QuotaModal.tsx) actually displays.

/**
 * QuotaType is the badge/payload-shape selector: FD's `type` field, not
 * `kind`. [fromWire] degrades any string it doesn't recognize to [UNKNOWN]
 * rather than throwing, so a Front Desk that has grown a ninth provider never
 * crashes an older Bellhop -- the badge for it simply doesn't render.
 */
enum class QuotaType {
    NANOGPT,
    ZAI_CODING,
    KIMI_CODE,
    MINIMAX,
    DEEPSEEK,
    OPENROUTER,
    OLLAMA_CLOUD,
    NEURALWATT,
    UNKNOWN,
    ;

    companion object {
        fun fromWire(wire: String): QuotaType =
            when (wire) {
                "nanogpt" -> NANOGPT
                "zai-coding" -> ZAI_CODING
                "kimi-code" -> KIMI_CODE
                "minimax" -> MINIMAX
                "deepseek" -> DEEPSEEK
                "openrouter" -> OPENROUTER
                "ollama-cloud" -> OLLAMA_CLOUD
                "neuralwatt" -> NEURALWATT
                else -> UNKNOWN
            }
    }
}

/**
 * QuotaWire is one raw entry of the GET /api/quota envelope, before payload
 * decoding. [payload] is left as a [JsonElement] (rather than a concrete
 * type) because its shape depends on [type]; it is null both when Front Desk
 * has no cached payload yet and when the upstream fetch failed
 * ([httpStatus] != 200).
 */
@Serializable
data class QuotaWire(
    @SerialName("provider_name") val providerName: String = "",
    val type: String = "",
    val kind: String = "",
    val payload: JsonElement? = null,
    @SerialName("http_status") val httpStatus: Int = 0,
    @SerialName("fetched_at") val fetchedAt: String = "",
)

/** QuotaEnvelope is the GET /api/quota top-level response: `{"quota": [...]}`. */
@Serializable
data class QuotaEnvelope(
    val quota: List<QuotaWire> = emptyList(),
)

/**
 * QuotaRefreshResult is the POST /api/quota/refresh response: how many
 * cached entries Front Desk re-polled just now, split by outcome. Bellhop
 * surfaces this as a one-shot confirmation of the manual refresh; the
 * badges themselves repaint from the next [QuotaEnvelope] read, not from
 * this tally directly.
 */
@Serializable
data class QuotaRefreshResult(
    val refreshed: Int = 0,
    val failed: Int = 0,
    val skipped: Int = 0,
)

// ── Per-type payload shapes ─────────────────────────────────────────────

/**
 * QuotaData is the decoded, per-type payload -- one variant per [QuotaType]
 * (excluding [QuotaType.UNKNOWN], which never has a payload variant). Each
 * variant models only the fields the dashboard badge or detail modal renders,
 * not the full upstream response.
 */
sealed interface QuotaData {
    /** Mirrors internal/provider/discovery_types.go NanoGPTUsageResponse. */
    @Serializable
    data class NanoGpt(
        val active: Boolean = false,
        val provider: String = "",
        val providerStatus: String = "",
        val allowOverage: Boolean = false,
        val cancelAtPeriodEnd: Boolean = false,
        val limits: NanoGptLimits = NanoGptLimits(),
        val period: NanoGptPeriod = NanoGptPeriod(),
        val weeklyInputTokens: NanoGptTokenInfo? = null,
        val dailyInputTokens: NanoGptTokenInfo? = null,
        val dailyImages: NanoGptTokenInfo? = null,
    ) : QuotaData

    /** Mirrors internal/provider/discovery_types.go ZAICodingQuotaResponse. */
    @Serializable
    data class ZaiCoding(
        val data: ZaiCodingQuotaBody = ZaiCodingQuotaBody(),
        val success: Boolean = false,
    ) : QuotaData

    /** Mirrors internal/provider/discovery_types.go KimiCodeQuotaResponse. */
    @Serializable
    data class KimiCode(
        val user: KimiCodeUser = KimiCodeUser(),
        val usage: KimiCodeDetail = KimiCodeDetail(),
        val limits: List<KimiCodeLimitEntry> = emptyList(),
        val parallel: KimiCodeParallel = KimiCodeParallel(),
        val totalQuota: KimiCodeDetail = KimiCodeDetail(),
    ) : QuotaData

    /** Mirrors internal/provider/discovery_types.go MiniMaxQuotaResponse. */
    @Serializable
    data class MiniMax(
        @SerialName("model_remains") val modelRemains: List<MiniMaxModelRemain> = emptyList(),
        @SerialName("base_resp") val baseResp: MiniMaxBaseResp = MiniMaxBaseResp(),
    ) : QuotaData

    /** Mirrors internal/provider/discovery_types.go DeepSeekBalanceResponse. */
    @Serializable
    data class DeepSeek(
        @SerialName("is_available") val isAvailable: Boolean = false,
        @SerialName("balance_infos") val balanceInfos: List<DeepSeekBalanceInfo> = emptyList(),
    ) : QuotaData

    /** Mirrors internal/provider/openrouter_types.go OpenRouterBalance. */
    @Serializable
    data class OpenRouter(
        val limit: Double? = null,
        @SerialName("limit_reset") val limitReset: String = "",
        @SerialName("limit_remaining") val limitRemaining: Double? = null,
        val usage: Double = 0.0,
        @SerialName("usage_daily") val usageDaily: Double = 0.0,
        @SerialName("usage_weekly") val usageWeekly: Double = 0.0,
        @SerialName("usage_monthly") val usageMonthly: Double = 0.0,
        @SerialName("credits_total") val creditsTotal: Double = 0.0,
        @SerialName("credits_used") val creditsUsed: Double = 0.0,
        @SerialName("credits_remaining") val creditsRemaining: Double = 0.0,
        @SerialName("is_free_tier") val isFreeTier: Boolean = false,
    ) : QuotaData

    /** Mirrors internal/provider/ollama_cloud_types.go OllamaCloudAccount. */
    @Serializable
    data class OllamaCloud(
        val plan: String = "",
        @SerialName("subscription_period_end")
        val subscriptionPeriodEnd: OllamaCloudNullableTime = OllamaCloudNullableTime(),
        @SerialName("suspended_at")
        val suspendedAt: OllamaCloudNullableTime = OllamaCloudNullableTime(),
    ) : QuotaData

    /** Mirrors internal/provider/discovery_types.go NeuralWattQuotaResponse. */
    @Serializable
    data class NeuralWatt(
        val balance: NeuralWattBalance = NeuralWattBalance(),
        val usage: NeuralWattUsage = NeuralWattUsage(),
        val limits: NeuralWattLimits = NeuralWattLimits(),
        val subscription: NeuralWattSubscription = NeuralWattSubscription(),
        val key: NeuralWattKey = NeuralWattKey(),
    ) : QuotaData
}

// ── NanoGPT support types ───────────────────────────────────────────────
// NanoGPT's JSON keys are already camelCase (no @SerialName needed).

@Serializable
data class NanoGptLimits(
    val weeklyInputTokens: Long? = null,
    val dailyInputTokens: Long? = null,
    val dailyImages: Long? = null,
)

@Serializable
data class NanoGptPeriod(
    val currentPeriodEnd: String = "",
)

@Serializable
data class NanoGptTokenInfo(
    val used: Long = 0,
    val remaining: Long = 0,
    val percentUsed: Double = 0.0,
    val resetAt: Long = 0,
)

// ── Z.ai Coding support types ───────────────────────────────────────────

@Serializable
data class ZaiCodingQuotaBody(
    val limits: List<ZaiCodingLimit> = emptyList(),
    val level: String = "",
)

@Serializable
data class ZaiCodingLimit(
    val type: String = "",
    val unit: Int = 0,
    val percentage: Double = 0.0,
    val remaining: Long = 0,
    val nextResetTime: Long = 0,
    val usageDetails: List<ZaiCodingUsageDetail> = emptyList(),
)

@Serializable
data class ZaiCodingUsageDetail(
    val modelCode: String = "",
    val usage: Long = 0,
)

// ── Kimi Code support types ─────────────────────────────────────────────
// Kimi's numeric limit/remaining fields arrive as JSON strings on the wire.

@Serializable
data class KimiCodeUser(
    val membership: KimiCodeMembership = KimiCodeMembership(),
)

@Serializable
data class KimiCodeMembership(
    val level: String = "",
)

@Serializable
data class KimiCodeDetail(
    val limit: String = "",
    val used: String = "",
    val remaining: String = "",
    val resetTime: String = "",
)

@Serializable
data class KimiCodeWindowSpec(
    val duration: Int = 0,
    val timeUnit: String = "",
)

@Serializable
data class KimiCodeLimitEntry(
    val window: KimiCodeWindowSpec = KimiCodeWindowSpec(),
    val detail: KimiCodeDetail = KimiCodeDetail(),
)

@Serializable
data class KimiCodeParallel(
    val limit: String = "",
)

// ── MiniMax support types ───────────────────────────────────────────────

@Serializable
data class MiniMaxModelRemain(
    @SerialName("model_name") val modelName: String = "",
    @SerialName("start_time") val startTime: Long = 0,
    @SerialName("end_time") val endTime: Long = 0,
    @SerialName("remains_time") val remainsTime: Long = 0,
    @SerialName("weekly_remains_time") val weeklyRemainsTime: Long = 0,
    @SerialName("current_interval_status") val currentIntervalStatus: Int = 0,
    @SerialName("current_interval_total_count") val currentIntervalTotalCount: Long = 0,
    @SerialName("current_interval_usage_count") val currentIntervalUsageCount: Long = 0,
    @SerialName("current_interval_remaining_percent") val currentIntervalRemainingPercent: Double = 0.0,
    @SerialName("current_weekly_status") val currentWeeklyStatus: Int = 0,
    @SerialName("current_weekly_total_count") val currentWeeklyTotalCount: Long = 0,
    @SerialName("current_weekly_usage_count") val currentWeeklyUsageCount: Long = 0,
    @SerialName("current_weekly_remaining_percent") val currentWeeklyRemainingPercent: Double = 0.0,
)

@Serializable
data class MiniMaxBaseResp(
    @SerialName("status_code") val statusCode: Int = 0,
)

// ── DeepSeek support types ──────────────────────────────────────────────

@Serializable
data class DeepSeekBalanceInfo(
    val currency: String = "",
    @SerialName("total_balance") val totalBalance: String = "",
)

// ── Ollama Cloud support types ──────────────────────────────────────────

@Serializable
data class OllamaCloudNullableTime(
    val time: String = "",
    val valid: Boolean = false,
)

// ── NeuralWatt support types ────────────────────────────────────────────

@Serializable
data class NeuralWattBalance(
    // Nullable on purpose: an absent field must not read as a real $0, or the
    // total-minus-remaining derivation in quotaMeters would render a healthy
    // account as fully spent (the Go normalizer draws the same distinction).
    @SerialName("credits_remaining_usd") val creditsRemainingUsd: Double? = null,
    @SerialName("total_credits_usd") val totalCreditsUsd: Double = 0.0,
    @SerialName("credits_used_usd") val creditsUsedUsd: Double = 0.0,
    @SerialName("accounting_method") val accountingMethod: String = "",
)

@Serializable
data class NeuralWattUsagePeriod(
    @SerialName("cost_usd") val costUsd: Double = 0.0,
    val requests: Long = 0,
    val tokens: Long = 0,
    @SerialName("energy_kwh") val energyKwh: Double = 0.0,
)

@Serializable
data class NeuralWattUsage(
    val lifetime: NeuralWattUsagePeriod = NeuralWattUsagePeriod(),
    @SerialName("current_month") val currentMonth: NeuralWattUsagePeriod = NeuralWattUsagePeriod(),
)

@Serializable
data class NeuralWattLimits(
    @SerialName("overage_limit_usd") val overageLimitUsd: Double? = null,
    @SerialName("rate_limit_tier") val rateLimitTier: String = "",
)

@Serializable
data class NeuralWattSubscription(
    val plan: String = "",
    val status: String = "",
    @SerialName("billing_interval") val billingInterval: String = "",
    @SerialName("current_period_start") val currentPeriodStart: String = "",
    @SerialName("current_period_end") val currentPeriodEnd: String = "",
    @SerialName("auto_renew") val autoRenew: Boolean = false,
    @SerialName("kwh_included") val kwhIncluded: Double = 0.0,
    @SerialName("kwh_used") val kwhUsed: Double = 0.0,
    @SerialName("kwh_remaining") val kwhRemaining: Double = 0.0,
    @SerialName("in_overage") val inOverage: Boolean = false,
)

@Serializable
data class NeuralWattKey(
    val allowance: Double? = null,
)

// ── Parsing ──────────────────────────────────────────────────────────────

/**
 * ProviderQuota is the parsed, display-ready form of one [QuotaWire] entry.
 * [available] is true only when Front Desk's own fetch succeeded
 * (http_status == 200) *and* the payload decoded into the shape [type]
 * expects; badge code should gate on [available], not on [data] alone.
 */
data class ProviderQuota(
    val providerName: String,
    val type: QuotaType,
    val data: QuotaData?,
    val fetchedAt: String,
    val available: Boolean,
)

// coerceInputValues: a Go nil slice marshals as an explicit null
// ("model_remains": null on an empty MiniMax plan, "balance_infos": null on
// DeepSeek), which must land on the field's default rather than fail the
// whole decode and turn a readable payload into an unavailable badge.
private val quotaPayloadJson =
    Json {
        ignoreUnknownKeys = true
        coerceInputValues = true
    }

/**
 * providerQuotaOf maps one [QuotaWire] entry to a [ProviderQuota], decoding
 * [QuotaWire.payload] into the variant matching [QuotaWire.type]. Mirrors
 * [FleetSnapshot.stateOf]'s tolerant stance: a missing payload, a non-200
 * [QuotaWire.httpStatus], an unrecognized [QuotaWire.type], or a payload that
 * doesn't decode into the expected shape all degrade to `data = null,
 * available = false` -- this must never throw on a foreign or malformed
 * payload from a newer Front Desk.
 */
fun providerQuotaOf(wire: QuotaWire): ProviderQuota {
    val type = QuotaType.fromWire(wire.type)
    val data = decodeQuotaPayload(type, wire.payload)
    return ProviderQuota(
        providerName = wire.providerName,
        type = type,
        data = data,
        fetchedAt = wire.fetchedAt,
        available = wire.httpStatus == 200 && data != null,
    )
}

private fun decodeQuotaPayload(
    type: QuotaType,
    payload: JsonElement?,
): QuotaData? {
    if (payload == null || payload is JsonNull) return null
    return runCatching {
        when (type) {
            QuotaType.NANOGPT -> quotaPayloadJson.decodeFromJsonElement<QuotaData.NanoGpt>(payload)
            QuotaType.ZAI_CODING -> quotaPayloadJson.decodeFromJsonElement<QuotaData.ZaiCoding>(payload)
            QuotaType.KIMI_CODE -> quotaPayloadJson.decodeFromJsonElement<QuotaData.KimiCode>(payload)
            QuotaType.MINIMAX -> quotaPayloadJson.decodeFromJsonElement<QuotaData.MiniMax>(payload)
            QuotaType.DEEPSEEK -> quotaPayloadJson.decodeFromJsonElement<QuotaData.DeepSeek>(payload)
            QuotaType.OPENROUTER -> quotaPayloadJson.decodeFromJsonElement<QuotaData.OpenRouter>(payload)
            QuotaType.OLLAMA_CLOUD -> quotaPayloadJson.decodeFromJsonElement<QuotaData.OllamaCloud>(payload)
            QuotaType.NEURALWATT -> quotaPayloadJson.decodeFromJsonElement<QuotaData.NeuralWatt>(payload)
            QuotaType.UNKNOWN -> null
        }
    }.getOrNull()
}

// ── Badge label formatting ──────────────────────────────────────────────
// Shared between the widget (this file feeds WidgetState.widgetQuotaOf) and
// the future main-page badge composable (D2), so the two surfaces never
// drift on what a badge says. Shapes mirror web/src/components/QuotaBadge.tsx
// (used-fraction / used-percent / dollars / plan name / kWh) -- data/shape
// parity, not a code port. These are numeric or symbol fragments (not
// natural-language copy), so no Android string resources apply, and they are
// formatted with a fixed Locale so the widget reads the same on every device.

/**
 * QuotaBarMode selects which polarity [quotaBadgeLabel] shows for METERED
 * (percentage/fraction) quota types -- REMAINING or USED -- mirroring the web
 * dashboard's `QuotaBarMode` (web/src/components/QuotaBadge.tsx), whose
 * default is also "remaining". BALANCE/credit types (OpenRouter, DeepSeek,
 * OllamaCloud, NeuralWatt) render the same figure regardless of [mode]: there
 * is no "used" polarity for an account balance or plan name.
 */
enum class QuotaBarMode { REMAINING, USED }

/**
 * QuotaBadgeAlign is where the home-screen widget's badge rows sit across the
 * widget's width. Widget-only: the phone dashboard strip has no such choice.
 */
enum class QuotaBadgeAlign { LEFT, CENTER, RIGHT }

/**
 * quotaBadgeLabel formats [pq] into the short text a badge shows, in the
 * polarity [mode] selects for METERED types. Unavailable or payload-less
 * quotas (see [ProviderQuota.available]) render as "-", mirroring the web
 * badge's fallback when its balance hook has no data yet.
 */
fun quotaBadgeLabel(
    pq: ProviderQuota,
    mode: QuotaBarMode,
): String {
    val data = pq.data
    if (!pq.available || data == null) return "-"
    return when (data) {
        is QuotaData.NanoGpt -> nanoGptBadgeLabel(data, mode)
        is QuotaData.ZaiCoding -> {
            val fiveHour = data.data.limits.find { it.type == "TOKENS_LIMIT" && it.unit == 3 }
            val weekly = data.data.limits.find { it.type == "TOKENS_LIMIT" && it.unit == 6 }
            val fiveHourPct = applyBarMode(fiveHour?.percentage, mode)
            val weeklyPct = applyBarMode(weekly?.percentage, mode)
            "${formatPercent(fiveHourPct)}/${formatPercent(weeklyPct)}"
        }
        is QuotaData.KimiCode -> {
            val fiveHour =
                data.limits
                    .find { it.window.timeUnit == "TIME_UNIT_MINUTE" && it.window.duration == 300 }
                    ?.detail
            val fiveHourUsed = applyBarMode(kimiUsedPercent(fiveHour), mode)
            val weeklyUsed = applyBarMode(kimiUsedPercent(data.usage), mode)
            "${formatPercent(fiveHourUsed)}/${formatPercent(weeklyUsed)}"
        }
        is QuotaData.MiniMax -> {
            val general = data.modelRemains.find { it.modelName == "general" && it.currentIntervalStatus == 1 }
            val fiveHourUsed = general?.let { 100.0 - it.currentIntervalRemainingPercent }
            val weeklyUsed = general?.let { 100.0 - it.currentWeeklyRemainingPercent }
            "${formatPercent(applyBarMode(fiveHourUsed, mode))}/${formatPercent(applyBarMode(weeklyUsed, mode))}"
        }
        is QuotaData.DeepSeek -> {
            val usd = data.balanceInfos.find { it.currency == "USD" }?.totalBalance
            if (usd.isNullOrBlank()) "-" else "$$usd"
        }
        is QuotaData.OpenRouter -> "$${formatDollarAmount(data.creditsRemaining)}"
        is QuotaData.OllamaCloud -> data.plan.ifBlank { "-" }
        is QuotaData.NeuralWatt -> {
            val used = data.subscription.kwhUsed
            val included = data.subscription.kwhIncluded
            if (included > 0) {
                "${formatKwhAmount(used)}/${formatKwhAmount(included)} kWh"
            } else {
                "${formatKwhAmount(used)} kWh"
            }
        }
    }
}

/**
 * quotaShortCode is the widget's stand-in for a provider name: two to four
 * characters that fit beside the reading on a home-screen pill, where the
 * operator's own provider name ("openrouter-personal") would eat the row. It
 * keys on [QuotaType], so two providers of the same type share a code -- the
 * badge tap toasts the full name, which is what tells them apart. Like the
 * label formatters above these are symbols, not translatable copy, so they
 * carry no string resources.
 */
fun quotaShortCode(type: QuotaType): String =
    when (type) {
        QuotaType.NANOGPT -> "NANO"
        QuotaType.ZAI_CODING -> "ZAI"
        QuotaType.KIMI_CODE -> "KIMI"
        QuotaType.MINIMAX -> "MMX"
        QuotaType.DEEPSEEK -> "DS"
        QuotaType.OPENROUTER -> "OR"
        QuotaType.OLLAMA_CLOUD -> "OLC"
        QuotaType.NEURALWATT -> "NW"
        // Unreachable in the widget (unknown types are dropped before a badge is
        // built), but a code is cheaper than making the caller handle a null.
        QuotaType.UNKNOWN -> "?"
    }

/**
 * quotaHasDetail reports whether a provider has anything worth opening a detail
 * view for, beyond what its badge already says.
 *
 * It follows Model Hotel's web dashboard, which gives six of the eight types a
 * quota modal and leaves DeepSeek and Ollama Cloud with a badge that only
 * refreshes: DeepSeek's whole reading is its balance, and Ollama Cloud's is its
 * plan name. Both are already printed on the badge, so opening a sheet -- from
 * the widget, possibly through an unlock prompt -- would spend a screen to
 * repeat one word. The six that do have a modal have genuinely more to say
 * (ZAI's 5-hour, weekly and MCP windows; OpenRouter's spend; Kimi's tiers).
 *
 * Keep this in step with `web/src/context/QuotaModalContext.tsx`: a type that
 * gains a modal there should gain a detail view here.
 */
fun quotaHasDetail(type: QuotaType): Boolean =
    when (type) {
        QuotaType.NANOGPT,
        QuotaType.ZAI_CODING,
        QuotaType.KIMI_CODE,
        QuotaType.MINIMAX,
        QuotaType.OPENROUTER,
        QuotaType.NEURALWATT,
        -> true
        // Balance-only and plan-only: the badge is the whole story.
        QuotaType.DEEPSEEK, QuotaType.OLLAMA_CLOUD -> false
        // Never rendered (unknown types are dropped before a badge is built).
        QuotaType.UNKNOWN -> false
    }

/**
 * QuotaMeterKind names one bar in a provider's detail sheet. It exists so the
 * data layer can say *which* reading a bar is without reaching for a string
 * resource: the sheet maps the kind onto translated copy, and the kinds a
 * provider yields are unit-testable without a Compose runtime.
 */
enum class QuotaMeterKind {
    FIVE_HOUR,
    WEEKLY,
    MCP,
    DAILY_INPUT_TOKENS,
    DAILY_IMAGES,
    CREDITS,
    ENERGY,
}

/**
 * QuotaMeter is one bar's worth of a reading.
 *
 * [usedPercent] is always the *used* polarity (0-100), whatever the badge mode
 * is: the sheet flips it for display the same way [quotaBadgeLabel] flips its
 * text, so the two never disagree about which end of the bar is full.
 * [subject] names what the bar is about when a provider meters more than one
 * thing of the same kind (MiniMax reports per model); it is blank otherwise.
 * [value] is the raw fraction behind the bar ("1.2M/2M", "12.5/20 kWh") when
 * the provider reports one, and blank when the only figure it gives is the
 * percentage itself, which the sheet already renders.
 */
data class QuotaMeter(
    val kind: QuotaMeterKind,
    val usedPercent: Double,
    val subject: String = "",
    val value: String = "",
)

/**
 * quotaMeters is the bar chart behind a badge: every reading of [pq] that has
 * both a used figure and a ceiling, in the order the matching Model Hotel web
 * modal shows them (the QuotaModal components under web/src/components/modals).
 *
 * A reading with no ceiling yields no bar -- a bar with no full end is a
 * decoration, not a reading -- so a provider on an unmetered plan simply shows
 * its supporting rows and nothing else. DeepSeek and Ollama Cloud have no
 * detail sheet at all (see [quotaHasDetail]) and never reach this.
 */
fun quotaMeters(pq: ProviderQuota): List<QuotaMeter> {
    val data = pq.data
    if (!pq.available || data == null) return emptyList()
    return when (data) {
        is QuotaData.NanoGpt ->
            listOfNotNull(
                tokenMeter(QuotaMeterKind.WEEKLY, data.weeklyInputTokens, data.limits.weeklyInputTokens),
                tokenMeter(QuotaMeterKind.DAILY_INPUT_TOKENS, data.dailyInputTokens, data.limits.dailyInputTokens),
                tokenMeter(QuotaMeterKind.DAILY_IMAGES, data.dailyImages, data.limits.dailyImages),
            )
        is QuotaData.ZaiCoding ->
            listOfNotNull(
                zaiMeter(data, QuotaMeterKind.FIVE_HOUR, "TOKENS_LIMIT", 3),
                zaiMeter(data, QuotaMeterKind.WEEKLY, "TOKENS_LIMIT", 6),
                zaiMeter(data, QuotaMeterKind.MCP, "TIME_LIMIT", 5),
            )
        is QuotaData.KimiCode -> {
            val fiveHour =
                data.limits
                    .find { it.window.timeUnit == "TIME_UNIT_MINUTE" && it.window.duration == 300 }
                    ?.detail
            listOfNotNull(
                kimiUsedPercent(fiveHour)?.let { QuotaMeter(QuotaMeterKind.FIVE_HOUR, it) },
                kimiUsedPercent(data.usage)?.let { QuotaMeter(QuotaMeterKind.WEEKLY, it) },
            )
        }
        // One pair of bars per model that is actually on the plan; the models
        // that are not keep their status row and would only draw empty bars.
        is QuotaData.MiniMax ->
            data.modelRemains
                .filter { it.currentIntervalStatus == 1 }
                .flatMap { entry ->
                    listOf(
                        QuotaMeter(
                            kind = QuotaMeterKind.FIVE_HOUR,
                            usedPercent = 100.0 - entry.currentIntervalRemainingPercent,
                            subject = entry.modelName,
                        ),
                        QuotaMeter(
                            kind = QuotaMeterKind.WEEKLY,
                            usedPercent = 100.0 - entry.currentWeeklyRemainingPercent,
                            subject = entry.modelName,
                        ),
                    )
                }
        is QuotaData.OpenRouter ->
            listOfNotNull(
                amountMeter(
                    kind = QuotaMeterKind.CREDITS,
                    used = data.creditsUsed,
                    ceiling = data.creditsTotal,
                    format = { "$${formatDollarAmount(it)}" },
                ),
            )
        // Energy only: NeuralWatt's credits_used_usd is a hardwired 0 and
        // total_credits_usd re-bases to remaining as spend settles (verified
        // live 2026-08-24), so a credits meter could only ever render as
        // untouched. The balance number itself is a detail-sheet row.
        is QuotaData.NeuralWatt ->
            listOfNotNull(
                amountMeter(
                    kind = QuotaMeterKind.ENERGY,
                    used = data.subscription.kwhUsed,
                    ceiling = data.subscription.kwhIncluded,
                    format = { formatKwhAmount(it) },
                    suffix = " kWh",
                ),
            )
        // Balance-only and plan-only: no ceiling exists to meter against.
        is QuotaData.DeepSeek, is QuotaData.OllamaCloud -> emptyList()
    }
}

private fun tokenMeter(
    kind: QuotaMeterKind,
    info: NanoGptTokenInfo?,
    limit: Long?,
): QuotaMeter? {
    if (info == null || limit == null || limit <= 0L) return null
    return QuotaMeter(
        kind = kind,
        usedPercent = info.used.toDouble() / limit.toDouble() * 100.0,
        value = "${formatTokenCount(info.used)}/${formatTokenCount(limit)}",
    )
}

private fun zaiMeter(
    data: QuotaData.ZaiCoding,
    kind: QuotaMeterKind,
    type: String,
    unit: Int,
): QuotaMeter? {
    val limit = data.data.limits.find { it.type == type && it.unit == unit } ?: return null
    return QuotaMeter(kind = kind, usedPercent = limit.percentage)
}

private fun amountMeter(
    kind: QuotaMeterKind,
    used: Double,
    ceiling: Double,
    format: (Double) -> String,
    suffix: String = "",
): QuotaMeter? {
    if (ceiling <= 0.0) return null
    return QuotaMeter(
        kind = kind,
        usedPercent = used / ceiling * 100.0,
        value = "${format(used)}/${format(ceiling)}$suffix",
    )
}

/**
 * parseKimiNumber reads one of Kimi's wire-string numbers, or null when the
 * field carries no value.
 *
 * Absent, "" and whitespace are one case: Kimi's proto3 JSON omits a
 * zero-valued field and the Go snapshot re-marshal writes "" for the omission,
 * so both arrive here as null and what the omission means is the caller's to
 * decide, field by field. Mirrors `parseKimiNumber` in web-shared/quota/kimi.ts.
 */
internal fun parseKimiNumber(v: String?): Double? {
    val trimmed = v?.trim().orEmpty()
    if (trimmed.isEmpty()) return null
    val n = trimmed.toDoubleOrNull() ?: return null
    return if (n.isFinite()) n else null
}

/**
 * resolveKimiRemaining is how many units of the window are still available.
 *
 * `remaining` wins when it carries a value. Otherwise the window is exhausted
 * enough for Kimi to have dropped `remaining` and `used` carries the count, so
 * remaining is limit minus used, floored at zero. An omitted `used` means zero
 * used, hence the full limit. A `used` that is present but unreadable yields
 * null: an unparseable window must not be turned into a number, ever.
 */
internal fun resolveKimiRemaining(
    limit: Double,
    remainingStr: String?,
    usedStr: String?,
): Double? {
    parseKimiNumber(remainingStr)?.let { return it }
    parseKimiNumber(usedStr)?.let { return (limit - it).coerceAtLeast(0.0) }
    return if (usedStr.isNullOrBlank()) limit else null
}

/**
 * kimiUsedPercent computes used% for one Kimi window, clamped to [0, 100].
 * Null means the window cannot be read (an unparseable limit, or a `used` that
 * is present but not a number), which callers render as "-". A zero or
 * negative limit meters as 0% rather than as no reading, matching
 * `toKimiCodeWindow` in web-shared/quota/kimi.ts; the shared fixtures under
 * testdata/quota-contract/kimi hold the two platforms to the same figures.
 */
private fun kimiUsedPercent(detail: KimiCodeDetail?): Double? {
    if (detail == null) return null
    val limit = parseKimiNumber(detail.limit) ?: return null
    val remaining = resolveKimiRemaining(limit, detail.remaining, detail.used) ?: return null
    val raw = if (limit > 0.0) (limit - remaining) / limit * 100.0 else 0.0
    return raw.coerceIn(0.0, 100.0)
}

/**
 * nanoGptBadgeLabel renders NanoGPT's weekly-token fraction. USED (the prior,
 * only behavior) shows used/limit; REMAINING shows (limit - used)/limit,
 * floored at zero, mirroring web's `nanoBadgeContent`.
 */
private fun nanoGptBadgeLabel(
    data: QuotaData.NanoGpt,
    mode: QuotaBarMode,
): String {
    val used = data.weeklyInputTokens?.used
    val limit = data.limits.weeklyInputTokens
    val numerator =
        if (mode == QuotaBarMode.REMAINING) {
            if (used != null && limit != null) (limit - used).coerceAtLeast(0) else null
        } else {
            used
        }
    return "${formatTokenCount(numerator)}/${formatTokenCount(limit)}"
}

/**
 * applyBarMode converts a used% value into the polarity [mode] wants: USED
 * passes it through unchanged (the prior, only behavior); REMAINING returns
 * 100 minus it. Null (no data) propagates through either polarity.
 */
private fun applyBarMode(
    usedPercent: Double?,
    mode: QuotaBarMode,
): Double? {
    if (usedPercent == null) return null
    return if (mode == QuotaBarMode.USED) usedPercent else 100.0 - usedPercent
}

private fun formatPercent(pct: Double?): String = if (pct == null) "-" else "${pct.roundToInt()}%"

private fun formatDollarAmount(v: Double): String = String.format(Locale.US, "%.2f", v)

private fun formatKwhAmount(v: Double): String = trimTrailingZero(v)

/** formatTokenCount mirrors formatCompact from web/src/utils/format.ts: K/M/B suffixes, one decimal. */
private fun formatTokenCount(n: Long?): String {
    if (n == null) return "-"
    if (n == 0L) return "0"
    val abs = kotlin.math.abs(n)
    return when {
        abs >= 1_000_000_000L -> "${trimTrailingZero(n / 1_000_000_000.0)}B"
        abs >= 1_000_000L -> "${trimTrailingZero(n / 1_000_000.0)}M"
        abs >= 1_000L -> "${trimTrailingZero(n / 1_000.0)}K"
        else -> n.toString()
    }
}

private fun trimTrailingZero(v: Double): String {
    val s = String.format(Locale.US, "%.1f", v)
    return if (s.endsWith(".0")) s.dropLast(2) else s
}
