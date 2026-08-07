package com.hugalafutro.bellhop.data

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Cross-platform drift net for Kimi Code quota parsing.
 *
 * The fixtures under `testdata/quota-contract/kimi/` are shared with the web
 * dashboard and Front Desk suites, so every app that renders a stored Kimi
 * snapshot derives the same numbers from it. Each fixture carries the raw
 * `payload` Front Desk stores plus the `expected` percentages; a fixture whose
 * expectation Bellhop cannot reproduce fails here rather than showing a
 * different figure than the web badge for the same snapshot.
 *
 * Payloads go through [providerQuotaOf] and [quotaMeters], the production
 * decode and metering path, so the test pins what the widget actually renders
 * and not a parallel parser written for the test.
 */
class KimiQuotaContractTest {
    @Serializable
    private data class KimiContractExpected(
        val fiveHourUsedPercent: Double,
        val weeklyUsedPercent: Double,
    )

    @Serializable
    private data class KimiContractFixture(
        val payload: JsonElement,
        val expected: KimiContractExpected,
    )

    // The fixture envelope only, never the payload: the payload is decoded by
    // the app's own quota Json config inside providerQuotaOf.
    private val fixtureJson = Json { ignoreUnknownKeys = true }

    /**
     * The shared fixture directory, handed to the test JVM by Gradle as
     * `modelhotel.testdata.dir` so it resolves the same whether Gradle is
     * invoked from `android/` or from the repo root.
     */
    private fun fixtureDir(): File {
        val root =
            System.getProperty("modelhotel.testdata.dir")
                ?: error("modelhotel.testdata.dir is unset; app/build.gradle.kts sets it from the repo root")
        return File(root, "quota-contract/kimi")
    }

    private fun fixtureFiles(): List<File> {
        val dir = fixtureDir()
        assertTrue("missing shared fixture directory ${dir.absolutePath}", dir.isDirectory)
        return dir.listFiles { f: File -> f.isFile && f.name.endsWith(".json") }.orEmpty().sortedBy { it.name }
    }

    private fun meterPercent(
        meters: List<QuotaMeter>,
        kind: QuotaMeterKind,
        label: String,
    ): Double {
        val meter = meters.find { it.kind == kind }
        assertNotNull("$label: no $kind meter, so the badge would render \"-\"", meter)
        return meter!!.usedPercent
    }

    @Test
    fun sharedFixtureDirectoryIsPopulated() {
        // Guards against a silently green run when the directory is gone or a
        // path change leaves the glob matching nothing.
        val files = fixtureFiles()
        assertTrue("expected at least 3 Kimi contract fixtures, found ${files.size}", files.size >= 3)
    }

    @Test
    fun everyFixtureYieldsTheExpectedPercentages() {
        for (file in fixtureFiles()) {
            val fixture = fixtureJson.decodeFromString<KimiContractFixture>(file.readText())
            val wire =
                QuotaWire(
                    providerName = "kimi",
                    type = "kimi-code",
                    kind = "usage",
                    payload = fixture.payload,
                    httpStatus = 200,
                    fetchedAt = "2026-08-07T00:00:00Z",
                )
            val meters = quotaMeters(providerQuotaOf(wire))

            assertEquals(
                "${file.name}: five-hour",
                fixture.expected.fiveHourUsedPercent,
                meterPercent(meters, QuotaMeterKind.FIVE_HOUR, file.name),
                1e-9,
            )
            assertEquals(
                "${file.name}: weekly",
                fixture.expected.weeklyUsedPercent,
                meterPercent(meters, QuotaMeterKind.WEEKLY, file.name),
                1e-9,
            )
        }
    }
}
