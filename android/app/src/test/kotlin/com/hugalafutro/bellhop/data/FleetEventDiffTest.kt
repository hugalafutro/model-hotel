package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * diffEvents is pure, so the "which Front Desk events get a notification" rule
 * is pinned here without a worker or a server: a silent first poll, the cursor
 * as the only memory, the picker as the only filter, instants (not text) as the
 * order with the id deciding ties the way Front Desk orders them, oldest-first
 * output so the newest lands on top of the shade, a cap so a phone that missed
 * a day is not paged once per line, and a cursor that never rests on a row
 * whose time cannot be read.
 */
class FleetEventDiffTest {
    private fun event(
        id: String,
        at: String,
        type: String = "member.state_changed",
        member: String = "m1",
    ): FdEvent =
        FdEvent(
            id = id,
            type = type,
            severity = "warning",
            source = "frontdesk",
            message = "$member $type",
            memberId = member,
            createdAt = at,
        )

    private val drainOn = setOf("member.state_changed")

    @Test
    fun firstPollRecordsTheNewestEventAndAlertsOnNothing() {
        // Newest first, as GET /api/events returns it.
        val page = listOf(event("e3", "2026-08-31T11:30:10Z"), event("e2", "2026-08-31T11:28:54Z"))

        val diff = diffEvents(null, page, drainOn)

        assertTrue(diff.alerts.isEmpty())
        assertEquals(EventCursor("2026-08-31T11:30:10Z", "e3"), diff.cursor)
    }

    @Test
    fun eventsAfterTheCursorAreReturnedOldestFirstAndTheCursorAdvances() {
        val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")
        val page =
            listOf(
                event("e4", "2026-08-31T11:31:13Z", member = "m3"),
                event("e3", "2026-08-31T11:30:10Z", member = "m2"),
                event("e2", "2026-08-31T11:28:54Z"),
                event("e1", "2026-08-31T11:20:00Z"),
            )

        val diff = diffEvents(cursor, page, drainOn)

        // Oldest first: posted in this order, the newest is the top row on the
        // shade and, for rows that share a tag, the one that survives.
        assertEquals(listOf("e3", "e4"), diff.alerts.map { it.id })
        assertEquals("m2", diff.alerts.first().memberId)
        assertEquals(EventCursor("2026-08-31T11:31:13Z", "e4"), diff.cursor)
    }

    @Test
    fun onlyTypesTheOperatorSwitchedOnAreReturnedButTheCursorStillAdvances() {
        val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")
        val page =
            listOf(
                event("e4", "2026-08-31T11:31:13Z", type = "config.sync_held"),
                event("e3", "2026-08-31T11:30:10Z", type = "health.down"),
                event("e2", "2026-08-31T11:28:54Z"),
            )

        val diff = diffEvents(cursor, page, setOf("health.down"))

        assertEquals(listOf("e3"), diff.alerts.map { it.id })
        // A switched-off event is still seen: the next poll must not report it
        // later just because the operator switched the type on in between.
        assertEquals(EventCursor("2026-08-31T11:31:13Z", "e4"), diff.cursor)
    }

    @Test
    fun instantsNotTextDecideWhatIsNewer() {
        // "...:54Z" sorts AFTER "...:54.5Z" as text ('Z' > '.'), but is earlier as
        // a time; Front Desk writes whatever fractional precision the row has, so
        // the comparison has to be on the instant.
        val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")
        val page =
            listOf(
                event("e3", "2026-08-31T11:28:54.5Z"),
                event("e2", "2026-08-31T11:28:54Z"),
                event("e1", "2026-08-31T11:28:53.9Z"),
            )

        val diff = diffEvents(cursor, page, drainOn)

        assertEquals(listOf("e3"), diff.alerts.map { it.id })
    }

    @Test
    fun atTheCursorsInstantTheIdDecidesTheWayFrontDeskOrders() {
        // Front Desk orders ties by id, so a row with a lower id at the cursor's
        // instant was ordered before the cursor and has been seen; only a higher
        // id is new. Anything else would re-report the lower row on every poll.
        val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")
        val page =
            listOf(
                event("e2b", "2026-08-31T11:28:54Z"),
                event("e2", "2026-08-31T11:28:54Z"),
                event("e1z", "2026-08-31T11:28:54Z"),
            )

        val diff = diffEvents(cursor, page, drainOn)

        assertEquals(listOf("e2b"), diff.alerts.map { it.id })
    }

    @Test
    fun aPollThatMissedManyEventsPostsOnlyTheNewestFew() {
        val cursor = EventCursor("2026-08-31T10:00:00Z", "e0")
        val page = (20 downTo 1).map { event("e$it", "2026-08-31T11:%02d:00Z".format(it)) }

        val diff = diffEvents(cursor, page, drainOn)

        assertEquals(MAX_EVENT_ALERTS_PER_POLL, diff.alerts.size)
        // The newest five, oldest of them first, newest last.
        assertEquals("e16", diff.alerts.first().id)
        assertEquals("e20", diff.alerts.last().id)
        assertEquals(EventCursor("2026-08-31T11:20:00Z", "e20"), diff.cursor)
    }

    @Test
    fun anEmptyPageKeepsTheCursorItHad() {
        val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")

        val diff = diffEvents(cursor, emptyList(), drainOn)

        assertTrue(diff.alerts.isEmpty())
        assertEquals(cursor, diff.cursor)
    }

    @Test
    fun anUnreadableRowIsNeitherAlertedOnNorMadeTheCursor() {
        val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")
        val page =
            listOf(
                event("e4", "not a time"),
                event("e3", "2026-08-31T11:30:10Z"),
                event("e2", "2026-08-31T11:28:54Z"),
            )

        val diff = diffEvents(cursor, page, drainOn)

        assertEquals(listOf("e3"), diff.alerts.map { it.id })
        // Resting the cursor on the unreadable row would blind the next poll to
        // everything after it, so it rests on the newest row that can be read.
        assertEquals(EventCursor("2026-08-31T11:30:10Z", "e3"), diff.cursor)
    }

    @Test
    fun anUnreadableCursorAlertsOnNothingAndResyncs() {
        val page = listOf(event("e3", "2026-08-31T11:30:10Z"))

        val diff = diffEvents(EventCursor("garbage", "e0"), page, drainOn)

        assertTrue(diff.alerts.isEmpty())
        assertEquals(EventCursor("2026-08-31T11:30:10Z", "e3"), diff.cursor)
        assertNull(diffEvents(null, emptyList(), drainOn).cursor)
    }
}
