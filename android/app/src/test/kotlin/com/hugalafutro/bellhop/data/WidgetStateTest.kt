package com.hugalafutro.bellhop.data

import kotlinx.serialization.json.Json
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
    fun widgetQuotaRespectsOrderAndVisibilityAndKeepsTheWholeSelection() {
        val quotas =
            (1..15).map {
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

        // Everything the operator selected, uncapped: how many fit is decided at
        // render time by width, and pre-trimming here would make the widget's
        // "+N" understate what is missing.
        assertEquals(14, badges.size)
        assertFalse(badges.any { it.providerName == "P2" })
    }

    @Test
    fun widgetQuotaCarriesTheSpentVerdictAndOlderStateDecodesWithoutIt() {
        val funded = QuotaData.OpenRouter(creditsTotal = 10.0, creditsRemaining = 0.0)
        val quota =
            ProviderQuota(
                providerName = "OR",
                type = QuotaType.OPENROUTER,
                data = funded,
                fetchedAt = "t",
                available = true,
            )
        val cfg = QuotaBadgeConfig(order = listOf("OR"))

        val badge = widgetQuotaOf(listOf(quota), cfg, QuotaBarMode.REMAINING).single()
        assertTrue(badge.spent)
        assertFalse(widgetQuotaOf(listOf(quota.copy(available = false)), cfg, QuotaBarMode.REMAINING).single().spent)

        // A state persisted before the flag existed decodes as not spent.
        val old =
            Json.decodeFromString<WidgetQuotaBadge>(
                """{"providerName":"OR","type":"OPENROUTER","label":"OR $0.00"}""",
            )
        assertFalse(old.spent)
    }

    @Test
    fun quotaBadgeRowsPacksToTheRowBudget() {
        // Each badge takes only its own label's width, so the row fills while
        // the sum fits: three short ones (21dp each) plus "999.9M/999.9M"
        // (13 characters, ~87dp) come to 150dp of the default 156dp budget.
        val badges =
            listOf(
                badge("A", "$1"),
                badge("B", "$2"),
                badge("C", "$3"),
                badge("D", "999.9M/999.9M"),
            )
        val rows = quotaBadgeRows(badges)
        assertEquals(listOf(listOf("A", "B", "C", "D")), rows.map { row -> row.map { it.providerName } })
    }

    @Test
    fun quotaBadgeRowsChargeEachBadgeOnlyItsOwnWidth() {
        // Regression on the equal-share layout this replaced: with uniform
        // columns, one long label priced every badge beside it at its own width,
        // so a row carried fewer badges than it had room for. Six "OR 5%" badges
        // (39dp each) beside one "NW 12.5/20 kWh" (93dp) come to 327dp and now
        // share one 327dp row; under the old rule that row fitted three, since
        // it had to hold seven times the widest label (651dp).
        val badges = (1..6).map { badge("S$it", "OR 5%") } + badge("LONG", "NW 12.5/20 kWh")

        val rows = quotaBadgeRows(badges, budgetDp = 327)

        assertEquals(1, rows.size)
        assertEquals(badges.size, rows.single().size)
        assertEquals(0, quotaBadgeOverflow(badges, rows))
    }

    @Test
    fun quotaBadgeRowsNeverOverfillARow() {
        // The badges are content-width and a Glance Row can't shrink them, so a
        // row packed past the budget would clip its last badge. No row may cost
        // more than it was given (a lone badge wider than the whole widget still
        // gets its line -- clipped beats disappeared).
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
                val cost = row.sumOf { badgeWidthDp(it) }
                assertTrue(
                    "row of ${row.size} costs ${cost}dp of a ${budget}dp budget",
                    row.size == 1 || cost <= budget,
                )
            }
        }
    }

    @Test
    fun quotaBadgeRowsPackAgainstTheWidthsTheCallerReports() {
        // The widget measures its labels for real (a TextPaint in the app's own
        // process) rather than guessing per character, so the cost of a badge is
        // the caller's to say. Told that each badge is 30dp instead of the
        // estimate's 45dp, the same 156dp row has to take five instead of three
        // -- that difference is the whole reason for measuring.
        val badges = (1..6).map { badge("P$it", "OR 50%") }

        val estimated = quotaBadgeRows(badges, budgetDp = 156)
        val measured = quotaBadgeRows(badges, budgetDp = 156, widthDp = { 30 })

        assertEquals(3, estimated.first().size)
        assertEquals(5, measured.first().size)
    }

    @Test
    fun theOverflowMarkerReserveUsesTheCallersWidthsToo() {
        // The last row's trim has to price badges the same way the packing did,
        // or a measured strip would be fitted for the marker by estimate and
        // give up a badge it had room for.
        val badges = (1..40).map { badge("P$it", "OR 50%") }
        val rows = quotaBadgeRows(badges, budgetDp = 156, widthDp = { 30 })

        assertTrue("40 badges cannot fit four rows", quotaBadgeOverflow(badges, rows) > 0)
        // 156dp less the 28dp marker leaves 128dp: four 30dp badges, not three.
        assertEquals(4, rows.last().size)
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
    fun quotaBadgeRowsLeaveTheOverflowMarkerItsWidth() {
        // The "+N" shares the last row with its badges, so they have only what
        // it doesn't take. A last row fitted against the full width would push
        // the marker past the edge (or clip the badge before it), so the
        // marker's width comes out of the budget first.
        //
        // 170dp is the width that makes this bind rather than merely hold: a
        // 12-character label is ~81dp, two of them fit 170dp on their own
        // (162dp), and only the marker's reserve pushes them over. Widths where
        // the row already held one badge, or had slack to spare, pass whether
        // the reserve is applied or not -- an earlier version of this test used
        // only those and went green against a deliberately reverted fix.
        val badges = (1..12).map { badge("P$it", "NANO 1.9M/3M") }

        listOf(120, 156, 170, 240).forEach { budget ->
            val rows = quotaBadgeRows(badges, budget)
            if (quotaBadgeOverflow(badges, rows) == 0) return@forEach
            val last = rows.last()
            val cost = last.sumOf { badgeWidthDp(it) }
            val room = budget - WIDGET_QUOTA_OVERFLOW_MARKER_DP
            assertTrue(
                "last row of ${last.size} costs ${cost}dp of the ${room}dp left beside the marker at ${budget}dp",
                last.size == 1 || cost <= room,
            )
        }
    }

    @Test
    fun theMarkerReserveActuallyTrimsTheLastRow() {
        // Guards the assertion above from going vacuous: at 170dp the packer
        // fills the last row with two badges and only the marker's reserve takes
        // one back, so this pins that the reserve is doing something. Remove the
        // reserve and this fails; the invariant test fails with it.
        val badges = (1..12).map { badge("P$it", "NANO 1.9M/3M") }
        val rows = quotaBadgeRows(badges, budgetDp = 170)

        assertEquals(1, rows.last().size)
        assertEquals(2, rows.first().size)
    }

    @Test
    fun badgesTrimmedForTheMarkerAreCountedAsOverflow() {
        // A badge pushed out of the last row to make room for the marker is not
        // lost track of -- it joins the count the marker itself reports. Fifteen
        // 45dp badges pack three to a 156dp row, so the strip overflows and the
        // last row gives one back to the marker: 3+3+3+2 shown, 4 counted.
        val badges = (1..15).map { badge("P$it", "OR 10%") }
        val rows = quotaBadgeRows(badges, budgetDp = 156)

        assertTrue("the strip must overflow for this to say anything", quotaBadgeOverflow(badges, rows) > 0)
        assertEquals(2, rows.last().size)
        assertEquals(badges.size, rows.sumOf { it.size } + quotaBadgeOverflow(badges, rows))
    }

    @Test
    fun overflowCountsEveryBadgeTheStripLeftOut() {
        // The marker has to speak for the whole selection. When the badge list
        // was pre-trimmed before it reached the strip, this arithmetic held
        // against the trimmed list and the "+N" understated the shortfall.
        val quotas =
            (1..15).map {
                ProviderQuota(
                    providerName = "P$it",
                    type = QuotaType.OPENROUTER,
                    data = QuotaData.OpenRouter(limitReset = "k", limit = 10.0, creditsRemaining = 1.0),
                    fetchedAt = "t",
                    available = true,
                )
            }
        val badges =
            widgetQuotaOf(
                quotas,
                QuotaBadgeConfig(order = quotas.map { it.providerName }, hidden = emptySet()),
                QuotaBarMode.REMAINING,
            )
        val rows = quotaBadgeRows(badges, budgetDp = 156)

        assertEquals(15, badges.size)
        assertEquals(15 - rows.sumOf { it.size }, quotaBadgeOverflow(badges, rows))
        assertTrue("a 15-badge selection cannot fit a narrow strip", quotaBadgeOverflow(badges, rows) > 0)
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
