package com.hugalafutro.bellhop.widget

import android.content.Context
import android.text.format.DateFormat
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
import com.hugalafutro.bellhop.data.TRAFFIC_BUCKETS
import com.hugalafutro.bellhop.data.WidgetState
import com.hugalafutro.bellhop.data.WidgetStore
import com.hugalafutro.bellhop.data.countsOf
import com.hugalafutro.bellhop.ui.theme.Brass300
import com.hugalafutro.bellhop.ui.theme.Brass600
import com.hugalafutro.bellhop.ui.theme.Ember300
import com.hugalafutro.bellhop.ui.theme.Ember600
import com.hugalafutro.bellhop.ui.theme.Ink100
import com.hugalafutro.bellhop.ui.theme.Ink300
import com.hugalafutro.bellhop.ui.theme.Moss300
import com.hugalafutro.bellhop.ui.theme.Moss600
import com.hugalafutro.bellhop.ui.theme.PaperInk
import com.hugalafutro.bellhop.ui.theme.PaperInkMuted
import com.hugalafutro.bellhop.ui.theme.SteelContainerDark
import com.hugalafutro.bellhop.ui.theme.SteelContainerLight
import com.hugalafutro.bellhop.work.FleetPollWorker
import kotlinx.coroutines.flow.first
import java.time.Instant
import java.time.ZoneId
import java.util.Date

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
    override val sizeMode: SizeMode = SizeMode.Responsive(setOf(COMPACT, TALL))

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
        provideContent {
            val state by widgetStore.state.collectAsState(initial = initialState)
            val monitoringActive by monitorStore.active.collectAsState(initial = initialActive)
            val graphs by prefsStore.widgetGraphs.collectAsState(initial = initialGraphs)
            WidgetContent(state, monitoringActive, fdName, graphs)
        }
    }

    companion object {
        // Two responsive tiers: COMPACT = rows + footer; TALL adds the event line.
        val COMPACT = DpSize(180.dp, 110.dp)
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

// Day/night pairs off the app palette (ui/theme/Color.kt); Glance can't read
// MaterialTheme, so the pairing is repeated here with the same named colors.
private val BrandAccent = ColorProvider(day = Brass600, night = Brass300)

// Bar tint for the opt-in traffic overlay: the steel containers read as a calm
// cool wash against the warm row surfaces without fighting the row text.
private val BarTint = ColorProvider(day = SteelContainerLight, night = SteelContainerDark)
private val TextPrimary = ColorProvider(day = PaperInk, night = Ink100)
private val TextMuted = ColorProvider(day = PaperInkMuted, night = Ink300)
private val DotUp = ColorProvider(day = Moss600, night = Moss300)
private val DotDown = ColorProvider(day = Ember600, night = Ember300)
private val DotDrained = ColorProvider(day = Brass600, night = Brass300)
private val DotUnknown = ColorProvider(day = PaperInkMuted, night = Ink300)

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
 * eventStamp dates the newest-event pill: clock time when the event is from
 * today, a short date otherwise, so a day-old event can't masquerade as fresh.
 * Absolute like the "as of" stamp (relative text would need timed re-renders).
 * Empty when the wire timestamp doesn't parse; the pill just omits the stamp.
 */
private fun eventStamp(
    context: Context,
    createdAt: String,
): String {
    val instant = runCatching { Instant.parse(createdAt) }.getOrNull() ?: return ""
    val zone = ZoneId.systemDefault()
    val asDate = Date(instant.toEpochMilli())
    return if (instant.atZone(zone).toLocalDate() == Instant.now().atZone(zone).toLocalDate()) {
        DateFormat.getTimeFormat(context).format(asDate)
    } else {
        DateFormat.getDateFormat(context).format(asDate)
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
        modifier = GlanceModifier.fillMaxWidth().height(16.dp).padding(horizontal = 6.dp),
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
) {
    val context = LocalContext.current
    Column(
        modifier =
            GlanceModifier
                .fillMaxSize()
                .background(ImageProvider(R.drawable.widget_bg))
                .padding(12.dp)
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
                            GlanceModifier.background(
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
                // the footer was the casualty). Worst case now: header + 5
                // rows + pill + weight spacer + footer = 9.
                state.members.forEach { member ->
                    Box(
                        contentAlignment = Alignment.BottomStart,
                        modifier =
                            GlanceModifier
                                .fillMaxWidth()
                                .padding(bottom = 3.dp)
                                .background(ImageProvider(R.drawable.widget_row_bg)),
                    ) {
                        if (graphs && member.traffic.isNotEmpty()) {
                            MemberBars(member.traffic)
                        }
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = GlanceModifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 5.dp),
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
        Spacer(GlanceModifier.defaultWeight())
        // Pinned above the footer rather than under the member rows, so a small
        // fleet doesn't render the event box glued on like an n+1th member.
        if (state != null && LocalSize.current.height >= BellhopWidget.TALL.height) {
            state.newestEvent?.let { event ->
                Column(
                    modifier =
                        GlanceModifier
                            .fillMaxWidth()
                            .padding(bottom = 6.dp)
                            .background(ImageProvider(R.drawable.widget_event_bg))
                            .padding(horizontal = 8.dp, vertical = 4.dp),
                ) {
                    Text(
                        context.getString(R.string.widget_event_header),
                        style = TextStyle(color = TextMuted, fontSize = 9.sp, fontWeight = FontWeight.Medium),
                    )
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = GlanceModifier.fillMaxWidth(),
                    ) {
                        Text(
                            event.message,
                            style = TextStyle(color = TextPrimary, fontSize = 11.sp),
                            maxLines = 1,
                            modifier = GlanceModifier.defaultWeight(),
                        )
                        Spacer(GlanceModifier.width(6.dp))
                        Text(
                            eventStamp(context, event.createdAt),
                            style = TextStyle(color = TextMuted, fontSize = 9.sp),
                        )
                    }
                }
            }
        }
        Row(modifier = GlanceModifier.fillMaxWidth()) {
            if (state != null) {
                val stamp = DateFormat.getTimeFormat(context).format(Date(state.updatedAt))
                Text(
                    context.getString(R.string.widget_as_of, stamp),
                    style = TextStyle(color = TextMuted, fontSize = 11.sp),
                )
            }
            Spacer(GlanceModifier.defaultWeight())
            when {
                state?.autosyncStale == true ->
                    Text(
                        context.getString(R.string.widget_stale),
                        style = TextStyle(color = DotDrained, fontSize = 11.sp),
                    )
                !monitoringActive && state != null ->
                    Text(
                        context.getString(R.string.widget_monitoring_off),
                        style = TextStyle(color = TextMuted, fontSize = 11.sp),
                    )
            }
            if (state != null) {
                Spacer(GlanceModifier.width(8.dp))
                Image(
                    ImageProvider(R.drawable.ic_widget_refresh),
                    context.getString(R.string.widget_refresh),
                    GlanceModifier.size(18.dp).clickable(actionRunCallback<WidgetRefreshAction>()),
                    // The vector's fill is opaque black; tint to the footer's muted
                    // pair or the icon vanishes on the night background.
                    colorFilter = ColorFilter.tint(TextMuted),
                )
            }
        }
    }
}
