package com.hugalafutro.bellhop.data

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins [isQuotaSpent] to the rules in web-shared/quota/spent.ts, case for
 * case: each arm pairs a payload that must read as spent with one that must
 * not, and the healthy twin is the spent one with a single field changed, so
 * the test also says which field decides. Payloads go through the wire decoder
 * so the fields the rules read are proven to parse, not only to compute.
 */
class QuotaSpentTest {
    private val json = Json { ignoreUnknownKeys = true }

    private fun quota(
        type: String,
        payload: String,
        status: Int = 200,
    ): ProviderQuota =
        providerQuotaOf(
            QuotaWire(
                providerName = "p",
                type = type,
                kind = "usage",
                payload = json.decodeFromString<JsonElement>(payload),
                httpStatus = status,
                fetchedAt = "2026-09-04T00:00:00Z",
            ),
        )

    private fun assertPair(
        type: String,
        spent: String,
        healthy: String,
    ) {
        assertTrue("$type must read as spent: $spent", isQuotaSpent(quota(type, spent)))
        assertFalse("$type must read as healthy: $healthy", isQuotaSpent(quota(type, healthy)))
    }

    private fun kimiWindow(remaining: String) =
        """{"limit":"1000","remaining":"$remaining","resetTime":"2026-09-05T00:00:00Z"}"""

    private fun kimi(
        weekly: String,
        duration: Int,
        rolling: String,
    ): String {
        val window = """{"duration":$duration,"timeUnit":"TIME_UNIT_MINUTE"}"""
        return """{"usage":${kimiWindow(weekly)},"limits":[{"window":$window,"detail":${kimiWindow(rolling)}}]}"""
    }

    private fun minimax(
        interval: Double,
        weekly: Double,
        extra: String = "",
    ) =
        """{"model_remains":[{"model_name":"general","current_interval_status":1,"current_interval_remaining_percent":$interval,
        "current_weekly_status":1,"current_weekly_remaining_percent":$weekly$extra}]}"""

    private fun zai(vararg limits: String) = """{"success":true,"data":{"limits":[${limits.joinToString(",")}]}}"""

    @Test
    fun nanogptWeeklyAllowanceUsedUp() =
        assertPair(
            "nanogpt",
            spent = """{"limits":{"weeklyInputTokens":1000},"weeklyInputTokens":{"used":1000}}""",
            healthy = """{"limits":{"weeklyInputTokens":1000},"weeklyInputTokens":{"used":999}}""",
        )

    @Test
    fun nanogptOverageKeepsServingPastTheAllowance() =
        assertPair(
            "nanogpt",
            spent = """{"limits":{"weeklyInputTokens":1000},"weeklyInputTokens":{"used":1000},"allowOverage":false}""",
            healthy = """{"limits":{"weeklyInputTokens":1000},"weeklyInputTokens":{"used":1000},"allowOverage":true}""",
        )

    @Test
    fun zaiWindowAtFullPercentIsSpentEvenWithAResidue() =
        assertPair(
            "zai-coding",
            spent = zai("""{"type":"TOKENS_LIMIT","unit":6,"percentage":100,"remaining":3}"""),
            healthy = zai("""{"type":"TOKENS_LIMIT","unit":6,"percentage":99,"remaining":0}"""),
        )

    @Test
    fun zaiRemainingDecidesWithoutASanePercentage() =
        assertPair(
            "zai-coding",
            spent = zai("""{"type":"TOKENS_LIMIT","unit":3,"percentage":250,"remaining":0}"""),
            healthy = zai("""{"type":"TOKENS_LIMIT","unit":3,"percentage":250,"remaining":1}"""),
        )

    @Test
    fun zaiSecondEntryForTheSameWindowCounts() =
        assertPair(
            "zai-coding",
            spent =
                zai(
                    """{"type":"TOKENS_LIMIT","unit":6,"percentage":10}""",
                    """{"type":"TOKENS_LIMIT","unit":6,"percentage":100}""",
                ),
            healthy =
                zai(
                    """{"type":"TOKENS_LIMIT","unit":6,"percentage":10}""",
                    """{"type":"TOKENS_LIMIT","unit":6,"percentage":90}""",
                ),
        )

    @Test
    fun zaiNonTokenWindowsAreSkipped() {
        assertFalse(isQuotaSpent(quota("zai-coding", zai("""{"type":"TIME_LIMIT","unit":5,"percentage":100}"""))))
    }

    @Test
    fun kimiEitherWindowSpentBlocksTheAccount() =
        assertPair(
            "kimi-code",
            spent = kimi(weekly = "5", duration = 300, rolling = "0"),
            healthy = kimi(weekly = "5", duration = 300, rolling = "1"),
        )

    @Test
    fun kimiEveryRollingWindowCountsNotOnlyTheFiveHour() =
        assertPair(
            "kimi-code",
            spent = kimi(weekly = "5", duration = 60, rolling = "0"),
            healthy = kimi(weekly = "5", duration = 60, rolling = "1"),
        )

    @Test
    fun kimiWeeklyBlockSpentOnItsOwn() =
        assertPair(
            "kimi-code",
            spent = kimi(weekly = "0", duration = 300, rolling = "5"),
            healthy = kimi(weekly = "1", duration = 300, rolling = "5"),
        )

    @Test
    fun kimiUnreadableWindowIsSkippedNotSpent() {
        val unreadable = """{"usage":{"limit":"1000","remaining":"","used":"lots"}}"""
        assertFalse(isQuotaSpent(quota("kimi-code", unreadable)))
    }

    @Test
    fun minimaxWeeklyWindowWithNothingRemaining() =
        assertPair("minimax", spent = minimax(40.0, 0.0), healthy = minimax(40.0, 1.0))

    @Test
    fun minimaxRollingWindowJudgedOnItsOwn() =
        assertPair("minimax", spent = minimax(0.0, 40.0), healthy = minimax(1.0, 40.0))

    @Test
    fun minimaxRequestCountsWinOverThePercent() =
        assertPair(
            "minimax",
            spent = minimax(40.0, 40.0, ""","current_interval_total_count":100,"current_interval_usage_count":100"""),
            healthy = minimax(0.0, 40.0, ""","current_interval_total_count":100,"current_interval_usage_count":99"""),
        )

    @Test
    fun minimaxWindowThePlanDoesNotCoverIsSkipped() =
        assertPair(
            "minimax",
            spent = minimax(40.0, 0.0),
            healthy =
                """{"model_remains":[{"model_name":"general","current_interval_status":1,"current_interval_remaining_percent":40,
                "current_weekly_status":3,"current_weekly_remaining_percent":0}]}""",
        )

    @Test
    fun minimaxNullModelListDecodesAsAnEmptyPlanNotAnUnavailableBadge() {
        val pq = quota("minimax", """{"model_remains":null,"base_resp":{"status_code":0}}""")
        assertTrue(pq.available)
        assertFalse(isQuotaSpent(pq))
    }

    @Test
    fun minimaxPercentOutsideRangeIsNonsenseNotASignal() =
        assertPair("minimax", spent = minimax(40.0, 0.0), healthy = minimax(40.0, -1.0))

    @Test
    fun deepseekAvailableAccountWithZeroBalance() =
        assertPair(
            "deepseek",
            spent = """{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"0.00"}]}""",
            healthy = """{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"0.01"}]}""",
        )

    @Test
    fun deepseekUnavailableIsSpentEmptyListOrBlankBalanceIsNot() {
        assertTrue(isQuotaSpent(quota("deepseek", """{"is_available":false}""")))
        assertFalse(isQuotaSpent(quota("deepseek", """{"is_available":true,"balance_infos":[]}""")))
        assertFalse(isQuotaSpent(quota("deepseek", """{"is_available":true,"balance_infos":null}""")))
        assertFalse(
            isQuotaSpent(quota("deepseek", """{"is_available":true,"balance_infos":[{"total_balance":" "}]}""")),
        )
    }

    @Test
    fun openrouterFundedKeyWithCreditsAtZero() =
        assertPair(
            "openrouter",
            spent = """{"credits_total":10,"credits_remaining":0,"limit_remaining":null}""",
            healthy = """{"credits_total":10,"credits_remaining":0.5,"limit_remaining":null}""",
        )

    @Test
    fun openrouterFreeTierKeyAtZeroStillServesTheFreeModels() =
        assertPair(
            "openrouter",
            spent = """{"credits_total":10,"credits_remaining":0,"is_free_tier":false}""",
            healthy = """{"credits_total":10,"credits_remaining":0,"is_free_tier":true}""",
        )

    @Test
    fun openrouterKeyThatNeverBoughtCreditsHasNoneToSpend() =
        assertPair(
            "openrouter",
            spent = """{"credits_total":10,"credits_remaining":0,"is_free_tier":false}""",
            healthy = """{"credits_total":0,"credits_remaining":0,"is_free_tier":false}""",
        )

    @Test
    fun openrouterUnfundedKeyNeverReadsSpentOnCreditsAlone() {
        assertFalse(isQuotaSpent(quota("openrouter", """{"credits_remaining":0}""")))
    }

    @Test
    fun openrouterPerKeyCapDecidesOnItsOwn() =
        assertPair(
            "openrouter",
            spent = """{"credits_total":10,"credits_remaining":12,"limit_remaining":0}""",
            healthy = """{"credits_total":10,"credits_remaining":12,"limit_remaining":5}""",
        )

    @Test
    fun neuralwattEnergyAtZeroCountsWithoutTheOverageFlag() =
        assertPair(
            "neuralwatt",
            spent = """{"subscription":{"plan":"basic","kwh_remaining":0},"balance":{"credits_remaining_usd":0}}""",
            healthy = """{"subscription":{"plan":"basic","kwh_remaining":0.2},"balance":{"credits_remaining_usd":0}}""",
        )

    @Test
    fun neuralwattInOverageWithCreditsBelowTheSubCentFloor() =
        assertPair(
            "neuralwatt",
            spent = neuralwattOverage(credits = "0.0035"),
            healthy = neuralwattOverage(credits = "0.01"),
        )

    // kwh_remaining is pinned above zero so in_overage is the field under
    // test: the wire always carries it, and the model default of zero would
    // otherwise read as energy spent on its own.
    private fun neuralwattOverage(credits: String) =
        """{"subscription":{"plan":"basic","kwh_remaining":1,"in_overage":true},"balance":{"credits_remaining_usd":$credits}}"""

    @Test
    fun neuralwattAbsentBalanceIsUnknownNotSpent() {
        assertFalse(isQuotaSpent(quota("neuralwatt", """{"subscription":{"in_overage":true},"balance":{}}""")))
    }

    @Test
    fun ollamaCloudNeverReadsAsSpent() {
        assertFalse(isQuotaSpent(quota("ollama-cloud", """{"plan":"pro"}""")))
    }

    @Test
    fun unavailableQuotaNeverReadsAsSpent() {
        val spentBody = """{"is_available":false}"""
        assertTrue(isQuotaSpent(quota("deepseek", spentBody)))
        assertFalse(isQuotaSpent(quota("deepseek", spentBody, status = 424)))
    }
}
