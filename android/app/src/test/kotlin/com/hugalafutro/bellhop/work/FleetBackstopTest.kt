package com.hugalafutro.bellhop.work

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.work.ListenableWorker.Result
import com.hugalafutro.bellhop.data.FleetSnapshot
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberHealthState
import com.hugalafutro.bellhop.data.MemberTransition
import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.data.PairedDevice
import com.hugalafutro.bellhop.data.TokenCipher
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
 * The worker's guard/dispatch layer, exercised through [runBackstop] so the three
 * short-circuits doWork() takes before ever polling are pinned without the
 * WorkManager runtime: monitoring off, the device unlinked, or the token
 * unreadable each end in success without touching the network. The remaining
 * cases cover the PollResult -> worker Result mapping and that a health edge is
 * handed to the notifier. Fetch/diff/persist itself lives in [FleetPollTest].
 */
class FleetBackstopTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private lateinit var server: MockWebServer
    private val client = FrontDeskClient()
    private val fired = mutableListOf<MemberTransition>()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    // Fake ciphers so LinkStore's persistence runs without an AndroidKeyStore
    // (Robolectric has no provider): passThrough round-trips the token, nulling
    // decrypts to a Linked-but-unreadable state for the token-null guard.
    private val passThrough =
        object : TokenCipher {
            override fun encrypt(plaintext: String) = plaintext

            override fun decrypt(stored: String) = stored
        }
    private val nulling =
        object : TokenCipher {
            override fun encrypt(plaintext: String) = plaintext

            override fun decrypt(stored: String): String? = null
        }

    private fun preferences(name: String): DataStore<Preferences> {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        return PreferenceDataStoreFactory.create(scope = scope) {
            File(tmp.newFolder(), "$name.preferences_pb")
        }
    }

    private fun monitorStore(): MonitorStore = MonitorStore(preferences("monitor"))

    private fun linkStore(cipher: TokenCipher): LinkStore = LinkStore(preferences("link"), cipher)

    private suspend fun linkedTo(
        url: String,
        cipher: TokenCipher = passThrough,
    ): LinkStore =
        linkStore(cipher).also {
            it.save(
                fdUrl = url,
                fdName = "Home Front Desk",
                token = "tok-1",
                device = PairedDevice(id = "dev-1", label = "Pixel 8", role = "operator"),
            )
        }

    private fun memberBody(healthy: Boolean): String =
        """[{"id":"m1","name":"hotel-1","state":"active",""" +
            """"status":{"health":{"known":true,"healthy":$healthy}}}]"""

    private suspend fun run(
        monitor: MonitorStore,
        link: LinkStore,
        canNotify: Boolean = true,
    ): Result = runBackstop(monitor, link, client, canNotify, notify = { fired += it })

    @Test
    fun disabledMonitoringSucceedsWithoutPolling() =
        runBlocking {
            // enabled defaults to off; a linked device must not be probed.
            val result = run(monitorStore(), linkedTo(server.url("/").toString()))

            assertEquals(Result.success(), result)
            assertEquals(0, server.requestCount)
            assertTrue(fired.isEmpty())
        }

    @Test
    fun unlinkedDeviceSucceedsWithoutPolling() =
        runBlocking {
            val monitor = monitorStore().apply { setEnabled(true) }

            val result = run(monitor, linkStore(passThrough))

            assertEquals(Result.success(), result)
            assertEquals(0, server.requestCount)
        }

    @Test
    fun unreadableTokenSucceedsWithoutPolling() =
        runBlocking {
            // Linked (token + url present) but the cipher can't decrypt it: the
            // foreground UI surfaces the revoke, the backstop just stops.
            val monitor = monitorStore().apply { setEnabled(true) }
            val link = linkedTo(server.url("/").toString(), cipher = nulling)

            val result = run(monitor, link)

            assertEquals(Result.success(), result)
            assertEquals(0, server.requestCount)
        }

    @Test
    fun blockedNotificationsSkipPollAndFreezeBaseline() =
        runBlocking {
            val monitor =
                monitorStore().apply {
                    setEnabled(true)
                    saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), epoch())
                }
            // With no way to deliver, polling would only advance the baseline over a
            // change the operator can't see; freeze instead so it fires once granted.
            val result = run(monitor, linkedTo(server.url("/").toString()), canNotify = false)

            assertEquals(Result.success(), result)
            assertEquals(0, server.requestCount)
            assertEquals(MemberHealthState.UP, monitor.snapshot()?.stateOf("m1"))
            assertTrue(fired.isEmpty())
        }

    @Test
    fun healthEdgeIsNotifiedAndSucceeds() =
        runBlocking {
            val monitor =
                monitorStore().apply {
                    setEnabled(true)
                    saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), epoch())
                }
            server.enqueue(MockResponse().setBody(memberBody(healthy = false)))

            val result = run(monitor, linkedTo(server.url("/").toString()))

            assertEquals(Result.success(), result)
            assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), fired)
        }

    @Test
    fun pushOnlyEnabledStillPollsAndNotifies() =
        runBlocking {
            // Layer 2 (periodic) off but Layer 3 (push) on: a push-triggered
            // one-shot must still fetch/diff/notify, so the guard keys off the
            // shared active flag, not the Layer-2 enabled flag alone.
            val monitor =
                monitorStore().apply {
                    setPushEnabled(true)
                    saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), epoch())
                }
            server.enqueue(MockResponse().setBody(memberBody(healthy = false)))

            val result = run(monitor, linkedTo(server.url("/").toString()))

            assertEquals(Result.success(), result)
            assertEquals(listOf(MemberTransition.WentDown("m1", "hotel-1")), fired)
        }

    @Test
    fun revokedTokenSucceedsWithoutRetryOrNotification() =
        runBlocking {
            val monitor = monitorStore().apply { setEnabled(true) }
            server.enqueue(MockResponse().setResponseCode(401).setBody("""{"error":{"code":"unauthorized"}}"""))

            val result = run(monitor, linkedTo(server.url("/").toString()))

            // A dead token can never succeed again, so end quietly rather than retry.
            assertEquals(Result.success(), result)
            assertTrue(fired.isEmpty())
        }

    @Test
    fun transientFailureRetriesWithoutNotification() =
        runBlocking {
            val monitor = monitorStore().apply { setEnabled(true) }
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            val result = run(monitor, linkedTo(server.url("/").toString()))

            assertEquals(Result.retry(), result)
            assertTrue(fired.isEmpty())
        }

    @Test
    fun pushOneShotTransientFailureSucceedsWithoutRetry() =
        runBlocking {
            // The push one-shot passes retryOnFailure = false: a retrying one-shot
            // would hold the KEEP unique-work slot through its backoff and drop
            // pushes that land during that window, so it ends in success and frees
            // the slot for the next wake instead.
            val monitor = monitorStore().apply { setPushEnabled(true) }
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            val result =
                runBackstop(
                    monitor,
                    linkedTo(server.url("/").toString()),
                    client,
                    canNotify = true,
                    notify = { fired += it },
                    retryOnFailure = false,
                )

            assertEquals(Result.success(), result)
            assertTrue(fired.isEmpty())
        }
}
