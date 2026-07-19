package com.hugalafutro.bellhop.data

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WidgetStoreTest {
    private fun newStore(): WidgetStore = WidgetStore(InMemoryPreferencesDataStore())

    private fun state(
        name: String = "hotel-1",
        health: String = "UP",
        at: Long = 1_000L,
    ): WidgetState = WidgetState(members = listOf(WidgetMember(name, health)), updatedAt = at)

    // save mimics a well-behaved caller: capture the generation, then write.
    // The stale-generation test below bypasses this on purpose.
    private suspend fun save(
        store: WidgetStore,
        state: WidgetState,
    ): Boolean = store.saveIfChanged(state, store.generation())

    @Test
    fun roundTripsState() =
        runBlocking {
            val store = newStore()
            assertTrue(save(store, state()))
            assertEquals(state(), store.read())
        }

    @Test
    fun emptyStoreReadsNull() = runBlocking { assertNull(newStore().read()) }

    @Test
    fun contentChangeAlwaysWrites() =
        runBlocking {
            val store = newStore()
            save(store, state(health = "UP", at = 1_000L))
            // Content changed one tick later: must write regardless of stamp age.
            assertTrue(save(store, state(health = "DOWN", at = 1_001L)))
            assertEquals("DOWN", store.read()?.members?.single()?.state)
        }

    @Test
    fun contentEqualFreshStampSkips() =
        runBlocking {
            val store = newStore()
            save(store, state(at = 1_000L))
            // Same content, stamp advanced less than STAMP_ADVANCE_MS: skip, so the
            // foreground 15s refresh cadence can't spam widget re-renders.
            assertFalse(save(store, state(at = 1_000L + WidgetStore.STAMP_ADVANCE_MS - 1)))
            assertEquals(1_000L, store.read()?.updatedAt)
        }

    @Test
    fun contentEqualOldStampRefreshesStamp() =
        runBlocking {
            val store = newStore()
            save(store, state(at = 1_000L))
            // Same content but the stored stamp has aged past the threshold: write,
            // so a long-open app still keeps the "as of" stamp roughly honest.
            assertTrue(save(store, state(at = 1_000L + WidgetStore.STAMP_ADVANCE_MS)))
            assertEquals(1_000L + WidgetStore.STAMP_ADVANCE_MS, store.read()?.updatedAt)
        }

    @Test
    fun clearWipesState() =
        runBlocking {
            val store = newStore()
            save(store, state())
            store.clear()
            assertNull(store.read())
        }

    @Test
    fun staleGenerationWriteAfterClearIsDropped() =
        runBlocking {
            val store = newStore()
            // A poll captures the generation, then an unlink clears the store
            // while its fetch is still in flight: the late write must be
            // dropped, or a re-pair would show the previous fleet.
            val generation = store.generation()
            store.clear()
            assertFalse(store.saveIfChanged(state(), generation))
            assertNull(store.read())
        }
}
