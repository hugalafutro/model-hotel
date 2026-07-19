package com.hugalafutro.bellhop.widget

import android.content.Context
import android.text.format.DateFormat
import androidx.compose.runtime.Composable
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.ImageProvider
import androidx.glance.LocalContext
import androidx.glance.LocalSize
import androidx.glance.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import com.hugalafutro.bellhop.MainActivity
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.MemberHealthState
import com.hugalafutro.bellhop.data.MonitorStore
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
import kotlinx.coroutines.flow.first
import java.util.Date

/** BellhopWidgetReceiver is the manifest entry point; all logic is in [BellhopWidget]. */
class BellhopWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = BellhopWidget()
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
        // Read once per render; updateAll() re-runs provideGlance, so writers
        // control freshness and the composition itself stays static.
        val state = WidgetStore.create(context).read()
        val monitoringActive = MonitorStore.create(context).active.first()
        provideContent { WidgetContent(state, monitoringActive) }
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

// Day/night pairs off the app palette (ui/theme/Color.kt); Glance can't read
// MaterialTheme, so the pairing is repeated here with the same named colors.
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

@Composable
private fun WidgetContent(
    state: WidgetState?,
    monitoringActive: Boolean,
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
                Text(
                    context.getString(R.string.widget_counts, c.up, c.down, c.drained),
                    style = TextStyle(color = TextPrimary, fontSize = 14.sp, fontWeight = FontWeight.Medium),
                )
            }
            else ->
                state.members.forEach { member ->
                    Row(modifier = GlanceModifier.padding(vertical = 2.dp)) {
                        Text("●", style = TextStyle(color = dotColor(member.healthState), fontSize = 13.sp))
                        Spacer(GlanceModifier.width(6.dp))
                        Text(member.name, style = TextStyle(color = TextPrimary, fontSize = 13.sp), maxLines = 1)
                    }
                }
        }
        if (state != null && LocalSize.current.height >= BellhopWidget.TALL.height) {
            state.newestEvent?.let { event ->
                Spacer(GlanceModifier.height(6.dp))
                Text(event.message, style = TextStyle(color = TextMuted, fontSize = 11.sp), maxLines = 2)
            }
        }
        Spacer(GlanceModifier.defaultWeight())
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
        }
    }
}
