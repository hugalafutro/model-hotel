package com.hugalafutro.bellhop.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import com.hugalafutro.bellhop.R

/**
 * Bellhop type system (fonts bundled in res/font, licenses in android/licenses/):
 * - Zilla Slab for display and headlines (slab serif, brass-signage feel)
 * - IBM Plex Sans (variable) for everything readable
 * - IBM Plex Mono for metrics, versions, and identifiers (used via [MonoFamily])
 */

val ZillaSlabFamily =
    FontFamily(
        Font(R.font.zillaslab_medium, FontWeight.Medium),
        Font(R.font.zillaslab_semibold, FontWeight.SemiBold),
    )

@OptIn(ExperimentalTextApi::class)
private fun plexSans(weight: FontWeight) =
    Font(
        R.font.ibm_plex_sans,
        weight = weight,
        variationSettings = FontVariation.Settings(FontVariation.weight(weight.weight)),
    )

val PlexSansFamily =
    FontFamily(
        plexSans(FontWeight.Normal),
        plexSans(FontWeight.Medium),
        plexSans(FontWeight.SemiBold),
    )

val MonoFamily = FontFamily(Font(R.font.ibm_plex_mono))

val BellhopTypography =
    Typography(
        displayLarge =
            TextStyle(
                fontFamily = ZillaSlabFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 48.sp,
                lineHeight = 54.sp,
            ),
        displayMedium =
            TextStyle(
                fontFamily = ZillaSlabFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 38.sp,
                lineHeight = 44.sp,
            ),
        displaySmall =
            TextStyle(
                fontFamily = ZillaSlabFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 32.sp,
                lineHeight = 38.sp,
            ),
        headlineLarge =
            TextStyle(
                fontFamily = ZillaSlabFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 28.sp,
                lineHeight = 34.sp,
            ),
        headlineMedium =
            TextStyle(
                fontFamily = ZillaSlabFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 24.sp,
                lineHeight = 30.sp,
            ),
        headlineSmall =
            TextStyle(
                fontFamily = ZillaSlabFamily,
                fontWeight = FontWeight.Medium,
                fontSize = 21.sp,
                lineHeight = 26.sp,
            ),
        titleLarge =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 19.sp,
                lineHeight = 25.sp,
            ),
        titleMedium =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 16.sp,
                lineHeight = 22.sp,
                letterSpacing = 0.1.sp,
            ),
        titleSmall =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.Medium,
                fontSize = 14.sp,
                lineHeight = 20.sp,
                letterSpacing = 0.1.sp,
            ),
        bodyLarge =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.Normal,
                fontSize = 16.sp,
                lineHeight = 24.sp,
                letterSpacing = 0.3.sp,
            ),
        bodyMedium =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.Normal,
                fontSize = 14.sp,
                lineHeight = 20.sp,
                letterSpacing = 0.2.sp,
            ),
        bodySmall =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.Normal,
                fontSize = 12.sp,
                lineHeight = 16.sp,
                letterSpacing = 0.2.sp,
            ),
        labelLarge =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.SemiBold,
                fontSize = 14.sp,
                lineHeight = 20.sp,
                letterSpacing = 0.4.sp,
            ),
        labelMedium =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.Medium,
                fontSize = 12.sp,
                lineHeight = 16.sp,
                letterSpacing = 0.5.sp,
            ),
        labelSmall =
            TextStyle(
                fontFamily = PlexSansFamily,
                fontWeight = FontWeight.Medium,
                fontSize = 11.sp,
                lineHeight = 16.sp,
                letterSpacing = 0.5.sp,
            ),
    )
