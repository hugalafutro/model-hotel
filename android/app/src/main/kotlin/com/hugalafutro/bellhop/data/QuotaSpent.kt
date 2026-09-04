package com.hugalafutro.bellhop.data

// Whether a readable payload says the account can serve nothing until it
// resets or is topped up. A port of web-shared/quota/spent.ts, which itself
// mirrors the gateway's quota normalizer (internal/quota/normalize.go): the
// same windows judged by the same fields, so a badge here, on the Model Hotel
// dashboard and on Front Desk all read one snapshot the same way. Web treats
// an absent field as unknown, never spent; here the model defaults stand in
// for absent, which is safe because Front Desk relays Go structs whose
// non-pointer fields are always on the wire. The one field a provider can
// really omit, NeuralWatt's credits balance, is nullable and guarded.

/**
 * Below this balance NeuralWatt credits count as spent. NeuralWatt blocks the
 * account with a sub-cent residue that never drains, so an exact zero test
 * would never become true.
 */
private const val NEURALWATT_CREDITS_SPENT_FLOOR_USD = 0.01

/**
 * isQuotaSpent is the badge's spent gate. An unavailable quota has no readable
 * payload, so it never reads as spent; Ollama Cloud's account payload names
 * the plan and never the usage, so it cannot either.
 */
fun isQuotaSpent(pq: ProviderQuota): Boolean {
    val data = pq.data
    if (!pq.available || data == null) return false
    return when (data) {
        is QuotaData.NanoGpt -> isNanoGptSpent(data)
        is QuotaData.ZaiCoding -> isZaiCodingSpent(data)
        is QuotaData.KimiCode -> isKimiCodeSpent(data)
        is QuotaData.MiniMax -> isMiniMaxSpent(data)
        is QuotaData.DeepSeek -> isDeepSeekSpent(data)
        is QuotaData.OpenRouter -> isOpenRouterSpent(data)
        is QuotaData.OllamaCloud -> false
        is QuotaData.NeuralWatt -> isNeuralWattSpent(data)
    }
}

private fun isNanoGptSpent(u: QuotaData.NanoGpt): Boolean {
    if (u.allowOverage) return false
    val limit = u.limits.weeklyInputTokens ?: return false
    val used = u.weeklyInputTokens?.used ?: return false
    return limit > 0 && used >= limit
}

// A sane percentage decides; Z.ai sends an explicit remaining: 0 on windows
// that are only partially used, so remaining is the fallback, not the rule.
private fun isZaiCodingWindowSpent(l: ZaiCodingLimit): Boolean {
    val pct = l.percentage
    if (pct >= 0.0 && pct <= 100.0) return pct >= 100.0
    return l.remaining <= 0
}

// Every rolling (unit 3) and weekly (unit 6) token window, as the gateway
// walks them.
private fun isZaiCodingSpent(u: QuotaData.ZaiCoding): Boolean =
    u.data.limits.any {
        it.type == "TOKENS_LIMIT" && (it.unit == 3 || it.unit == 6) && isZaiCodingWindowSpent(it)
    }

// A window whose limit or remaining cannot be read is skipped, the same
// windows kimiUsedPercent renders as "-".
private fun isKimiWindowSpent(detail: KimiCodeDetail): Boolean {
    val limit = parseKimiNumber(detail.limit) ?: return false
    val remaining = resolveKimiRemaining(limit, detail.remaining, detail.used) ?: return false
    return remaining <= 0.0
}

// The weekly block plus every rolling window, as the gateway walks them.
private fun isKimiCodeSpent(u: QuotaData.KimiCode): Boolean =
    isKimiWindowSpent(u.usage) || u.limits.any { isKimiWindowSpent(it.detail) }

// One MiniMax window: only a window the plan covers (status 1) counts;
// request counts decide when the payload carries them, else a remaining
// percent inside [0, 100]; anything else is skipped.
private fun isMiniMaxWindowSpent(
    status: Int,
    total: Long,
    used: Long,
    remainingPercent: Double,
): Boolean {
    if (status != 1) return false
    if (total > 0) return used >= total
    if (remainingPercent >= 0.0 && remainingPercent <= 100.0) return remainingPercent <= 0.0
    return false
}

// Every model class: the breaker is per provider, so any spent window on any
// class pins it.
private fun isMiniMaxSpent(u: QuotaData.MiniMax): Boolean =
    u.modelRemains.any {
        isMiniMaxWindowSpent(
            it.currentIntervalStatus,
            it.currentIntervalTotalCount,
            it.currentIntervalUsageCount,
            it.currentIntervalRemainingPercent,
        ) ||
            isMiniMaxWindowSpent(
                it.currentWeeklyStatus,
                it.currentWeeklyTotalCount,
                it.currentWeeklyUsageCount,
                it.currentWeeklyRemainingPercent,
            )
    }

private fun isDeepSeekSpent(b: QuotaData.DeepSeek): Boolean {
    if (!b.isAvailable) return true
    if (b.balanceInfos.isEmpty()) return false
    return b.balanceInfos.all {
        val n = it.totalBalance.trim().toDoubleOrNull() ?: return@all false
        n.isFinite() && n <= 0.0
    }
}

// A key that never bought credits reports credits_remaining 0 (the gateway
// clamps total minus usage) yet still serves the free models, so only a
// funded key can spend its credits. A spent per-key cap blocks regardless.
private fun isOpenRouterSpent(b: QuotaData.OpenRouter): Boolean {
    val funded = !b.isFreeTier && b.creditsTotal > 0.0
    val limitRemaining = b.limitRemaining
    return (funded && b.creditsRemaining <= 0.0) || (limitRemaining != null && limitRemaining <= 0.0)
}

// Spent means both meters are gone: the energy allowance (kwh_remaining at
// zero, or the in_overage flag NeuralWatt sets on entering overage) and the
// credits that overage draws on.
private fun isNeuralWattSpent(q: QuotaData.NeuralWatt): Boolean {
    val energySpent = q.subscription.kwhRemaining <= 0.0 || q.subscription.inOverage
    val credits = q.balance.creditsRemainingUsd ?: return false
    return energySpent && credits < NEURALWATT_CREDITS_SPENT_FLOOR_USD
}
