package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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
                WidgetMember("hotel-1", "UP"),
                WidgetMember("hotel-2", "DOWN"),
                WidgetMember("hotel-3", "DRAINED"),
                WidgetMember("hotel-4", "UNKNOWN"),
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
}
