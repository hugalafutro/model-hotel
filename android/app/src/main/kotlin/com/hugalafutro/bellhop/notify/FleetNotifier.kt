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
    }

    /** notify posts one health-edge notification, or does nothing if it can't. */
    fun notify(
        context: Context,
        transition: MemberTransition,
    ) {
        if (!canPost(context)) return
        ensureChannels(context)

        val (channel, title) =
            when (transition) {
                is MemberTransition.WentDown ->
                    CHANNEL_DOWN to context.getString(R.string.notif_down_title, transition.name)
                is MemberTransition.Recovered ->
                    CHANNEL_UP to context.getString(R.string.notif_up_title, transition.name)
            }
        val body =
            when (transition) {
                is MemberTransition.WentDown -> context.getString(R.string.notif_down_body)
                is MemberTransition.Recovered -> context.getString(R.string.notif_up_body)
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

        // Keyed on the member id so a member flapping down->up->down updates its
        // one notification in place rather than stacking a fresh row each poll.
        // canPost checked the permission, but it can be revoked between that check
        // and here, so swallow the resulting SecurityException rather than crash a
        // background worker over a lost notification.
        try {
            NotificationManagerCompat.from(context).notify(notificationId(transition.id), notification)
        } catch (_: SecurityException) {
        }
    }

    // Deep-linking to the specific member's detail is a later slice; for now the
    // tap just brings Bellhop to the front on its current screen.
    private fun openAppIntent(context: Context): android.app.PendingIntent {
        val intent =
            Intent(context, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP
            }
        return android.app.PendingIntent.getActivity(
            context,
            0,
            intent,
            android.app.PendingIntent.FLAG_IMMUTABLE or android.app.PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }

    private fun notificationId(memberId: String): Int = memberId.hashCode()

    // POST_NOTIFICATIONS is a runtime permission from API 33; below that a channel
    // notification always posts. Guarding here keeps the worker unconditional.
    private fun canPost(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
}
