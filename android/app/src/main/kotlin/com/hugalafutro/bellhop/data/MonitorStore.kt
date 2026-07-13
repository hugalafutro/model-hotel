package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.MutablePreferences
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
import java.util.UUID
import kotlin.random.Random

// Separate DataStore from the link and lock records so background-monitoring
// policy has its own lifecycle; unlink clears all three explicitly.
private val Context.monitorDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_monitor")

/**
 * MonitorStore persists the background-backstop policy (plan section 5.2): whether
 * the Layer-2 periodic fleet poll is enabled, whether the Layer-3 real-time push
 * wake is enabled (plus the distributor [endpoint] it registered), and the
 * last-seen [FleetSnapshot] both layers diff against. The snapshot lives here, not
 * in the worker, because a periodic worker is torn down between runs; only a
 * persisted baseline lets it tell what changed since last time.
 *
 * Both push flags default to off: turning either on schedules/registers background
 * work and (on API 33+) prompts for the notification permission, so each is an
 * explicit opt-in rather than a surprise on first launch (the same stance the app
 * lock ships with). The two layers share one snapshot and one session [epoch]
 * because they answer the same question — "has fleet health changed?" — from the
 * same Front Desk truth; push is just a faster wake for the same poll.
 */
class MonitorStore(
    private val dataStore: DataStore<Preferences>,
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    /** enabled emits the Layer-2 periodic-poll toggle, defaulting to off. */
    val enabled: Flow<Boolean> = dataStore.data.map { it[ENABLED] ?: false }

    /** pushEnabled emits the Layer-3 real-time push toggle, defaulting to off. */
    val pushEnabled: Flow<Boolean> = dataStore.data.map { it[PUSH_ENABLED] ?: false }

    /**
     * active emits whether the backstop is tracking at all: true when either the
     * Layer-2 periodic poll or the Layer-3 push wake is on. Both drive the same
     * fetch/diff/notify against one shared [snapshot], so the guards in
     * [saveSnapshot] and the worker key off this, not either layer alone.
     */
    val active: Flow<Boolean> = dataStore.data.map { it[ENABLED] == true || it[PUSH_ENABLED] == true }

    /**
     * endpoint emits the UnifiedPush endpoint URL the distributor handed back for
     * Layer 3, or null before one arrives (or after push is turned off). It's shown
     * in Settings so the user can point Front Desk's Apprise phone-topic at it.
     */
    val endpoint: Flow<String?> = dataStore.data.map { it[PUSH_ENDPOINT] }

    suspend fun setEnabled(enabled: Boolean) {
        dataStore.edit { prefs ->
            val wasActive = prefs[ENABLED] == true || prefs[PUSH_ENABLED] == true
            prefs[ENABLED] = enabled
            stampSessionIfNewlyActive(prefs, wasActive)
        }
    }

    suspend fun setPushEnabled(enabled: Boolean) {
        dataStore.edit { prefs ->
            val wasActive = prefs[ENABLED] == true || prefs[PUSH_ENABLED] == true
            prefs[PUSH_ENABLED] = enabled
            // Mint a fresh registration id on every enable so endpoint callbacks can
            // be attributed to the registration that produced them: a late
            // onNewEndpoint (or onUnregistered) from a superseded registration
            // carries the OLD id and is dropped rather than overwriting the current
            // topic (see [saveEndpoint]/[clearEndpoint]). The id is retained on
            // disable so the pending unregister can still target the right instance;
            // the next enable overwrites it and clear() wipes it.
            if (enabled) prefs[PUSH_INSTANCE] = UUID.randomUUID().toString()
            // Turning push off drops the stale endpoint: it names a distributor
            // topic we're about to unregister, so leaving it in Settings would be a
            // lie. A fresh onNewEndpoint repopulates it if push comes back on.
            if (!enabled) prefs.remove(PUSH_ENDPOINT)
            stampSessionIfNewlyActive(prefs, wasActive)
        }
    }

    /** pushInstance is the id of the current push registration, or null if none. */
    suspend fun pushInstance(): String? = dataStore.data.first()[PUSH_INSTANCE]

    /**
     * saveEndpoint records the distributor's endpoint URL, but only while push is
     * still on AND the callback belongs to the current registration [instance]: a
     * late onNewEndpoint arriving after the user turned Layer 3 off (or unlinked)
     * must not resurrect an endpoint Settings would then display, and one from a
     * superseded registration (push toggled off/on, or unlink + re-pair) must not
     * overwrite the live topic with a stale one the distributor no longer routes.
     */
    suspend fun saveEndpoint(
        url: String,
        instance: String,
    ) {
        dataStore.edit { prefs ->
            if (prefs[PUSH_ENABLED] == true && prefs[PUSH_INSTANCE] == instance) {
                prefs[PUSH_ENDPOINT] = url
            }
        }
    }

    /**
     * clearEndpoint drops the endpoint on unregister or a registration failure, but
     * only when the callback's [instance] is the current registration: an
     * onUnregistered/onRegistrationFailed for a superseded registration must not wipe
     * the endpoint a newer registration just published.
     */
    suspend fun clearEndpoint(instance: String) {
        dataStore.edit { prefs ->
            if (prefs[PUSH_INSTANCE] == instance) prefs.remove(PUSH_ENDPOINT)
        }
    }

    // stampSessionIfNewlyActive rotates the session epoch on the inactive -> active
    // edge (the first layer coming on), so a poll from a previous session (see
    // [saveSnapshot]) can't persist its snapshot after an unlink + re-enable churned
    // the store. Enabling the second layer mid-session keeps the epoch, so an
    // in-flight poll's save survives. A random id rather than a counter because
    // clear() wipes the counter, so a monotone one would reset and collide across
    // the very unlink + re-enable this guards against.
    private fun stampSessionIfNewlyActive(
        prefs: MutablePreferences,
        wasActive: Boolean,
    ) {
        val nowActive = prefs[ENABLED] == true || prefs[PUSH_ENABLED] == true
        if (!wasActive && nowActive) prefs[EPOCH] = Random.nextLong()
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
     * saveSnapshot persists the baseline, but only if the backstop is still active
     * (either layer on) AND still the same session ([epoch]) that started the poll.
     * A poll can finish after an unlink cleared the store, or after an unlink +
     * re-enable started a fresh session; a bare active check would let that late
     * write repopulate a cleared store or poison the new session's baseline. The
     * epoch the caller captured before fetching, the active flags, and the write all
     * share one atomic edit, so a concurrent clear/re-enable is either seen (we skip)
     * or overwrites us.
     */
    suspend fun saveSnapshot(
        snapshot: FleetSnapshot,
        epoch: Long,
    ) {
        dataStore.edit { prefs ->
            val active = prefs[ENABLED] == true || prefs[PUSH_ENABLED] == true
            if (active && (prefs[EPOCH] ?: 0L) == epoch) {
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
        private val PUSH_ENABLED = booleanPreferencesKey("push_enabled")
        private val PUSH_ENDPOINT = stringPreferencesKey("push_endpoint")
        private val PUSH_INSTANCE = stringPreferencesKey("push_instance")
        private val EPOCH = longPreferencesKey("epoch")
        private val SNAPSHOT = stringPreferencesKey("fleet_snapshot")
    }
}
