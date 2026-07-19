package com.hugalafutro.bellhop.work

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.work.ListenableWorker.Result
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.PairedDevice
import com.hugalafutro.bellhop.data.TokenCipher
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
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

/**
 * refreshWidgetOnly is the widget refresh button's display-only poll, so these
 * tests pin the three properties that separate it from [runBackstop]: it works
 * with monitoring fully off (its whole reason to exist), it can never touch the
 * alert baseline (it takes no MonitorStore, so this holds by construction), and
 * it always ends in success — a user-initiated one-shot must not hold the retry
 * backoff slot, and the stale stamp already tells the truth about a failed
 * refresh. Fetch/persist wiring mirrors [FleetPollTest]; the linked-store seeding
 * mirrors [FleetBackstopTest].
 */
class WidgetRefreshTest {
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

    // A fake cipher round-trips the token so LinkStore persists without an
    // AndroidKeyStore provider (Robolectric has none), same as FleetBackstopTest.
    private val passThrough =
        object : TokenCipher {
            override fun encrypt(plaintext: String) = plaintext

            override fun decrypt(stored: String) = stored
        }

    private fun preferences(name: String): DataStore<Preferences> {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        return PreferenceDataStoreFactory.create(scope = scope) {
            File(tmp.newFolder(), "$name.preferences_pb")
        }
    }

    private fun newWidgetStore(): WidgetStore = WidgetStore(preferences("widget"))

    private fun unlinkedLinkStore(): LinkStore = LinkStore(preferences("link"), passThrough)

    private suspend fun linkedLinkStore(): LinkStore =
        LinkStore(preferences("link"), passThrough).also {
            it.save(
                fdUrl = server.url("/").toString(),
                fdName = "Home Front Desk",
                token = "tok-1",
                device = PairedDevice(id = "dev-1", label = "Pixel 8", role = "operator"),
            )
        }

    private fun memberBody(healthy: Boolean): String =
        """[{"id":"m1","name":"hotel-1","state":"active",""" +
            """"status":{"health":{"known":true,"healthy":$healthy}}}]"""

    // A successful refresh fetches members then auto-sync, so enqueue both.
    private fun enqueuePoll(
        healthy: Boolean,
        stale: Boolean = false,
    ) {
        server.enqueue(MockResponse().setBody(memberBody(healthy)))
        server.enqueue(MockResponse().setBody("""{"enabled":true,"primary_id":"m1","stale":$stale}"""))
    }

    @Test
    fun refreshWritesWidgetState() =
        runBlocking {
            // Monitoring fully OFF: refresh must still work (that is its point).
            // Baseline safety is by construction: refreshWidgetOnly does not even
            // take a MonitorStore, so it CANNOT touch the alert baseline.
            val widget = newWidgetStore()
            enqueuePoll(healthy = true)

            val result = refreshWidgetOnly(linkedLinkStore(), widget, client, now = { 42L })

            assertEquals(Result.success(), result)
            assertEquals(listOf(WidgetMember("hotel-1", "UP")), widget.read()?.members)
            assertEquals(42L, widget.read()?.updatedAt)
        }

    @Test
    fun refreshWhileUnlinkedIsANoOp() =
        runBlocking {
            val widget = newWidgetStore()

            val result = refreshWidgetOnly(unlinkedLinkStore(), widget, client, now = { 42L })

            assertEquals(Result.success(), result)
            assertNull(widget.read())
            assertEquals(0, server.requestCount)
        }

    @Test
    fun failedRefreshKeepsStaleStateAndSucceeds() =
        runBlocking {
            val widget = newWidgetStore()
            // Seed a previous fetch's state; a failed refresh must keep showing it
            // (a wiped store would read null, so the surviving seed proves "kept").
            widget.saveIfChanged(
                WidgetState(members = listOf(WidgetMember("hotel-1", "UP")), updatedAt = 7L),
                widget.generation(),
            )
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            val result = refreshWidgetOnly(linkedLinkStore(), widget, client, now = { 42L })

            // User-initiated one-shot never retries; stale beats blank.
            assertEquals(Result.success(), result)
            assertEquals(7L, widget.read()?.updatedAt)
        }

    @Test
    fun autoSyncFailureKeepsLastKnownStaleFlag() =
        runBlocking {
            val widget = newWidgetStore()
            // Seed a stale flag; a failed auto-sync read must fall back to it rather
            // than clear it out from under the display.
            widget.saveIfChanged(
                WidgetState(
                    autosyncStale = true,
                    members = listOf(WidgetMember("hotel-1", "UP")),
                    updatedAt = 7L,
                ),
                widget.generation(),
            )
            server.enqueue(MockResponse().setBody(memberBody(healthy = true)))
            server.enqueue(MockResponse().setResponseCode(500).setBody("nope"))

            val result = refreshWidgetOnly(linkedLinkStore(), widget, client, now = { 42L })

            assertEquals(Result.success(), result)
            assertEquals(true, widget.read()?.autosyncStale)
        }
}
