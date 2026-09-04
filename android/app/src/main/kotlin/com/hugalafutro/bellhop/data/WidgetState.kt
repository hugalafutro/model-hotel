package com.hugalafutro.bellhop.data

import kotlinx.serialization.Serializable

/**
 * WidgetMember is one row of the home-screen widget: the display name and the
 * member's [MemberHealthState] stored by enum name, so a value from a future
 * build degrades to UNKNOWN (same stance as [FleetSnapshot.stateOf]) instead of
 * crashing the render.
 */
@Serializable
data class WidgetMember(
    val name: String,
    val state: String,
    // Last hour of the member's request counts in 5-minute buckets (oldest
    // first, at most [TRAFFIC_BUCKETS]); empty when traffic graphs are off or
    // the series was never fetched. Display data only, like everything here.
    val traffic: List<Int> = emptyList(),
    // Stable member id for cross-poll matching (keeping a member's bars through
    // a failed series read must not key on the display name, which can collide
    // or be renamed). Never rendered. Defaulted so pre-field state decodes.
    val id: String = "",
) {
    val healthState: MemberHealthState
        get() = runCatching { MemberHealthState.valueOf(state) }.getOrDefault(MemberHealthState.UNKNOWN)
}

/** WidgetEvent is the fleet-wide newest event line, shown on the tall layout only. */
@Serializable
data class WidgetEvent(
    val message: String,
    val createdAt: String,
)

/**
 * WidgetQuotaBadge is one quota badge on the widget: the provider identity
 * ([providerName], the wire's own key -- never type or UUID, see
 * global-constraints badge-identity note) plus [type] (kept as the enum's
 * wire name so a future build's new type degrades gracefully, same stance as
 * [WidgetMember.state]) and a precomputed [label] so the Glance render stays
 * a pure string (no [ProviderQuota]/formatting logic in the render path).
 * [spent] is [isQuotaSpent]'s verdict, precomputed for the same reason; it
 * defaults to false so a state written by an older build still decodes.
 */
@Serializable
data class WidgetQuotaBadge(
    val providerName: String,
    val type: String,
    val label: String,
    val spent: Boolean = false,
) {
    /** Decoded [type], UNKNOWN for a name this build doesn't know (see the class note). */
    val quotaType: QuotaType
        get() = runCatching { QuotaType.valueOf(type) }.getOrDefault(QuotaType.UNKNOWN)
}

/**
 * WidgetState is the widget's whole persisted render model. It is written only
 * by code paths that already fetched the fleet (background poll, foreground
 * refresh, widget refresh tap) so the widget itself never needs the network;
 * [updatedAt] drives the honest "as of" stamp that makes lazy updates safe.
 */
@Serializable
data class WidgetState(
    val members: List<WidgetMember> = emptyList(),
    val autosyncStale: Boolean = false,
    val newestEvent: WidgetEvent? = null,
    val updatedAt: Long = 0L,
    // Defaulted so widget state persisted before quota badges existed still
    // decodes.
    val quota: List<WidgetQuotaBadge> = emptyList(),
)

/**
 * WIDGET_QUOTA_MAX_ROWS caps the strip's height. Four rows of 9sp pills is
 * roughly a third of the smallest widget; past that the fleet rows the widget
 * exists for would be the ones squeezed out.
 */
const val WIDGET_QUOTA_MAX_ROWS = 4

/**
 * WIDGET_QUOTA_DEFAULT_ROW_BUDGET_DP is the packing width for a caller that
 * doesn't know the widget's own: the narrowest the widget can be resized to
 * (180dp) minus the root Column's 12dp side padding.
 */
const val WIDGET_QUOTA_DEFAULT_ROW_BUDGET_DP = 156

// Glance cannot measure text, so a badge's width is estimated from its label.
// Labels are digits, percent signs and a short provider code at 9sp; 6dp per
// character is a deliberate over-estimate, and the chrome term is the pill's
// horizontal padding plus its 1dp end gap.
private const val WIDGET_QUOTA_CHAR_DP = 6
private const val WIDGET_QUOTA_PILL_CHROME_DP = 9

/**
 * WIDGET_QUOTA_MAX_PER_ROW is Glance's 10-children cap on the Row each line of
 * badges becomes, less one slot held for the overflow marker
 * ([quotaBadgeOverflow]) the last row may have to carry.
 */
private const val WIDGET_QUOTA_MAX_PER_ROW = 9

/**
 * badgeWidthDp estimates what one badge occupies, label plus pill chrome, for a
 * caller that cannot measure the text itself. The widget CAN (it hands
 * [quotaBadgeRows] a real measurement, see BellhopWidget), so this is the
 * fallback and the tests' yardstick: it deliberately over-estimates, since a row
 * fitted too wide clips its last badge while one fitted too narrow only wastes
 * space.
 *
 * Internal rather than private so the packing tests measure rows with the same
 * arithmetic the default packer uses instead of restating the constants, which
 * is how they came to check a marker reserve production had already outgrown.
 */
internal fun badgeWidthDp(badge: WidgetQuotaBadge): Int =
    badge.label.length * WIDGET_QUOTA_CHAR_DP + WIDGET_QUOTA_PILL_CHROME_DP

/**
 * WIDGET_QUOTA_OVERFLOW_MARKER_DP is what the trailing "+N" costs the row that
 * carries it: four characters ("+999", far past any real fleet's provider
 * count) plus its gap. The marker sits after the last badge on the same row, so
 * the badges have only the remainder to spend -- which is why the row that
 * carries it is fitted against a budget short by this much, rather than against
 * the full width.
 *
 * Internal for the same reason as [badgeWidthDp]: the tests assert against this
 * number, so they must read it rather than repeat it.
 */
internal const val WIDGET_QUOTA_OVERFLOW_MARKER_DP = 28

/**
 * quotaBadgeRows packs [badges] into the rows the widget renders, keeping the
 * given order and fitting as many per row as [budgetDp] -- the widget's real
 * inner width -- allows. [widthDp] says what one badge costs; the widget passes
 * a real text measurement and everyone else gets [badgeWidthDp]'s estimate.
 *
 * Each badge is as wide as its own label, so a row fits while the SUM of its
 * badges' widths does. That is looser than the equal-share layout this replaced,
 * where a row of n badges had to fit n times its own widest label: uniform
 * columns meant one long label ("NW 12.5/20 kWh") priced every badge beside it
 * at its own width, so rows carried a badge or two fewer than they had room for
 * and a row left holding one badge stretched it across the full width.
 *
 * Rows past [WIDGET_QUOTA_MAX_ROWS] are still dropped -- the strip must not eat
 * the fleet rows the widget exists for -- so the caller asks
 * [quotaBadgeOverflow] how many were left out and says so rather than dropping
 * them silently. When that happens the last visible row also gives up whatever
 * it must to leave room for the "+N" marker it will carry.
 */
fun quotaBadgeRows(
    badges: List<WidgetQuotaBadge>,
    budgetDp: Int = WIDGET_QUOTA_DEFAULT_ROW_BUDGET_DP,
    widthDp: (WidgetQuotaBadge) -> Int = ::badgeWidthDp,
): List<List<WidgetQuotaBadge>> {
    val packed = packRows(badges, budgetDp, widthDp)
    if (packed.size <= WIDGET_QUOTA_MAX_ROWS) return packed
    val kept = packed.take(WIDGET_QUOTA_MAX_ROWS).toMutableList()
    kept[kept.lastIndex] = fitAroundMarker(kept.last(), budgetDp, widthDp)
    return kept
}

/**
 * fitAroundMarker trims [row] until its badges still fit beside the overflow
 * marker. A badge dropped here is not lost, only re-counted: it lands in
 * [quotaBadgeOverflow] along with the rows that didn't fit at all. At least one
 * badge is always kept -- a row of just the marker would say less than a
 * clipped badge does.
 */
private fun fitAroundMarker(
    row: List<WidgetQuotaBadge>,
    budgetDp: Int,
    widthDp: (WidgetQuotaBadge) -> Int,
): List<WidgetQuotaBadge> {
    val budget = budgetDp - WIDGET_QUOTA_OVERFLOW_MARKER_DP
    var out = row
    while (out.size > 1 && out.sumOf(widthDp) > budget) {
        out = out.dropLast(1)
    }
    return out
}

/** packRows fills rows against [budgetDp] without regard to the row cap. */
private fun packRows(
    badges: List<WidgetQuotaBadge>,
    budgetDp: Int,
    widthDp: (WidgetQuotaBadge) -> Int,
): List<List<WidgetQuotaBadge>> {
    val rows = mutableListOf<MutableList<WidgetQuotaBadge>>()
    // What the row being filled has already spent, since each badge takes only
    // its own label's width.
    var rowWidth = 0
    badges.forEach { badge ->
        val width = widthDp(badge)
        val row = rows.lastOrNull()
        val fits = row != null && row.size < WIDGET_QUOTA_MAX_PER_ROW && rowWidth + width <= budgetDp
        if (fits) {
            row.add(badge)
            rowWidth += width
        } else {
            // A new row always accepts its first badge, so one wider than the
            // whole widget still gets a line rather than disappearing.
            rows += mutableListOf(badge)
            rowWidth = width
        }
    }
    // Every row, cap included: the caller needs to see that it overflowed to
    // know the last visible row has a marker to make room for.
    return rows
}

/**
 * quotaBadgeOverflow counts the badges [rows] left out of [badges] -- what the
 * [WIDGET_QUOTA_MAX_ROWS] cap dropped. Zero when everything fit.
 */
fun quotaBadgeOverflow(
    badges: List<WidgetQuotaBadge>,
    rows: List<List<WidgetQuotaBadge>>,
): Int = badges.size - rows.sumOf { it.size }

/**
 * widgetQuotaOf resolves [quota] against [config] the same way the main-page
 * badge list does ([orderedVisible]: hidden/unavailable names dropped, order
 * preserved) and precomputes each badge's short [WidgetQuotaBadge.label] via
 * [quotaBadgeLabel] (in [mode]'s polarity) so the widget's render stays
 * pure-string.
 *
 * It carries the operator's WHOLE selection, uncapped. Trimming it here to a
 * fixed count would make [quotaBadgeOverflow] -- which measures what the strip
 * showed against what it was given -- report a shortfall short by whatever this
 * had already discarded, so a "+3" could stand for eleven missing badges. How
 * many actually fit is [quotaBadgeRows]' business, and depends on the widget's
 * width, which nothing upstream of the render knows. The label leads with
 * [quotaShortCode] because a widget badge showing only a number says nothing
 * about whose number it is, and the dashboard's full provider name would eat
 * the row at this size.
 */
fun widgetQuotaOf(
    quota: List<ProviderQuota>,
    config: QuotaBadgeConfig,
    mode: QuotaBarMode,
): List<WidgetQuotaBadge> =
    orderedVisible(config, quota)
        .map {
            WidgetQuotaBadge(
                providerName = it.providerName,
                type = it.type.name,
                label = "${quotaShortCode(it.type)} ${quotaBadgeLabel(it, mode)}",
                spent = isQuotaSpent(it),
            )
        }

/** TRAFFIC_BUCKETS is the widget's bar-graph window: one hour of 5-minute buckets. */
const val TRAFFIC_BUCKETS = 12

/** WidgetCounts is the collapsed fallback face for fleets too big for per-member rows. */
data class WidgetCounts(
    val up: Int,
    val down: Int,
    val drained: Int,
    val unknown: Int,
)

fun countsOf(state: WidgetState): WidgetCounts {
    val byState = state.members.groupingBy { it.healthState }.eachCount()
    return WidgetCounts(
        up = byState[MemberHealthState.UP] ?: 0,
        down = byState[MemberHealthState.DOWN] ?: 0,
        drained = byState[MemberHealthState.DRAINED] ?: 0,
        unknown = byState[MemberHealthState.UNKNOWN] ?: 0,
    )
}

/**
 * widgetStateOf collapses a fetched fleet into the widget's render model. The
 * newest event is the fleet-wide max over the inline per-member newest events;
 * Front Desk stamps created_at as RFC3339 UTC, which sorts lexicographically,
 * so a string max needs no parsing.
 */
fun widgetStateOf(
    members: List<FleetMember>,
    autosyncStale: Boolean,
    now: Long,
    traffic: Map<String, List<Int>> = emptyMap(),
    quota: List<WidgetQuotaBadge> = emptyList(),
): WidgetState =
    WidgetState(
        members =
            members.map {
                WidgetMember(
                    name = it.name.ifBlank { it.id },
                    state = healthStateOf(it).name,
                    // Writers hand over whatever window they fetched; the model
                    // owns the widget's newest-TRAFFIC_BUCKETS contract.
                    traffic = traffic[it.id].orEmpty().takeLast(TRAFFIC_BUCKETS),
                    id = it.id,
                )
            },
        autosyncStale = autosyncStale,
        newestEvent =
            members
                .mapNotNull { it.newestEvent }
                .maxByOrNull { it.createdAt }
                ?.let { WidgetEvent(it.message, it.createdAt) },
        updatedAt = now,
        // Writers already computed the badge list via widgetQuotaOf (needs the
        // fetched ProviderQuota list plus the surface's QuotaBadgeConfig,
        // neither of which this function otherwise touches); threaded through
        // verbatim.
        quota = quota,
    )
