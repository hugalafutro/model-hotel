package com.hugalafutro.bellhop.notify

import android.Manifest
import android.app.Application
import android.app.NotificationManager
import com.hugalafutro.bellhop.data.AutoSyncAlert
import com.hugalafutro.bellhop.data.MemberTransition
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * Posting behaviour: alerts stay distinct per member, and nothing posts without
 * the runtime permission. Pinned to API 34 so the POST_NOTIFICATIONS check is the
 * one that runs (below 33 it's granted at install and the deny case is moot).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FleetNotifierTest {
    private val app: Application = RuntimeEnvironment.getApplication()
    private val notifications: NotificationManager
        get() = app.getSystemService(NotificationManager::class.java)

    @Test
    fun membersWithCollidingIdHashesEachGetTheirOwnNotification() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        // "Aa" and "BB" share a String.hashCode() (both 2112); a bare int id would
        // fold them onto one row and drop an alert. The member-id tag keeps them
        // apart, so both survive.
        FleetNotifier.notify(app, MemberTransition.WentDown("Aa", "Alpha"))
        FleetNotifier.notify(app, MemberTransition.WentDown("BB", "Bravo"))
        assertEquals(2, shadowOf(notifications).size())
    }

    @Test
    fun nothingIsPostedWithoutTheNotificationPermission() {
        shadowOf(app).denyPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, MemberTransition.WentDown("m1", "One"))
        assertEquals(0, shadowOf(notifications).size())
    }

    @Test
    fun autoSyncAlertPostsOnTheStaleChannel() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, AutoSyncAlert.WentStale)
        val posted = shadowOf(notifications).size()
        assertEquals(1, posted)
        assertTrue(
            shadowOf(notifications).allNotifications.any { it.channelId == FleetNotifier.CHANNEL_STALE },
        )
    }

    @Test
    fun autoSyncResumeReplacesTheStaleRowInPlace() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        // Stale then Resumed share one fixed tag, so the resume updates the row
        // rather than stacking a second notification.
        FleetNotifier.notify(app, AutoSyncAlert.WentStale)
        FleetNotifier.notify(app, AutoSyncAlert.Resumed)
        assertEquals(1, shadowOf(notifications).size())
    }
}
