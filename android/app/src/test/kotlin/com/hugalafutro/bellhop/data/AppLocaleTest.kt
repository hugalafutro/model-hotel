package com.hugalafutro.bellhop.data

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import java.util.Locale

@RunWith(RobolectricTestRunner::class)
class AppLocaleTest {
    private lateinit var savedDefault: Locale

    @Before
    fun capture() {
        // wrap() mutates the process-global default locale; snapshot it so this test
        // can't leak German into locale-sensitive tests that run after it.
        savedDefault = Locale.getDefault()
    }

    @After
    fun restore() {
        Locale.setDefault(savedDefault)
    }

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

    @Test
    fun revertingToSystemRestoresTheDefaultLocale() {
        val context = RuntimeEnvironment.getApplication()
        val system = context.resources.configuration.locales[0]

        // Choosing a language applies it to the JVM default (the date formatters read it).
        AppLocale.store(context, "de")
        AppLocale.wrap(context)
        assertEquals(Locale.GERMAN.language, Locale.getDefault().language)

        // Reverting to system default must un-freeze the default, not leave it German.
        AppLocale.store(context, AppLocale.SYSTEM)
        AppLocale.wrap(context)
        assertEquals(system.language, Locale.getDefault().language)
    }
}
