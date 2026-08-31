package com.hugalafutro.bellhop.notify

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.hugalafutro.bellhop.MainActivity
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.AutoSyncAlert
import com.hugalafutro.bellhop.data.FleetAlert
import com.hugalafutro.bellhop.data.FrontDeskEvent
import com.hugalafutro.bellhop.data.MemberTransition
import com.hugalafutro.bellhop.ui.common.eventTypeLabelRes

/**
 * FleetNotifier renders the background backstop's alerts as local notifications
 * (plan section 5.2): Bellhop's own fleet-health edges, the auto-sync drift edge,
 * and the Front Desk events the operator switched on in the alert picker. The
 * last are Front Desk's own sentence about what happened, titled with the same
 * event name the Alerts screen shows; this exists so a tap lands the operator
 * back in Bellhop, and so a phone with no real-time layer still learns "a member
 * went down" within a poll or two.
 *
 * Channels split by severity so Android's per-channel muting works: "member
 * down" is high importance (it may page) and also carries error-severity Front
 * Desk events, "member recovered" is low (a quiet status update) and carries
 * info and success events, and "Front Desk alerts" is default importance for
 * warnings. Posting is a no-op when the POST_NOTIFICATIONS permission is
 * absent, so the worker never has to guard the call itself.
 *
 * [notifyPushTest] is the one row that does not come from fleet state: it answers
 * Front Desk's test push, which a healthy fleet would otherwise leave invisible.
 */
object FleetNotifier {
    const val CHANNEL_DOWN = "member_down"
    const val CHANNEL_UP = "member_up"
    const val CHANNEL_STALE = "config_stale"
    const val CHANNEL_EVENTS = "frontdesk_events"

    // A constant numeric id: the member id is carried as the notification tag
    // instead, so two members whose ids collide under String.hashCode() (an int id
    // would fold them onto one row and drop an alert) still get separate rows.
    private const val NOTIFICATION_ID = 1

    // The auto-sync drift alert is fleet-wide, not per-member, so it uses one fixed
    // tag: WentStale and Resumed share it, so the resume updates the stale row in
    // place instead of stacking a second notification.
    private const val AUTOSYNC_TAG = "autosync-stale"

    // The push test is also fleet-wide and repeatable, so one fixed tag keeps a
    // second test from stacking a second row.
    private const val PUSH_TEST_TAG = "push_test"

    // What the test title says when the payload carries no readable sender. It is
    // a product name rather than a translated string, and it is the only thing a
    // paired Bellhop can be receiving a test from.
    private const val DEFAULT_TEST_SENDER = "Front Desk"

    /**
     * ensureChannels registers both notification channels. Safe to call
     * repeatedly (createNotificationChannel is idempotent) and cheap, so it runs
     * at app start and again defensively before each post.
     */
    fun ensureChannels(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_DOWN,
                context.getString(R.string.notif_channel_down),
                NotificationManager.IMPORTANCE_HIGH,
            ),
        )
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_UP,
                context.getString(R.string.notif_channel_up),
                NotificationManager.IMPORTANCE_LOW,
            ),
        )
        // The drift warning is a nudge, not a page: default importance so it shows
        // and can chime, but never heads-up like a member going down.
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_STALE,
                context.getString(R.string.notif_channel_stale),
                NotificationManager.IMPORTANCE_DEFAULT,
            ),
        )
        // Front Desk's warning-severity events (a drain, a held sync, a stale
        // backup): the operator asked for each of these by name, so they show and
        // can chime, but a warning is not an outage and never heads-up.
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_EVENTS,
                context.getString(R.string.notif_channel_events),
                NotificationManager.IMPORTANCE_DEFAULT,
            ),
        )
    }

    /** notify posts one fleet-alert notification, or does nothing if it can't. */
    fun notify(
        context: Context,
        alert: FleetAlert,
    ) {
        if (!canPost(context)) return
        ensureChannels(context)

        // Each alert maps to a channel (which drives importance/muting), a title +
        // body, and a tag. Member alerts tag by member id so distinct members never
        // share a row and one flapping member updates in place; the fleet-wide
        // drift alert uses one fixed tag so its resume replaces its stale row.
        val (channel, title, body) =
            when (alert) {
                is MemberTransition.WentDown ->
                    Triple(
                        CHANNEL_DOWN,
                        context.getString(R.string.notif_down_title, alert.name),
                        context.getString(R.string.notif_down_body),
                    )
                is MemberTransition.Recovered ->
                    Triple(
                        CHANNEL_UP,
                        context.getString(R.string.notif_up_title, alert.name),
                        context.getString(R.string.notif_up_body),
                    )
                AutoSyncAlert.WentStale ->
                    Triple(
                        CHANNEL_STALE,
                        context.getString(R.string.notif_stale_title),
                        context.getString(R.string.notif_stale_body),
                    )
                AutoSyncAlert.Resumed ->
                    Triple(
                        CHANNEL_STALE,
                        context.getString(R.string.notif_stale_resumed_title),
                        context.getString(R.string.notif_stale_resumed_body),
                    )
                // Front Desk's own sentence, under the event's catalogue name. The
                // message is composed server-side and shown whole: it is what the
                // operator would read in the Front Desk event log, and the one thing
                // a push could not carry.
                is FrontDeskEvent ->
                    Triple(
                        channelForSeverity(alert.severity),
                        eventTypeLabelRes(alert.type)?.let(context::getString) ?: alert.type,
                        alert.message,
                    )
            }
        // A Front Desk event tags by type and member, so the same kind of event
        // about the same member updates its row in place (a drain followed by the
        // re-activation reads as one row saying the latest) while distinct members
        // and distinct kinds keep their own rows.
        val tag =
            when (alert) {
                is MemberTransition -> alert.id
                is AutoSyncAlert -> AUTOSYNC_TAG
                is FrontDeskEvent -> "event:${alert.type}:${alert.memberId}"
            }

        val notification =
            NotificationCompat
                .Builder(context, channel)
                .setSmallIcon(R.drawable.ic_stat_bellhop)
                .setContentTitle(title)
                .setContentText(body)
                .setStyle(NotificationCompat.BigTextStyle().bigText(body))
                .setContentIntent(openAppIntent(context))
                .setAutoCancel(true)
                .setCategory(NotificationCompat.CATEGORY_STATUS)
                .build()

        // canPost checked the permission, but it can be revoked between that check
        // and here, so swallow the resulting SecurityException rather than crash a
        // background worker over a lost notification.
        try {
            NotificationManagerCompat.from(context).notify(tag, NOTIFICATION_ID, notification)
        } catch (_: SecurityException) {
        }
    }

    /**
     * channelForSeverity maps a Front Desk event's severity onto the channels
     * above, so an error pages like a member going down, a warning shows without
     * heads-up, and an info or success line stays quiet.
     */
    private fun channelForSeverity(severity: String): String =
        when (severity) {
            "error" -> CHANNEL_DOWN
            "warning" -> CHANNEL_EVENTS
            else -> CHANNEL_UP
        }

    /**
     * notifyPushTest acknowledges Front Desk's "Send test" push. It rides the quiet
     * recovered channel because a test is a status update, not a page, and it exists
     * because the wake poll it accompanies finds a healthy fleet and so reports
     * nothing: without this row the operator cannot tell a working push pipeline
     * from a dead one. Real alerts never take this path.
     *
     * [sender] is the name the sending gateway gave itself, so a phone linked to
     * one Front Desk while another operator tests a second one can see which
     * pipeline just proved itself. Null when the payload carried no readable
     * name, which falls back to [DEFAULT_TEST_SENDER].
     */
    fun notifyPushTest(
        context: Context,
        sender: String? = null,
    ) {
        if (!canPost(context)) return
        ensureChannels(context)

        val notification =
            NotificationCompat
                .Builder(context, CHANNEL_UP)
                .setSmallIcon(R.drawable.ic_stat_bellhop)
                .setContentTitle(
                    context.getString(R.string.notif_push_test_title, sender ?: DEFAULT_TEST_SENDER),
                ).setContentText(context.getString(R.string.notif_push_test_body))
                .setStyle(
                    NotificationCompat.BigTextStyle().bigText(context.getString(R.string.notif_push_test_body)),
                ).setContentIntent(openAppIntent(context))
                .setAutoCancel(true)
                .setCategory(NotificationCompat.CATEGORY_STATUS)
                .build()

        // Same race as notify: the permission can be revoked between canPost and
        // here, and a lost test notification must not crash the push callback.
        try {
            NotificationManagerCompat.from(context).notify(PUSH_TEST_TAG, NOTIFICATION_ID, notification)
        } catch (_: SecurityException) {
        }
    }

    // Deep-linking to the specific member's detail is a later slice; for now the
    // tap just brings Bellhop to the front on its current screen.
    private fun openAppIntent(context: Context): android.app.PendingIntent {
        // Explicit target (component + package) so this can only ever launch our
        // own activity, and immutable so a holder can't rewrite it: an implicit or
        // mutable PendingIntent could be hijacked by another app (CWE-927).
        val intent = Intent(context, MainActivity::class.java)
        intent.setPackage(context.packageName)
        intent.flags = Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP
        return android.app.PendingIntent.getActivity(
            context,
            0,
            intent,
            android.app.PendingIntent.FLAG_IMMUTABLE or android.app.PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }

    // POST_NOTIFICATIONS is a runtime permission from API 33; below that a channel
    // notification always posts. Exposed so the worker can skip a poll it could
    // never deliver rather than silently advance its baseline.
    fun canPost(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
}
