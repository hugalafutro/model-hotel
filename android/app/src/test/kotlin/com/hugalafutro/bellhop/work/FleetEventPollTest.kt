package com.hugalafutro.bellhop.work

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

/**
 * The Front Desk half of a poll: the alert picker and the event log are read
 * after the fleet, the events the picker has on come back as alerts, Front
 * Desk's health events replace Bellhop's own edge for the same outage, and a
 * failed read of either half costs the operator nothing (the local edge stands
 * and the cursor stays put). Fetch order is what the enqueue order pins:
 * members, auto-sync, quota, alert selection, events.
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
            (listOf("member.state_changed", "health.down", "health.up", "config.sync_held"))
                .joinToString(
                    ",",
                ) { """{"type":"$it","category":"Fleet","severity":"warning","enabled":${it in on}}""" } +
            "]}"

    private fun event(
        id: String,
        type: String,
        at: String,
        message: String = "$id $type",
    ): String =
        """{"id":"$id","type":"$type","severity":"warning","source":"frontdesk",""" +
            """"message":"$message","member_id":"m1","created_at":"$at"}"""

    private fun eventsPage(vararg events: String): String =
        """{"events":[${events.joinToString(",")}],"total":${events.size}}"""

    private fun enqueueFleet(healthy: Boolean) {
        server.enqueue(MockResponse().setBody(memberBody(healthy)))
        server.enqueue(MockResponse().setBody("""{"enabled":false,"primary_id":"m1","stale":false}"""))
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

            // The picker says Front Desk pages on health.down, but its log could
            // not be read this poll, so Bellhop's own edge is not surrendered.
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
}
