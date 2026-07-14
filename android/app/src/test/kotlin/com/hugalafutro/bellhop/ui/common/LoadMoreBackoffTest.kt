package com.hugalafutro.bellhop.ui.common

import org.junit.Assert.assertEquals
import org.junit.Test

class LoadMoreBackoffTest {
    @Test
    fun `no failures means no delay`() {
        assertEquals(0L, loadMoreBackoffMillis(0))
        assertEquals(0L, loadMoreBackoffMillis(-1))
    }

    @Test
    fun `failures back off exponentially from one second`() {
        assertEquals(1_000L, loadMoreBackoffMillis(1))
        assertEquals(2_000L, loadMoreBackoffMillis(2))
        assertEquals(4_000L, loadMoreBackoffMillis(3))
        assertEquals(8_000L, loadMoreBackoffMillis(4))
        assertEquals(16_000L, loadMoreBackoffMillis(5))
    }

    @Test
    fun `backoff is capped at thirty seconds`() {
        assertEquals(30_000L, loadMoreBackoffMillis(6))
        assertEquals(30_000L, loadMoreBackoffMillis(50))
    }
}
