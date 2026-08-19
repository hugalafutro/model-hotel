package com.hugalafutro.bellhop.widget

import android.content.Context
import android.content.Intent
import android.graphics.Typeface
import android.text.TextPaint
import android.util.TypedValue
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.ColorFilter
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalContext
import androidx.glance.LocalSize
import androidx.glance.action.ActionParameters
import androidx.glance.action.actionParametersOf
import androidx.glance.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.ActionCallback
import androidx.glance.appwidget.action.actionRunCallback
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import com.hugalafutro.bellhop.MainActivity
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.LinkState
import com.hugalafutro.bellhop.data.LinkStore
import com.hugalafutro.bellhop.data.MemberHealthState
import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.data.PrefsStore
import com.hugalafutro.bellhop.data.QuotaBadgeAlign
import com.hugalafutro.bellhop.data.QuotaType
import com.hugalafutro.bellhop.data.TRAFFIC_BUCKETS
import com.hugalafutro.bellhop.data.TimeFormat
import com.hugalafutro.bellhop.data.WidgetQuotaBadge
import com.hugalafutro.bellhop.data.WidgetState
import com.hugalafutro.bellhop.data.WidgetStore
import com.hugalafutro.bellhop.data.countsOf
import com.hugalafutro.bellhop.data.quotaBadgeOverflow
import com.hugalafutro.bellhop.data.quotaBadgeRows
import com.hugalafutro.bellhop.data.quotaHasDetail
import com.hugalafutro.bellhop.data.timePattern
import com.hugalafutro.bellhop.ui.common.timeAndDate
import com.hugalafutro.bellhop.ui.theme.Brass300
import com.hugalafutro.bellhop.ui.theme.Brass600
import com.hugalafutro.bellhop.ui.theme.Ember300
import com.hugalafutro.bellhop.ui.theme.Ember600
import com.hugalafutro.bellhop.ui.theme.Ink100
import com.hugalafutro.bellhop.ui.theme.Ink300
import com.hugalafutro.bellhop.ui.theme.Ink700
import com.hugalafutro.bellhop.ui.theme.Moss300
import com.hugalafutro.bellhop.ui.theme.Moss600
import com.hugalafutro.bellhop.ui.theme.Paper200
import com.hugalafutro.bellhop.ui.theme.PaperInk
import com.hugalafutro.bellhop.ui.theme.PaperInkMuted
import com.hugalafutro.bellhop.ui.theme.SteelContainerDark
import com.hugalafutro.bellhop.ui.theme.SteelContainerLight
import com.hugalafutro.bellhop.ui.theme.quotaBrand
import com.hugalafutro.bellhop.work.FleetPollWorker
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.ceil
import androidx.glance.appwidget.action.actionStartActivity as actionStartActivityIntent

/** BellhopWidgetReceiver is the manifest entry point; all logic is in [BellhopWidget]. */
class BellhopWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = BellhopWidget()

    // Placing the first widget instance is a user action asking for fleet state
    // now, so fire the same display-only one-shot as the refresh button rather
    // than sitting on empty-or-stale until the next organic write (the periodic
    // backstop's first run can be a full period away). Not polling: one fetch
    // per placement, and the linked/token guards inside make it a no-op when
    // unpaired. onEnabled fires on first placement only, not on boot, so a
    // reboot still renders purely from persisted state.
    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        FleetPollWorker.runWidgetRefresh(context)
    }
}

/**
 * BellhopWidget renders the persisted [WidgetState] and NOTHING live: no
 * network, no timers (spec hard rule). It re-renders only when a writer calls
 * [update] after a store write, or on system broadcasts (placement, reboot).
 * The "as of" stamp is absolute clock time, never relative, because relative
 * text would need timed re-renders just to tick.
 */
class BellhopWidget : GlanceAppWidget() {
    // Exact, not Responsive: the badge strip packs its rows against the widget's
    // real inner width, and Responsive reports whichever breakpoint matched, so
    // a 4-column widget would still be laid out as if it were 180dp wide. TALL
    // below stays on as a plain height threshold.
    override val sizeMode: SizeMode = SizeMode.Exact

    override suspend fun provideGlance(
        context: Context,
        id: GlanceId,
    ) {
        val widgetStore = WidgetStore.create(context)
        val monitorStore = MonitorStore.create(context)
        // The header names the linked Front Desk. Read once per session: it
        // only changes across link/unlink, and unlink tears the session down.
        val fdName = (LinkStore.create(context).state.first() as? LinkState.Linked)?.fdName.orEmpty()
        // Seed synchronously so the first frame shows real data, then keep
        // collecting inside the composition: a Glance session outlives its
        // first frame, and an update landing while it is alive (the placement
        // refresh finishes seconds after placement) re-runs only the
        // composition, not this function - a read captured out here would
        // pin every recomposition to placement-time state. Collecting is not
        // polling: the flow only emits when a writer commits.
        val prefsStore = PrefsStore.create(context)
        val initialState = widgetStore.read()
        val initialActive = monitorStore.active.first()
        val initialGraphs = prefsStore.widgetGraphs.first()
        val initialQuota = prefsStore.widgetQuota.first()
        val initialQuotaAlign = prefsStore.widgetQuotaAlign.first()
        val initialTimeFormat = prefsStore.timeFormat.first()
        provideContent {
            val state by widgetStore.state.collectAsState(initial = initialState)
            val monitoringActive by monitorStore.active.collectAsState(initial = initialActive)
            val graphs by prefsStore.widgetGraphs.collectAsState(initial = initialGraphs)
            val quotaStrip by prefsStore.widgetQuota.collectAsState(initial = initialQuota)
            val quotaAlign by prefsStore.widgetQuotaAlign.collectAsState(initial = initialQuotaAlign)
            val timeFormat by prefsStore.timeFormat.collectAsState(initial = initialTimeFormat)
            WidgetContent(state, monitoringActive, fdName, graphs, quotaStrip, quotaAlign, timeFormat)
        }
    }

    companion object {
        // The height at which the widget is tall enough to spend a block on the
        // latest event as well as the member rows and the footer.
        val TALL = DpSize(180.dp, 180.dp)

        // Per-member rows up to here; larger fleets collapse to counts.
        const val MAX_MEMBER_ROWS = 5

        /** update re-renders every placed instance; a no-op when none is placed. */
        suspend fun update(context: Context) {
            BellhopWidget().updateAll(context)
        }
    }
}

/**
 * WidgetRefreshAction hands the tap off to WorkManager and returns; the
 * poll's own completion re-renders the widget via [FleetPollWorker]'s update
 * call, so the action itself stays instant and the widget never blocks on
 * the network.
 */
class WidgetRefreshAction : ActionCallback {
    override suspend fun onAction(
        context: Context,
        glanceId: GlanceId,
        parameters: ActionParameters,
    ) {
        FleetPollWorker.runWidgetRefresh(context)
    }
}

// Deep-link contract for a quota badge tap on a provider that has a detail view
// (MainActivity owns the other side: the extra read + the app-lock gate).
// Top-level and public so both sides of the contract share the literal instead
// of duplicating strings.
const val ACTION_OPEN_QUOTA = "com.hugalafutro.bellhop.OPEN_QUOTA"
const val EXTRA_BADGE_PROVIDER_NAME = "badge_provider_name"

/**
 * quotaBadgeIntent builds the explicit intent a quota badge tap fires: same
 * destination and launch flags as the widget's default open-app tap
 * (`actionStartActivity<MainActivity>()` below), plus the deep-link
 * action/extra above so MainActivity can route straight to that provider's
 * quota sheet. Extracted as a plain function (no Glance/Composable types) so
 * it is unit-testable without a composition.
 *
 * Rendered via the aliased `actionStartActivityIntent` (`androidx.glance.
 * appwidget.action.actionStartActivity(Intent, ...)`): the core-module
 * `actionStartActivity` imported above only targets a ComponentName/Class
 * and can't carry this Intent's custom action + extra.
 */
fun quotaBadgeIntent(
    context: Context,
    providerName: String,
): Intent =
    Intent(context, MainActivity::class.java).apply {
        action = ACTION_OPEN_QUOTA
        putExtra(EXTRA_BADGE_PROVIDER_NAME, providerName)
        flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
    }

/** BADGE_PROVIDER_NAME carries the tapped badge's provider to [QuotaBadgeNameAction]. */
val BADGE_PROVIDER_NAME = ActionParameters.Key<String>("badge_provider_name")

/**
 * QuotaBadgeNameAction answers a badge tap with the provider's full name in a
 * toast, and nothing else. It is what a badge gets instead of a deep link when
 * its provider has nothing more to show than the badge already does
 * ([com.hugalafutro.bellhop.data.quotaHasDetail]): launching the app, possibly
 * through an unlock prompt, to read the same "pro" back is a poor trade. The
 * toast still earns its tap, because the badge only has room for a short code
 * ([com.hugalafutro.bellhop.data.quotaShortCode]) that two providers of the
 * same type share.
 */
class QuotaBadgeNameAction : ActionCallback {
    override suspend fun onAction(
        context: Context,
        glanceId: GlanceId,
        parameters: ActionParameters,
    ) {
        val name = parameters[BADGE_PROVIDER_NAME] ?: return
        // Glance runs actions off the main thread and a Toast needs a Looper, so
        // hop to main -- immediate, so a caller already on it runs inline rather
        // than posting to a looper that may be waiting on this call to return.
        withContext(Dispatchers.Main.immediate) {
            Toast.makeText(context, name, Toast.LENGTH_SHORT).show()
        }
    }
}

// Day/night pairs off the app palette (ui/theme/Color.kt); Glance can't read
// MaterialTheme, so the pairing is repeated here with the same named colors.
private val BrandAccent = ColorProvider(day = Brass600, night = Brass300)

// Bar tint for the opt-in traffic overlay: the steel containers read as a calm
// cool wash against the warm row surfaces without fighting the row text.
private val BarTint = ColorProvider(day = SteelContainerLight, night = SteelContainerDark)
private val TextPrimary = ColorProvider(day = PaperInk, night = Ink100)
private val TextMuted = ColorProvider(day = PaperInkMuted, night = Ink300)

// Hairline rules. Same tones the row and pill fills use, which on the widget
// background read as a line rather than a second surface.
private val RuleTint = ColorProvider(day = Paper200, night = Ink700)

// The root Column's inset. Named because the badge strip has to subtract it
// from the reported widget width to know how much room a row really has.
private val ROOT_PADDING = 12.dp
private val DotUp = ColorProvider(day = Moss600, night = Moss300)
private val DotDown = ColorProvider(day = Ember600, night = Ember300)
private val DotDrained = ColorProvider(day = Brass600, night = Brass300)
private val DotUnknown = ColorProvider(day = PaperInkMuted, night = Ink300)

// Quota badges are the one place the widget shows third-party brand colors
// rather than the app palette; quotaBrand is the shared source (ui/theme),
// so the widget pill and the dashboard chip can never drift apart.
private fun quotaBadgeColor(type: QuotaType) = quotaBrand(type).let { ColorProvider(day = it.day, night = it.night) }

/**
 * quotaRowAlignment maps the stored preference to the Glance alignment the badge
 * rows are laid out with. A function rather than an inline `when` so the mapping
 * is testable: the widget's render is only covered on-device, and a transposed
 * arm here would quietly send RIGHT to the left edge.
 *
 * LEFT/RIGHT are absolute labels mapped to Start/End, which Glance resolves
 * against layout direction -- that only lines up while every shipped locale is
 * LTR. Adding an RTL locale means revisiting either this mapping or the copy.
 */
internal fun quotaRowAlignment(align: QuotaBadgeAlign): Alignment.Horizontal =
    when (align) {
        QuotaBadgeAlign.LEFT -> Alignment.Start
        QuotaBadgeAlign.CENTER -> Alignment.CenterHorizontally
        QuotaBadgeAlign.RIGHT -> Alignment.End
    }

private fun dotColor(state: MemberHealthState) =
    when (state) {
        MemberHealthState.UP -> DotUp
        MemberHealthState.DOWN -> DotDown
        MemberHealthState.DRAINED -> DotDrained
        MemberHealthState.UNKNOWN -> DotUnknown
    }

/** stateLabel is the row chip's short localized status word. */
private fun stateLabel(
    context: Context,
    state: MemberHealthState,
): String =
    context.getString(
        when (state) {
            MemberHealthState.UP -> R.string.widget_state_up
            MemberHealthState.DOWN -> R.string.widget_state_down
            MemberHealthState.DRAINED -> R.string.widget_state_drained
            MemberHealthState.UNKNOWN -> R.string.widget_state_unknown
        },
    )

/**
 * clockStamp renders a moment as wall-clock time on the clock [format] names.
 * The widget can't use the app's LocalTimePattern (it isn't a Compose UI
 * composition), so it resolves the same preference itself.
 */
private fun clockStamp(
    context: Context,
    format: TimeFormat,
    millis: Long,
): String =
    DateTimeFormatter
        .ofPattern(timePattern(format, context), Locale.getDefault())
        .withZone(ZoneId.systemDefault())
        .format(Instant.ofEpochMilli(millis))

/**
 * eventStamp stamps the newest-event line with the time AND the date, always
 * both: it used to show one or the other (time for today, date for anything
 * older), which reads as a complete stamp either way, so a line dated
 * "22/07/26" left you guessing whether that morning's event or that night's was
 * the fleet's last word. Absolute like the "as of" stamp (relative text would
 * need timed re-renders). Empty when the wire timestamp doesn't parse; the line
 * just omits the stamp.
 */
private fun eventStamp(
    context: Context,
    format: TimeFormat,
    createdAt: String,
): String {
    val instant = runCatching { Instant.parse(createdAt) }.getOrNull() ?: return ""
    return timeAndDate(context, format, instant.toEpochMilli())
}

// The badge pill's own text: 9sp Medium in the platform's sans, matching the
// Text below it. Named here because the packer needs the same numbers to know
// how wide a label comes out.
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
private fun badgeMeasurer(context: Context): (WidgetQuotaBadge) -> Int {
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

/**
 * MemberBars is the opt-in traffic overlay: the member's last-hour request
 * buckets as bottom-aligned bars behind the row text, split into two
 * six-bar halves because a Glance container is capped at 10 children.
 * Heights are normalized per member (3..15dp inside the 16dp strip); an
 * empty bucket keeps a 1dp baseline so the hour reads as continuous.
 */
@Composable
private fun MemberBars(traffic: List<Int>) {
    val buckets = List((TRAFFIC_BUCKETS - traffic.size).coerceAtLeast(0)) { 0 } + traffic.takeLast(TRAFFIC_BUCKETS)
    val max = buckets.max().coerceAtLeast(1)
    Row(
        verticalAlignment = Alignment.Bottom,
        // Inset from the card's bottom edge so the rounded surface stays visible
        // under the baseline and neighbouring cards don't blend into one strip.
        modifier = GlanceModifier.fillMaxWidth().height(16.dp).padding(horizontal = 6.dp, vertical = 2.dp),
    ) {
        buckets.chunked(TRAFFIC_BUCKETS / 2).forEach { half ->
            Row(verticalAlignment = Alignment.Bottom, modifier = GlanceModifier.defaultWeight()) {
                half.forEach { v ->
                    val h = if (v == 0) 1 else 3 + 12 * v / max
                    Box(
                        modifier =
                            GlanceModifier
                                .defaultWeight()
                                .height(h.dp)
                                .padding(horizontal = 1.dp)
                                .background(BarTint),
                    ) {}
                }
            }
        }
    }
}

@Composable
private fun WidgetContent(
    state: WidgetState?,
    monitoringActive: Boolean,
    fdName: String,
    graphs: Boolean,
    quotaStrip: Boolean,
    quotaAlign: QuotaBadgeAlign,
    timeFormat: TimeFormat,
) {
    val context = LocalContext.current
    Column(
        modifier =
            GlanceModifier
                .fillMaxSize()
                .background(ImageProvider(R.drawable.widget_bg))
                .padding(ROOT_PADDING)
                .clickable(actionStartActivity<MainActivity>()),
    ) {
        if (state != null) {
            // Header carries the always-present chrome: FD name on the left,
            // freshness stamp + refresh on the right. Living up here means the
            // row cards and event pill can never push them off the bottom.
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = GlanceModifier.fillMaxWidth().padding(bottom = 6.dp),
            ) {
                Image(
                    ImageProvider(R.drawable.ic_stat_bellhop),
                    null,
                    GlanceModifier.size(14.dp),
                    colorFilter = ColorFilter.tint(BrandAccent),
                )
                Spacer(GlanceModifier.width(6.dp))
                Text(
                    fdName,
                    style = TextStyle(color = TextMuted, fontSize = 11.sp, fontWeight = FontWeight.Medium),
                    maxLines = 1,
                    modifier = GlanceModifier.defaultWeight(),
                )
                Spacer(GlanceModifier.width(8.dp))
                // Overall fleet badge: up/total ratio, locale-neutral, colored
                // by the worst state present (all up wins green, any down wins
                // ember, drained/unknown mixes read brass).
                if (state.members.isNotEmpty()) {
                    val c = countsOf(state)
                    val badgeColor =
                        when {
                            c.down > 0 -> DotDown
                            c.up == state.members.size -> DotUp
                            else -> DotDrained
                        }
                    Row(
                        modifier =
                            GlanceModifier
                                .background(
                                    ImageProvider(R.drawable.widget_pill_bg),
                                ).padding(horizontal = 7.dp, vertical = 2.dp),
                    ) {
                        Text(
                            "${c.up}/${state.members.size}",
                            style = TextStyle(color = badgeColor, fontSize = 10.sp, fontWeight = FontWeight.Bold),
                        )
                    }
                }
            }
        }
        when {
            state == null ->
                Text(
                    context.getString(R.string.widget_unpaired),
                    style = TextStyle(color = TextMuted, fontSize = 13.sp),
                )
            state.members.isEmpty() ->
                Text(
                    context.getString(R.string.widget_no_members),
                    style = TextStyle(color = TextMuted, fontSize = 13.sp),
                )
            state.members.size > BellhopWidget.MAX_MEMBER_ROWS -> {
                val c = countsOf(state)
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier =
                        GlanceModifier
                            .fillMaxWidth()
                            .background(ImageProvider(R.drawable.widget_row_bg))
                            .padding(horizontal = 10.dp, vertical = 7.dp),
                ) {
                    Text(
                        context.getString(R.string.widget_counts, c.up, c.down, c.drained),
                        style = TextStyle(color = TextPrimary, fontSize = 13.sp, fontWeight = FontWeight.Medium),
                    )
                }
            }
            else ->
                // Row gaps ride as outer padding, not Spacer children: a Glance
                // Column translates to a RemoteViews container capped at 10
                // children, and per-row Spacers blew past it on a 3-member
                // fleet (the children beyond the cap are silently dropped -
                // the footer was the casualty). Worst case now sits AT the cap:
                // header + 5 rows + quota Column + weight spacer + event Column
                // + footer = 10, no headroom. The quota strip is ONE child
                // whatever its badge count (its rows are children of its own
                // nested Column), so a bigger selection is free here - but
                // adding any new top-level SECTION will silently drop a child;
                // free a slot (nest a singleton) before doing so.
                state.members.forEach { member ->
                    // The gap rides an outer Box because Glance padding is a
                    // view's *inner* padding: put it on the card itself and the
                    // background paints over it, which is why the rows used to
                    // sit flush against each other with a 4dp gap declared.
                    Box(modifier = GlanceModifier.fillMaxWidth().padding(bottom = 1.dp)) {
                        Box(
                            contentAlignment = Alignment.BottomStart,
                            modifier =
                                GlanceModifier
                                    .fillMaxWidth()
                                    .background(ImageProvider(R.drawable.widget_row_bg)),
                        ) {
                            if (graphs && member.traffic.isNotEmpty()) {
                                MemberBars(member.traffic)
                            }
                            Row(
                                verticalAlignment = Alignment.CenterVertically,
                                modifier =
                                    GlanceModifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 3.dp),
                            ) {
                                Image(
                                    ImageProvider(R.drawable.widget_dot),
                                    null,
                                    GlanceModifier.size(9.dp),
                                    colorFilter = ColorFilter.tint(dotColor(member.healthState)),
                                )
                                Spacer(GlanceModifier.width(8.dp))
                                Text(
                                    member.name,
                                    style =
                                        TextStyle(
                                            color = TextPrimary,
                                            fontSize = 13.sp,
                                            fontWeight = FontWeight.Medium,
                                        ),
                                    maxLines = 1,
                                    modifier = GlanceModifier.defaultWeight(),
                                )
                                Spacer(GlanceModifier.width(8.dp))
                                Text(
                                    stateLabel(context, member.healthState),
                                    style = TextStyle(color = dotColor(member.healthState), fontSize = 11.sp),
                                )
                            }
                        }
                    }
                }
        }
        // One badge per configured provider (when Settings has the strip on at
        // all), pre-ordered and filtered by the poll layer and packed into lines
        // by quotaBadgeRows
        // -- Glance has no wrapping layout, so the rows are explicit. The whole
        // strip is ONE child of the Column above (the badge rows are children
        // of this nested Column, which has its own 10-child budget that
        // WIDGET_QUOTA_MAX_ROWS keeps it well inside), so it stays cheap
        // against the 10-child cap the member rows already have to mind.
        if (state != null && quotaStrip && state.quota.isNotEmpty()) {
            Column(modifier = GlanceModifier.fillMaxWidth().padding(top = 4.dp)) {
                // Packed against the widget's real inner width (its own width
                // less the root Column's side padding), so a wide widget fits a
                // wide row instead of the two the narrowest breakpoint allowed.
                val budgetDp = (LocalSize.current.width - ROOT_PADDING * 2).value.toInt()
                val rows = quotaBadgeRows(state.quota, budgetDp, badgeMeasurer(context))
                // What the row cap left out. Said out loud on the last row: the
                // operator picked these badges, so a strip too short to hold
                // them all has to admit it rather than quietly showing fewer.
                val overflow = quotaBadgeOverflow(state.quota, rows)
                rows.forEachIndexed { index, row ->
                    Row(
                        // Where the strip sits across the widget. This can only
                        // do anything because the badges below take no weight --
                        // each pill is its own label's width, so the row has real
                        // slack to distribute.
                        horizontalAlignment = quotaRowAlignment(quotaAlign),
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = GlanceModifier.fillMaxWidth().padding(bottom = 1.dp),
                    ) {
                        row.forEach { badge ->
                            // Outer Box carries the gap, inner Box the pill: Glance
                            // padding is a view's inner padding, so a gap declared on
                            // the pill itself is painted over by its own background
                            // and the badges come out touching.
                            //
                            // Neither Box takes a weight: a badge is as wide as its
                            // own label, like the dashboard chips. Equal shares made
                            // every row a grid of uniform columns, which left a row
                            // holding one badge painting its brand colour across the
                            // full width and cost the packer a badge per row (it had
                            // to fit n times the row's WIDEST label, not the sum).
                            Box(modifier = GlanceModifier.padding(end = 1.dp)) {
                                // A provider with a detail view worth the trip opens it;
                                // one whose badge is already the whole reading just names
                                // itself, rather than launching the app to repeat a word.
                                val onTap =
                                    if (quotaHasDetail(badge.quotaType)) {
                                        actionStartActivityIntent(
                                            quotaBadgeIntent(context, badge.providerName),
                                        )
                                    } else {
                                        actionRunCallback<QuotaBadgeNameAction>(
                                            actionParametersOf(BADGE_PROVIDER_NAME to badge.providerName),
                                        )
                                    }
                                Box(
                                    modifier =
                                        GlanceModifier
                                            .background(ImageProvider(R.drawable.widget_pill_bg))
                                            .padding(horizontal = 4.dp, vertical = 2.dp)
                                            .clickable(onTap),
                                ) {
                                    Text(
                                        badge.label,
                                        style =
                                            TextStyle(
                                                // Provider brand colour, same source as the
                                                // dashboard chips and the Model Hotel sidebar
                                                // pills, so a strip of badges is scannable
                                                // instead of eight identical pills.
                                                color = quotaBadgeColor(badge.quotaType),
                                                fontSize = 9.sp,
                                                fontWeight = FontWeight.Medium,
                                            ),
                                        maxLines = 1,
                                    )
                                }
                            }
                        }
                        if (overflow > 0 && index == rows.lastIndex) {
                            Text(
                                "+$overflow",
                                style = TextStyle(color = TextMuted, fontSize = 9.sp),
                                maxLines = 1,
                            )
                        }
                    }
                }
            }
        }
        Spacer(GlanceModifier.defaultWeight())
        // Pinned above the footer rather than under the member rows, so a small
        // fleet doesn't render the event glued on like an n+1th member. It reads
        // as a titled section now instead of a filled panel: a centred caption
        // with a rule running out to both edges, the line itself, and a closing
        // rule that doubles as the footer's separator.
        if (state != null && LocalSize.current.height >= BellhopWidget.TALL.height) {
            state.newestEvent?.let { event ->
                Column(modifier = GlanceModifier.fillMaxWidth().padding(bottom = 2.dp)) {
                    CaptionRule(context.getString(R.string.widget_event_header))
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = GlanceModifier.fillMaxWidth().padding(vertical = 2.dp),
                    ) {
                        Text(
                            event.message,
                            style = TextStyle(color = TextPrimary, fontSize = 11.sp),
                            maxLines = 1,
                            modifier = GlanceModifier.defaultWeight(),
                        )
                        Spacer(GlanceModifier.width(6.dp))
                        Text(
                            eventStamp(context, timeFormat, event.createdAt),
                            style = TextStyle(color = TextMuted, fontSize = 9.sp),
                        )
                    }
                }
            }
        }
        // Footer chrome is informative, not content: 9sp muted text and a small
        // icon so it recedes behind the rows it annotates. Rule and row share
        // one Column so the footer still costs the root a single child.
        Column(modifier = GlanceModifier.fillMaxWidth()) {
            Rule()
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = GlanceModifier.fillMaxWidth().padding(top = 3.dp),
            ) {
                if (state != null) {
                    val stamp = clockStamp(context, timeFormat, state.updatedAt)
                    Text(
                        context.getString(R.string.widget_as_of, stamp),
                        style = TextStyle(color = TextMuted, fontSize = 9.sp),
                    )
                }
                Spacer(GlanceModifier.defaultWeight())
                when {
                    state?.autosyncStale == true ->
                        Text(
                            context.getString(R.string.widget_stale),
                            style = TextStyle(color = DotDrained, fontSize = 9.sp),
                        )
                    !monitoringActive && state != null ->
                        Text(
                            context.getString(R.string.widget_monitoring_off),
                            style = TextStyle(color = TextMuted, fontSize = 9.sp),
                        )
                }
                if (state != null) {
                    Spacer(GlanceModifier.width(8.dp))
                    Image(
                        ImageProvider(R.drawable.ic_widget_refresh),
                        context.getString(R.string.widget_refresh),
                        GlanceModifier.size(14.dp).clickable(actionRunCallback<WidgetRefreshAction>()),
                        // The vector's fill is opaque black; tint to the footer's muted
                        // pair or the icon vanishes on the night background.
                        colorFilter = ColorFilter.tint(TextMuted),
                    )
                }
            }
        }
    }
}

/** Rule is a hairline separator across the widget's width. */
@Composable
private fun Rule() {
    Box(modifier = GlanceModifier.fillMaxWidth().height(1.dp).background(RuleTint)) {}
}

/**
 * CaptionRule is a section heading: the caption centred with a rule running
 * from it out to each edge, which labels the block below without wrapping it
 * in a filled panel.
 */
@Composable
private fun CaptionRule(caption: String) {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = GlanceModifier.fillMaxWidth()) {
        Box(modifier = GlanceModifier.defaultWeight().height(1.dp).background(RuleTint)) {}
        Text(
            caption,
            style = TextStyle(color = TextMuted, fontSize = 9.sp, fontWeight = FontWeight.Medium),
            maxLines = 1,
            modifier = GlanceModifier.padding(horizontal = 6.dp),
        )
        Box(modifier = GlanceModifier.defaultWeight().height(1.dp).background(RuleTint)) {}
    }
}
