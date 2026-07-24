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
