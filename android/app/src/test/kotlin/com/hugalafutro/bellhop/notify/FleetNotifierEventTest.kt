package com.hugalafutro.bellhop.notify

import android.Manifest
import android.app.Application
import android.app.Notification
import android.app.NotificationManager
import com.hugalafutro.bellhop.R
import com.hugalafutro.bellhop.data.FrontDeskEvent
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * A Front Desk event renders under the event's catalogue name with Front Desk's
 * own sentence as the body, on one of two channels by severity, and tagged by
 * type, member and severity so a repeat about the same member at the same
 * severity updates in place while distinct members, kinds and severities keep
 * their own rows.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FleetNotifierEventTest {
    private val app: Application = RuntimeEnvironment.getApplication()
    private val notifications: NotificationManager
        get() = app.getSystemService(NotificationManager::class.java)

    private fun drained(
        member: String,
        id: String = "e-$member",
    ): FrontDeskEvent =
        FrontDeskEvent(
            id = id,
            type = "member.state_changed",
            severity = "warning",
            message = "MH $member set to drained",
            memberId = member,
            createdAt = "2026-08-31T11:28:54Z",
        )

    @Test
    fun eventPostsUnderItsCatalogueNameWithFrontDesksSentence() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, drained("docker-pc"))

        val posted = notifications.activeNotifications.single()
        val extras = posted.notification.extras
        assertEquals(
            app.getString(R.string.alerts_event_member_state_changed),
            extras.getString(Notification.EXTRA_TITLE),
        )
        assertEquals("MH docker-pc set to drained", extras.getString(Notification.EXTRA_TEXT))
        assertEquals(FleetNotifier.CHANNEL_EVENTS, posted.notification.channelId)
    }

    @Test
    fun onlyAnErrorRidesTheErrorsChannel() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, drained("a").copy(type = "health.down", severity = "error"))
        FleetNotifier.notify(app, drained("b").copy(type = "health.up", severity = "success"))
        FleetNotifier.notify(app, drained("c").copy(type = "member.added", severity = "info"))

        val channels = notifications.activeNotifications.map { it.tag to it.notification.channelId }.toMap()
        assertEquals(FleetNotifier.CHANNEL_ERRORS, channels["event:health.down:a:error"])
        assertEquals(FleetNotifier.CHANNEL_EVENTS, channels["event:health.up:b:success"])
        assertEquals(FleetNotifier.CHANNEL_EVENTS, channels["event:member.added:c:info"])
    }

    @Test
    fun aDrainAndItsReactivationAreTwoRowsWhileARepeatedDrainIsOne() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, drained("docker-pc", id = "e1"))
        FleetNotifier.notify(app, drained("docker-pc", id = "e2"))
        FleetNotifier.notify(
            app,
            drained("docker-pc", id = "e3").copy(severity = "info", message = "MH docker-pc set to active"),
        )
        FleetNotifier.notify(app, drained("truenas"))

        // docker-pc drained (updated in place), docker-pc active, truenas drained.
        assertEquals(3, shadowOf(notifications).size())
        val texts = notifications.activeNotifications.map { it.notification.extras.getString(Notification.EXTRA_TEXT) }
        assertEquals(
            setOf("MH docker-pc set to drained", "MH docker-pc set to active", "MH truenas set to drained"),
            texts.toSet(),
        )
    }

    @Test
    fun fleetWideEventsOfDifferentKindsKeepTheirOwnRows() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, drained("").copy(type = "config.autosync_stale"))
        FleetNotifier.notify(app, drained("").copy(type = "backup.stale"))

        assertEquals(2, shadowOf(notifications).size())
    }

    @Test
    fun anUnknownTypeFallsBackToItsRawCodeAsTheTitle() {
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        FleetNotifier.notify(app, drained("m").copy(type = "future.thing"))

        val title =
            notifications.activeNotifications
                .single()
                .notification.extras
                .getString(Notification.EXTRA_TITLE)
        assertEquals("future.thing", title)
    }
}
