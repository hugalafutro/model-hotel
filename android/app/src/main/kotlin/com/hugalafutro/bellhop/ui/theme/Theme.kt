package com.hugalafutro.bellhop.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable

// Deliberately no Material You dynamic color: Bellhop commits to its own brand
// palette (plan section 5.5). Named themes (green phosphor) arrive with the
// Settings screen; until then the app follows the system light/dark setting.

private val NightLobbyColors =
    darkColorScheme(
        primary = Brass400,
        onPrimary = Ink950,
        primaryContainer = BrassContainerDark,
        onPrimaryContainer = Brass300,
        secondary = Steel300,
        onSecondary = Ink950,
        secondaryContainer = SteelContainerDark,
        onSecondaryContainer = Steel300,
        tertiary = Moss300,
        onTertiary = Ink950,
        tertiaryContainer = MossContainerDark,
        onTertiaryContainer = Moss300,
        error = Ember300,
        onError = Ink950,
        errorContainer = EmberContainerDark,
        onErrorContainer = Ember300,
        background = Ink950,
        onBackground = Ink100,
        surface = Ink900,
        onSurface = Ink100,
        surfaceVariant = Ink800,
        onSurfaceVariant = Ink300,
        outline = Ink700,
        outlineVariant = Ink800,
        surfaceContainerLowest = Ink950,
        surfaceContainerLow = Ink900,
        surfaceContainer = Ink800,
        surfaceContainerHigh = Ink700,
    )

private val DayShiftColors =
    lightColorScheme(
        primary = Brass600,
        onPrimary = Paper50,
        primaryContainer = BrassContainerLight,
        onPrimaryContainer = Brass800,
        secondary = Steel600,
        onSecondary = Paper50,
        secondaryContainer = SteelContainerLight,
        onSecondaryContainer = Steel600,
        tertiary = Moss600,
        onTertiary = Paper50,
        tertiaryContainer = MossContainerLight,
        onTertiaryContainer = Moss600,
        error = Ember600,
        onError = Paper50,
        errorContainer = EmberContainerLight,
        onErrorContainer = Ember600,
        background = Paper50,
        onBackground = PaperInk,
        surface = Paper50,
        onSurface = PaperInk,
        surfaceVariant = Paper100,
        onSurfaceVariant = PaperInkMuted,
        outline = Paper200,
        outlineVariant = Paper100,
        surfaceContainerLowest = Paper50,
        surfaceContainerLow = Paper100,
        surfaceContainer = Paper100,
        surfaceContainerHigh = Paper200,
    )

@Composable
fun BellhopTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) NightLobbyColors else DayShiftColors,
        typography = BellhopTypography,
        content = content,
    )
}
