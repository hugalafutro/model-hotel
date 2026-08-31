package com.hugalafutro.bellhop.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

/**
 * The event cursor is the second baseline the backstop keeps beside the health
 * snapshot, and it has to obey the same save gate: a write from a stale session,
 * or with the backstop off, must be dropped, and an unlink must wipe it.
 */
class MonitorStoreEventCursorTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private fun newStore(): MonitorStore {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        val ds: DataStore<Preferences> =
            PreferenceDataStoreFactory.create(scope = scope) {
                File(tmp.newFolder(), "monitor.preferences_pb")
            }
        return MonitorStore(ds)
    }

    private val cursor = EventCursor("2026-08-31T11:28:54Z", "e2")

    @Test
    fun cursorIsNullUntilSavedAndRoundTrips() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            assertNull(store.eventCursor())
            store.saveEventCursor(cursor, store.epoch())
            assertEquals(cursor, store.eventCursor())
        }

    @Test
    fun cursorSaveIsIgnoredWhileMonitoringOff() =
        runBlocking {
            val store = newStore()
            store.saveEventCursor(cursor, store.epoch())
            assertNull(store.eventCursor())
        }

    @Test
    fun cursorSaveFromAStaleSessionIsDropped() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            val staleEpoch = store.epoch()
            // Unlink + re-enable: a fresh session, which must not inherit a
            // baseline a poll from the old one is still trying to write.
            store.clear()
            store.setEnabled(true)
            store.saveEventCursor(cursor, staleEpoch)
            assertNull(store.eventCursor())
        }

    @Test
    fun clearWipesTheCursor() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveEventCursor(cursor, store.epoch())
            store.clear()
            assertNull(store.eventCursor())
        }
}
