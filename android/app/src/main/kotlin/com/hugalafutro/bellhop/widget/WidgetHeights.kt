package com.hugalafutro.bellhop.widget

import android.content.Context
import android.graphics.Typeface
import android.os.Build
import android.text.TextPaint
import android.util.TypedValue
import com.hugalafutro.bellhop.data.WIDGET_QUOTA_MAX_ROWS
import com.hugalafutro.bellhop.data.WidgetQuotaBadge
import kotlin.math.ceil

/**
 * WidgetHeights prices WidgetContent's vertical stack in pixels, so the widget
 * can decide what to leave out before RemoteViews clips the footer off its
 * bottom edge. Glance cannot measure a layout, and a fixed dp threshold cannot
 * follow the device's font size: text grows with it while a dp figure stays
 * put, which is how a 3x2 widget at 130% font lost its footer.
 *
 * Every figure mirrors a modifier in WidgetContent; change one, change both.
 * Every text it prices is a single line (maxLines = 1 in the layout). Glance
 * truncates paddings to whole pixels; fixed sizes round on API 31+, where the
 * framework applies them, and truncate below it, where Glance converts them
 * itself ([roundsSizes]). With real font metrics in [linePx] the sum matched
 * the rendered views to the pixel on a 420dpi emulator. [linePx] is the height
 * of one line of text at the given sp, font scale included.
 *
 * What gives way, in order: the event block, then quota badge rows (down to
 * none), then the member rows, which collapse to the one-line counts row. The
 * header and footer always render.
 */
internal class WidgetHeights(
    private val density: Float,
    private val roundsSizes: Boolean,
    private val linePx: (Float) -> Int,
) {
    private fun pad(dp: Int) = (dp * density).toInt()

    private fun size(dp: Int) = if (roundsSizes) (dp * density + 0.5f).toInt() else (dp * density).toInt()

    /**
     * membersCollapse reports whether the fleet renders as the one-line counts
     * row: past [BellhopWidget.MAX_MEMBER_ROWS], or when its rows would not fit
     * between the header and footer at this height and font scale.
     */
    fun membersCollapse(
        heightPx: Int,
        members: Int,
    ): Boolean = members > BellhopWidget.MAX_MEMBER_ROWS || chrome(members) + memberRows(members) > heightPx

    /**
     * quotaRowsThatFit is how many badge rows the strip may use: whatever the
     * members and footer leave, at most [WIDGET_QUOTA_MAX_ROWS]. Zero hides the
     * strip; the footer outranks it.
     */
    fun quotaRowsThatFit(
        heightPx: Int,
        members: Int,
    ): Int = ((heightPx - fixed(heightPx, members) - pad(4)) / quotaRow()).coerceIn(0, WIDGET_QUOTA_MAX_ROWS)

    /** eventFits reports whether the latest-event block fits under everything else. */
    fun eventFits(
        heightPx: Int,
        members: Int,
        quotaRows: Int,
    ): Boolean = fixed(heightPx, members) + quotaStrip(quotaRows) + event() <= heightPx

    /** fixed is what always renders: the chrome plus the members in their chosen form. */
    private fun fixed(
        heightPx: Int,
        members: Int,
    ): Int = chrome(members) + if (membersCollapse(heightPx, members)) countsRow() else memberRows(members)

    /** chrome is root padding, header and footer. */
    private fun chrome(members: Int): Int = 2 * pad(ROOT_PADDING_DP) + header(members > 0) + footer()

    // Icon 14dp, FD name 11sp, fleet pill 10sp in 2dp padding; 6dp below.
    private fun header(hasMembers: Boolean): Int {
        val pill = if (hasMembers) linePx(10f) + 2 * pad(2) else 0
        return maxOf(size(14), linePx(11f), pill) + pad(6)
    }

    // Each row is 13sp in 3dp padding plus a 1dp gap; no members is one 13sp
    // line.
    private fun memberRows(count: Int): Int =
        if (count == 0) linePx(13f) else count * (linePx(13f) + 2 * pad(3) + pad(1))

    // The counts row: one 13sp line in 7dp padding.
    private fun countsRow(): Int = linePx(13f) + 2 * pad(7)

    // A 9sp pill in 2dp padding plus a 1dp gap; 4dp above the strip.
    private fun quotaRow(): Int = linePx(9f) + 2 * pad(2) + pad(1)

    private fun quotaStrip(rows: Int): Int = if (rows == 0) 0 else pad(4) + rows * quotaRow()

    // 9sp caption, then an 11sp line in 2dp padding, then 2dp below.
    private fun event(): Int = linePx(9f) + linePx(11f) + 2 * pad(2) + pad(2)

    // 1dp rule, 3dp gap, then 9sp text beside a 14dp icon.
    private fun footer(): Int = size(1) + pad(3) + maxOf(linePx(9f), size(14))

    companion object {
        const val ROOT_PADDING_DP = 12

        /** of measures lines with the device's own font and text-size setting. */
        fun of(context: Context): WidgetHeights {
            val metrics = context.resources.displayMetrics
            val paint = TextPaint()
            return WidgetHeights(metrics.density, Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) { sp ->
                paint.textSize = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, sp, metrics)
                paint.fontMetricsInt.let { it.bottom - it.top }
            }
        }
    }
}

// The badge pill's own text: 9sp Medium in the platform's sans, matching the
// badge Text in WidgetContent. Named here because the packer needs the same
// numbers to know how wide a label comes out.
private const val BADGE_TEXT_SP = 9f
private val BADGE_TEXT_TYPEFACE: Typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)

// The pill's chrome around its label: 4dp of padding each side plus the 1dp gap
// to the next badge. Same figure WIDGET_QUOTA_PILL_CHROME_DP estimates with.
private const val BADGE_CHROME_DP = 9

// One dp of slack per badge on top of the measured width. Rounding up is not
// quite enough on its own: the measurement is of the same string in the same
// font at the same size, but it is not the RemoteViews layout pass, and these
// pills cannot shrink to fit -- a dp too generous costs nothing visible, a dp
// too tight clips a label.
private const val BADGE_MEASURE_SLACK_DP = 1

/**
 * badgeMeasurer returns the width function [quotaBadgeRows] packs with: the
 * label's real rendered width rather than a per-character guess. Glance can't
 * measure text, but this code runs in the app's own process, so a [TextPaint]
 * set up like the pill's Text can -- through [TypedValue], so the device's own
 * text-size setting is already in the answer.
 *
 * This is what lets the strip fill its rows: the estimate it replaces ran some
 * 30% wide on a typical label ("KIMI 0%/0%" measures 53dp against a 69dp guess),
 * which cost most rows a badge or two they had room for. The estimate stays as
 * [badgeWidthDp] for callers with no Context.
 */
internal fun badgeMeasurer(context: Context): (WidgetQuotaBadge) -> Int {
    val metrics = context.resources.displayMetrics
    val paint =
        TextPaint().apply {
            typeface = BADGE_TEXT_TYPEFACE
            textSize = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, BADGE_TEXT_SP, metrics)
        }
    return { badge ->
        val labelDp = ceil(paint.measureText(badge.label) / metrics.density).toInt()
        labelDp + BADGE_CHROME_DP + BADGE_MEASURE_SLACK_DP
    }
}
