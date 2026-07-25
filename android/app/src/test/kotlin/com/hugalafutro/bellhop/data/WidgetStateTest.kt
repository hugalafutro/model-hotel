package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The widget's render model is built by a pure function from the same fetch the
 * backstop poll already makes; these tests pin the mapping so the widget can
 * stay logic-free.
 */
class WidgetStateTest {
    private fun member(
        id: String,
        name: String = "",
        healthy: Boolean = true,
        known: Boolean = true,
        drained: Boolean = false,
        newestEvent: FdEvent? = null,
    ): FleetMember =
        FleetMember(
            id = id,
            name = name,
            state = if (drained) "drained" else "active",
            status = MemberStatus(health = HealthStatus(known = known, healthy = healthy)),
            newestEvent = newestEvent,
        )

    @Test
    fun mapsMembersToNameAndHealthState() {
        val state =
            widgetStateOf(
                members =
                    listOf(
                        member("m1", name = "hotel-1", healthy = true),
                        member("m2", name = "hotel-2", healthy = false),
                        member("m3", name = "hotel-3", drained = true),
                        member("m4", name = "hotel-4", known = false),
                    ),
                autosyncStale = true,
                now = 42L,
            )
        assertEquals(
            listOf(
                WidgetMember("hotel-1", "UP", id = "m1"),
                WidgetMember("hotel-2", "DOWN", id = "m2"),
                WidgetMember("hotel-3", "DRAINED", id = "m3"),
                WidgetMember("hotel-4", "UNKNOWN", id = "m4"),
            ),
            state.members,
        )
        assertEquals(true, state.autosyncStale)
        assertEquals(42L, state.updatedAt)
    }

    @Test
    fun blankNameFallsBackToId() {
        val state = widgetStateOf(listOf(member("m1")), autosyncStale = false, now = 0L)
        assertEquals("m1", state.members.single().name)
    }

    @Test
    fun newestEventIsFleetWideMaxByCreatedAt() {
        val older = FdEvent(id = "e1", message = "older", createdAt = "2026-07-18T10:00:00Z")
        val newer = FdEvent(id = "e2", message = "newer", createdAt = "2026-07-18T11:00:00Z")
        val state =
            widgetStateOf(
                listOf(member("m1", newestEvent = older), member("m2", newestEvent = newer)),
                autosyncStale = false,
                now = 0L,
            )
        assertEquals(WidgetEvent("newer", "2026-07-18T11:00:00Z"), state.newestEvent)
    }

    @Test
    fun noEventsMeansNullNewestEvent() {
        val state = widgetStateOf(listOf(member("m1")), autosyncStale = false, now = 0L)
        assertNull(state.newestEvent)
    }

    @Test
    fun unknownStoredStateDegradesToUnknown() {
        // A future build may persist a state name this build doesn't know.
        assertEquals(MemberHealthState.UNKNOWN, WidgetMember("x", "SOMETHING_NEW").healthState)
    }

    @Test
    fun countsBucketAllFourStates() {
        val state =
            widgetStateOf(
                listOf(
                    member("m1", healthy = true),
                    member("m2", healthy = true),
                    member("m3", healthy = false),
                    member("m4", drained = true),
                    member("m5", known = false),
                ),
                autosyncStale = false,
                now = 0L,
            )
        assertEquals(WidgetCounts(up = 2, down = 1, drained = 1, unknown = 1), countsOf(state))
    }

    @Test
    fun trafficBucketsMapByIdAndMissingMembersGetNone() {
        val state =
            widgetStateOf(
                listOf(member("m1", name = "hotel-1"), member("m2", name = "hotel-2")),
                autosyncStale = false,
                now = 0L,
                traffic = mapOf("m1" to listOf(1, 2, 3)),
            )
        assertEquals(listOf(1, 2, 3), state.members[0].traffic)
        assertEquals(emptyList<Int>(), state.members[1].traffic)
    }

    @Test
    fun trafficKeepsOnlyTheNewestTwelveBuckets() {
        // Writers hand over whatever window they fetched; the model owns the
        // widget's 12-bucket (one hour of 5-minute buckets) contract.
        val state =
            widgetStateOf(
                listOf(member("m1")),
                autosyncStale = false,
                now = 0L,
                traffic = mapOf("m1" to (1..15).toList()),
            )
        assertEquals((4..15).toList(), state.members.single().traffic)
    }

    @Test
    fun widgetQuotaRespectsOrderVisibilityAndCap() {
        val quotas =
            (1..WIDGET_QUOTA_CAP + 3).map {
                ProviderQuota(
                    providerName = "P$it",
                    type = QuotaType.OPENROUTER,
                    data = QuotaData.OpenRouter(limitReset = "k", limit = 10.0, creditsRemaining = it.toDouble()),
                    fetchedAt = "t",
                    available = true,
                )
            }
        val cfg = QuotaBadgeConfig(order = quotas.map { it.providerName }, hidden = setOf("P2"))
        val badges = widgetQuotaOf(quotas, cfg, QuotaBarMode.REMAINING)
        assertEquals(WIDGET_QUOTA_CAP, badges.size)
        assertFalse(badges.any { it.providerName == "P2" }) // hidden dropped before the cap
    }

    @Test
    fun quotaBadgeRowsPacksToTheRowBudget() {
        // Uniform columns, so a row of n badges needs n * its own widest to fit:
        // "999.9M/999.9M" is 13 characters (~87dp), which claims a row alone,
        // while the three short ones share theirs.
        val badges =
            listOf(
                badge("A", "$1"),
                badge("B", "$2"),
                badge("C", "$3"),
                badge("D", "999.9M/999.9M"),
            )
        val rows = quotaBadgeRows(badges)
        assertEquals(
            listOf(listOf("A", "B", "C"), listOf("D")),
            rows.map { row -> row.map { it.providerName } },
        )
    }

    @Test
    fun quotaBadgeRowsSizeEachRowOffItsOwnWidest() {
        // Regression: sizing every row off the *strip's* widest label let one
        // long badge force the whole strip to one per row, and the row cap then
        // hid the rest of the selection. The long one takes its own row; the
        // short ones must still share.
        val badges = (1..6).map { badge("S$it", "OR 5%") } + badge("LONG", "NW 12.5/20 kWh")

        val rows = quotaBadgeRows(badges, budgetDp = 156)

        assertTrue("short badges must still share a row", rows.first().size > 1)
        assertEquals(badges.size, rows.sumOf { it.size })
        assertEquals(0, quotaBadgeOverflow(badges, rows))
    }

    @Test
    fun quotaBadgeRowsNeverGiveARowAShareNarrowerThanItsWidestBadge() {
        // The widget stretches a row's badges to equal shares, so any row whose
        // share is narrower than its longest label would clip that label.
        val badges =
            listOf(
                badge("a", "OR $1"),
                badge("b", "NANO 1.9M/3M"),
                badge("c", "ZAI 50%/80%"),
                badge("d", "DS $5.00"),
                badge("e", "NW 12.5/20 kWh"),
                badge("f", "OLC pro"),
            )
        listOf(120, 156, 240, 320, 480).forEach { budget ->
            quotaBadgeRows(badges, budget).forEach { row ->
                val widest = row.maxOf { it.label.length * 6 + 9 }
                assertTrue(
                    "row of ${row.size} at ${budget}dp gives ${budget / row.size}dp to a ${widest}dp badge",
                    row.size == 1 || budget / row.size >= widest,
                )
            }
        }
    }

    @Test
    fun quotaBadgeRowsFitMoreOnAWiderWidget() {
        // Same badges, a 4-column widget's inner width: the strip fills the row
        // it was given instead of the narrowest one the widget can be resized to.
        val badges = (1..6).map { badge("P$it", "OR ${it}0%") }

        assertEquals(2, quotaBadgeRows(badges, budgetDp = 100).first().size)
        assertEquals(6, quotaBadgeRows(badges, budgetDp = 300).single().size)
    }

    @Test
    fun quotaBadgeRowsLeaveGlanceAChildForTheOverflowMarker() {
        // A Row is one Glance container and Glance drops children past ten, so
        // the packer stops at nine to leave the "+N" marker a slot.
        val rows = quotaBadgeRows((1..20).map { badge("P$it", "1") }, budgetDp = 4000)
        assertEquals(9, rows.first().size)
    }

    @Test
    fun quotaBadgeRowsGivesAnOversizedBadgeItsOwnRow() {
        val rows = quotaBadgeRows(listOf(badge("wide", "x".repeat(80)), badge("next", "$1")))
        assertEquals(listOf(listOf("wide"), listOf("next")), rows.map { row -> row.map { it.providerName } })
    }

    @Test
    fun quotaBadgeRowsStopsAtTheRowCapAndReportsWhatItDropped() {
        // One badge per row (each fills the budget on its own), so the row cap
        // is what bounds the strip's height -- and the overflow it drops is
        // counted rather than vanishing.
        val badges = (1..WIDGET_QUOTA_MAX_ROWS + 2).map { badge("P$it", "x".repeat(40)) }
        val rows = quotaBadgeRows(badges)

        assertEquals(WIDGET_QUOTA_MAX_ROWS, rows.size)
        assertEquals("P1", rows.first().single().providerName)
        assertEquals(2, quotaBadgeOverflow(badges, rows))
    }

    @Test
    fun quotaBadgeOverflowIsZeroWhenEverythingFits() {
        val badges = listOf(badge("a", "1"), badge("b", "2"))
        assertEquals(0, quotaBadgeOverflow(badges, quotaBadgeRows(badges, budgetDp = 320)))
    }

    private fun badge(
        name: String,
        label: String,
    ) = WidgetQuotaBadge(providerName = name, type = QuotaType.OPENROUTER.name, label = label)

    @Test
    fun widgetQuotaThreadsModeIntoLabel() {
        val quota =
            ProviderQuota(
                providerName = "OR",
                type = QuotaType.OPENROUTER,
                data = QuotaData.OpenRouter(limitReset = "k", limit = 10.0, creditsRemaining = 4.0),
                fetchedAt = "t",
                available = true,
            )
        val cfg = QuotaBadgeConfig(order = listOf("OR"), hidden = emptySet())
        val remaining = widgetQuotaOf(listOf(quota), cfg, QuotaBarMode.REMAINING)
        val used = widgetQuotaOf(listOf(quota), cfg, QuotaBarMode.USED)
        // OpenRouter is a BALANCE type: mode doesn't change the figure.
        assertEquals(remaining.single().label, used.single().label)
    }
}
