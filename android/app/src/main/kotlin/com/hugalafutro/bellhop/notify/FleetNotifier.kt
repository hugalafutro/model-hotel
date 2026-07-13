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
import com.hugalafutro.bellhop.data.MemberTransition

/**
 * FleetNotifier renders the background backstop's fleet-health edges as local
 * notifications (plan section 5.2). It is deliberately not an alert *source*:
 * Front Desk's own Apprise pipeline already pages on the same events; this exists
 * so a tap lands the operator back in Bellhop, and so a phone with no real-time
 * layer still learns "a member went down" within a poll or two.
 *
 * Two channels split by severity so Android's per-channel muting works: "member
 * down" is high importance (it may page), "member recovered" is low (a quiet
 * status update). Posting is a no-op when the POST_NOTIFICATIONS permission is
 * absent, so the worker never has to guard the call itself.
 */
object FleetNotifier {
    const val CHANNEL_DOWN = "member_down"
    const val CHANNEL_UP = "member_up"
    const val CHANNEL_STALE = "config_stale"

    // A constant numeric id: the member id is carried as the notification tag
    // instead, so two members whose ids collide under String.hashCode() (an int id
    // would fold them onto one row and drop an alert) still get separate rows.
    private const val NOTIFICATION_ID = 1

    // The auto-sync drift alert is fleet-wide, not per-member, so it uses one fixed
    // tag: WentStale and Resumed share it, so the resume updates the stale row in
    // place instead of stacking a second notification.
    private const val AUTOSYNC_TAG = "autosync-stale"

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
            }
        val tag =
            when (alert) {
                is MemberTransition -> alert.id
                is AutoSyncAlert -> AUTOSYNC_TAG
            }

        val notification =
            NotificationCompat
                .Builder(context, channel)
                .setSmallIcon(R.drawable.ic_stat_bellhop)
                .setContentTitle(title)
                .setContentText(body)
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
