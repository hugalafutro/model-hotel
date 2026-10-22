package com.hugalafutro.bellhop.widget

import android.content.Context
import android.text.TextPaint
import android.util.TypedValue
import com.hugalafutro.bellhop.data.WIDGET_QUOTA_MAX_ROWS

/**
 * WidgetHeights prices WidgetContent's vertical stack in pixels, so the widget
 * can decide what to leave out before RemoteViews clips the footer off its
 * bottom edge. Glance cannot measure a layout, and a fixed dp threshold cannot
 * follow the device's font size: text grows with it while a dp figure stays
 * put, which is how a 3x2 widget at 130% font lost its footer.
 *
 * Every figure mirrors a modifier in WidgetContent; change one, change both.
 * Paddings truncate to whole pixels and fixed sizes round, as RemoteViews
 * applies them; with real font metrics in [linePx] the sum matched the rendered
 * views to the pixel on a 420dpi emulator. [linePx] is the height of one line
 * of text at the given sp, font scale included.
 */
internal class WidgetHeights(
    private val density: Float,
    private val linePx: (Float) -> Int,
) {
    private fun pad(dp: Int) = (dp * density).toInt()

    private fun size(dp: Int) = (dp * density + 0.5f).toInt()

    /**
     * quotaRowsThatFit is how many badge rows the strip may use: whatever the
     * member rows and footer leave, never more than [WIDGET_QUOTA_MAX_ROWS] and
     * never fewer than one, so a cramped widget still shows a badge and the
     * "+N" for the rest rather than losing the strip without a word.
     */
    fun quotaRowsThatFit(
        heightPx: Int,
        members: Int,
    ): Int = ((heightPx - fixed(members) - pad(4)) / quotaRow()).coerceIn(1, WIDGET_QUOTA_MAX_ROWS)

    /** eventFits reports whether the latest-event block fits under everything else. */
    fun eventFits(
        heightPx: Int,
        members: Int,
        quotaRows: Int,
    ): Boolean = fixed(members) + quotaStrip(quotaRows) + event() <= heightPx

    /** fixed is what always renders: root padding, header, member rows, footer. */
    private fun fixed(members: Int): Int = 2 * pad(ROOT_PADDING_DP) + header(members > 0) + members(members) + footer()

    // Icon 14dp, FD name 11sp, fleet pill 10sp in 2dp padding; 6dp below.
    private fun header(hasMembers: Boolean): Int {
        val pill = if (hasMembers) linePx(10f) + 2 * pad(2) else 0
        return maxOf(size(14), linePx(11f), pill) + pad(6)
    }

    // Each row is 13sp in 3dp padding plus a 1dp gap; a fleet past the row cap
    // is one 13sp counts line in 7dp padding; no members is one 13sp line.
    private fun members(count: Int): Int =
        when {
            count == 0 -> linePx(13f)
            count > BellhopWidget.MAX_MEMBER_ROWS -> linePx(13f) + 2 * pad(7)
            else -> count * (linePx(13f) + 2 * pad(3) + pad(1))
        }

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
            return WidgetHeights(metrics.density) { sp ->
                paint.textSize = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, sp, metrics)
                paint.fontMetricsInt.let { it.bottom - it.top }
            }
        }
    }
}
