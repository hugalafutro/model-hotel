package com.hugalafutro.bellhop.ui.common

import org.junit.Assert.assertEquals
import org.junit.Test

class ReorderableListTest {
    @Test
    fun `moves item down`() {
        assertEquals(listOf("B", "C", "A"), moveItem(listOf("A", "B", "C"), from = 0, to = 2))
    }

    @Test
    fun `moves item up`() {
        assertEquals(listOf("C", "A", "B"), moveItem(listOf("A", "B", "C"), from = 2, to = 0))
    }

    @Test
    fun `same index is a no-op`() {
        assertEquals(listOf("A", "B", "C"), moveItem(listOf("A", "B", "C"), from = 1, to = 1))
    }

    @Test
    fun `out-of-range from is a no-op`() {
        val list = listOf("A", "B", "C")
        assertEquals(list, moveItem(list, from = -1, to = 1))
        assertEquals(list, moveItem(list, from = 3, to = 1))
    }

    @Test
    fun `out-of-range to is clamped`() {
        assertEquals(listOf("B", "C", "A"), moveItem(listOf("A", "B", "C"), from = 0, to = 99))
        assertEquals(listOf("C", "A", "B"), moveItem(listOf("A", "B", "C"), from = 2, to = -99))
    }

    @Test
    fun `empty list is a no-op`() {
        val empty: List<String> = emptyList()
        assertEquals(empty, moveItem(empty, from = 0, to = 1))
    }
}
