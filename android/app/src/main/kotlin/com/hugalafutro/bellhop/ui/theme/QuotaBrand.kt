package com.hugalafutro.bellhop.ui.theme

import androidx.compose.ui.graphics.Color
import com.hugalafutro.bellhop.data.QuotaType

/*
 * Provider brand colours for quota badges, so a strip of badges reads as
 * eight providers rather than eight identical pills. Values are lifted from
 * the Model Hotel dashboard's sidebar quota pills
 * (web/src/utils/providerBrands.ts + the .sidebar-quota-pill-* rules in
 * web/src/index.css), including the web's own light-mode overrides: the near
 * black brands (Z.ai, Kimi, Ollama Cloud) would vanish on the night scheme,
 * so they lighten to the same grey the web uses there. Deliberately outside
 * Color.kt: those are Bellhop's scheme roles, these are third-party brands
 * that must not drift toward the palette.
 */

/** QuotaBrand is one provider's badge colour in each scheme (day = paper, night = ink). */
data class QuotaBrand(
    val day: Color,
    val night: Color,
)

private val BrandNanoGpt = QuotaBrand(day = Color(0xFF0EA5B0), night = Color(0xFF0EA5B0))
private val BrandMiniMax = QuotaBrand(day = Color(0xFFF23F5B), night = Color(0xFFF23F5B))
private val BrandDeepSeek = QuotaBrand(day = Color(0xFF4D6BFE), night = Color(0xFF4D6BFE))
private val BrandOpenRouter = QuotaBrand(day = Color(0xFF6366F1), night = Color(0xFF6366F1))
private val BrandNeuralWatt = QuotaBrand(day = Color(0xFFAC4324), night = Color(0xFFAC4324))
private val BrandZaiCoding = QuotaBrand(day = Color(0xFF2D2D2D), night = Color(0xFFC8C8C8))
private val BrandKimiCode = QuotaBrand(day = Color(0xFF2D2D2D), night = Color(0xFFC8C8C8))
private val BrandOllamaCloud = QuotaBrand(day = Color(0xFF3D3D3D), night = Color(0xFFC8C8C8))

// UNKNOWN never reaches a badge (FrontDeskClient.quota drops unknown types
// before they render), but the map is total so a future type added upstream
// falls back to the scheme's own muted neutrals instead of failing to compile
// or picking someone else's brand.
private val BrandUnknown = QuotaBrand(day = PaperInkMuted, night = Ink300)

/** quotaBrand returns [type]'s badge colours; see the file note for provenance. */
fun quotaBrand(type: QuotaType): QuotaBrand =
    when (type) {
        QuotaType.NANOGPT -> BrandNanoGpt
        QuotaType.ZAI_CODING -> BrandZaiCoding
        QuotaType.KIMI_CODE -> BrandKimiCode
        QuotaType.MINIMAX -> BrandMiniMax
        QuotaType.DEEPSEEK -> BrandDeepSeek
        QuotaType.OPENROUTER -> BrandOpenRouter
        QuotaType.OLLAMA_CLOUD -> BrandOllamaCloud
        QuotaType.NEURALWATT -> BrandNeuralWatt
        QuotaType.UNKNOWN -> BrandUnknown
    }

/** quotaBrandColor picks [type]'s colour for the active scheme. */
fun quotaBrandColor(
    type: QuotaType,
    dark: Boolean,
): Color = quotaBrand(type).let { if (dark) it.night else it.day }
