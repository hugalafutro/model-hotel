package com.hugalafutro.bellhop.data

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import java.util.Locale

// Isolated so the pure AppLocale checks stay fast: only wrap() needs a real
// Context (to reach a Configuration's locale), so only this one test pays for
// Robolectric.
@RunWith(RobolectricTestRunner::class)
class AppLocaleWrapTest {
    private lateinit var savedDefault: Locale

    @Before
    fun capture() {
        // wrap() mutates the process-global default locale; snapshot it so this test
        // can't leak a language into locale-sensitive tests that run after it.
        savedDefault = Locale.getDefault()
    }

    @After
    fun restore() {
        Locale.setDefault(savedDefault)
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
