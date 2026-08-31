package com.hugalafutro.bellhop.work

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.hugalafutro.bellhop.data.AutoSyncAlert
import com.hugalafutro.bellhop.data.EventCursor
import com.hugalafutro.bellhop.data.FleetSnapshot
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.FrontDeskEvent
import com.hugalafutro.bellhop.data.MemberHealthState
import com.hugalafutro.bellhop.data.MemberTransition
import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.data.QuotaBadgeConfigStore
import com.hugalafutro.bellhop.data.WidgetStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * The Front Desk half of a poll: the alert picker and the event log are read
 * after the fleet, the events the picker has on come back as alerts, Front
 * Desk's own event for the same outage or drift replaces Bellhop's own alert
 * only when it is actually being posted this poll, and a failed read of either
 * half costs the operator nothing (the local alert stands and the cursor stays
 * put). Fetch order is what the enqueue order pins: members, auto-sync, quota,
 * alert selection, events.
 */
class FleetEventPollTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private lateinit var server: MockWebServer
    private val client = FrontDeskClient()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    private fun <T> preferences(
        name: String,
        build: (DataStore<Preferences>) -> T,
    ): T {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        return build(PreferenceDataStoreFactory.create(scope = scope) { File(tmp.newFolder(), "$name.preferences_pb") })
    }

    private fun newStore(): MonitorStore = preferences("monitor") { MonitorStore(it) }

    private fun memberBody(healthy: Boolean): String =
        """[{"id":"m1","name":"hotel-1","state":"active",""" +
            """"status":{"health":{"known":true,"healthy":$healthy}}}]"""

    private fun selection(vararg on: String): String =
        """{"events":[""" +
            listOf("member.state_changed", "health.down", "health.up", "config.sync_held", "config.autosync_stale")
                .joinToString(
                    ",",
                ) { """{"type":"$it","category":"Fleet","severity":"warning","enabled":${it in on}}""" } +
            "]}"

    private fun event(
        id: String,
        type: String,
        at: String,
        message: String = "$id $type",
        member: String = "m1",
    ): String =
        """{"id":"$id","type":"$type","severity":"warning","source":"frontdesk",""" +
            """"message":"$message","member_id":"$member","created_at":"$at"}"""

    private fun eventsPage(vararg events: String): String =
        """{"events":[${events.joinToString(",")}],"total":${events.size}}"""

    private fun enqueueFleet(
        healthy: Boolean,
        stale: Boolean = false,
    ) {
        server.enqueue(MockResponse().setBody(memberBody(healthy)))
        server.enqueue(MockResponse().setBody("""{"enabled":false,"primary_id":"m1","stale":$stale}"""))
        server.enqueue(MockResponse().setBody("""{"quota":[]}"""))
    }

    private suspend fun poll(store: MonitorStore): PollResult =
        pollFleet(
            client,
            server.url("/").toString(),
            "tok-1",
            store,
            preferences("widget") { WidgetStore(it) },
            preferences("quota-config") { QuotaBadgeConfigStore(it) },
            now = { 42L },
        )

    /**
     * eventsRequestPath returns the path of the events read, the fifth request of a
     * poll. Bounded takes, so a poll that made fewer requests fails the test
     * rather than hanging it.
     */
    private fun eventsRequestPath(): String {
        repeat(4) { checkNotNull(server.takeRequest(2, TimeUnit.SECONDS)) { "poll made fewer than 5 requests" } }
        return checkNotNull(server.takeRequest(2, TimeUnit.SECONDS)) { "no events request" }.path.orEmpty()
    }

    private val seen = EventCursor("2026-08-31T11:20:00Z", "e1")

    @Test
    fun firstPollRecordsTheEventCursorWithoutAlerting() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            enqueueFleet(healthy = true)
            server.enqueue(MockResponse().setBody(selection("member.state_changed")))
            server.enqueue(
                MockResponse().setBody(eventsPage(event("e2", "member.state_changed", "2026-08-31T11:28:54Z"))),
            )

            val result = poll(store)

            assertTrue((result as PollResult.Changed).alerts.isEmpty())
            assertEquals(EventCursor("2026-08-31T11:28:54Z", "e2"), store.eventCursor())
            assertEquals(5, server.requestCount)
            // A baseline needs only the newest row, so that is all that is asked for.
            val path = eventsRequestPath()
            assertTrue(path, path.contains("limit=1") && !path.contains("since="))
        }

    @Test
    fun aSwitchedOnEventAfterTheCursorIsAlertedAsFrontDesksOwnSentence() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = true)
            server.enqueue(MockResponse().setBody(selection("member.state_changed")))
            server.enqueue(
                MockResponse().setBody(
                    eventsPage(
                        event("e2", "member.state_changed", "2026-08-31T11:28:54Z", "MH docker-pc set to drained"),
                        event("e1", "member.state_changed", "2026-08-31T11:20:00Z"),
                    ),
                ),
            )

            val result = poll(store)

            assertEquals(
                listOf(
                    FrontDeskEvent(
                        "e2",
                        "member.state_changed",
                        "warning",
                        "MH docker-pc set to drained",
                        "m1",
                        "2026-08-31T11:28:54Z",
                    ),
                ),
                (result as PollResult.Changed).alerts,
            )
            assertEquals(EventCursor("2026-08-31T11:28:54Z", "e2"), store.eventCursor())
            // With a cursor the log is read from its instant, not from the top.
            val path = eventsRequestPath()
            assertTrue(path, path.contains("since=2026-08-31T11%3A20%3A00Z"))
        }

    @Test
    fun aSwitchedOffEventIsNotAlertedButIsStillSeen() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = true)
            server.enqueue(MockResponse().setBody(selection("config.sync_held")))
            server.enqueue(
                MockResponse().setBody(eventsPage(event("e2", "member.state_changed", "2026-08-31T11:28:54Z"))),
            )

            val result = poll(store)

            assertTrue((result as PollResult.Changed).alerts.isEmpty())
            assertEquals(EventCursor("2026-08-31T11:28:54Z", "e2"), store.eventCursor())
        }

    @Test
    fun frontDesksHealthEventReplacesBellhopsOwnDownEdge() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setBody(selection("health.down")))
            server.enqueue(
                MockResponse().setBody(
                    eventsPage(
                        event("e2", "health.down", "2026-08-31T11:29:45Z", "hotel-1 is unreachable after 3 checks"),
                    ),
                ),
            )

            val alerts = (poll(store) as PollResult.Changed).alerts

            // One outage, one notification: Front Desk's sentence, not both.
            assertEquals(listOf("e2"), alerts.filterIsInstance<FrontDeskEvent>().map { it.id })
            assertTrue(alerts.none { it is MemberTransition })
        }

    @Test
    fun frontDesksHealthEventForAnotherMemberDoesNotSilenceThisOne() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setBody(selection("health.down")))
            server.enqueue(
                MockResponse().setBody(
                    eventsPage(event("e2", "health.down", "2026-08-31T11:29:45Z", member = "m2")),
                ),
            )

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertTrue(alerts.contains(MemberTransition.WentDown("m1", "hotel-1")))
            assertEquals(listOf("e2"), alerts.filterIsInstance<FrontDeskEvent>().map { it.id })
        }

    @Test
    fun bellhopsOwnDownEdgeStandsWhenFrontDeskDoesNotAlertOnHealth() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setBody(selection("member.state_changed")))
            server.enqueue(MockResponse().setBody(eventsPage()))

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), alerts)
        }

    @Test
    fun anUpgradeWithASnapshotButNoCursorKeepsBellhopsOwnDownEdge() =
        runBlocking {
            // The health snapshot survives an upgrade; the cursor is new. The first
            // poll records the baseline and posts no Front Desk event, so it must
            // not surrender the local edge to a Front Desk event it is not posting.
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setBody(selection("health.down")))
            server.enqueue(MockResponse().setBody(eventsPage(event("e2", "health.down", "2026-08-31T11:29:45Z"))))

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), alerts)
            assertEquals(EventCursor("2026-08-31T11:29:45Z", "e2"), store.eventCursor())
        }

    @Test
    fun frontDesksDriftEventReplacesBellhopsOwnDriftEdge() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(
                FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name), autosyncStale = false),
                store.epoch(),
            )
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = true, stale = true)
            server.enqueue(MockResponse().setBody(selection("config.autosync_stale")))
            server.enqueue(
                MockResponse().setBody(
                    eventsPage(event("e2", "config.autosync_stale", "2026-08-31T11:29:45Z", member = "")),
                ),
            )

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertEquals(listOf("e2"), alerts.filterIsInstance<FrontDeskEvent>().map { it.id })
            assertFalse(alerts.contains(AutoSyncAlert.WentStale))
        }

    @Test
    fun aHealthEventPushedPastTheCapLeavesBellhopsOwnEdgeStanding() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setBody(selection("health.down", "config.sync_held")))
            // Six held-sync events newer than the outage push health.down past the
            // five-per-poll cap: it is not posted, so the local edge must be.
            val held = (6 downTo 1).map { event("h$it", "config.sync_held", "2026-08-31T11:4$it:00Z") }
            server.enqueue(
                MockResponse().setBody(
                    eventsPage(*held.toTypedArray(), event("e2", "health.down", "2026-08-31T11:29:45Z")),
                ),
            )

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertTrue(alerts.contains(MemberTransition.WentDown("m1", "hotel-1")))
            assertEquals(5, alerts.filterIsInstance<FrontDeskEvent>().size)
        }

    @Test
    fun aFailedPickerReadKeepsTheLocalEdgeAndTheCursor() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setResponseCode(500))

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), alerts)
            assertEquals(seen, store.eventCursor())
            // No picker, no log read: the page could not be filtered anyway.
            assertEquals(4, server.requestCount)
        }

    @Test
    fun aFailedLogReadKeepsTheLocalEdgeAndTheCursor() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.saveEventCursor(seen, store.epoch())
            enqueueFleet(healthy = false)
            server.enqueue(MockResponse().setBody(selection("health.down")))
            server.enqueue(MockResponse().setResponseCode(500))

            val alerts = (poll(store) as PollResult.Changed).alerts

            assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), alerts)
            assertEquals(seen, store.eventCursor())
        }

    @Test
    fun aDeadTokenNeverReachesTheEventLog() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            server.enqueue(MockResponse().setResponseCode(401))

            assertEquals(PollResult.Unauthorized, poll(store))
            assertNull(store.eventCursor())
            assertEquals(1, server.requestCount)
        }

    @Test
    fun aBaselineIsNeededOnlyWhileALayerIsOnNotificationsCanPostAndNoCursorExists() {
        assertTrue(needsEventBaseline(active = true, canNotify = true, cursor = null))
        assertFalse(needsEventBaseline(active = false, canNotify = true, cursor = null))
        assertFalse(needsEventBaseline(active = true, canNotify = false, cursor = null))
        assertFalse(needsEventBaseline(active = true, canNotify = true, cursor = seen))
    }
}
