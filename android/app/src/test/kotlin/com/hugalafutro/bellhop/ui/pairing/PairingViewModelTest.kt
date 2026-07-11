package com.hugalafutro.bellhop.ui.pairing

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.hugalafutro.bellhop.data.FakeCipher
import com.hugalafutro.bellhop.data.FrontDeskClient
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.PairResponse
import com.hugalafutro.bellhop.data.PairResult
import com.hugalafutro.bellhop.data.PairedDevice
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.io.File

/** FakeClient returns a canned result without touching the network. */
private class FakeClient(
    private val result: PairResult,
) : FrontDeskClient() {
    var lastArgs: Triple<String, String, String>? = null

    override suspend fun pair(
        fdUrl: String,
        code: String,
        label: String,
    ): PairResult {
        lastArgs = Triple(fdUrl, code, label)
        return result
    }
}

@RunWith(RobolectricTestRunner::class)
class PairingViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    // viewModelScope dispatches on Main; Unconfined runs launched work inline so
    // runBlocking assertions see the result without a test scheduler to pump.
    @Before
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Unconfined)
    }

    @After
    fun tearDown() {
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

    @Test
    fun pastedStringFillsAllFieldsAndEnablesPair() {
        val vm = PairingViewModel(FakeClient(PairResult.InvalidCode), newLinkStore())
        vm.onPastePayload(
            """{"fd_url":"http://10.0.2.2:8080","pairing_code":"ABC","fd_name":"Home"}""",
        )
        val s = vm.state.value
        assertEquals("http://10.0.2.2:8080", s.fdUrl)
        assertEquals("ABC", s.code)
        assertEquals("Home", s.fdName)
        assertTrue(s.parsed)
        assertTrue(s.canSubmit)
    }

    @Test
    fun unreadableStringSurfacesBadStringAndBlocksPair() {
        val vm = PairingViewModel(FakeClient(PairResult.InvalidCode), newLinkStore())
        vm.onPastePayload("not a pairing string")
        val s = vm.state.value
        assertFalse(s.parsed)
        assertFalse(s.canSubmit)
        assertEquals(PairingError.BadString, s.error)
    }

    @Test
    fun clearingPasteResets() {
        val vm = PairingViewModel(FakeClient(PairResult.InvalidCode), newLinkStore())
        vm.onPastePayload("""{"fd_url":"http://h:1","pairing_code":"ABC","fd_name":"H"}""")
        assertTrue(vm.state.value.parsed)
        vm.onPastePayload("")
        val s = vm.state.value
        assertFalse(s.parsed)
        assertEquals("", s.code)
        assertEquals(null, s.error)
    }

    @Test
    fun successfulPairPersistsLink() =
        runBlocking {
            val response =
                PairResponse(
                    token = "tok",
                    device = PairedDevice(id = "d1", label = "Pixel", role = "operator"),
                )
            val client = FakeClient(PairResult.Success(response))
            val store = newLinkStore()
            val vm = PairingViewModel(client, store)
            vm.onPastePayload(
                """{"fd_url":"http://10.0.2.2:8080/","pairing_code":"ABC","fd_name":"Home"}""",
            )

            vm.pair()

            // viewModelScope work completes; the link is persisted with the
            // trailing slash trimmed.
            val state = store.state.first { it is LinkState.Linked }
            state as LinkState.Linked
            assertEquals("http://10.0.2.2:8080", state.fdUrl)
            assertEquals("operator", state.role)
            assertEquals("ABC", client.lastArgs?.second)
        }

    @Test
    fun invalidCodeSurfacesError() =
        runBlocking {
            val vm = PairingViewModel(FakeClient(PairResult.InvalidCode), newLinkStore())
            vm.onPastePayload("""{"fd_url":"http://h:1","pairing_code":"BAD","fd_name":"H"}""")

            vm.pair()
            val s = vm.state.first { it.error == PairingError.InvalidCode }

            assertFalse(s.busy)
        }

    @Test
    fun resetClearsStaleFormState() {
        // Regression: after a successful pair the Activity-scoped VM keeps busy
        // + pasteText; reset must clear them so re-entering pairing is clean.
        val vm = PairingViewModel(FakeClient(PairResult.InvalidCode), newLinkStore())
        vm.onPastePayload("""{"fd_url":"http://h:1","pairing_code":"ABC","fd_name":"H"}""")
        vm.reset()
        val s = vm.state.value
        assertFalse(s.busy)
        assertFalse(s.parsed)
        assertEquals("", s.pasteText)
        assertEquals("", s.code)
    }

    @Test
    fun blankStateBlocksSubmit() {
        val vm = PairingViewModel(FakeClient(PairResult.InvalidCode), newLinkStore())
        assertFalse(vm.state.value.canSubmit)
        vm.onPastePayload("""{"fd_url":"http://h:1","pairing_code":"ABC","fd_name":"H"}""")
        assertTrue(vm.state.value.canSubmit)
    }
}
