package com.hugalafutro.bellhop.work

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.hugalafutro.bellhop.data.FleetSnapshot
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.MemberHealthState
import com.hugalafutro.bellhop.data.MemberTransition
import com.hugalafutro.bellhop.data.MonitorStore
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

    private fun memberBody(healthy: Boolean): String =
        """[{"id":"m1","name":"hotel-1","state":"active",""" +
            """"status":{"health":{"known":true,"healthy":$healthy}}}]"""

    private suspend fun poll(store: MonitorStore): PollResult =
        pollFleet(client, server.url("/").toString(), "tok-1", store)

    @Test
    fun firstPollSavesBaselineWithNoTransitions() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            server.enqueue(MockResponse().setBody(memberBody(healthy = true)))

            val result = poll(store)

            assertTrue(result is PollResult.Changed)
            assertTrue((result as PollResult.Changed).transitions.isEmpty())
            // The baseline now exists for the next poll to diff against.
            assertEquals(MemberHealthState.UP, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun memberGoingDownAcrossPollsIsNotified() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)))
            server.enqueue(MockResponse().setBody(memberBody(healthy = false)))

            val result = poll(store)

            assertEquals(
                listOf(MemberTransition.WentDown("m1", "hotel-1")),
                (result as PollResult.Changed).transitions,
            )
            assertEquals(MemberHealthState.DOWN, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun deadTokenReportsUnauthorizedAndLeavesBaseline() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)))
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
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)))
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            assertEquals(PollResult.Failed, poll(store))
            // A blip mustn't advance or clear the baseline, or the next poll would
            // diff against nothing and re-alert the whole fleet.
            assertEquals(MemberHealthState.UP, store.snapshot()?.stateOf("m1"))
        }
}
