package com.hugalafutro.bellhop.work

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.hugalafutro.bellhop.data.AutoSyncAlert
import com.hugalafutro.bellhop.data.FleetSnapshot
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.MemberHealthState
import com.hugalafutro.bellhop.data.MemberTransition
import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.data.WidgetMember
import com.hugalafutro.bellhop.data.WidgetState
import com.hugalafutro.bellhop.data.WidgetStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

/**
 * The worker's Android shell (WorkManager runtime, notification posting) is thin;
 * the fetch/diff/persist logic lives in [pollFleet], so it is tested here against
 * a MockWebServer-backed client and a temp store. The snapshot side effects are
 * the point: a success advances the baseline, a failure or dead token leaves it
 * untouched so a transient blip can't erase the baseline and re-alert everything.
 */
class FleetPollTest {
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

    private fun newStore(): MonitorStore {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        val ds: DataStore<Preferences> =
            PreferenceDataStoreFactory.create(scope = scope) {
                File(tmp.newFolder(), "monitor.preferences_pb")
            }
        return MonitorStore(ds)
    }

    private fun newWidgetStore(): WidgetStore {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        val ds: DataStore<Preferences> =
            PreferenceDataStoreFactory.create(scope = scope) {
                File(tmp.newFolder(), "widget.preferences_pb")
            }
        return WidgetStore(ds)
    }

    private fun memberBody(healthy: Boolean): String =
        """[{"id":"m1","name":"hotel-1","state":"active",""" +
            """"status":{"health":{"known":true,"healthy":$healthy}}}]"""

    private fun autoSyncBody(stale: Boolean): String = """{"enabled":false,"primary_id":"m1","stale":$stale}"""

    // A successful poll fetches members then auto-sync, so enqueue both.
    private fun enqueuePoll(
        healthy: Boolean,
        stale: Boolean = false,
    ) {
        server.enqueue(MockResponse().setBody(memberBody(healthy)))
        server.enqueue(MockResponse().setBody(autoSyncBody(stale)))
    }

    private suspend fun poll(
        store: MonitorStore,
        widget: WidgetStore = newWidgetStore(),
    ): PollResult = pollFleet(client, server.url("/").toString(), "tok-1", store, widget, now = { 42L })

    @Test
    fun firstPollSavesBaselineWithNoTransitions() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            enqueuePoll(healthy = true)

            val result = poll(store)

            assertTrue(result is PollResult.Changed)
            assertTrue((result as PollResult.Changed).alerts.isEmpty())
            // The baseline now exists for the next poll to diff against.
            assertEquals(MemberHealthState.UP, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun memberGoingDownAcrossPollsIsNotified() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            enqueuePoll(healthy = false)

            val result = poll(store)

            assertEquals(
                listOf(MemberTransition.WentDown("m1", "hotel-1")),
                (result as PollResult.Changed).alerts,
            )
            assertEquals(MemberHealthState.DOWN, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun autoSyncGoingStaleAcrossPollsIsNotified() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            // Baseline is healthy and not stale; this poll reports stale.
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            enqueuePoll(healthy = true, stale = true)

            val result = poll(store)

            assertEquals(listOf(AutoSyncAlert.WentStale), (result as PollResult.Changed).alerts)
            assertEquals(true, store.snapshot()?.autosyncStale)
        }

    @Test
    fun autoSyncReadFailureFallsBackWithoutLosingHealthPoll() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            // Prior stale flag set; the health edge must still fire and the stale
            // value must be kept (no phantom edge) when the auto-sync read fails.
            store.saveSnapshot(
                FleetSnapshot(states = mapOf("m1" to MemberHealthState.UP.name), autosyncStale = true),
                store.epoch(),
            )
            server.enqueue(MockResponse().setBody(memberBody(healthy = false)))
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            val result = poll(store)

            assertEquals(
                listOf(MemberTransition.WentDown("m1", "hotel-1")),
                (result as PollResult.Changed).alerts,
            )
            assertEquals(true, store.snapshot()?.autosyncStale)
        }

    @Test
    fun deadTokenReportsUnauthorizedAndLeavesBaseline() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            server.enqueue(
                MockResponse().setResponseCode(401).setBody(
                    """{"error":{"code":"unauthorized","message":"bad token"}}""",
                ),
            )

            assertEquals(PollResult.Unauthorized, poll(store))
            // A revoked token mustn't wipe the baseline.
            assertEquals(MemberHealthState.UP, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun transientFailureReportsFailedAndLeavesBaseline() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            assertEquals(PollResult.Failed, poll(store))
            // A blip mustn't advance or clear the baseline, or the next poll would
            // diff against nothing and re-alert the whole fleet.
            assertEquals(MemberHealthState.UP, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun successfulPollWritesWidgetState() =
        runBlocking {
            val store = newStore()
            val widget = newWidgetStore()
            store.setEnabled(true)
            enqueuePoll(healthy = true)

            poll(store, widget)

            val ws = widget.read()
            assertEquals(listOf(WidgetMember("hotel-1", "UP", id = "m1")), ws?.members)
            assertEquals(42L, ws?.updatedAt)
        }

    @Test
    fun failedPollLeavesWidgetStateUntouched() =
        runBlocking {
            val store = newStore()
            val widget = newWidgetStore()
            store.setEnabled(true)
            // Seed a previous fetch's state: an untouched store and a wiped one
            // both read null, so only a surviving seed proves "untouched".
            widget.saveIfChanged(
                WidgetState(members = listOf(WidgetMember("hotel-1", "UP")), updatedAt = 7L),
                widget.generation(),
            )
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            poll(store, widget)

            // Stale beats blank: the widget keeps showing the last fetch + stamp.
            assertEquals(7L, widget.read()?.updatedAt)
        }

    @Test
    fun includeTrafficFetchesPerMemberBuckets() =
        runBlocking {
            val store = newStore()
            val widget = newWidgetStore()
            store.setEnabled(true)
            enqueuePoll(healthy = true)
            server.enqueue(
                MockResponse().setBody(
                    """{"member_id":"m1","reachable":true,"window_minutes":60,"total_requests":6,""" +
                        """"points":[{"bucket":"b1","requests":1,"errors":0},""" +
                        """{"bucket":"b2","requests":5,"errors":0}]}""",
                ),
            )

            pollFleet(client, server.url("/").toString(), "tok-1", store, widget, includeTraffic = true, now = { 42L })

            assertEquals(listOf(1, 5), widget.read()?.members?.single()?.traffic)
            // members + autosync + one traffic call for the one member.
            assertEquals(3, server.requestCount)
        }

    @Test
    fun trafficOffMakesNoTrafficRequests() =
        runBlocking {
            val store = newStore()
            val widget = newWidgetStore()
            store.setEnabled(true)
            enqueuePoll(healthy = true)

            poll(store, widget)

            assertEquals(emptyList<Int>(), widget.read()?.members?.single()?.traffic)
            // The exact request count the battery baseline was measured against.
            assertEquals(2, server.requestCount)
        }

    @Test
    fun trafficReadFailureKeepsPreviousBars() =
        runBlocking {
            val store = newStore()
            val widget = newWidgetStore()
            store.setEnabled(true)
            widget.saveIfChanged(
                WidgetState(
                    members = listOf(WidgetMember("hotel-1", "UP", traffic = listOf(7, 8), id = "m1")),
                    updatedAt = 1L,
                ),
                widget.generation(),
            )
            enqueuePoll(healthy = true)
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            pollFleet(client, server.url("/").toString(), "tok-1", store, widget, includeTraffic = true, now = { 42L })

            // A failed series read keeps the member's previous bars: stale beats
            // blank, and a blip must not blank the whole hour.
            assertEquals(listOf(7, 8), widget.read()?.members?.single()?.traffic)
        }
}
