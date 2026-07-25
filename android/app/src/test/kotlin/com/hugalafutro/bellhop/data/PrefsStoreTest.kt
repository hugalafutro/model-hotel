package com.hugalafutro.bellhop.data

import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * PrefsStore is device-local UI taste, not link metadata; these tests pin the
 * DataStore round-trip and the unknown-value degrade stance shared with
 * [WidgetMember.healthState].
 */
class PrefsStoreTest {
    private fun newStore(): PrefsStore = PrefsStore(InMemoryPreferencesDataStore())

    @Test
    fun quotaBarModeDefaultsToRemaining() =
        runBlocking {
            assertEquals(QuotaBarMode.REMAINING, newStore().quotaBarMode.first())
        }

    @Test
    fun quotaBarModeRoundTripsThroughSet() =
        runBlocking {
            val store = newStore()
            store.setQuotaBarMode(QuotaBarMode.USED)
            assertEquals(QuotaBarMode.USED, store.quotaBarMode.first())

            store.setQuotaBarMode(QuotaBarMode.REMAINING)
            assertEquals(QuotaBarMode.REMAINING, store.quotaBarMode.first())
        }

    @Test
    fun unrecognizedStoredValueDegradesToDefault() =
        runBlocking {
            val dataStore = InMemoryPreferencesDataStore()
            // Simulate a value written by a future build with a mode this build
            // doesn't know: must fall back to the default, not throw.
            dataStore.edit { it[stringPreferencesKey("quota_bar_mode")] = "SOME_FUTURE_MODE" }

            assertEquals(QuotaBarMode.REMAINING, PrefsStore(dataStore).quotaBarMode.first())
        }
}
