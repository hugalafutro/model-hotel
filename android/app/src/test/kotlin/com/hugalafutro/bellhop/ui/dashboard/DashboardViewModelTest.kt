package com.hugalafutro.bellhop.ui.dashboard

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.hugalafutro.bellhop.data.AutoSyncConfig
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetEvent
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.PairedDevice
import com.hugalafutro.bellhop.data.SseMessage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.io.File
import java.util.concurrent.atomic.AtomicInteger

/** FakeFleetClient serves canned read-tier results without touching the network. */
private class FakeFleetClient(
    var membersResult: FetchResult<List<FleetMember>>,
    var autoSyncResult: FetchResult<AutoSyncConfig> = FetchResult.Failure("no autosync"),
    // Default stream stays open and quiet so activating the loops does not spin
    // on reconnect; tests that exercise SSE pass their own flow.
    var sseFlow: Flow<SseMessage> = flow { awaitCancellation() },
) : FrontDeskClient() {
    var lastToken: String? = null

    // Atomic: members() runs on the ViewModel's coroutine while tests read the
    // count from the runBlocking thread, so a plain Int could race/tear.
    val memberCalls = AtomicInteger(0)

    // Counts stream subscriptions; the reconnect loop calls streamEvents once per
    // attempt, so this is how the disconnect→reconnect test observes it retrying.
    val streamCalls = AtomicInteger(0)

    override suspend fun members(
        fdUrl: String,
        token: String,
    ): FetchResult<List<FleetMember>> {
        lastToken = token
        memberCalls.incrementAndGet()
        return membersResult
    }

    override suspend fun autoSync(
        fdUrl: String,
        token: String,
    ): FetchResult<AutoSyncConfig> = autoSyncResult

    override fun streamEvents(
        fdUrl: String,
        token: String,
    ): Flow<SseMessage> {
        streamCalls.incrementAndGet()
        return sseFlow
    }
}

@RunWith(RobolectricTestRunner::class)
class DashboardViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    // viewModelScope dispatches on Main; run it inline for tests.
    @Before
    fun setUpMain() {
        Dispatchers.setMain(Dispatchers.Unconfined)
    }

    @After
    fun tearDownMain() {
        Dispatchers.resetMain()
    }

    private fun newLinkStore(): LinkStore {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        val ds =
            PreferenceDataStoreFactory.create(scope = scope) {
                File(tmp.newFolder(), "link.preferences_pb")
            }
        return LinkStore(ds, FakeCipher)
    }

    private suspend fun linkedStore(): LinkStore =
        newLinkStore().also {
            it.save(
                fdUrl = "http://fd:1",
                fdName = "FD",
                token = "tok-1",
                device = PairedDevice(id = "d1", label = "Pixel", role = "monitor"),
            )
        }

    private val member =
        FleetMember(
            id = "m1",
            name = "hotel-1",
            url = "http://h1:8080",
            status = MemberStatus(health = HealthStatus(known = true, healthy = true, latencyMs = 5)),
        )

    @Test
    fun refreshPopulatesMembersAndPrimaryWithStoredToken() =
        runBlocking {
            val client =
                FakeFleetClient(
                    membersResult = FetchResult.Success(listOf(member)),
                    autoSyncResult = FetchResult.Success(AutoSyncConfig(enabled = true, primaryId = "m1")),
                )
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

            vm.refreshOnce()

            val s = vm.state.value
            assertFalse(s.loading)
            assertEquals(listOf(member), s.members)
            assertEquals("m1", s.primaryId)
            assertNull(s.error)
            assertFalse(s.revoked)
            assertEquals("tok-1", client.lastToken)
        }

    @Test
    fun failedRefreshKeepsStaleMembersAndRecovers() =
        runBlocking {
            val client = FakeFleetClient(FetchResult.Success(listOf(member)))
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()

            // Stale beats blank: the last good list stays, the error surfaces.
            client.membersResult = FetchResult.Failure("boom")
            vm.refreshOnce()
            assertEquals(listOf(member), vm.state.value.members)
            assertEquals("boom", vm.state.value.error)

            // The next good refresh clears the error again.
            client.membersResult = FetchResult.Success(listOf(member))
            vm.refreshOnce()
            assertNull(vm.state.value.error)
        }

    @Test
    fun unauthorizedFlagsRevoked() =
        runBlocking {
            val vm = DashboardViewModel(FakeFleetClient(FetchResult.Unauthorized), linkedStore(), "http://fd:1")
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertFalse(vm.state.value.loading)
        }

    @Test
    fun missingTokenFlagsRevokedWithoutNetworkCall() =
        runBlocking {
            // Linked metadata without a readable token (e.g. lost Keystore key):
            // no request can succeed, so don't make one.
            val client = FakeFleetClient(FetchResult.Success(listOf(member)))
            val vm = DashboardViewModel(client, newLinkStore(), "http://fd:1")
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertNull(client.lastToken)
        }

    @Test
    fun autoSyncFailureDoesNotFailTheRefresh() =
        runBlocking {
            // The Primary badge is best-effort; members must still land.
            val client =
                FakeFleetClient(
                    membersResult = FetchResult.Success(listOf(member)),
                    autoSyncResult = FetchResult.Failure("nope"),
                )
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()
            val s = vm.state.value
            assertEquals(listOf(member), s.members)
            assertEquals("", s.primaryId)
            assertNull(s.error)
        }

    @Test
    fun subscribingStartsThePoll() =
        runBlocking {
            // The poll loop is gated on collectors; the first collector must
            // trigger a refresh on its own, with no manual refreshOnce call.
            val client = FakeFleetClient(FetchResult.Success(listOf(member)))
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

            val s = withTimeout(5_000) { vm.state.first { !it.loading } }
            assertEquals(listOf(member), s.members)
        }

    @Test
    fun sseRefreshEventRefetchesMembers() =
        runBlocking {
            // A relevant event on the stream must refetch and push the change
            // through without waiting for the slow fallback poll.
            val events = MutableSharedFlow<SseMessage>(extraBufferCapacity = 8)
            val client = FakeFleetClient(FetchResult.Success(emptyList()), sseFlow = events)
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

            val job = launch { vm.state.collect {} }
            withTimeout(5_000) { vm.state.first { !it.loading } }
            assertEquals(emptyList<FleetMember>(), vm.state.value.members)

            // Newer data appears at Front Desk; a health event announces it.
            client.membersResult = FetchResult.Success(listOf(member))
            withTimeout(5_000) { events.subscriptionCount.first { it > 0 } }
            events.emit(SseMessage.Event(FleetEvent(type = "health.up")))

            val s = withTimeout(5_000) { vm.state.first { it.members == listOf(member) } }
            assertEquals(listOf(member), s.members)
            job.cancel()
        }

    @Test
    fun sseIrrelevantEventDoesNotRefetch() =
        runBlocking {
            // Events the dashboard doesn't render (e.g. alerts) ride the same stream
            // but must not trigger a members refetch. One completing stream delivers
            // the alert then an Unauthorized, in order; once revoked is observed the
            // alert has definitely been processed — and must have left the fetch
            // count untouched. Deterministic: no wall-clock waiting.
            val client =
                FakeFleetClient(
                    FetchResult.Success(emptyList()),
                    sseFlow =
                        flowOf(
                            SseMessage.Event(FleetEvent(type = "alert.fired")),
                            SseMessage.Unauthorized,
                        ),
                )
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

            val job = launch { vm.state.collect {} }
            withTimeout(5_000) { vm.state.first { !it.loading } }
            val callsAfterInitialLoad = client.memberCalls.get()

            withTimeout(5_000) { vm.state.first { it.revoked } }
            assertEquals(callsAfterInitialLoad, client.memberCalls.get())
            job.cancel()
        }

    @Test
    fun sseReconnectsAfterDisconnect() =
        runBlocking {
            // A stream that completes at once models a dropped connection; the loop
            // must back off and reconnect (subscribe again), not give up after one.
            val client = FakeFleetClient(FetchResult.Success(listOf(member)), sseFlow = flowOf())
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

            val job = launch { vm.state.collect {} }
            withTimeout(5_000) {
                while (client.streamCalls.get() < 2) delay(50)
            }
            assertTrue(client.streamCalls.get() >= 2)
            job.cancel()
        }

    @Test
    fun sseUnauthorizedFlagsRevoked() =
        runBlocking {
            // A 401 on the stream means the device token is dead; surface it the
            // same way a 401 on the poll does.
            val client =
                FakeFleetClient(
                    FetchResult.Success(listOf(member)),
                    sseFlow = flowOf(SseMessage.Unauthorized),
                )
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

            val job = launch { vm.state.collect {} }
            val s = withTimeout(5_000) { vm.state.first { it.revoked } }
            assertTrue(s.revoked)
            job.cancel()
        }
}
