package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

// Separate DataStore from the link and lock records so background-monitoring
// policy has its own lifecycle; unlink clears all three explicitly.
private val Context.monitorDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_monitor")

/**
 * MonitorStore persists the background-backstop policy (plan section 5.2, Layer
 * 2): whether the periodic fleet poll is enabled, plus the last-seen
 * [FleetSnapshot] the poll diffs against. The snapshot lives here, not in the
 * worker, because a periodic worker is torn down between runs; only a persisted
 * baseline lets it tell what changed since last time.
 *
 * Enabled defaults to off: turning it on schedules background work and (on API
 * 33+) prompts for the notification permission, so it's an explicit opt-in rather
 * than a surprise on first launch (the same stance the app lock ships with).
 */
class MonitorStore(
    private val dataStore: DataStore<Preferences>,
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    /** enabled emits the backstop toggle, defaulting to off. */
    val enabled: Flow<Boolean> = dataStore.data.map { it[ENABLED] ?: false }

    suspend fun setEnabled(enabled: Boolean) {
        dataStore.edit { it[ENABLED] = enabled }
    }

    /**
     * snapshot reads the last-seen fleet health, or null if none was ever saved
     * (a fresh opt-in) or the stored form can't be parsed (a corrupt or
     * incompatible record). Null means "no baseline", which [diffFleet] treats as
     * a silent first poll rather than alerting on every member.
     */
    suspend fun snapshot(): FleetSnapshot? {
        val stored = dataStore.data.first()[SNAPSHOT] ?: return null
        return runCatching { json.decodeFromString<FleetSnapshot>(stored) }.getOrNull()
    }

    suspend fun saveSnapshot(snapshot: FleetSnapshot) {
        dataStore.edit { it[SNAPSHOT] = json.encodeToString(snapshot) }
    }

    suspend fun clear() {
        dataStore.edit { it.clear() }
    }

    companion object {
        fun create(context: Context): MonitorStore = MonitorStore(context.applicationContext.monitorDataStore)

        private val ENABLED = booleanPreferencesKey("enabled")
        private val SNAPSHOT = stringPreferencesKey("fleet_snapshot")
    }
}
