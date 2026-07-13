package com.hugalafutro.bellhop.ui.dashboard

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.hugalafutro.bellhop.data.ActionResult
import com.hugalafutro.bellhop.data.AutoSyncConfig
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetEvent
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.HealthStatus
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberStatus
import com.hugalafutro.bellhop.data.MemberTraffic
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

    // Pause/resume operator action: canned result plus captured args so a test can
    // prove the toggle sends the unchanged primary.
    var setAutoSyncResult: ActionResult<AutoSyncConfig> = ActionResult.Failure("no setAutoSync")
    var lastSetAutoSyncEnabled: Boolean? = null
    var lastSetAutoSyncPrimary: String? = null

    override suspend fun setAutoSync(
        fdUrl: String,
        token: String,
        enabled: Boolean,
        primaryId: String,
    ): ActionResult<AutoSyncConfig> {
        lastSetAutoSyncEnabled = enabled
        lastSetAutoSyncPrimary = primaryId
        return setAutoSyncResult
    }

    // Per-member traffic for the viewport-lazy sparkline. Records every id
    // fetched so a test can prove only the visible members were requested.
    var trafficResults: Map<String, FetchResult<MemberTraffic>> = emptyMap()
    val trafficFetched = java.util.concurrent.CopyOnWriteArrayList<String>()

    override suspend fun memberTraffic(
        fdUrl: String,
        token: String,
        memberId: String,
    ): FetchResult<MemberTraffic> {
        trafficFetched.add(memberId)
        return trafficResults[memberId] ?: FetchResult.Failure("no traffic for $memberId")
    }

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

    private fun autoSyncClient(enabled: Boolean = true) =
        FakeFleetClient(
            membersResult = FetchResult.Success(listOf(member)),
            autoSyncResult = FetchResult.Success(AutoSyncConfig(enabled = enabled, primaryId = "m1")),
        )

    @Test
    fun setAutoSyncAcceptsAndShowsPendingUntilReconciled() =
        runBlocking {
            val client = autoSyncClient(enabled = true)
            client.setAutoSyncResult = ActionResult.Success(AutoSyncConfig(enabled = false, primaryId = "m1"))
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()
            assertTrue(vm.state.value.autoSyncEnabled)

            vm.setAutoSync(false)
            val pending =
                withTimeout(5_000) { vm.state.first { !it.autoSync.inProgress && it.autoSync.pendingEnabled != null } }
            assertEquals(false, pending.autoSync.pendingEnabled)
            // Toggling the unchanged primary sends it back verbatim.
            assertEquals(false, client.lastSetAutoSyncEnabled)
            assertEquals("m1", client.lastSetAutoSyncPrimary)

            // A live read reflecting the paused state reconciles the hint away.
            client.autoSyncResult = FetchResult.Success(AutoSyncConfig(enabled = false, primaryId = "m1"))
            vm.refreshOnce()
            assertNull(vm.state.value.autoSync.pendingEnabled)
            assertFalse(vm.state.value.autoSyncEnabled)
        }

    @Test
    fun setAutoSyncReadFailurePromotesPendingInsteadOfStranding() =
        runBlocking {
            val client = autoSyncClient(enabled = true)
            client.setAutoSyncResult = ActionResult.Success(AutoSyncConfig(enabled = false, primaryId = "m1"))
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()

            vm.setAutoSync(false)
            withTimeout(5_000) { vm.state.first { !it.autoSync.inProgress && it.autoSync.pendingEnabled != null } }

            // The confirming endpoint goes dark while members still read fine. The
            // pending hint must not linger forever: the PUT's 200 echo already
            // applied the paused value, so it's promoted to the baseline and the
            // hint clears rather than showing "pausing…" against a dead read.
            client.autoSyncResult = FetchResult.Failure("autosync down")
            vm.refreshOnce()
            assertNull(vm.state.value.autoSync.pendingEnabled)
            assertFalse(vm.state.value.autoSyncEnabled)
        }

    @Test
    fun setAutoSyncForbiddenSetsFlag() =
        runBlocking {
            val client = autoSyncClient()
            client.setAutoSyncResult = ActionResult.Forbidden
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()

            vm.setAutoSync(false)
            val s = withTimeout(5_000) { vm.state.first { it.autoSync.forbidden } }
            assertTrue(s.autoSync.forbidden)
        }

    @Test
    fun setAutoSyncUnauthorizedRevokes() =
        runBlocking {
            val client = autoSyncClient()
            client.setAutoSyncResult = ActionResult.Unauthorized
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()

            vm.setAutoSync(false)
            val s = withTimeout(5_000) { vm.state.first { it.revoked } }
            assertTrue(s.revoked)
        }

    @Test
    fun setAutoSyncFailureSurfacesErrorThenDismisses() =
        runBlocking {
            val client = autoSyncClient()
            client.setAutoSyncResult = ActionResult.Failure("boom")
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()

            vm.setAutoSync(false)
            val s = withTimeout(5_000) { vm.state.first { it.autoSync.error != null } }
            assertEquals("boom", s.autoSync.error)

            vm.dismissAutoSyncError()
            assertNull(vm.state.value.autoSync.error)
        }

    @Test
    fun setAutoSyncWithoutAPrimaryIsDroppedBeforeTheClient() =
        runBlocking {
            val client =
                FakeFleetClient(
                    membersResult = FetchResult.Success(listOf(member)),
                    autoSyncResult = FetchResult.Success(AutoSyncConfig(enabled = false, primaryId = "")),
                )
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            vm.refreshOnce()

            vm.setAutoSync(true)
            assertNull(client.lastSetAutoSyncEnabled)
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
    fun onlyVisibleMembersGetTrafficFetched() =
        runBlocking {
            // Two members, but only m1 is reported visible: the off-screen m2
            // must never have its traffic requested (the whole point of the
            // viewport-lazy fetch that keeps a big fleet cheap).
            val m1 = member
            val m2 = member.copy(id = "m2", name = "beta")
            val client = FakeFleetClient(FetchResult.Success(listOf(m1, m2)))
            client.trafficResults =
                mapOf(
                    "m1" to
                        FetchResult.Success(
                            MemberTraffic(memberId = "m1", reachable = true, totalRequests = 5),
                        ),
                )
            val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")
            val job = launch { vm.state.collect {} }
            withTimeout(5_000) { vm.state.first { it.members.size == 2 } }

            vm.setVisibleMembers(listOf("m1"))
            withTimeout(5_000) { vm.state.first { it.traffic.containsKey("m1") } }

            assertTrue(client.trafficFetched.contains("m1"))
            assertFalse(client.trafficFetched.contains("m2"))
            job.cancel()
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
    fun triggersRefreshOnlyForRenderedEventFamilies() {
        // Only membership/config/health/version events change a member card; alerts
        // and traefik notices ride the same stream but must not trigger a refetch.
        assertTrue(DashboardViewModel.triggersRefresh("member.added"))
        assertTrue(DashboardViewModel.triggersRefresh("config.auto_synced"))
        assertTrue(DashboardViewModel.triggersRefresh("health.down"))
        assertTrue(DashboardViewModel.triggersRefresh("version.fetch_failed"))
        assertFalse(DashboardViewModel.triggersRefresh("alert.fired"))
        assertFalse(DashboardViewModel.triggersRefresh("traefik.stale"))
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
    fun revokedTokenSwallowsFurtherRefreshNudges() =
        runBlocking {
            // Revocation is terminal (only unlink fixes it, and relinking
            // rebuilds the ViewModel), so stream/poll nudges after it must not
            // keep hitting Front Desk with a token that can never work. The
            // outer timeout turns any future deadlock in this loop machinery
            // into a fast failure instead of a hung CI job.
            withTimeout(30_000) {
                val events = MutableSharedFlow<SseMessage>(extraBufferCapacity = 8)
                val client = FakeFleetClient(FetchResult.Unauthorized, sseFlow = events)
                val vm = DashboardViewModel(client, linkedStore(), "http://fd:1")

                val job = launch { vm.state.collect {} }
                withTimeout(5_000) { vm.state.first { it.revoked } }
                val calls = client.memberCalls.get()

                withTimeout(5_000) { events.subscriptionCount.first { it > 0 } }
                events.emit(SseMessage.Event(FleetEvent(type = "health.down")))
                delay(200)
                assertEquals(calls, client.memberCalls.get())
                job.cancel()

                // A collector restart with the flag already set (backgrounding
                // and reopening the app) must not fire one more doomed
                // request: runRefreshes' initial refresh has to be gated too.
                val restarted = launch { vm.state.collect {} }
                delay(200)
                assertEquals(calls, client.memberCalls.get())
                restarted.cancel()
            }
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
            // revoked here is reached only through streamLoop (the poll succeeds), so
            // it hangs off the stream subscription's one IO hop with no polling slack.
            // The wait is a failsafe against a genuine hang, not an expected latency —
            // it normally completes in milliseconds — so give it the same generous
            // bound as revokedTokenSwallowsFurtherRefreshNudges rather than a tight 5s
            // window a loaded CI runner can starve past.
            val s = withTimeout(30_000) { vm.state.first { it.revoked } }
            assertTrue(s.revoked)
            job.cancel()
        }
}
