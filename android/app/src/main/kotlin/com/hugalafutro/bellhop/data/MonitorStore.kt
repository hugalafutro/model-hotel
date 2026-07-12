package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.random.Random

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
        dataStore.edit { prefs ->
            prefs[ENABLED] = enabled
            // Each fresh enable stamps the session with a new random epoch, so a poll
            // from a previous session (see [saveSnapshot]) can't persist its snapshot
            // after an unlink + re-enable churned the store. A random id rather than a
            // counter because clear() wipes the counter, so a monotone one would reset
            // and collide across the very unlink+re-enable this guards against.
            if (enabled) prefs[EPOCH] = Random.nextLong()
        }
    }

    /** epoch identifies the current monitoring session; 0 when never enabled. */
    suspend fun epoch(): Long = dataStore.data.first()[EPOCH] ?: 0L

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

    /**
     * saveSnapshot persists the baseline, but only if monitoring is still on AND
     * still the same session ([epoch]) that started the poll. A poll can finish
     * after an unlink cleared the store, or after an unlink + re-enable started a
     * fresh session; a bare "enabled" check would let that late write repopulate a
     * cleared store or poison the new session's baseline. The epoch the caller
     * captured before fetching, the enabled flag, and the write all share one
     * atomic edit, so a concurrent clear/re-enable is either seen (we skip) or
     * overwrites us.
     */
    suspend fun saveSnapshot(
        snapshot: FleetSnapshot,
        epoch: Long,
    ) {
        dataStore.edit { prefs ->
            if (prefs[ENABLED] == true && (prefs[EPOCH] ?: 0L) == epoch) {
                prefs[SNAPSHOT] = json.encodeToString(snapshot)
            }
        }
    }

    suspend fun clear() {
        dataStore.edit { it.clear() }
    }

    companion object {
        fun create(context: Context): MonitorStore = MonitorStore(context.applicationContext.monitorDataStore)

        private val ENABLED = booleanPreferencesKey("enabled")
        private val EPOCH = longPreferencesKey("epoch")
        private val SNAPSHOT = stringPreferencesKey("fleet_snapshot")
    }
}
