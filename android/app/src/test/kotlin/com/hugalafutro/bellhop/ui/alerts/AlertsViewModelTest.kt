package com.hugalafutro.bellhop.ui.alerts

import com.hugalafutro.bellhop.data.ActionResult
import com.hugalafutro.bellhop.data.AlertEventDef
import com.hugalafutro.bellhop.data.AlertStatus
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.InMemoryPreferencesDataStore
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.PairedDevice
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
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
import java.util.concurrent.atomic.AtomicInteger

// FakeAlertsClient stubs the reads the alerts ViewModel makes plus the flip
// action; each result is swappable so a test can move a call from success to
// failure between refreshes.
private class FakeAlertsClient(
    var status: FetchResult<AlertStatus>,
    var catalog: FetchResult<List<AlertEventDef>>,
    var toggleResult: ActionResult<List<AlertEventDef>> = ActionResult.Failure("unset"),
) : FrontDeskClient() {
    val statusCalls = AtomicInteger(0)
    val catalogCalls = AtomicInteger(0)
    var lastToken: String? = null
    var lastToggle: Pair<String, Boolean>? = null

    override suspend fun alertStatus(
        fdUrl: String,
        token: String,
    ): FetchResult<AlertStatus> {
        statusCalls.incrementAndGet()
        lastToken = token
        return status
    }

    override suspend fun alertSelection(
        fdUrl: String,
        token: String,
    ): FetchResult<List<AlertEventDef>> {
        catalogCalls.incrementAndGet()
        lastToken = token
        return catalog
    }

    override suspend fun setAlertEvent(
        fdUrl: String,
        token: String,
        type: String,
        enabled: Boolean,
    ): ActionResult<List<AlertEventDef>> {
        lastToggle = type to enabled
        lastToken = token
        return toggleResult
    }
}

@RunWith(RobolectricTestRunner::class)
class AlertsViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private fun newVm(
        client: FrontDeskClient,
        linkStore: LinkStore,
    ): AlertsViewModel = AlertsViewModel(client, linkStore, "http://fd:1")

    @Before
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Unconfined)
    }

    @After
    fun tearDownMain() {
        Dispatchers.resetMain()
    }

    // An in-memory DataStore (no disk, no Dispatchers.IO hop) keeps the token
    // read synchronous, so these Unconfined + runBlocking + withTimeout tests
    // can't flake on IO latency starving past the wall-clock bound.
    private fun newLinkStore(): LinkStore = LinkStore(InMemoryPreferencesDataStore(), FakeCipher)

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

    private val okStatus = AlertStatus(configured = true, reachable = true, healthy = true)
    private val okCatalog =
        listOf(AlertEventDef("health.down", "Health", "error", defaultOn = true))

    @Test
    fun firstRefreshLoadsStatusAndCatalog() =
        runBlocking {
            val client = FakeAlertsClient(FetchResult.Success(okStatus), FetchResult.Success(okCatalog))
            val vm = newVm(client, linkedStore())

            vm.refreshOnce()

            val s = vm.state.value
            assertFalse(s.loading)
            assertEquals(okStatus, s.status)
            assertEquals(okCatalog, s.catalog)
            assertNull(s.error)
            assertFalse(s.revoked)
            assertEquals("tok-1", client.lastToken)
        }

    @Test
    fun failedRefreshKeepsStaleAndRecovers() =
        runBlocking {
            val client = FakeAlertsClient(FetchResult.Success(okStatus), FetchResult.Success(okCatalog))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            client.status = FetchResult.Failure("boom")
            vm.refreshOnce()
            // Stale status stays on screen and the error surfaces.
            assertEquals(okStatus, vm.state.value.status)
            assertEquals("boom", vm.state.value.error)

            client.status = FetchResult.Success(okStatus)
            vm.refreshOnce()
            assertNull(vm.state.value.error)
        }

    @Test
    fun catalogFailureRaisesErrorButKeepsStatus() =
        runBlocking {
            // Status read succeeds, catalog read fails: the fresh status still
            // updates, and the failure is surfaced rather than swallowed.
            val client =
                FakeAlertsClient(FetchResult.Success(okStatus), FetchResult.Failure("cat down"))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            assertEquals(okStatus, vm.state.value.status)
            assertEquals("cat down", vm.state.value.error)
            assertTrue(vm.state.value.catalog.isEmpty())
        }

    @Test
    fun statusUnauthorizedFlagsRevoked() =
        runBlocking {
            val client = FakeAlertsClient(FetchResult.Unauthorized, FetchResult.Success(okCatalog))
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertFalse(vm.state.value.loading)
        }

    @Test
    fun catalogUnauthorizedAlsoFlagsRevoked() =
        runBlocking {
            val client = FakeAlertsClient(FetchResult.Success(okStatus), FetchResult.Unauthorized)
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
        }

    @Test
    fun unreadableTokenFlagsRevokedWithoutCalling() =
        runBlocking {
            // Linked in name only (e.g. the Keystore key is gone): no request
            // can succeed, so surface the same flag a remote revoke raises.
            val client = FakeAlertsClient(FetchResult.Success(okStatus), FetchResult.Success(okCatalog))
            val vm = newVm(client, newLinkStore())
            vm.refreshOnce()
            assertTrue(vm.state.value.revoked)
            assertNull(client.lastToken)
            assertEquals(0, client.statusCalls.get())
            assertEquals(0, client.catalogCalls.get())
        }

    @Test
    fun toggleAdoptsEchoedSelection() =
        runBlocking {
            // Front Desk echoes the whole refreshed selection; the VM adopts it
            // wholesale (the ack doubles as the re-read) rather than guessing.
            val echoed = listOf(AlertEventDef("health.down", "Health", "error", defaultOn = true, enabled = true))
            val client =
                FakeAlertsClient(
                    FetchResult.Success(okStatus),
                    FetchResult.Success(okCatalog),
                    toggleResult = ActionResult.Success(echoed),
                )
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            vm.toggleEvent("health.down", true)

            assertEquals("health.down" to true, client.lastToggle)
            assertEquals(echoed, vm.state.value.catalog)
            assertNull(vm.state.value.action.togglingType)
            assertFalse(vm.state.value.action.forbidden)
        }

    @Test
    fun toggleForbiddenSetsFlagAndKeepsCatalog() =
        runBlocking {
            val client =
                FakeAlertsClient(
                    FetchResult.Success(okStatus),
                    FetchResult.Success(okCatalog),
                    toggleResult = ActionResult.Forbidden,
                )
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            vm.toggleEvent("health.down", false)

            assertTrue(vm.state.value.action.forbidden)
            assertEquals(okCatalog, vm.state.value.catalog)
            assertNull(vm.state.value.action.togglingType)
        }

    @Test
    fun toggleUnauthorizedRevokes() =
        runBlocking {
            val client =
                FakeAlertsClient(
                    FetchResult.Success(okStatus),
                    FetchResult.Success(okCatalog),
                    toggleResult = ActionResult.Unauthorized,
                )
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            vm.toggleEvent("health.down", true)

            assertTrue(vm.state.value.revoked)
        }

    @Test
    fun toggleFailureSurfacesErrorThenDismiss() =
        runBlocking {
            val client =
                FakeAlertsClient(
                    FetchResult.Success(okStatus),
                    FetchResult.Success(okCatalog),
                    toggleResult = ActionResult.Failure("nope"),
                )
            val vm = newVm(client, linkedStore())
            vm.refreshOnce()

            vm.toggleEvent("health.down", true)
            assertEquals("nope", vm.state.value.action.error)

            vm.dismissActionError()
            assertNull(vm.state.value.action.error)
        }

    @Test
    fun enabledSeverityCountsAlwaysReturnsAllFourKeys() {
        val catalog =
            listOf(
                AlertEventDef("health.down", "Health", "error", enabled = true),
                AlertEventDef("config.sync_failed", "Config", "warning", enabled = true),
                AlertEventDef("config.synced", "Config", "info", enabled = false),
                AlertEventDef("health.up", "Health", "success", enabled = true),
                AlertEventDef("member.added", "Membership", "warning", enabled = true),
            )
        val counts = enabledSeverityCounts(catalog)
        assertEquals(setOf("error", "warning", "info", "success"), counts.keys)
        assertEquals(1, counts["error"])
        assertEquals(2, counts["warning"]) // enabled warnings only
        assertEquals(0, counts["info"]) // present as 0 even though nothing enabled
        assertEquals(1, counts["success"])
    }
}
