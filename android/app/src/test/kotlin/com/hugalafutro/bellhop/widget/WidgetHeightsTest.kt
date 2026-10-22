package com.hugalafutro.bellhop.widget

import com.hugalafutro.bellhop.data.WIDGET_QUOTA_MAX_ROWS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The line heights and the 610px widget below are what uiautomator measured on
 * the 420dpi emulator (density 2.625) with a 3x2 widget, four members and a
 * three-badge strip. At 100% font that widget had 4px to spare with the event
 * block and two badge rows; at 130% its footer was clipped before this fix.
 */
class WidgetHeightsTest {
    private val density = 2.625f
    private val widgetPx = 610

    private val defaultLines = mapOf(9f to 32, 10f to 36, 11f to 39, 13f to 47)

    private fun heights(
        lines: Map<Float, Int>,
        roundsSizes: Boolean = true,
    ) = WidgetHeights(density, roundsSizes) { sp -> lines.getValue(sp) }

    private val atDefaultFont = heights(defaultLines)
    private val atLargeFont = heights(mapOf(9f to 42, 10f to 47, 11f to 51, 13f to 61))

    @Test
    fun defaultFontKeepsTheEventBlockWithTwoBadgeRows() {
        // Fixed 422px (padding 62, header 61, rows 4x63, footer 47) leaves room
        // for the full row cap, and two 44px rows plus the 86px event block
        // land 4px short of the edge, as rendered.
        assertEquals(WIDGET_QUOTA_MAX_ROWS, atDefaultFont.quotaRowsThatFit(widgetPx, 4))
        assertTrue(atDefaultFont.eventFits(widgetPx, 4, 2))
        assertFalse(atDefaultFont.eventFits(widgetPx, 4, 3))
    }

    @Test
    fun largeFontDropsTheEventBlockAndTrimsTheStripToKeepTheFooter() {
        // Fixed 494px leaves 116: one 54px row fits under the 10px gap, a
        // second would overrun by 2px, and the event block cannot fit at all.
        assertEquals(1, atLargeFont.quotaRowsThatFit(widgetPx, 4))
        assertFalse(atLargeFont.eventFits(widgetPx, 4, 1))
    }

    @Test
    fun aWidgetTooShortForOneBadgeRowHidesTheStripRatherThanClipTheFooter() {
        // 540px leaves 46 under the fixed 494, short of the 10px gap plus one
        // 54px row.
        assertEquals(0, atLargeFont.quotaRowsThatFit(540, 4))
        assertFalse(atLargeFont.eventFits(540, 4, 0))
    }

    @Test
    fun memberRowsCollapseToTheCountsLineWhenTheyDoNotFit() {
        // Five 77px rows under the 186px of padding, header and footer need
        // 571px; a pixel less and the fleet reads as one counts line, which
        // frees room for a badge row again.
        assertEquals(MemberForm.ROWS, atLargeFont.memberForm(571, 5))
        assertEquals(MemberForm.ONE_LINE, atLargeFont.memberForm(570, 5))
        assertEquals(4, atLargeFont.quotaRowsThatFit(570, 5))
    }

    @Test
    fun theMinimumWidgetAtTheLargestFontHidesTheMembersToKeepTheFooter() {
        // 110dp (289px) at roughly twice the default line heights: padding,
        // header and footer take 232px, so not even the 130px counts line fits
        // and the members give way, then the strip; the header's up/total
        // pill still reads the fleet. One pixel past chrome plus the counts
        // line and it returns.
        val atLargestFont = heights(mapOf(9f to 64, 10f to 71, 11f to 78, 13f to 94))
        assertEquals(MemberForm.HIDDEN, atLargestFont.memberForm(289, 4))
        assertEquals(0, atLargestFont.quotaRowsThatFit(289, 4))
        assertFalse(atLargestFont.eventFits(289, 4, 0))
        assertEquals(MemberForm.ONE_LINE, atLargestFont.memberForm(362, 4))
        assertEquals(MemberForm.HIDDEN, atLargestFont.memberForm(361, 4))
    }

    @Test
    fun anEmptyFleetIsPricedAsItsBareNoMembersLine() {
        // A 47px line, no padding and no header pill (header 39 + 15): 62 +
        // 54 + 47 + footer 47 = 210px.
        assertEquals(MemberForm.ONE_LINE, atDefaultFont.memberForm(210, 0))
        assertEquals(MemberForm.HIDDEN, atDefaultFont.memberForm(209, 0))
    }

    @Test
    fun fixedSizesTruncateBelowApi31() {
        // Below API 31 Glance converts the 1dp rule and the 14dp refresh icon
        // itself and truncates (2px, 36px) where the framework rounds (3px,
        // 37px): the footer is 45px, not 47, so the same widget needs 604px.
        val pre31 = heights(defaultLines, roundsSizes = false)
        assertTrue(pre31.eventFits(604, 4, 2))
        assertFalse(pre31.eventFits(603, 4, 2))
        assertTrue(atDefaultFont.eventFits(606, 4, 2))
        assertFalse(atDefaultFont.eventFits(605, 4, 2))
    }

    @Test
    fun aFleetPastTheRowCapIsPricedAsItsOneCountsLine() {
        // Eight members render as one 47px line in 7dp (18px) padding, not
        // eight rows: fixed 253px, plus a 54px one-row strip and the event.
        val members = BellhopWidget.MAX_MEMBER_ROWS + 3
        val needed = 253 + 10 + 44 + 86
        assertTrue(atDefaultFont.eventFits(needed, members, 1))
        assertFalse(atDefaultFont.eventFits(needed - 1, members, 1))
    }
}
