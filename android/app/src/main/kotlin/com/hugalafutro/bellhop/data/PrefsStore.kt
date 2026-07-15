package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

// Separate DataStore for lightweight, non-secret UI preferences so it survives
// unlink (they are device-local tastes, not tied to any one Front Desk link).
private val Context.prefsDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_prefs")

/**
 * PrefsStore persists device-local UI preferences that are neither link metadata
 * nor security policy: the copy gesture (whether tapping a log or member cell
 * copies it, or a safer long-press is required, the default) and the traffic
 * graph range (how far back the request charts reach).
 */
class PrefsStore(
    private val dataStore: DataStore<Preferences>,
) {
    /** holdToCopy emits whether copy requires a long-press; defaults on. */
    val holdToCopy: Flow<Boolean> = dataStore.data.map { it[HOLD_TO_COPY] ?: true }

    suspend fun setHoldToCopy(enabled: Boolean) {
        dataStore.edit { it[HOLD_TO_COPY] = enabled }
    }

    /**
     * graphRangeMinutes emits the traffic-graph lookback in minutes, defaulting
     * to one hour. A stored value that is no longer an offered preset (e.g. after
     * a version trims the list) falls back to the default rather than sticking on
     * a range the picker can't show.
     */
    val graphRangeMinutes: Flow<Int> =
        dataStore.data.map {
            val stored = it[GRAPH_RANGE_MINUTES] ?: DEFAULT_GRAPH_RANGE_MINUTES
            if (stored in GRAPH_RANGE_OPTIONS) stored else DEFAULT_GRAPH_RANGE_MINUTES
        }

    suspend fun setGraphRangeMinutes(minutes: Int) {
        dataStore.edit { it[GRAPH_RANGE_MINUTES] = minutes }
    }

    companion object {
        fun create(context: Context): PrefsStore = PrefsStore(context.applicationContext.prefsDataStore)

        private val HOLD_TO_COPY = booleanPreferencesKey("hold_to_copy")
        private val GRAPH_RANGE_MINUTES = intPreferencesKey("graph_range_minutes")

        /** Default traffic-graph lookback: the last hour. */
        const val DEFAULT_GRAPH_RANGE_MINUTES = 60

        /**
         * Coarse presets the graph-range picker offers, in minutes (1h to 24h).
         * Deliberately sparse: nobody needs to pick an arbitrary span, and 24h is
         * the ceiling the member's 5-minute series can cover.
         */
        val GRAPH_RANGE_OPTIONS = listOf(60, 180, 360, 720, 1440)
    }
}
