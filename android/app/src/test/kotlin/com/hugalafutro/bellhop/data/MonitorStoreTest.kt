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
            store.saveSnapshot(snapshot, store.epoch())
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
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to "FROM_THE_FUTURE")), store.epoch())
            assertNull(store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun snapshotSaveIsIgnoredWhileMonitoringOff() =
        runBlocking {
            // A poll finishing after unlink cleared the store must not resurrect a
            // baseline: with monitoring off, saveSnapshot is a no-op.
            val store = newStore()
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            assertNull(store.snapshot())
        }

    @Test
    fun snapshotSaveFromAStaleSessionIsDropped() =
        runBlocking {
            // An in-flight poll captured this session's epoch...
            val store = newStore()
            store.setEnabled(true)
            val staleEpoch = store.epoch()
            // ...then the operator unlinked and re-enabled, starting a new session.
            store.clear()
            store.setEnabled(true)
            // The old poll now tries to persist against the stale epoch: it must be
            // dropped so it can't poison the new session's baseline.
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), staleEpoch)
            assertNull(store.snapshot())
        }

    @Test
    fun clearResetsEnabledAndSnapshot() =
        runBlocking {
            val store = newStore()
            store.setEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            store.clear()
            assertFalse(store.enabled.first())
            assertNull(store.snapshot())
        }

    @Test
    fun pushDefaultsToDisabled() =
        runBlocking {
            assertFalse(newStore().pushEnabled.first())
        }

    @Test
    fun activeIsTrueWhenEitherLayerOn() =
        runBlocking {
            // The shared snapshot machinery keys off active, so push alone must
            // count: a Layer-3-only user still needs the baseline maintained.
            val store = newStore()
            assertFalse(store.active.first())
            store.setPushEnabled(true)
            assertTrue(store.active.first())
            store.setPushEnabled(false)
            store.setEnabled(true)
            assertTrue(store.active.first())
        }

    @Test
    fun pushOnlySessionStampsFreshEpoch() =
        runBlocking {
            val store = newStore()
            assertEquals(0L, store.epoch())
            store.setPushEnabled(true)
            assertTrue(store.epoch() != 0L)
        }

    @Test
    fun enablingSecondLayerKeepsSessionEpoch() =
        runBlocking {
            // Turning push on mid-session must not rotate the epoch, or an in-flight
            // Layer-2 poll's snapshot save would be dropped for no reason.
            val store = newStore()
            store.setEnabled(true)
            val session = store.epoch()
            store.setPushEnabled(true)
            assertEquals(session, store.epoch())
        }

    @Test
    fun snapshotPersistsWhenOnlyPushEnabled() =
        runBlocking {
            val store = newStore()
            store.setPushEnabled(true)
            store.saveSnapshot(FleetSnapshot(mapOf("m1" to MemberHealthState.UP.name)), store.epoch())
            assertEquals(MemberHealthState.UP, store.snapshot()?.stateOf("m1"))
        }

    @Test
    fun endpointRoundTripsWhilePushEnabled() =
        runBlocking {
            val store = newStore()
            store.setPushEnabled(true)
            store.saveEndpoint("https://ntfy.sh/upABC123", store.pushInstance()!!)
            assertEquals("https://ntfy.sh/upABC123", store.endpoint.first())
        }

    @Test
    fun endpointIgnoredWhenPushDisabled() =
        runBlocking {
            // A late onNewEndpoint arriving after push was turned off (or before it
            // was ever on) must not resurrect a topic Settings would then display.
            val store = newStore()
            store.saveEndpoint("https://ntfy.sh/upABC123", "any-instance")
            assertNull(store.endpoint.first())
        }

    @Test
    fun endpointFromSupersededRegistrationIgnored() =
        runBlocking {
            // A late onNewEndpoint from a registration that was already replaced
            // (push toggled off/on, or unlink + re-pair) carries the old instance id
            // and must not overwrite the live topic with a stale one the distributor
            // no longer routes.
            val store = newStore()
            store.setPushEnabled(true)
            val stale = store.pushInstance()!!
            store.setPushEnabled(false)
            store.setPushEnabled(true)
            val current = store.pushInstance()!!
            store.saveEndpoint("https://ntfy.sh/upOLD", stale)
            assertNull(store.endpoint.first())
            store.saveEndpoint("https://ntfy.sh/upNEW", current)
            assertEquals("https://ntfy.sh/upNEW", store.endpoint.first())
        }

    @Test
    fun clearEndpointFromSupersededRegistrationIgnored() =
        runBlocking {
            // An onUnregistered/onRegistrationFailed for a superseded registration
            // must not wipe the endpoint a newer registration just published.
            val store = newStore()
            store.setPushEnabled(true)
            val current = store.pushInstance()!!
            store.saveEndpoint("https://ntfy.sh/upABC123", current)
            store.clearEndpoint("stale-instance")
            assertEquals("https://ntfy.sh/upABC123", store.endpoint.first())
            store.clearEndpoint(current)
            assertNull(store.endpoint.first())
        }

    @Test
    fun disablingPushClearsEndpoint() =
        runBlocking {
            val store = newStore()
            store.setPushEnabled(true)
            store.saveEndpoint("https://ntfy.sh/upABC123", store.pushInstance()!!)
            store.setPushEnabled(false)
            assertNull(store.endpoint.first())
        }

    @Test
    fun clearWipesPushState() =
        runBlocking {
            val store = newStore()
            store.setPushEnabled(true)
            store.saveEndpoint("https://ntfy.sh/upABC123", store.pushInstance()!!)
            store.clear()
            assertFalse(store.pushEnabled.first())
            assertNull(store.endpoint.first())
            assertNull(store.pushInstance())
        }
}
