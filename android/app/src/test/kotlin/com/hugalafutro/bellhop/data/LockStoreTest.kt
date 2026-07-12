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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class LockStoreTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private fun newStore(): LockStore {
        val scope = CoroutineScope(Dispatchers.IO + Job())
        val ds: DataStore<Preferences> =
            PreferenceDataStoreFactory.create(scope = scope) {
                File(tmp.newFolder(), "lock.preferences_pb")
            }
        return LockStore(ds)
    }

    @Test
    fun defaultsToDisabledWithThirtyMinuteWindow() =
        runBlocking {
            val config = newStore().config.first()
            assertFalse(config.enabled)
            assertEquals(LockTimeout.THIRTY_MINUTES.millis, config.timeoutMs)
        }

    @Test
    fun enablingStampsExitToNow() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true, now = 5_000L)
            val snap = store.snapshot()
            assertTrue(snap.config.enabled)
            assertEquals(5_000L, snap.lastForegroundExit)
        }

    @Test
    fun timeoutAndExitRoundTrip() =
        runBlocking {
            val store = newStore()
            store.setTimeout(LockTimeout.FIVE_MINUTES.millis)
            store.stampExit(12_345L)
            val snap = store.snapshot()
            assertEquals(LockTimeout.FIVE_MINUTES.millis, snap.config.timeoutMs)
            assertEquals(12_345L, snap.lastForegroundExit)
        }

    @Test
    fun clearResetsToDefaults() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true, now = 1L)
            store.setTimeout(LockTimeout.ONE_HOUR.millis)
            store.clear()
            val snap = store.snapshot()
            assertFalse(snap.config.enabled)
            assertEquals(LockTimeout.THIRTY_MINUTES.millis, snap.config.timeoutMs)
            assertEquals(0L, snap.lastForegroundExit)
        }
}
