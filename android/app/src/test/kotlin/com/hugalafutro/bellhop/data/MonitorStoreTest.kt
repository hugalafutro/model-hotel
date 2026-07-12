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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class MonitorStoreTest {
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

    @Test
    fun defaultsToDisabled() =
        runBlocking {
            assertFalse(newStore().enabled.first())
        }

    @Test
    fun enabledFlagRoundTrips() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            assertTrue(store.enabled.first())
        }

    @Test
    fun snapshotIsNullUntilSaved() =
        runBlocking {
            // A fresh opt-in has no baseline, which the diff reads as a silent
            // first poll rather than alerting on every member.
            assertNull(newStore().snapshot())
        }

    @Test
    fun snapshotRoundTrips() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            val snapshot =
                FleetSnapshot(
                    mapOf(
                        "m1" to MemberHealthState.UP.name,
                        "m2" to MemberHealthState.DOWN.name,
                    ),
                )
            store.saveSnapshot(snapshot)
            val read = store.snapshot()
            assertEquals(MemberHealthState.UP, read?.stateOf("m1"))
            assertEquals(MemberHealthState.DOWN, read?.stateOf("m2"))
        }

    @Test
    fun unknownStoredStateDegradesToNull() =
        runBlocking {
            // A state name a future build wrote but this one doesn't know must not
            // crash the diff; stateOf returns null so it's treated as "no baseline".
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to "FROM_THE_FUTURE")))
            assertNull(store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun snapshotSaveIsIgnoredWhileMonitoringOff() =
        runBlocking {
            // A poll finishing after unlink cleared the store must not resurrect a
            // baseline: with monitoring off, saveSnapshot is a no-op.
            val store = newStore()
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)))
            assertNull(store.snapshot())
        }

    @Test
    fun clearResetsEnabledAndSnapshot() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)))
            store.clear()
            assertFalse(store.enabled.first())
            assertNull(store.snapshot())
        }
}
