package com.hugalafutro.bellhop.data

import androidx.datastore.preferences.core.mutablePreferencesOf
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class QuotaBadgeConfigStoreTest {
    // ── reconcileConfig (pure) ──────────────────────────────────────────

    @Test
    fun newMainBadgeDefaultsVisibleWidgetHidden() {
        val start = QuotaBadgeConfig(order = emptyList(), hidden = emptySet())

        val main = reconcileConfig(start, listOf("OR"), QuotaSurface.MAIN)
        assertEquals(listOf("OR"), main.order)
        assertFalse("OR" in main.hidden)

        val widget = reconcileConfig(start, listOf("OR"), QuotaSurface.WIDGET)
        assertEquals(listOf("OR"), widget.order)
        assertTrue("OR" in widget.hidden)
    }

    @Test
    fun vanishedNameRetainsSlotAndVisibility() {
        val prev = QuotaBadgeConfig(order = listOf("OR", "NG"), hidden = setOf("NG"))
        val after = reconcileConfig(prev, available = listOf("OR"), QuotaSurface.MAIN)
        // NG is no longer reported but keeps its order slot and hidden state.
        assertEquals(listOf("OR", "NG"), after.order)
        assertTrue("NG" in after.hidden)
    }

    @Test
    fun reconcileAppendsOnlyNewlySeenNames() {
        val prev = QuotaBadgeConfig(order = listOf("OR"), hidden = emptySet())
        val after = reconcileConfig(prev, available = listOf("OR", "NG"), QuotaSurface.MAIN)
        assertEquals(listOf("OR", "NG"), after.order)
        assertFalse("NG" in after.hidden)
    }

    @Test
    fun reconcileNoOpWhenNothingNew() {
        val prev = QuotaBadgeConfig(order = listOf("OR"), hidden = setOf("OR"))
        val after = reconcileConfig(prev, available = listOf("OR"), QuotaSurface.MAIN)
        assertEquals(prev, after)
    }

    // ── orderedVisible (pure) ────────────────────────────────────────────

    @Test
    fun orderedVisibleFiltersHiddenAndUnavailable() {
        val config = QuotaBadgeConfig(order = listOf("OR", "NG"), hidden = setOf("NG"))
        val available = listOf(quota("OR"), quota("NG"))
        val visible = orderedVisible(config, available)
        assertEquals(listOf("OR"), visible.map { it.providerName })
    }

    @Test
    fun orderedVisibleDropsNamesNotCurrentlyAvailable() {
        // NG has an order slot but isn't in the current fleet snapshot.
        val config = QuotaBadgeConfig(order = listOf("OR", "NG"), hidden = emptySet())
        val available = listOf(quota("OR"))
        val visible = orderedVisible(config, available)
        assertEquals(listOf("OR"), visible.map { it.providerName })
    }

    @Test
    fun orderedVisibleFollowsConfigOrder() {
        val config = QuotaBadgeConfig(order = listOf("NG", "OR"), hidden = emptySet())
        val available = listOf(quota("OR"), quota("NG"))
        val visible = orderedVisible(config, available)
        assertEquals(listOf("NG", "OR"), visible.map { it.providerName })
    }

    @Test
    fun orderedVisibleAppendsUnknownNamesInArrivalOrder() {
        val config = QuotaBadgeConfig(order = listOf("OR"), hidden = emptySet())
        val available = listOf(quota("OR"), quota("NG"), quota("DS"))
        val visible = orderedVisible(config, available)
        assertEquals(listOf("OR", "NG", "DS"), visible.map { it.providerName })
    }

    // ── QuotaBadgeConfigStore (DataStore round-trip) ─────────────────────

    private fun newStore(): QuotaBadgeConfigStore = QuotaBadgeConfigStore(InMemoryPreferencesDataStore())

    @Test
    fun emptyStoreReadsDefaultConfig() =
        runBlocking {
            assertEquals(QuotaBadgeConfig(), newStore().config(QuotaSurface.MAIN).first())
        }

    @Test
    fun reconcilePersistsIndependentlyPerSurface() =
        runBlocking {
            val store = newStore()
            store.reconcile(QuotaSurface.MAIN, listOf("OR"))
            store.reconcile(QuotaSurface.WIDGET, listOf("NG"))

            val main = store.config(QuotaSurface.MAIN).first()
            val widget = store.config(QuotaSurface.WIDGET).first()
            assertEquals(listOf("OR"), main.order)
            assertFalse("OR" in main.hidden)
            assertEquals(listOf("NG"), widget.order)
            assertTrue("NG" in widget.hidden)
        }

    @Test
    fun setOrderPersists() =
        runBlocking {
            val store = newStore()
            store.reconcile(QuotaSurface.MAIN, listOf("OR", "NG"))
            store.setOrder(QuotaSurface.MAIN, listOf("NG", "OR"))
            assertEquals(listOf("NG", "OR"), store.config(QuotaSurface.MAIN).first().order)
        }

    @Test
    fun setVisibleTogglesHidden() =
        runBlocking {
            val store = newStore()
            store.reconcile(QuotaSurface.MAIN, listOf("OR"))

            store.setVisible(QuotaSurface.MAIN, "OR", visible = false)
            assertTrue("OR" in store.config(QuotaSurface.MAIN).first().hidden)

            store.setVisible(QuotaSurface.MAIN, "OR", visible = true)
            assertFalse("OR" in store.config(QuotaSurface.MAIN).first().hidden)
        }

    @Test
    fun reconcileLeavesTheStoredRecordAloneWhenNothingIsNew() =
        runBlocking {
            // reconcile runs on every quota fetch, so the common case is a no-op.
            // The seeded record carries a field this build doesn't know: a
            // re-encode would silently drop it, so its survival is the proof
            // that nothing was rewritten.
            val key = stringPreferencesKey("quota_badges_main")
            val seeded = """{"order":["OR"],"hidden":[],"fieldFromALaterBuild":1}"""
            val data = InMemoryPreferencesDataStore(mutablePreferencesOf(key to seeded))

            QuotaBadgeConfigStore(data).reconcile(QuotaSurface.MAIN, listOf("OR"))

            assertEquals(seeded, data.data.first()[key])
        }

    @Test
    fun reconcileStillWritesWhenAProviderIsNew() =
        runBlocking {
            val key = stringPreferencesKey("quota_badges_main")
            val seeded = """{"order":["OR"],"hidden":[]}"""
            val data = InMemoryPreferencesDataStore(mutablePreferencesOf(key to seeded))
            val store = QuotaBadgeConfigStore(data)

            store.reconcile(QuotaSurface.MAIN, listOf("OR", "NG"))

            assertEquals(listOf("OR", "NG"), store.config(QuotaSurface.MAIN).first().order)
        }

    private fun quota(name: String): ProviderQuota =
        ProviderQuota(
            providerName = name,
            type = QuotaType.OPENROUTER,
            data = null,
            fetchedAt = "",
            available = true,
        )
}
