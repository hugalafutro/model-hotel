package com.hugalafutro.bellhop.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

// Separate DataStore for lightweight, non-secret UI preferences so it survives
// unlink (they are device-local tastes, not tied to any one Front Desk link).
private val Context.prefsDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_prefs")

/**
 * PrefsStore persists device-local UI preferences that are neither link metadata
 * nor security policy. Today that is just the copy gesture: whether tapping a log
 * or member cell copies it (easy but easy to trigger by accident while scrolling)
 * or a long-press is required (the default, safer against stray copies).
 */
class PrefsStore(
    private val dataStore: DataStore<Preferences>,
) {
    /** holdToCopy emits whether copy requires a long-press; defaults on. */
    val holdToCopy: Flow<Boolean> = dataStore.data.map { it[HOLD_TO_COPY] ?: true }

    suspend fun setHoldToCopy(enabled: Boolean) {
        dataStore.edit { it[HOLD_TO_COPY] = enabled }
    }

    companion object {
        fun create(context: Context): PrefsStore = PrefsStore(context.applicationContext.prefsDataStore)

        private val HOLD_TO_COPY = booleanPreferencesKey("hold_to_copy")
    }
}
