package com.hugalafutro.bellhop.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class LinkStoreTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private fun newStore(): LinkStore {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        val ds: DataStore<Preferences> =
            PreferenceDataStoreFactory.create(scope = scope) {
                File(tmp.newFolder(), "link.preferences_pb")
            }
        return LinkStore(ds, FakeCipher)
    }

    @Test
    fun startsUnlinked() =
        runBlocking {
            assertEquals(LinkState.Unlinked, newStore().state.first())
        }

    @Test
    fun saveThenClearRoundTrips() =
        runBlocking {
            val store = newStore()
            store.save(
                fdUrl = "http://10.0.2.2:8080",
                fdName = "Home Front Desk",
                token = "device-token",
                device = PairedDevice(id = "dev-1", label = "Pixel 8", role = "operator"),
            )

            val state = store.state.first()
            assertTrue(state is LinkState.Linked)
            state as LinkState.Linked
            assertEquals("http://10.0.2.2:8080", state.fdUrl)
            assertEquals("Home Front Desk", state.fdName)
            assertEquals("operator", state.role)
            assertEquals("dev-1", state.deviceId)
            assertEquals("Pixel 8", state.label)

            assertEquals("device-token", store.token())

            store.clear()
            assertEquals(LinkState.Unlinked, store.state.first())
            assertNull(store.token())
        }

    @Test
    fun saveStampsLinkedAt() =
        runBlocking {
            val store = newStore()
            store.save(
                fdUrl = "http://10.0.2.2:8080",
                fdName = "Home Front Desk",
                token = "device-token",
                device = PairedDevice(id = "dev-1", label = "Pixel 8", role = "operator"),
                now = 1_700_000_000_000L,
            )
            val state = store.state.first() as LinkState.Linked
            assertEquals(1_700_000_000_000L, state.linkedAt)
        }
}
