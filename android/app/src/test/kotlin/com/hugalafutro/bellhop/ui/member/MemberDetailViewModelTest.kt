package com.hugalafutro.bellhop.ui.member

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.hugalafutro.bellhop.data.ActionResult
import com.hugalafutro.bellhop.data.EventQuery
import com.hugalafutro.bellhop.data.EventsResponse
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FleetMember
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.PairedDevice
import com.hugalafutro.bellhop.data.SyncResponse
import com.hugalafutro.bellhop.data.SyncResultItem
import com.hugalafutro.bellhop.data.TrafficPoint
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
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

// FakeTrafficClient stubs the two reads the detail ViewModel makes (traffic +
// the member's recent events). Events default to a successful empty page so a
// traffic-focused test needn't set them.
private class FakeTrafficClient(
    var trafficResult: FetchResult<MemberTraffic>,
    var eventsResult: FetchResult<EventsResponse> =
        FetchResult.Success(
            EventsResponse(events = emptyList(), total = 0),
        ),
) : FrontDeskClient() {
    val trafficCalls = AtomicInteger(0)
    var lastToken: String? = null
    var lastMemberId: String? = null
    var lastEventQuery: EventQuery? = null

    // Operator-action stubs: what setMemberState/syncFleet return, and what the
    // ViewModel last sent, so the action tests can assert both directions.
    var stateResult: ActionResult<FleetMember> = ActionResult.Success(FleetMember(id = "m1", state = "drained"))
    var syncResult: ActionResult<SyncResponse> = ActionResult.Success(SyncResponse())
    var lastStateTarget: String? = null
    var lastSyncPrimaryId: String? = null

    override suspend fun memberTraffic(
        fdUrl: String,
        token: String,
        memberId: String,
    ): FetchResult<MemberTraffic> {
        trafficCalls.incrementAndGet()
        lastToken = token
        lastMemberId = memberId
        return trafficResult
    }

    override suspend fun events(
        fdUrl: String,
        token: String,
        query: EventQuery,
    ): FetchResult<EventsResponse> {
        lastEventQuery = query
        return eventsResult
    }

    override suspend fun setMemberState(
        fdUrl: String,
        token: String,
        memberId: String,
        state: String,
    ): ActionResult<FleetMember> {
        lastToken = token
        lastMemberId = memberId
        lastStateTarget = state
        return stateResult
    }

    override suspend fun syncFleet(
        fdUrl: String,
        token: String,
        primaryId: String,
    ): ActionResult<SyncResponse> {
        lastToken = token
        lastSyncPrimaryId = primaryId
        return syncResult
    }
}

@RunWith(RobolectricTestRunner::class)
class MemberDetailViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    // viewModelScope launches on Main; run it inline in tests.
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

    private val traffic =
        MemberTraffic(
            memberId = "m1",
            reachable = true,
            totalRequests = 10,
            totalErrors = 1,
            points = listOf(TrafficPoint(bucket = "b0", requests = 10, errors = 1)),
        )

    @Test
    fun refreshPopulatesTrafficWithStoredToken() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.refreshOnce()

            val s = vm.state.value
            assertFalse(s.loading)
            assertEquals(traffic, s.traffic)
            assertNull(s.error)
            assertFalse(s.revoked)
            assertEquals("tok-1", client.lastToken)
            assertEquals("m1", client.lastMemberId)
        }

    @Test
    fun refreshPopulatesMemberEventsFilteredToTheMember() =
        runBlocking {
            val client =
                FakeTrafficClient(
                    FetchResult.Success(traffic),
                    eventsResult =
                        FetchResult.Success(
                            EventsResponse(
                                events =
                                    listOf(
                                        FdEvent(id = "e1", severity = "error", message = "down", memberId = "m1"),
                                    ),
                                total = 1,
                            ),
                        ),
                )
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.refreshOnce()

            assertEquals(listOf("e1"), vm.state.value.events.map { it.id })
            // The events read is scoped to this member so the detail shows only
            // its own history, not the whole fleet's log.
            assertEquals("m1", client.lastEventQuery?.memberId)
            assertEquals(MemberDetailViewModel.EVENTS_LIMIT, client.lastEventQuery?.limit)
        }

    @Test
    fun eventsUnauthorizedFlagsRevoked() =
        runBlocking {
            val client =
                FakeTrafficClient(FetchResult.Success(traffic), eventsResult = FetchResult.Unauthorized)
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
        }

    @Test
    fun failedRefreshKeepsStaleTrafficAndRecovers() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")
            vm.refreshOnce()

            client.trafficResult = FetchResult.Failure("boom")
            vm.refreshOnce()
            assertEquals(traffic, vm.state.value.traffic)
            assertEquals("boom", vm.state.value.error)

            client.trafficResult = FetchResult.Success(traffic)
            vm.refreshOnce()
            assertNull(vm.state.value.error)
        }

    @Test
    fun unauthorizedFlagsRevoked() =
        runBlocking {
            val vm =
                MemberDetailViewModel(
                    FakeTrafficClient(FetchResult.Unauthorized),
                    linkedStore(),
                    "http://fd:1",
                    "m1",
                )
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertFalse(vm.state.value.loading)
        }

    @Test
    fun unreadableTokenFlagsRevokedWithoutCalling() =
        runBlocking {
            // Linked in name only (e.g. Keystore key gone): no request can
            // succeed, so surface the same flag a remote revoke raises.
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            val vm = MemberDetailViewModel(client, newLinkStore(), "http://fd:1", "m1")
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertNull(client.lastToken)
            assertEquals(0, client.trafficCalls.get())
        }

    @Test
    fun revokedTokenStopsThePoll() =
        runBlocking {
            // Revocation is terminal (only unlink fixes it), so the loop must
            // not keep hitting Front Desk with a token that can never work.
            // The outer timeout turns any future deadlock in this loop
            // machinery into a fast failure instead of a hung CI job.
            withTimeout(30_000) {
                val client = FakeTrafficClient(FetchResult.Unauthorized)
                val vm =
                    MemberDetailViewModel(
                        client,
                        linkedStore(),
                        "http://fd:1",
                        "m1",
                        pollIntervalMs = 10,
                    )

                val job = launch { vm.state.collect {} }
                withTimeout(5_000) { vm.state.first { it.revoked } }
                val calls = client.trafficCalls.get()
                delay(200)
                assertEquals(calls, client.trafficCalls.get())
                job.cancel()

                // A collector restart with the flag already set (backgrounding
                // and reopening the screen) must not fire one more doomed
                // request: the loop's entry check has to catch it.
                val restarted = launch { vm.state.collect {} }
                delay(200)
                assertEquals(calls, client.trafficCalls.get())
                restarted.cancel()
            }
        }

    @Test
    fun subscribingStartsThePoll() =
        runBlocking {
            // The poll loop is gated on collectors; the first collector must
            // trigger a refresh on its own, with no manual refreshOnce call.
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            val s = withTimeout(5_000) { vm.state.first { !it.loading } }
            assertEquals(traffic, s.traffic)
        }

    @Test
    fun setMemberStateAcceptsAndFlipsToPending() =
        runBlocking {
            // Pessimistic-accept: on Front Desk's 200 the recorded state becomes the
            // optimistic pending target, and the action clears its in-flight flag.
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.stateResult = ActionResult.Success(FleetMember(id = "m1", state = "drained"))
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.setMemberState("drained")

            val action = withTimeout(5_000) { vm.state.first { it.action.pendingState != null } }.action
            assertEquals("drained", action.pendingState)
            assertFalse(action.inProgress)
            assertFalse(action.forbidden)
            assertNull(action.error)
            assertEquals("drained", client.lastStateTarget)
            assertEquals("tok-1", client.lastToken)
        }

    @Test
    fun setMemberStateForbiddenSurfacesTheGuard() =
        runBlocking {
            // A monitor-role token's 403 is the real guard, distinct from revoked.
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.stateResult = ActionResult.Forbidden
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.setMemberState("drained")

            val s = withTimeout(5_000) { vm.state.first { it.action.forbidden } }
            assertFalse(s.revoked)
            assertNull(s.action.pendingState)
        }

    @Test
    fun setMemberStateUnauthorizedFlagsRevoked() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.stateResult = ActionResult.Unauthorized
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.setMemberState("active")

            val s = withTimeout(5_000) { vm.state.first { it.revoked } }
            assertFalse(s.action.forbidden)
        }

    @Test
    fun setMemberStateFailureSurfacesError() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.stateResult = ActionResult.Failure("boom")
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.setMemberState("active")

            val s = withTimeout(5_000) { vm.state.first { it.action.error != null } }
            assertEquals("boom", s.action.error)
            assertFalse(s.action.inProgress)
        }

    @Test
    fun setMemberStateOnUnreadableTokenFlagsRevokedWithoutCall() =
        runBlocking {
            // Linked in name only (Keystore key gone): no request can ever succeed,
            // so surface the revoked flag and never touch Front Desk.
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            val vm = MemberDetailViewModel(client, newLinkStore(), "http://fd:1", "m1")

            vm.setMemberState("drained")

            withTimeout(5_000) { vm.state.first { it.revoked } }
            assertNull(client.lastStateTarget)
        }

    @Test
    fun reconcileClearsPendingOnceLiveCatchesUp() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.stateResult = ActionResult.Success(FleetMember(id = "m1", state = "drained"))
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.setMemberState("drained")
            withTimeout(5_000) { vm.state.first { it.action.pendingState == "drained" } }

            // A live state that hasn't caught up leaves the pending target intact.
            vm.reconcile("active")
            assertEquals("drained", vm.state.value.action.pendingState)

            // Once the live state matches the accepted target the optimism is done.
            vm.reconcile("drained")
            assertNull(vm.state.value.action.pendingState)
        }

    @Test
    fun syncFleetTalliesResults() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.syncResult =
                ActionResult.Success(
                    SyncResponse(
                        primaryId = "m1",
                        results =
                            listOf(
                                SyncResultItem(memberId = "m2", ok = true),
                                SyncResultItem(memberId = "m3", ok = false, error = "unreachable"),
                            ),
                    ),
                )
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.syncFleet("m1")

            val summary = withTimeout(5_000) { vm.state.first { it.action.syncSummary != null } }.action.syncSummary
            assertEquals(2, summary?.total)
            assertEquals(1, summary?.failed)
            assertEquals("m1", client.lastSyncPrimaryId)
        }

    @Test
    fun dismissActionErrorClearsBanners() =
        runBlocking {
            val client = FakeTrafficClient(FetchResult.Success(traffic))
            client.stateResult = ActionResult.Failure("boom")
            val vm = MemberDetailViewModel(client, linkedStore(), "http://fd:1", "m1")

            vm.setMemberState("active")
            withTimeout(5_000) { vm.state.first { it.action.error == "boom" } }

            vm.dismissActionError()
            assertNull(vm.state.value.action.error)
            assertNull(vm.state.value.action.syncSummary)
        }
}
