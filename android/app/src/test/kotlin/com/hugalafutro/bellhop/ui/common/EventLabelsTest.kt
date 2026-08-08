package com.hugalafutro.bellhop.ui.common

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * withoutMemberName is what keeps a member-scoped line from saying the member's
 * name twice. These pin the two shapes Front Desk composes -- the name leading
 * the sentence and a " to <name>" sync target -- and, just as importantly, the
 * cases it must leave alone: another member's name, a name that is only part of
 * a longer word, and a message that is nothing but the name.
 */
class EventLabelsTest {
    @Test
    fun `a leading name and its separator are dropped`() {
        assertEquals("is healthy", withoutMemberName("MH docker-vm is healthy", "MH docker-vm"))
        assertEquals("now holds the primary's config", withoutMemberName("MH2: now holds the primary's config", "MH2"))
        assertEquals("sync failed", withoutMemberName("MH2, sync failed", "MH2"))
        assertEquals("sync failed", withoutMemberName("MH2 - sync failed", "MH2"))
    }

    @Test
    fun `the remainder keeps its own capitalisation`() {
        // The line reads as a continuation of the name in the header above it, so
        // a lowercase verb is the wanted output, not a re-capitalised sentence.
        assertEquals("is healthy", withoutMemberName("MH2 is healthy", "MH2"))
        assertEquals("Traefik is stale", withoutMemberName("MH2: Traefik is stale", "MH2"))
    }

    @Test
    fun `a sync target is removed from the middle of the message`() {
        assertEquals(
            "Held sync: its app version differs from the primary's",
            withoutMemberName("Held sync to MH2: its app version differs from the primary's", "MH2"),
        )
        assertEquals(
            "Resumed sync: its app version matches the primary's again",
            withoutMemberName("Resumed sync to MH2: its app version matches the primary's again", "MH2"),
        )
        assertEquals("Pushed config", withoutMemberName("Pushed config to MH2", "MH2"))
    }

    @Test
    fun `a name elsewhere in the sentence stays`() {
        // Only a leading name and a sync target are ours to strip: anywhere else
        // the name is carrying meaning the sentence needs.
        assertEquals("Primary moved from MH2", withoutMemberName("Primary moved from MH2", "MH2"))
        assertEquals("Compared MH2 against the primary", withoutMemberName("Compared MH2 against the primary", "MH2"))
    }

    @Test
    fun `a name that is only part of a longer word stays`() {
        assertEquals("MH20 is healthy", withoutMemberName("MH20 is healthy", "MH2"))
        assertEquals("Held sync to MH20: it lags", withoutMemberName("Held sync to MH20: it lags", "MH2"))
    }

    @Test
    fun `another member's name is left alone`() {
        assertEquals("MH2 is healthy", withoutMemberName("MH2 is healthy", "MH1"))
    }

    @Test
    fun `a message that is nothing but the name survives`() {
        // Stripping would leave an empty line, which says less than the name does.
        assertEquals("MH2", withoutMemberName("MH2", "MH2"))
        assertEquals("MH2:", withoutMemberName("MH2:", "MH2"))
    }

    @Test
    fun `a blank name or message changes nothing`() {
        assertEquals("MH2 is healthy", withoutMemberName("MH2 is healthy", ""))
        assertEquals("MH2 is healthy", withoutMemberName("MH2 is healthy", "   "))
        assertEquals("", withoutMemberName("", "MH2"))
    }
}
