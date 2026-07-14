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

/**
 * TokenCipher is the seam between [LinkStore] and hardware-backed crypto:
 * production uses [KeystoreCrypto] (AndroidKeyStore), tests inject a plain fake
 * so persistence logic is exercised without a real keystore (Robolectric has no
 * AndroidKeyStore provider).
 */
interface TokenCipher {
    fun encrypt(plaintext: String): String

    fun decrypt(stored: String): String?
}

// Single process-wide DataStore for the persisted link. Named delegate must be
// top-level (one instance per name per process).
private val Context.linkDataStore: DataStore<Preferences> by
    preferencesDataStore(name = "bellhop_link")

/**
 * LinkStore persists the one Front Desk link: the device token (encrypted via
 * [TokenCipher]) plus non-secret metadata for display. It exposes the link as a
 * [Flow] the UI gate observes, and burns everything on [clear] (unlink).
 */
class LinkStore(
    private val dataStore: DataStore<Preferences>,
    private val cipher: TokenCipher = KeystoreCrypto,
) {
    /**
     * state emits [LinkState.Linked] when a token and its metadata are present,
     * [LinkState.Unlinked] otherwise. It never emits [LinkState.Loading]; that
     * is the collector's initial value while the first read is in flight.
     */
    val state: Flow<LinkState> =
        dataStore.data.map { prefs ->
            val token = prefs[TOKEN]
            val fdUrl = prefs[FD_URL]
            if (token.isNullOrEmpty() || fdUrl.isNullOrEmpty()) {
                LinkState.Unlinked
            } else {
                LinkState.Linked(
                    fdUrl = fdUrl,
                    fdName = prefs[FD_NAME].orEmpty(),
                    role = prefs[ROLE].orEmpty(),
                    deviceId = prefs[DEVICE_ID].orEmpty(),
                    label = prefs[LABEL].orEmpty(),
                    linkedAt = prefs[LINKED_AT] ?: 0L,
                )
            }
        }

    suspend fun save(
        fdUrl: String,
        fdName: String,
        token: String,
        device: PairedDevice,
        // Injectable so persistence tests get a deterministic stamp.
        now: Long = System.currentTimeMillis(),
    ) {
        val encrypted = cipher.encrypt(token)
        dataStore.edit { prefs ->
            prefs[TOKEN] = encrypted
            prefs[FD_URL] = fdUrl
            prefs[FD_NAME] = fdName
            prefs[ROLE] = device.role
            prefs[DEVICE_ID] = device.id
            prefs[LABEL] = device.label
            prefs[LINKED_AT] = now
        }
    }

    /** token decrypts and returns the stored device token, or null if unlinked. */
    suspend fun token(): String? {
        val stored = dataStore.data.first()[TOKEN] ?: return null
        return cipher.decrypt(stored)
    }

    suspend fun clear() {
        dataStore.edit { it.clear() }
    }

    companion object {
        fun create(context: Context): LinkStore = LinkStore(context.applicationContext.linkDataStore)

        private val TOKEN = stringPreferencesKey("token")
        private val FD_URL = stringPreferencesKey("fd_url")
        private val FD_NAME = stringPreferencesKey("fd_name")
        private val ROLE = stringPreferencesKey("role")
        private val DEVICE_ID = stringPreferencesKey("device_id")
        private val LABEL = stringPreferencesKey("label")
        private val LINKED_AT = longPreferencesKey("linked_at")
    }
}
