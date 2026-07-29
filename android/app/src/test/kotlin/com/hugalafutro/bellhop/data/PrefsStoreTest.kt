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
    fun widgetQuotaDefaultsOnAndWidgetGraphsOff() =
        runBlocking {
            // Opposite defaults on purpose: the widget carried the badge strip
            // before this switch existed, so leaving it out must not remove it,
            // while the traffic bars were always opt-in for their extra request.
            val store = newStore()
            assertEquals(true, store.widgetQuota.first())
            assertEquals(false, store.widgetGraphs.first())
        }

    @Test
    fun widgetQuotaRoundTripsThroughSet() =
        runBlocking {
            val store = newStore()
            store.setWidgetQuota(false)
            assertEquals(false, store.widgetQuota.first())

            store.setWidgetQuota(true)
            assertEquals(true, store.widgetQuota.first())
        }

    @Test
    fun timeFormatDefaultsToFollowingTheDevice() =
        runBlocking {
            assertEquals(TimeFormat.SYSTEM, newStore().timeFormat.first())
        }

    @Test
    fun timeFormatRoundTripsThroughSet() =
        runBlocking {
            val store = newStore()
            store.setTimeFormat(TimeFormat.H12)
            assertEquals(TimeFormat.H12, store.timeFormat.first())

            store.setTimeFormat(TimeFormat.H24)
            assertEquals(TimeFormat.H24, store.timeFormat.first())
        }

    @Test
    fun unrecognizedStoredTimeFormatDegradesToDefault() =
        runBlocking {
            val dataStore = InMemoryPreferencesDataStore()
            dataStore.edit { it[stringPreferencesKey("time_format")] = "SOME_FUTURE_CLOCK" }

            assertEquals(TimeFormat.SYSTEM, PrefsStore(dataStore).timeFormat.first())
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

    @Test
    fun widgetQuotaAlignDefaultsToLeft() =
        runBlocking {
            // LEFT is exactly what the widget did before this preference existed,
            // so an upgrading user must see no change until they choose otherwise.
            assertEquals(QuotaBadgeAlign.LEFT, newStore().widgetQuotaAlign.first())
        }

    @Test
    fun widgetQuotaAlignRoundTripsThroughSet() =
        runBlocking {
            val store = newStore()
            store.setWidgetQuotaAlign(QuotaBadgeAlign.CENTER)
            assertEquals(QuotaBadgeAlign.CENTER, store.widgetQuotaAlign.first())

            store.setWidgetQuotaAlign(QuotaBadgeAlign.RIGHT)
            assertEquals(QuotaBadgeAlign.RIGHT, store.widgetQuotaAlign.first())

            store.setWidgetQuotaAlign(QuotaBadgeAlign.LEFT)
            assertEquals(QuotaBadgeAlign.LEFT, store.widgetQuotaAlign.first())
        }

    @Test
    fun unrecognizedStoredWidgetQuotaAlignDegradesToDefault() =
        runBlocking {
            val dataStore = InMemoryPreferencesDataStore()
            // A value written by a future build with an alignment this build does
            // not know: must fall back to the default, not throw.
            dataStore.edit { it[stringPreferencesKey("widget_quota_align")] = "DIAGONAL" }

            assertEquals(QuotaBadgeAlign.LEFT, PrefsStore(dataStore).widgetQuotaAlign.first())
        }
}
