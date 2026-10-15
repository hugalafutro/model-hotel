package com.hugalafutro.bellhop.data

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The circuits ledger decodes as Front Desk's /api/members sends it. */
class MemberCircuitsTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun decodesLedgerWithPinSource() {
        val member =
            json.decodeFromString<FleetMember>(
                """{"id":"m1","name":"a","status":{"health":{"known":true,"healthy":true},
                "circuits":{"checked_at":"2026-10-15T17:39:47Z","total":3,"open":[
                {"provider_id":"p","provider":"OpenCode Go","model":"(account quota)","state":"open",
                "cause":"quota pin seeded (advisor)","next_retry_at":"2026-10-16T17:07:09Z",
                "quota_pinned":true,"pin_source":"reserve"},
                {"provider_id":"abcdefgh-1234","model":"k3","state":"half-open"}]}}}""",
            )
        val ledger = member.status.circuits!!
        assertEquals(3, ledger.total)
        assertEquals(2, ledger.open.size)
        val pinned = ledger.open[0]
        assertTrue(pinned.quotaPinned)
        assertEquals("reserve", pinned.pinSource)
        assertEquals("OpenCode Go", pinned.providerLabel)
        // A row without a display name falls back to the id's head.
        assertEquals("abcdefgh", ledger.open[1].providerLabel)
    }

    @Test
    fun missingLedgerDecodesAsNull() {
        val member = json.decodeFromString<FleetMember>("""{"id":"m1","status":{"health":{"known":false}}}""")
        assertNull(member.status.circuits)
    }
}
