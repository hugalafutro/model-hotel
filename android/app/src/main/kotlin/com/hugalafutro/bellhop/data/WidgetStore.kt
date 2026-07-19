package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
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

// Separate DataStore from the monitor/link/lock records: widget display state
// has its own trivial lifecycle (written by any fresh fetch, cleared on unlink)
// and must never share a file with the alert baseline it is forbidden to touch.
private val Context.widgetDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_widget")

/**
 * WidgetStore persists the home-screen widget's render model ([WidgetState]).
 * It is deliberately NOT [MonitorStore]: the fleet snapshot there is the alert
 * diff baseline, guarded by an active/epoch save-gate, and a display write
 * advancing it would swallow real down/up notifications. This store carries
 * display state only, so anything with a fresh fleet in hand may write it.
 */
class WidgetStore(
    private val dataStore: DataStore<Preferences>,
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    /** state emits the stored render model; null when never written or unparsable. */
    val state: Flow<WidgetState?> =
        dataStore.data.map { prefs -> prefs[STATE]?.let(::decode) }

    suspend fun read(): WidgetState? = dataStore.data.first()[STATE]?.let(::decode)

    /**
     * generation is the write fence a caller must capture BEFORE fetching the
     * data it intends to save (see [saveIfChanged]). [clear] stamps a fresh
     * one, so a fetch that started before an unlink cleared the store cannot
     * land afterwards and resurrect the old fleet. Same idea as
     * [MonitorStore]'s session epoch, kept store-local so display state stays
     * decoupled from the alert baseline.
     */
    suspend fun generation(): Long = dataStore.data.first()[GENERATION] ?: 0L

    /**
     * saveIfChanged persists [state] and reports whether it wrote, so callers
     * only trigger a Glance re-render on a real change. The write is dropped
     * when [generation] no longer matches the store (a clear happened since
     * the caller captured it — the fetched fleet belongs to a dead link). A
     * content-equal state (ignoring [WidgetState.updatedAt]) is skipped while
     * the stored stamp is fresher than [STAMP_ADVANCE_MS], which stops the
     * foreground dashboard's 15s refresh cadence from re-rendering the widget
     * on every tick, while a long-open app still refreshes the stamp every
     * few minutes so the "as of" line stays roughly honest. The generation
     * check, content check, and write share one atomic edit.
     */
    suspend fun saveIfChanged(
        state: WidgetState,
        generation: Long,
    ): Boolean {
        var written = false
        dataStore.edit { prefs ->
            if ((prefs[GENERATION] ?: 0L) != generation) return@edit
            val stored = prefs[STATE]?.let(::decode)
            if (stored != null &&
                stored.copy(updatedAt = 0L) == state.copy(updatedAt = 0L) &&
                state.updatedAt - stored.updatedAt < STAMP_ADVANCE_MS
            ) {
                return@edit
            }
            prefs[STATE] = json.encodeToString(state)
            written = true
        }
        return written
    }

    /**
     * clear wipes the render model and stamps a fresh [generation] in the same
     * atomic edit, fencing out any in-flight write whose generation was
     * captured before this clear. Random rather than a counter: a wipe-and-
     * reseed counter would restart at the same values and collide across the
     * exact unlink + re-pair this guards against.
     */
    suspend fun clear() {
        dataStore.edit { prefs ->
            prefs.clear()
            prefs[GENERATION] = Random.nextLong()
        }
    }

    private fun decode(stored: String): WidgetState? =
        runCatching { json.decodeFromString<WidgetState>(stored) }.getOrNull()

    companion object {
        fun create(context: Context): WidgetStore = WidgetStore(context.applicationContext.widgetDataStore)

        // How stale the stored stamp may get before a content-equal write is
        // accepted anyway, purely to keep the widget's "as of" line honest.
        const val STAMP_ADVANCE_MS = 300_000L

        private val STATE = stringPreferencesKey("widget_state")
        private val GENERATION = longPreferencesKey("widget_generation")
    }
}
