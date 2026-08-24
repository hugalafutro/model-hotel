package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * quotaMeters is the detail sheet's bar chart, derived from the same payloads
 * the badge label reads. The contract worth pinning is the one that keeps a
 * bar honest: a bar is drawn only where the provider reports both a used
 * figure and a ceiling, [QuotaMeter.usedPercent] is always the used polarity
 * whatever the badge mode is, and every reading that gets a bar has its
 * duplicate row removed from the sheet -- so a missing bar here silently
 * loses a reading rather than merely dropping a decoration.
 */
class QuotaMetersTest {
    private fun quotaOf(
        type: QuotaType,
        data: QuotaData?,
        available: Boolean = true,
    ) = ProviderQuota(
        providerName = "p",
        type = type,
        data = data,
        fetchedAt = "2026-07-26T00:00:00Z",
        available = available,
    )

    @Test
    fun nanoGptMetersEveryCappedPeriod() {
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.NANOGPT,
                    QuotaData.NanoGpt(
                        limits =
                            NanoGptLimits(
                                weeklyInputTokens = 2_000_000,
                                dailyInputTokens = 500_000,
                                dailyImages = 100,
                            ),
                        weeklyInputTokens = NanoGptTokenInfo(used = 500_000),
                        dailyInputTokens = NanoGptTokenInfo(used = 250_000),
                        dailyImages = NanoGptTokenInfo(used = 25),
                    ),
                ),
            )

        assertEquals(
            listOf(QuotaMeterKind.WEEKLY, QuotaMeterKind.DAILY_INPUT_TOKENS, QuotaMeterKind.DAILY_IMAGES),
            meters.map { it.kind },
        )
        assertEquals(25.0, meters[0].usedPercent, 0.001)
        // The fraction rides along so the bar can print the figures behind it
        // rather than only a percentage.
        assertEquals("500K/2M", meters[0].value)
    }

    @Test
    fun uncappedPeriodGetsNoMeter() {
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.NANOGPT,
                    QuotaData.NanoGpt(
                        // No weekly ceiling: nothing to fill, so no bar. The
                        // sheet keeps its "used/∞" row for exactly this case.
                        limits = NanoGptLimits(weeklyInputTokens = null, dailyInputTokens = 0),
                        weeklyInputTokens = NanoGptTokenInfo(used = 500_000),
                        dailyInputTokens = NanoGptTokenInfo(used = 250_000),
                    ),
                ),
            )

        assertTrue("expected no meters, got ${meters.map { it.kind }}", meters.isEmpty())
    }

    @Test
    fun zaiMetersFiveHourWeeklyAndMcp() {
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.ZAI_CODING,
                    QuotaData.ZaiCoding(
                        data =
                            ZaiCodingQuotaBody(
                                limits =
                                    listOf(
                                        ZaiCodingLimit(type = "TOKENS_LIMIT", unit = 3, percentage = 40.0),
                                        ZaiCodingLimit(type = "TOKENS_LIMIT", unit = 6, percentage = 12.0),
                                        ZaiCodingLimit(type = "TIME_LIMIT", unit = 5, percentage = 90.0),
                                    ),
                            ),
                    ),
                ),
            )

        assertEquals(
            listOf(QuotaMeterKind.FIVE_HOUR, QuotaMeterKind.WEEKLY, QuotaMeterKind.MCP),
            meters.map { it.kind },
        )
        // Z.ai reports a used percentage and no raw figures, so the bar's own
        // length is the whole reading and the value string stays empty.
        assertEquals(90.0, meters[2].usedPercent, 0.001)
        assertEquals("", meters[2].value)
    }

    @Test
    fun kimiMetersConvertWireStringsToUsedPercent() {
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.KIMI_CODE,
                    QuotaData.KimiCode(
                        limits =
                            listOf(
                                KimiCodeLimitEntry(
                                    window = KimiCodeWindowSpec(duration = 300, timeUnit = "TIME_UNIT_MINUTE"),
                                    detail = KimiCodeDetail(limit = "100", remaining = "25"),
                                ),
                            ),
                        usage = KimiCodeDetail(limit = "1000", remaining = "900"),
                    ),
                ),
            )

        assertEquals(listOf(QuotaMeterKind.FIVE_HOUR, QuotaMeterKind.WEEKLY), meters.map { it.kind })
        assertEquals(75.0, meters[0].usedPercent, 0.001)
        assertEquals(10.0, meters[1].usedPercent, 0.001)
    }

    @Test
    fun kimiUnreadableUsedDrawsNoBarForThatWindow() {
        // `used` present but unreadable, and no `remaining` to fall back on:
        // the window has no reading, and a bar drawn from a guess is worse
        // than no bar. The weekly window beside it still reads, so the loss is
        // confined to the window that stopped making sense.
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.KIMI_CODE,
                    QuotaData.KimiCode(
                        limits =
                            listOf(
                                KimiCodeLimitEntry(
                                    window = KimiCodeWindowSpec(duration = 300, timeUnit = "TIME_UNIT_MINUTE"),
                                    detail = KimiCodeDetail(limit = "100", used = "abc"),
                                ),
                            ),
                        usage = KimiCodeDetail(limit = "1000", remaining = "900"),
                    ),
                ),
            )

        assertEquals(listOf(QuotaMeterKind.WEEKLY), meters.map { it.kind })
        assertEquals(10.0, meters.single().usedPercent, 0.001)
    }

    @Test
    fun miniMaxMetersOnlyModelsOnThePlan() {
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.MINIMAX,
                    QuotaData.MiniMax(
                        modelRemains =
                            listOf(
                                MiniMaxModelRemain(
                                    modelName = "general",
                                    currentIntervalStatus = 1,
                                    currentIntervalRemainingPercent = 70.0,
                                    currentWeeklyRemainingPercent = 40.0,
                                ),
                                MiniMaxModelRemain(modelName = "abab", currentIntervalStatus = 3),
                            ),
                    ),
                ),
            )

        // An off-plan model has no windows to meter; it keeps its status row.
        assertEquals(listOf("general", "general"), meters.map { it.subject })
        assertEquals(30.0, meters[0].usedPercent, 0.001)
        assertEquals(60.0, meters[1].usedPercent, 0.001)
    }

    @Test
    fun openRouterCreditsMeterNeedsACeiling() {
        val withTotal =
            quotaMeters(
                quotaOf(QuotaType.OPENROUTER, QuotaData.OpenRouter(creditsUsed = 4.0, creditsTotal = 10.0)),
            )
        assertEquals(listOf(QuotaMeterKind.CREDITS), withTotal.map { it.kind })
        assertEquals(40.0, withTotal[0].usedPercent, 0.001)
        assertEquals("$4.00/$10.00", withTotal[0].value)

        // A pay-as-you-go key reports spend with no ceiling: no bar, and the
        // sheet falls back to its credits-used row.
        val payAsYouGo =
            quotaMeters(quotaOf(QuotaType.OPENROUTER, QuotaData.OpenRouter(creditsUsed = 4.0, creditsTotal = 0.0)))
        assertTrue(payAsYouGo.isEmpty())
    }

    @Test
    fun neuralWattMetersEnergyThenCredits() {
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.NEURALWATT,
                    QuotaData.NeuralWatt(
                        balance =
                            NeuralWattBalance(
                                creditsUsedUsd = 3.0,
                                creditsRemainingUsd = 9.0,
                                totalCreditsUsd = 12.0,
                            ),
                        subscription = NeuralWattSubscription(kwhIncluded = 20.0, kwhUsed = 12.5),
                    ),
                ),
            )

        assertEquals(listOf(QuotaMeterKind.ENERGY, QuotaMeterKind.CREDITS), meters.map { it.kind })
        assertEquals("12.5/20 kWh", meters[0].value)
        assertEquals(25.0, meters[1].usedPercent, 0.001)
    }

    @Test
    fun neuralWattCreditsMeterDerivesSpendFromRemaining() {
        // NeuralWatt reports credits_used_usd = 0 even while overage spend
        // drains credits_remaining_usd; the meter must show the real draw.
        val meters =
            quotaMeters(
                quotaOf(
                    QuotaType.NEURALWATT,
                    QuotaData.NeuralWatt(
                        balance =
                            NeuralWattBalance(
                                creditsUsedUsd = 0.0,
                                creditsRemainingUsd = 9.0,
                                totalCreditsUsd = 12.0,
                            ),
                    ),
                ),
            )

        assertEquals(listOf(QuotaMeterKind.CREDITS), meters.map { it.kind })
        assertEquals(25.0, meters[0].usedPercent, 0.001)
        assertEquals("$3.00/$12.00", meters[0].value)
    }

    @Test
    fun balanceOnlyProvidersHaveNoMeters() {
        val deepSeek =
            quotaMeters(
                quotaOf(
                    QuotaType.DEEPSEEK,
                    QuotaData.DeepSeek(
                        balanceInfos = listOf(DeepSeekBalanceInfo(currency = "USD", totalBalance = "4")),
                    ),
                ),
            )
        val ollama = quotaMeters(quotaOf(QuotaType.OLLAMA_CLOUD, QuotaData.OllamaCloud(plan = "pro")))

        assertTrue(deepSeek.isEmpty())
        assertTrue(ollama.isEmpty())
    }

    @Test
    fun deadKeyHasNoMeters() {
        // available == false is the gate, not data alone: Front Desk can hand
        // back a stale payload alongside a failed re-poll.
        val stale =
            quotaOf(
                QuotaType.OPENROUTER,
                QuotaData.OpenRouter(creditsUsed = 4.0, creditsTotal = 10.0),
                available = false,
            )

        assertTrue(quotaMeters(stale).isEmpty())
        assertTrue(quotaMeters(quotaOf(QuotaType.UNKNOWN, null)).isEmpty())
    }
}
