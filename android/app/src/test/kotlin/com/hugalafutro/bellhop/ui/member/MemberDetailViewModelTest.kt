package com.hugalafutro.bellhop.ui.member

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FetchResult
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberTraffic
import com.hugalafutro.bellhop.data.PairedDevice
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

// FakeTrafficClient stubs the one read the detail ViewModel makes.
private class FakeTrafficClient(
    var trafficResult: FetchResult<MemberTraffic>,
) : FrontDeskClient() {
    val trafficCalls = AtomicInteger(0)
    var lastToken: String? = null
    var lastMemberId: String? = null

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
}
