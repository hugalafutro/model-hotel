package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The rule here has to match frontdesk/web/src/utils/build.ts and the verdict
 * half of internal/frontdesk/versionskew.go: a member Front Desk shows as one
 * build and Bellhop shows as another is worse than either showing nothing.
 */
class BuildLabelTest {
    @Test
    fun `only an exact semver tag is a release`() {
        assertFalse(isDevVersion("v1.2.3"))
        assertFalse(isDevVersion("1.2.3"))
        assertTrue(isDevVersion("dev"))
        assertTrue(isDevVersion("v1.2.3-15-gabc123"))
        assertTrue(isDevVersion("v1.2.3-dirty"))
    }

    @Test
    fun `sentinels name no build`() {
        assertTrue(stampedCommit("b80c04d4494f"))
        assertFalse(stampedCommit(""))
        assertFalse(stampedCommit("unknown"))
    }

    @Test
    fun `card shows the commit for a dev build`() {
        assertEquals("b80c04d4494f", buildLabel("dev", "b80c04d4494f"))
    }

    @Test
    fun `card keeps a release tag, which identifies itself`() {
        assertEquals("v1.2.3", buildLabel("v1.2.3", "b80c04d4494f"))
    }

    @Test
    fun `card falls back to the version when no commit names the build`() {
        assertEquals("dev", buildLabel("dev", ""))
        assertEquals("dev", buildLabel("dev", "unknown"))
    }

    @Test
    fun `detail screen carries both halves`() {
        assertEquals("dev · b80c04d4494f", buildDetail("dev", "b80c04d4494f"))
        assertEquals("v1.2.3 · b80c04d4494f", buildDetail("v1.2.3", "b80c04d4494f"))
    }

    @Test
    fun `detail screen shows what it has`() {
        assertEquals("dev", buildDetail("dev", "unknown"))
        assertEquals("", buildDetail("", "b80c04d4494f"))
    }
}
