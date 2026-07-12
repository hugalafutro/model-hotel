package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

/**
 * LockSnapshot is a one-shot read of everything the gate decision needs: the
 * current [LockConfig] plus the persisted last-foreground-exit stamp. The stamp
 * is read on demand rather than carried on the config flow because it churns on
 * every background, while the config only changes when the user edits Settings.
 */
data class LockSnapshot(
    val config: LockConfig,
    val lastForegroundExit: Long,
)

// Separate from the link DataStore so lock policy and the token record have
// independent lifecycles; unlink clears both explicitly.
private val Context.lockDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_lock")

/**
 * LockStore persists the app-lock policy and the last-foreground-exit timestamp
 * that the idle timer measures from. The stamp is deliberately in DataStore, not
 * in memory, so it survives OS process death: a cold start after a long absence
 * still sees the stale exit and locks (plan section 3.1).
 */
class LockStore(
    private val dataStore: DataStore<Preferences>,
) {
    /** config emits the policy, defaulting to disabled with a 30-minute window. */
    val config: Flow<LockConfig> = dataStore.data.map { it.toConfig() }

    /** snapshot reads the policy and exit stamp together for a gate decision. */
    suspend fun snapshot(): LockSnapshot {
        val prefs = dataStore.data.first()
        return LockSnapshot(prefs.toConfig(), prefs[LAST_EXIT] ?: 0L)
    }

    /**
     * setEnabled toggles the lock. Enabling stamps the exit to [now] so a policy
     * turned on while foregrounded can't retroactively lock from an old exit the
     * next time the gate is evaluated; the real exit is restamped on background.
     */
    suspend fun setEnabled(
        enabled: Boolean,
        now: Long = System.currentTimeMillis(),
    ) {
        dataStore.edit { prefs ->
            prefs[ENABLED] = enabled
            if (enabled) prefs[LAST_EXIT] = now
        }
    }

    suspend fun setTimeout(timeoutMs: Long) {
        dataStore.edit { it[TIMEOUT] = timeoutMs }
    }

    /** stampExit records when Bellhop last left the foreground. */
    suspend fun stampExit(now: Long) {
        dataStore.edit { it[LAST_EXIT] = now }
    }

    suspend fun clear() {
        dataStore.edit { it.clear() }
    }

    private fun Preferences.toConfig(): LockConfig =
        LockConfig(
            enabled = this[ENABLED] ?: false,
            timeoutMs = this[TIMEOUT] ?: LockTimeout.DEFAULT.millis,
        )

    companion object {
        fun create(context: Context): LockStore = LockStore(context.applicationContext.lockDataStore)

        private val ENABLED = booleanPreferencesKey("enabled")
        private val TIMEOUT = longPreferencesKey("timeout_ms")
        private val LAST_EXIT = longPreferencesKey("last_foreground_exit")
    }
}
