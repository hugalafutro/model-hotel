package com.hugalafutro.bellhop.data

import android.content.Context
import android.content.res.Configuration
import androidx.core.content.edit
import java.util.Locale

/**
 * AppLocale persists the in-app language override and applies it to a Context.
 * Bellhop is Compose on a plain FragmentActivity (no AppCompat), so per-app
 * language uses a base-context wrapper ([wrap], from attachBaseContext) plus an
 * activity recreate on change, rather than AppCompatDelegate. The tag lives in a
 * small synchronous SharedPreferences so attachBaseContext can read it before the
 * UI inflates; an empty tag means "follow the system locale".
 */
object AppLocale {
    private const val PREFS = "bellhop_locale"
    private const val KEY_TAG = "app_locale_tag"

    // SYSTEM is the sentinel for "follow the system locale" (no override).
    const val SYSTEM = ""

    // SUPPORTED is the offered language set, matching Front Desk's. The picker
    // prepends the system-default option; the order here is the picker order.
    val SUPPORTED = listOf("en", "cs", "de", "es", "fr", "ja", "nl", "pl", "ru", "sk", "zh")

    // endonyms names each language in its own language, so the picker reads the
    // same whatever the current locale. Not string resources by design: a language
    // name is never localized (French stays "Français" in a Japanese UI).
    val endonyms =
        mapOf(
            "en" to "English",
            "cs" to "Čeština",
            "de" to "Deutsch",
            "es" to "Español",
            "fr" to "Français",
            "ja" to "日本語",
            "nl" to "Nederlands",
            "pl" to "Polski",
            "ru" to "Русский",
            "sk" to "Slovenčina",
            "zh" to "中文",
        )

    /** stored returns the persisted language tag, or [SYSTEM] when none is set. */
    fun stored(context: Context): String =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY_TAG, SYSTEM) ?: SYSTEM

    /** store persists the chosen tag; apply() updates the in-memory value at once,
     * so the recreate that follows reads it back in attachBaseContext. */
    fun store(
        context: Context,
        tag: String,
    ) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit { putString(KEY_TAG, tag) }
    }

    /**
     * wrap returns a Context configured for the stored language, or [base]
     * unchanged when following the system. Called from Activity.attachBaseContext,
     * so the whole resource lookup (strings and, on recreate, everything) resolves
     * in the chosen language.
     */
    fun wrap(base: Context): Context {
        val tag = stored(base)
        if (tag == SYSTEM) return base
        val locale = Locale.forLanguageTag(tag)
        Locale.setDefault(locale)
        val config = Configuration(base.resources.configuration)
        config.setLocale(locale)
        return base.createConfigurationContext(config)
    }
}
