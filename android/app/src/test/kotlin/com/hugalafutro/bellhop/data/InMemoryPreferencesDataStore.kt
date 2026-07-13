package com.hugalafutro.bellhop.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * In-memory [DataStore] of [Preferences] for unit tests: no disk file and no
 * Dispatchers.IO hop, so a read replays the current value synchronously.
 *
 * The file-backed store from PreferenceDataStoreFactory put a real IO read on
 * Dispatchers.IO into the token path ([LinkStore.token]); the ViewModel tests
 * drive that path under Dispatchers.setMain(Unconfined) + runBlocking and wait
 * on the result with a wall-clock withTimeout, so under CI load that IO hop
 * could occasionally starve past the bound and fail an otherwise-correct test.
 * Backing reads with a StateFlow removes the timing entirely.
 */
class InMemoryPreferencesDataStore(
    initial: Preferences = emptyPreferences(),
) : DataStore<Preferences> {
    private val state = MutableStateFlow(initial)
    private val writeLock = Mutex()

    override val data: Flow<Preferences> = state

    override suspend fun updateData(transform: suspend (t: Preferences) -> Preferences): Preferences =
        // Serialize writers like the real store does, so a read-modify-write can't
        // interleave and lose an update.
        writeLock.withLock {
            val updated = transform(state.value).toPreferences()
            state.value = updated
            updated
        }
}
