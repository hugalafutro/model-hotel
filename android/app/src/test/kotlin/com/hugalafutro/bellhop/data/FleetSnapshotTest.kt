package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The backstop's diff is pure, so it is tested directly (no worker, no clock, no
 * counters) the way the SSE filter and the lock timer are: assert the edge set a
 * snapshot pair produces, so the "what warrants a notification" rule is pinned
 * without any Android or network machinery.
 */
class FleetSnapshotTest {
    private fun member(
        id: String,
        name: String = id,
        state: String = "active",
        known: Boolean = true,
        healthy: Boolean = true,
    ): FleetMember =
        FleetMember(
            id = id,
            name = name,
            state = state,
            status = MemberStatus(health = HealthStatus(known = known, healthy = healthy)),
        )

    private fun snapshotOf(vararg members: FleetMember): FleetSnapshot = FleetSnapshot.of(members.toList())

    @Test
    fun healthStateCollapsesTheFourCases() {
        assertEquals(MemberHealthState.UP, healthStateOf(member("m", healthy = true)))
        assertEquals(MemberHealthState.DOWN, healthStateOf(member("m", healthy = false)))
        assertEquals(MemberHealthState.UNKNOWN, healthStateOf(member("m", known = false)))
        // Drained wins over health: a drained member that still probes healthy is
        // DRAINED, so draining never reads as an outage.
        assertEquals(MemberHealthState.DRAINED, healthStateOf(member("m", state = "drained", healthy = true)))
    }

    @Test
    fun firstPollHasNoBaselineSoStaysSilent() {
        // Null previous = fresh opt-in; alerting here would buzz once per member.
        assertTrue(diffFleet(null, listOf(member("m1", healthy = false))).isEmpty())
    }

    @Test
    fun upToDownIsWentDown() {
        val previous = snapshotOf(member("m1", name = "hotel-1", healthy = true))
        val transitions = diffFleet(previous, listOf(member("m1", name = "hotel-1", healthy = false)))
        assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), transitions)
    }

    @Test
    fun downToUpIsRecovered() {
        val previous = snapshotOf(member("m1", name = "hotel-1", healthy = false))
        val transitions = diffFleet(previous, listOf(member("m1", name = "hotel-1", healthy = true)))
        assertEquals(listOf(MemberTransition.Recovered("m1", "hotel-1")), transitions)
    }

    @Test
    fun steadyStateProducesNoTransition() {
        val previous = snapshotOf(member("m1", healthy = true))
        assertTrue(diffFleet(previous, listOf(member("m1", healthy = true))).isEmpty())
    }

    @Test
    fun drainingIsNotAnOutage() {
        val previous = snapshotOf(member("m1", healthy = true))
        val transitions = diffFleet(previous, listOf(member("m1", state = "drained", healthy = true)))
        assertTrue(transitions.isEmpty())
    }

    @Test
    fun edgesThroughUnknownStayQuiet() {
        // A poller briefly losing sight of a member (UP -> UNKNOWN -> UP) must not
        // fire a down/recovered pair, so neither edge is a transition.
        val wasUp = snapshotOf(member("m1", healthy = true))
        assertTrue(diffFleet(wasUp, listOf(member("m1", known = false))).isEmpty())

        val wasUnknown = snapshotOf(member("m1", known = false))
        assertTrue(diffFleet(wasUnknown, listOf(member("m1", healthy = false))).isEmpty())
    }

    @Test
    fun newlyAddedMemberHasNoBaselineEdge() {
        // m2 wasn't in the previous snapshot; it must wait for a baseline before it
        // can ever produce a transition, even if it appears already down.
        val previous = snapshotOf(member("m1", healthy = true))
        val transitions =
            diffFleet(
                previous,
                listOf(member("m1", healthy = true), member("m2", healthy = false)),
            )
        assertTrue(transitions.isEmpty())
    }

    @Test
    fun transitionLabelFallsBackToIdWhenNameBlank() {
        val previous = snapshotOf(member("m1", name = "", healthy = true))
        val transitions = diffFleet(previous, listOf(member("m1", name = "", healthy = false)))
        assertEquals(listOf(MemberTransition.WentDown("m1", "m1")), transitions)
    }

    @Test
    fun autoSyncFirstPollHasNoBaselineSoStaysSilent() {
        assertEquals(null, diffAutoSync(null, current = true))
    }

    @Test
    fun autoSyncNotStaleToStaleIsWentStale() {
        val previous = FleetSnapshot(autosyncStale = false)
        assertEquals(AutoSyncAlert.WentStale, diffAutoSync(previous, current = true))
    }

    @Test
    fun autoSyncStaleToNotStaleIsResumed() {
        val previous = FleetSnapshot(autosyncStale = true)
        assertEquals(AutoSyncAlert.Resumed, diffAutoSync(previous, current = false))
    }

    @Test
    fun autoSyncSteadyStateProducesNoAlert() {
        assertEquals(null, diffAutoSync(FleetSnapshot(autosyncStale = true), current = true))
        assertEquals(null, diffAutoSync(FleetSnapshot(autosyncStale = false), current = false))
    }

    @Test
    fun ofCarriesAutoSyncStaleIntoSnapshot() {
        assertTrue(FleetSnapshot.of(emptyList(), autosyncStale = true).autosyncStale)
        assertTrue(!FleetSnapshot.of(emptyList()).autosyncStale)
    }
}
