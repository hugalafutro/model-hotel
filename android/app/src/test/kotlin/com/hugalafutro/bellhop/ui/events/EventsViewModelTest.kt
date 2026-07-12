package com.hugalafutro.bellhop.ui.events

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.lifecycle.viewModelScope
import com.hugalafutro.bellhop.data.EventQuery
import com.hugalafutro.bellhop.data.EventsResponse
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FdEvent
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.PairedDevice
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
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
import java.time.Instant
import java.util.concurrent.atomic.AtomicInteger

// FakeEventsClient stubs the one call the events ViewModel makes.
private class FakeEventsClient(
    var result: FetchResult<EventsResponse>,
) : FrontDeskClient() {
    val calls = AtomicInteger(0)
    var lastToken: String? = null
    var lastQuery: EventQuery? = null

    override suspend fun events(
        fdUrl: String,
        token: String,
        query: EventQuery,
    ): FetchResult<EventsResponse> {
        calls.incrementAndGet()
        lastToken = token
        lastQuery = query
        return result
    }
}

// GatedEventsClient parks every events call on a gate so a test can change
// state while a fetch is provably mid-flight ([entered] completes once the
// call is inside and parked).
private class GatedEventsClient : FrontDeskClient() {
    val entered = CompletableDeferred<Unit>()
    val gate = CompletableDeferred<FetchResult<EventsResponse>>()

    override suspend fun events(
        fdUrl: String,
        token: String,
        query: EventQuery,
    ): FetchResult<EventsResponse> {
        entered.complete(Unit)
        return gate.await()
    }
}

@RunWith(RobolectricTestRunner::class)
class EventsViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    // Every ViewModel built in a test, so tearDown can drain its scope: a VM's
    // collector-gated loops outlive the test method (nothing clears the VM),
    // and a leftover dispatch on the test Main dispatcher can collide with a
    // later test class's Dispatchers.setMain ("Dispatchers.Main is used
    // concurrently with setting it").
    private val vms = mutableListOf<EventsViewModel>()

    private fun newVm(
        client: FrontDeskClient,
        linkStore: LinkStore,
        pollIntervalMs: Long = EventsViewModel.POLL_INTERVAL_MS,
        now: () -> Long = System::currentTimeMillis,
    ): EventsViewModel =
        EventsViewModel(client, linkStore, "http://fd:1", pollIntervalMs, now)
            .also { vms += it }

    @Before
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Unconfined)
    }

    @After
    fun tearDown() {
        runBlocking {
            vms.forEach { vm ->
                vm.viewModelScope.cancel()
                vm.viewModelScope.coroutineContext[Job]?.join()
            }
        }
        vms.clear()
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

    private fun linkedStore(): LinkStore =
        newLinkStore().also {
            runBlocking {
                it.save(
                    "http://fd:1",
                    "FD",
                    "tok-1",
                    PairedDevice(id = "d1", label = "Pixel", role = "monitor"),
                )
            }
        }

    private fun ev(
        id: String,
        severity: String = "info",
    ) = FdEvent(
        id = id,
        type = "health.down",
        severity = severity,
        source = "poller",
        message = "event $id",
        createdAt = "2026-07-12T10:00:00Z",
    )

    private fun page(
        vararg ids: String,
        total: Int = ids.size,
    ) = FetchResult.Success(EventsResponse(events = ids.map { ev(it) }, total = total))

    @Test
    fun firstRefreshLoadsFirstPage() =
        runBlocking {
            val client = FakeEventsClient(page("e1", "e2", total = 40))
            val vm = newVm(client, linkedStore())

            vm.refreshOnce()

            val s = vm.state.value
            assertFalse(s.loading)
            assertEquals(listOf("e1", "e2"), s.events.map { it.id })
            assertEquals(40, s.total)
            assertNull(s.error)
            assertFalse(s.revoked)
            assertEquals("tok-1", client.lastToken)
            val q = client.lastQuery!!
            assertEquals(EventsViewModel.PAGE_SIZE, q.limit)
            assertEquals(0, q.offset)
            assertEquals("", q.severity)
            assertEquals("", q.since)
        }

    @Test
    fun failedRefreshKeepsStaleListAndRecovers() =
        runBlocking {
            val client = FakeEventsClient(page("e1", total = 1))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            client.result = FetchResult.Failure("boom")
            vm.refreshOnce()
            assertEquals(listOf("e1"), vm.state.value.events.map { it.id })
            assertEquals("boom", vm.state.value.error)

            client.result = page("e1", total = 1)
            vm.refreshOnce()
            assertNull(vm.state.value.error)
        }

    @Test
    fun unauthorizedFlagsRevoked() =
        runBlocking {
            val vm = newVm(FakeEventsClient(FetchResult.Unauthorized), linkedStore())
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertFalse(vm.state.value.loading)
        }

    @Test
    fun unreadableTokenFlagsRevokedWithoutCalling() =
        runBlocking {
            // Linked in name only (e.g. the Keystore key is gone): no request
            // can succeed, so surface the same flag a remote revoke raises.
            val client = FakeEventsClient(page("e1"))
            val vm = newVm(client, newLinkStore())
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertNull(client.lastToken)
            assertEquals(0, client.calls.get())
        }

    @Test
    fun loadMoreAppendsNextPageAndDedups() =
        runBlocking {
            val client = FakeEventsClient(page("e1", "e2", total = 4))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            // The next page overlaps by one row (a new event shifted offsets
            // between fetches); the duplicate must not render twice.
            client.result = page("e2", "e3", total = 4)
            vm.loadMore()

            // Spin on state.value instead of collecting: a collector would
            // wake the gated refresh loop, whose full-window reload with the
            // swapped fake result would fight this assertion.
            withTimeout(5_000) {
                while (vm.state.value.loadingMore || vm.state.value.events.size != 3) delay(10)
            }
            assertEquals(listOf("e1", "e2", "e3"), vm.state.value.events.map { it.id })
            assertEquals(2, client.lastQuery!!.offset)
            assertEquals(EventsViewModel.PAGE_SIZE, client.lastQuery!!.limit)
        }

    @Test
    fun loadMoreIsNoopWhenAllRowsLoaded() =
        runBlocking {
            val client = FakeEventsClient(page("e1", "e2"))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()
            val calls = client.calls.get()

            vm.loadMore()
            delay(100)
            assertEquals(calls, client.calls.get())
            assertFalse(vm.state.value.loadingMore)
        }

    @Test
    fun refreshReloadsTheWholeLoadedWindow() =
        runBlocking {
            // More rows loaded than one page: the next refresh must ask for
            // the full window, not truncate back to the first page.
            val ids = (1..30).map { "e$it" }.toTypedArray()
            val client = FakeEventsClient(page(*ids, total = 60))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()
            assertEquals(30, vm.state.value.events.size)

            vm.refreshOnce()
            assertEquals(30, client.lastQuery!!.limit)
            assertEquals(0, client.lastQuery!!.offset)
        }

    @Test
    fun severityChangeResetsAndRefetchesFiltered() =
        runBlocking {
            val client = FakeEventsClient(page("e1", "e2", total = 2))
            val vm = newVm(client, linkedStore())
            // UNDISPATCHED so this keep-alive collector subscribes before the
            // transient first{} collectors below; otherwise the subscription
            // count can bounce through zero between them, restarting the
            // collector-gated loops and refetching after assertions sampled.
            val collector = launch(start = CoroutineStart.UNDISPATCHED) { vm.state.collect {} }
            withTimeout(5_000) { vm.state.first { !it.loading } }

            client.result = page("e9", total = 1)
            vm.setSeverity("error")

            val s =
                withTimeout(5_000) {
                    vm.state.first { !it.loading && it.events.map { e -> e.id } == listOf("e9") }
                }
            assertEquals("error", s.severity)
            assertEquals("error", client.lastQuery!!.severity)
            collector.cancel()
        }

    @Test
    fun rangeChangeSendsSinceFromClock() =
        runBlocking {
            val nowMs = 1_752_300_000_000L
            val client = FakeEventsClient(page("e1", total = 1))
            val vm = newVm(client, linkedStore(), now = { nowMs })
            // UNDISPATCHED so this keep-alive collector subscribes before the
            // transient first{} collectors below; otherwise the subscription
            // count can bounce through zero between them, restarting the
            // collector-gated loops and refetching after assertions sampled.
            val collector = launch(start = CoroutineStart.UNDISPATCHED) { vm.state.collect {} }
            withTimeout(5_000) { vm.state.first { !it.loading } }

            vm.setRange(EventRange.H1)
            val expected = Instant.ofEpochMilli(nowMs - EventRange.H1.ms).toString()
            withTimeout(5_000) { vm.state.first { !it.loading && it.range == EventRange.H1 } }
            assertEquals(expected, client.lastQuery!!.since)
            collector.cancel()
        }

    @Test
    fun staleFilterResultIsDropped() =
        runBlocking {
            // A fetch for the old filter that lands after the filter changed
            // must not overwrite the (empty, loading) reset state.
            val client = GatedEventsClient()
            val vm = newVm(client, linkedStore())
            val inFlight = launch { vm.refreshOnce() }
            // Only change the filter once the fetch has captured the old one
            // and parked; otherwise it starts after the change and its result
            // is legitimately current, not stale.
            withTimeout(5_000) { client.entered.await() }

            vm.setSeverity("error")
            client.gate.complete(page("old1", "old2"))
            inFlight.join()

            assertTrue(vm.state.value.events.isEmpty())
            assertTrue(vm.state.value.loading)
        }

    @Test
    fun revokedTokenStopsThePoll() =
        runBlocking {
            // Once revoked, the collector-gated loop must stop hitting Front
            // Desk, including across a collector restart.
            val client = FakeEventsClient(FetchResult.Unauthorized)
            val vm = newVm(client, linkedStore(), pollIntervalMs = 10)
            // UNDISPATCHED so this keep-alive collector subscribes before the
            // transient first{} collectors below; otherwise the subscription
            // count can bounce through zero between them, restarting the
            // collector-gated loops and refetching after assertions sampled.
            val job = launch(start = CoroutineStart.UNDISPATCHED) { vm.state.collect {} }
            withTimeout(5_000) { vm.state.first { it.revoked } }
            val calls = client.calls.get()
            delay(200)
            assertEquals(calls, client.calls.get())
            job.cancel()

            val restarted = launch(start = CoroutineStart.UNDISPATCHED) { vm.state.collect {} }
            delay(200)
            assertEquals(calls, client.calls.get())
            restarted.cancel()
        }

    @Test
    fun subscribingStartsThePoll() =
        runBlocking {
            // The poll loop is gated on collectors; the first collector must
            // trigger a refresh on its own, with no manual refreshOnce call.
            val client = FakeEventsClient(page("e1", total = 1))
            val vm = newVm(client, linkedStore())

            val s = withTimeout(5_000) { vm.state.first { !it.loading } }
            assertEquals(listOf("e1"), s.events.map { it.id })
        }
}
