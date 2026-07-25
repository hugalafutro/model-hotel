package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.Serializable
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/** QuotaSurface identifies which UI surface a badge order/visibility config belongs to. */
enum class QuotaSurface { MAIN, WIDGET }

/**
 * QuotaBadgeConfig is the persisted per-surface badge layout: [order] is the
 * user's chosen provider ordering and [hidden] is the set of names toggled
 * off. Both are keyed by provider **name** -- the export's own identity on
 * the wire -- never by type or UUID (see global-constraints badge-identity
 * note).
 */
@Serializable
data class QuotaBadgeConfig(
    val order: List<String> = emptyList(),
    val hidden: Set<String> = emptySet(),
)

/**
 * reconcileConfig folds a fresh [available] list of provider names into
 * [prev]: names not yet in [QuotaBadgeConfig.order] are appended -- visible
 * by default on [QuotaSurface.MAIN], hidden by default on
 * [QuotaSurface.WIDGET] (the home-screen widget is opt-in per badge, MAIN is
 * opt-out). Names already known but no longer in [available] are retained
 * verbatim -- a vanished provider keeps its order slot and hidden state so
 * it reappears in the same place if Front Desk starts reporting it again.
 * Nothing is ever dropped from [QuotaBadgeConfig.order] or [hidden] here.
 */
fun reconcileConfig(
    prev: QuotaBadgeConfig,
    available: List<String>,
    surface: QuotaSurface,
): QuotaBadgeConfig {
    val known = prev.order.toSet()
    val newlySeen = available.filter { it !in known }
    if (newlySeen.isEmpty()) return prev
    val hidden =
        when (surface) {
            QuotaSurface.MAIN -> prev.hidden
            QuotaSurface.WIDGET -> prev.hidden + newlySeen
        }
    return prev.copy(order = prev.order + newlySeen, hidden = hidden)
}

/**
 * orderedVisible resolves [config] against the currently [available] quotas
 * for rendering: hidden names and names not currently available are
 * dropped, the remainder follows [QuotaBadgeConfig.order], and any available
 * name not yet in the order (a provider [reconcileConfig] hasn't folded in
 * yet) is appended in [available]'s arrival order rather than silently
 * dropped.
 */
fun orderedVisible(
    config: QuotaBadgeConfig,
    available: List<ProviderQuota>,
): List<ProviderQuota> {
    val orderIndex = config.order.withIndex().associate { (i, name) -> name to i }
    return available
        .filter { it.providerName !in config.hidden }
        .sortedBy { orderIndex[it.providerName] ?: Int.MAX_VALUE }
}

// Separate DataStore file from the fleet/lock/widget records: this is a
// rendering preference with its own lifecycle, grown as new providers are
// discovered and never cleared on unlink.
private val Context.quotaBadgeDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_quota_badges")

/**
 * QuotaBadgeConfigStore persists a [QuotaBadgeConfig] per [QuotaSurface],
 * one preference key each so MAIN and WIDGET layouts evolve independently.
 */
class QuotaBadgeConfigStore(
    private val dataStore: DataStore<Preferences>,
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    /** config emits the persisted layout for [surface], defaulting to empty. */
    fun config(surface: QuotaSurface): Flow<QuotaBadgeConfig> =
        dataStore.data.map { prefs -> prefs[keyFor(surface)]?.let(::decode) ?: QuotaBadgeConfig() }

    /** reconcile folds [available] provider names into [surface]'s stored config via [reconcileConfig]. */
    suspend fun reconcile(
        surface: QuotaSurface,
        available: List<String>,
    ) {
        edit(surface) { prev -> reconcileConfig(prev, available, surface) }
    }

    /** setOrder overwrites [surface]'s badge ordering, leaving [QuotaBadgeConfig.hidden] untouched. */
    suspend fun setOrder(
        surface: QuotaSurface,
        order: List<String>,
    ) {
        edit(surface) { prev -> prev.copy(order = order) }
    }

    /** setVisible toggles one provider name's hidden state for [surface]. */
    suspend fun setVisible(
        surface: QuotaSurface,
        name: String,
        visible: Boolean,
    ) {
        edit(surface) { prev ->
            prev.copy(hidden = if (visible) prev.hidden - name else prev.hidden + name)
        }
    }

    private suspend fun edit(
        surface: QuotaSurface,
        transform: (QuotaBadgeConfig) -> QuotaBadgeConfig,
    ) {
        val key = keyFor(surface)
        dataStore.edit { prefs ->
            val stored = prefs[key]?.let(::decode)
            val next = transform(stored ?: QuotaBadgeConfig())
            // reconcile runs on every successful quota fetch (foreground poll
            // and background worker) and yields the same config whenever no new
            // provider appeared, so leave the stored value alone rather than
            // re-encoding it each time. A missing or undecodable value is still
            // written, so a corrupt record heals on the next edit.
            if (stored != null && next == stored) return@edit
            prefs[key] = json.encodeToString(next)
        }
    }

    private fun decode(stored: String): QuotaBadgeConfig? =
        runCatching { json.decodeFromString<QuotaBadgeConfig>(stored) }.getOrNull()

    private fun keyFor(surface: QuotaSurface): Preferences.Key<String> =
        when (surface) {
            QuotaSurface.MAIN -> MAIN_KEY
            QuotaSurface.WIDGET -> WIDGET_KEY
        }

    companion object {
        fun create(context: Context): QuotaBadgeConfigStore =
            QuotaBadgeConfigStore(context.applicationContext.quotaBadgeDataStore)

        private val MAIN_KEY = stringPreferencesKey("quota_badges_main")
        private val WIDGET_KEY = stringPreferencesKey("quota_badges_widget")
    }
}
