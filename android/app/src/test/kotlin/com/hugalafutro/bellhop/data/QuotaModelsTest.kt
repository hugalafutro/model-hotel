package com.hugalafutro.bellhop.data

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the FD `/api/quota` wire shape and per-type payload parsing: `type`
 * selects the badge/payload variant (never `kind`, which is just
 * usage|balance|account bookkeeping), and a non-200 status or an unparseable
 * payload degrades to `data = null, available = false` rather than throwing —
 * mirroring [FleetSnapshot.stateOf]'s tolerant stance on bad/foreign data.
 */
class QuotaModelsTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun parsesOpenRouterPayloadOnSuccess() {
        val body =
            """
            {"quota":[{"provider_name":"OR","type":"openrouter","kind":"usage",
            "payload":{"label":"k","limit":10.0,"usage":2.5},"http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals("OR", pq.providerName)
        assertEquals(QuotaType.OPENROUTER, pq.type)
        assertEquals("2026-07-24T00:00:00Z", pq.fetchedAt)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.OpenRouter
        assertEquals(2.5, data.usage, 0.0)
        assertEquals(10.0, data.limit)
    }

    @Test
    fun nonSuccessStatusDegradesToUnavailableNullData() {
        val body =
            """
            {"quota":[{"provider_name":"OR","type":"openrouter","kind":"usage",
            "payload":null,"http_status":424,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.OPENROUTER, pq.type)
        assertFalse(pq.available)
        assertNull(pq.data)
    }

    @Test
    fun unrecognizedWireTypeMapsToUnknown() {
        val wire =
            QuotaWire(
                providerName = "brand-new",
                type = "some-future-provider",
                kind = "usage",
                payload = null,
                httpStatus = 200,
                fetchedAt = "2026-07-24T00:00:00Z",
            )
        val pq = providerQuotaOf(wire)

        assertEquals(QuotaType.UNKNOWN, pq.type)
        assertFalse(pq.available)
        assertNull(pq.data)
    }

    @Test
    fun unparseablePayloadDegradesRatherThanThrowing() {
        // http_status is 200 (a "success" per FD) but the payload doesn't decode
        // into the expected shape for this type -- must not throw.
        val body =
            """
            {"quota":[{"provider_name":"OR","type":"openrouter","kind":"usage",
            "payload":{"usage":"not-a-number"},"http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertFalse(pq.available)
        assertNull(pq.data)
    }

    @Test
    fun parsesDeepSeekBalancePayload() {
        val body =
            """
            {"quota":[{"provider_name":"DS","type":"deepseek","kind":"balance",
            "payload":{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"12.34","granted_balance":"0","topped_up_balance":"12.34"}]},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.DEEPSEEK, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.DeepSeek
        assertTrue(data.isAvailable)
        assertEquals("USD", data.balanceInfos.single().currency)
        assertEquals("12.34", data.balanceInfos.single().totalBalance)
    }

    @Test
    fun parsesNanoGptUsagePayloadWithCamelCaseKeys() {
        val body =
            """
            {"quota":[{"provider_name":"NG","type":"nanogpt","kind":"usage",
            "payload":{"active":true,"provider":"nanogpt","providerStatus":"active",
            "allowOverage":false,"cancelAtPeriodEnd":false,
            "limits":{"weeklyInputTokens":1000000,"dailyInputTokens":null,"dailyImages":null},
            "period":{"currentPeriodEnd":"2026-08-01T00:00:00Z"},
            "weeklyInputTokens":{"used":250000,"remaining":750000,"percentUsed":25.0,"resetAt":1234567890}},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.NANOGPT, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.NanoGpt
        assertTrue(data.active)
        assertEquals(1_000_000L, data.limits.weeklyInputTokens)
        assertEquals(250_000L, data.weeklyInputTokens?.used)
        assertNull(data.limits.dailyImages)
    }

    @Test
    fun parsesZaiCodingQuotaPayload() {
        val body =
            """
            {"quota":[{"provider_name":"ZAI","type":"zai-coding","kind":"usage",
            "payload":{"code":0,"msg":"ok","success":true,"data":{"level":"pro","limits":[
            {"type":"TOKENS_LIMIT","unit":3,"number":100,"usage":10,"currentValue":90,"remaining":90,
            "percentage":10.0,"nextResetTime":1234567890,"usageDetails":[{"modelCode":"glm-4.6","usage":10}]}]}},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.ZAI_CODING, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.ZaiCoding
        assertTrue(data.success)
        assertEquals("pro", data.data.level)
        assertEquals(10.0, data.data.limits.single().percentage, 0.0)
        assertEquals(1234567890L, data.data.limits.single().nextResetTime)
    }

    @Test
    fun parsesKimiCodeQuotaPayload() {
        val body =
            """
            {"quota":[{"provider_name":"KIMI","type":"kimi-code","kind":"usage",
            "payload":{"user":{"membership":{"level":"pro"}},
            "usage":{"limit":"1000000","remaining":"500000","resetTime":"2026-07-25T00:00:00Z"},
            "limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},
            "detail":{"limit":"100000","remaining":"40000","resetTime":"2026-07-24T05:00:00Z"}}],
            "parallel":{"limit":"5"},
            "totalQuota":{"limit":"2000000","remaining":"900000","resetTime":"2026-08-01T00:00:00Z"}},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.KIMI_CODE, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.KimiCode
        assertEquals("pro", data.user.membership.level)
        assertEquals(300, data.limits.single().window.duration)
        assertEquals("40000", data.limits.single().detail.remaining)
        assertEquals("900000", data.totalQuota.remaining)
    }

    @Test
    fun parsesMiniMaxQuotaPayloadWithSnakeCaseKeys() {
        val body =
            """
            {"quota":[{"provider_name":"MM","type":"minimax","kind":"usage",
            "payload":{"model_remains":[{"model_name":"general","start_time":1000,"end_time":19000000,
            "remains_time":18000000,"weekly_remains_time":600000000,"current_interval_status":1,
            "current_interval_remaining_percent":72.5,"current_weekly_status":1,
            "current_weekly_remaining_percent":88.0}],"base_resp":{"status_code":0,"status_msg":"ok"}},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.MINIMAX, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.MiniMax
        val entry = data.modelRemains.single()
        assertEquals("general", entry.modelName)
        assertEquals(72.5, entry.currentIntervalRemainingPercent, 0.0)
        assertEquals(88.0, entry.currentWeeklyRemainingPercent, 0.0)
        assertEquals(0, data.baseResp.statusCode)
    }

    @Test
    fun parsesNeuralWattQuotaPayloadWithSnakeCaseKeys() {
        val body =
            """
            {"quota":[{"provider_name":"NW","type":"neuralwatt","kind":"account",
            "payload":{"snapshot_at":"2026-07-24T00:00:00Z",
            "balance":{"credits_remaining_usd":42.5,"total_credits_usd":100.0,"credits_used_usd":57.5,
            "accounting_method":"prepaid"},
            "usage":{"lifetime":{"cost_usd":500.0,"requests":1000,"tokens":2000000,"energy_kwh":12.5},
            "current_month":{"cost_usd":10.0,"requests":20,"tokens":40000,"energy_kwh":0.5}},
            "limits":{"overage_limit_usd":25.0,"rate_limit_tier":"standard"},
            "subscription":{"plan":"pro","status":"active","billing_interval":"monthly",
            "current_period_start":"2026-07-01T00:00:00Z","current_period_end":"2026-08-01T00:00:00Z",
            "auto_renew":true,"kwh_included":50.0,"kwh_used":20.0,"kwh_remaining":30.0,"in_overage":false},
            "key":{"name":"prod","allowance":100.0}},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.NEURALWATT, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.NeuralWatt
        assertEquals(42.5, data.balance.creditsRemainingUsd, 0.0)
        assertEquals(20.0, data.subscription.kwhUsed, 0.0)
        assertEquals(100.0, data.key.allowance)
    }

    @Test
    fun parsesOllamaCloudPayloadWithSnakeCaseKeys() {
        val body =
            """
            {"quota":[{"provider_name":"OC","type":"ollama-cloud","kind":"account",
            "payload":{"id":"acc_1","email":"a@b.com","name":"A","plan":"pro",
            "customer_id":{"string":"cus_1","valid":true},
            "subscription_id":{"string":"sub_1","valid":true},
            "subscription_period_start":{"time":"2026-07-01T00:00:00Z","valid":true},
            "subscription_period_end":{"time":"2026-08-01T00:00:00Z","valid":true},
            "suspended_at":{"time":"","valid":false}},
            "http_status":200,"fetched_at":"2026-07-24T00:00:00Z"}]}
            """.trimIndent()
        val env = json.decodeFromString<QuotaEnvelope>(body)
        val pq = providerQuotaOf(env.quota.single())

        assertEquals(QuotaType.OLLAMA_CLOUD, pq.type)
        assertTrue(pq.available)
        val data = pq.data as QuotaData.OllamaCloud
        assertEquals("pro", data.plan)
        assertEquals("2026-08-01T00:00:00Z", data.subscriptionPeriodEnd.time)
        assertTrue(data.subscriptionPeriodEnd.valid)
        assertFalse(data.suspendedAt.valid)
    }

    @Test
    fun fromWireCoversAllEightKnownTypes() {
        val known =
            mapOf(
                "nanogpt" to QuotaType.NANOGPT,
                "zai-coding" to QuotaType.ZAI_CODING,
                "kimi-code" to QuotaType.KIMI_CODE,
                "minimax" to QuotaType.MINIMAX,
                "deepseek" to QuotaType.DEEPSEEK,
                "openrouter" to QuotaType.OPENROUTER,
                "ollama-cloud" to QuotaType.OLLAMA_CLOUD,
                "neuralwatt" to QuotaType.NEURALWATT,
            )
        known.forEach { (wire, expected) -> assertEquals(expected, QuotaType.fromWire(wire)) }
        assertEquals(QuotaType.UNKNOWN, QuotaType.fromWire("garbage"))
    }
}
