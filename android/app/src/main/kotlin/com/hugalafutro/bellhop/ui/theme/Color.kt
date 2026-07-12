package com.hugalafutro.bellhop.ui.theme

import androidx.compose.ui.graphics.Color

/*
 * Bellhop brand palette. Single source of truth for every scheme; screens never
 * reference these directly, only MaterialTheme.colorScheme roles.
 *
 * Dark = "night lobby": deep ink-blue surfaces, brass/copper accents (ties into
 * Model Hotel's copper prod theme). Light = "day shift": warm paper, same brass.
 * A green-phosphor terminal theme slots in later (plan section 5.5).
 */

// Brass / copper accent ramp
val Brass300 = Color(0xFFE2A96B)
val Brass400 = Color(0xFFD08E45)
val Brass600 = Color(0xFFA5642A)
val Brass800 = Color(0xFF54331A)
val BrassContainerDark = Color(0xFF3E2A16)
val BrassContainerLight = Color(0xFFF3DEC4)

// Night lobby (dark) neutrals: ink-blue, never pure black
val Ink950 = Color(0xFF0C1118)
val Ink900 = Color(0xFF111823)
val Ink800 = Color(0xFF1A2331)
val Ink700 = Color(0xFF273344)
val Ink300 = Color(0xFF8B97A8)
val Ink100 = Color(0xFFDCE2EA)

// Day shift (light) neutrals: warm paper, never stark white
val Paper50 = Color(0xFFFAF6EF)
val Paper100 = Color(0xFFF2ECE0)
val Paper200 = Color(0xFFE5DCCB)
val PaperInk = Color(0xFF23201A)
val PaperInkMuted = Color(0xFF5C564A)

// Steel blue secondary (calm, recedes behind brass)
val Steel300 = Color(0xFF9BB2C8)
val Steel600 = Color(0xFF46617C)
val SteelContainerDark = Color(0xFF243546)
val SteelContainerLight = Color(0xFFDBE5EE)

// Health greens (tertiary: "all green" is the point of the app)
val Moss300 = Color(0xFF8FC49A)
val Moss600 = Color(0xFF3E7A4E)
val MossContainerDark = Color(0xFF1F3A28)
val MossContainerLight = Color(0xFFD9EBDC)

// Error reds (warm, matches the brass family)
val Ember300 = Color(0xFFE89A8A)
val Ember600 = Color(0xFFB3402B)
val EmberContainerDark = Color(0xFF44201A)
val EmberContainerLight = Color(0xFFF6DDD6)

// Semantic severity badges (event / alert levels). Deliberately conventional
// red/orange/blue/green rather than brass-tinted: a status level must read
// unambiguously at a glance, so these override the brand's warm palette for
// badges only. Saturated mid-tones with the paired text colour stay legible on
// both the ink (dark) and paper (light) surfaces, so one value serves both.
val SeverityErrorBg = Color(0xFFC62828) // red
val SeverityErrorFg = Color(0xFFFFFFFF)
val SeverityWarnBg = Color(0xFFE8820C) // orange
val SeverityWarnFg = Color(0xFF241100) // near-black: higher contrast on orange than white
val SeverityInfoBg = Color(0xFF1E6FD9) // blue
val SeverityInfoFg = Color(0xFFFFFFFF)
val SeveritySuccessBg = Color(0xFF2E7D32) // green
val SeveritySuccessFg = Color(0xFFFFFFFF)
