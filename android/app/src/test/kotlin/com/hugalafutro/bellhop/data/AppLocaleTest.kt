package com.hugalafutro.bellhop.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AppLocaleTest {
    @Test
    fun supportedMatchesFrontDeskSet() {
        // Bellhop offers the same limited set as Front Desk; en is the source locale.
        assertEquals(
            listOf("en", "cs", "de", "es", "fr", "ja", "nl", "pl", "ru", "sk", "zh"),
            AppLocale.SUPPORTED,
        )
    }

    @Test
    fun everySupportedLanguageHasAnEndonym() {
        // The picker labels each language in its own name, so a supported tag with no
        // endonym would render as a bare code.
        for (tag in AppLocale.SUPPORTED) {
            assertTrue("missing endonym for $tag", AppLocale.endonyms[tag]?.isNotBlank() == true)
        }
    }

    @Test
    fun systemSentinelIsEmpty() {
        assertEquals("", AppLocale.SYSTEM)
    }
}
