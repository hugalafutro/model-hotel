package com.hugalafutro.bellhop.work

import android.app.Application
import androidx.work.Configuration
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.testing.SynchronousExecutor
import androidx.work.testing.WorkManagerTestInitHelper
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

/**
 * The widget's one-shot refresh coalesces under a unique name, and its two callers
 * need opposite answers from that. These pin the difference through WorkManager
 * itself rather than through the flag: the point is what WorkManager DOES with
 * each policy, which is the part a reader would otherwise have to take on trust.
 *
 * Work here never runs -- both requests carry a CONNECTED constraint and the test
 * driver leaves constraints unmet -- so what is measured is the enqueued state,
 * which is exactly where a dropped refresh is lost.
 */
@RunWith(RobolectricTestRunner::class)
class WidgetRefreshCoalescingTest {
    private val app: Application = RuntimeEnvironment.getApplication()

    @Before
    fun setUp() {
        WorkManagerTestInitHelper.initializeTestWorkManager(
            app,
            Configuration.Builder().setExecutor(SynchronousExecutor()).build(),
        )
    }

    // WorkManager tags every request with its worker's class name, and these tests
    // enqueue nothing but widget refreshes, so the class tag finds them without
    // widening the unique name's visibility for a test's sake.
    private fun pending(): List<WorkInfo> =
        WorkManager
            .getInstance(app)
            .getWorkInfosByTag(FleetPollWorker::class.java.name)
            .get()
            .filter { it.state == WorkInfo.State.ENQUEUED || it.state == WorkInfo.State.RUNNING }

    @Test
    fun refreshButtonTapsCoalesceOntoOneRequest() {
        // Every tap asks for the same thing, so a burst must not fan out into one
        // network call per tap.
        FleetPollWorker.runWidgetRefresh(app)
        val first = pending().single().id

        FleetPollWorker.runWidgetRefresh(app)
        FleetPollWorker.runWidgetRefresh(app)

        val still = pending()
        assertEquals(1, still.size)
        assertEquals("the first request must be the one that survives", first, still.single().id)
    }

    @Test
    fun aSettingsToggleSupersedesTheRefreshAlreadyQueued() {
        // A queued run was configured by the preference values as they were before
        // the toggle wrote, so coalescing onto it would drop the refresh the toggle
        // asked for and leave the widget on its old contents.
        FleetPollWorker.runWidgetRefresh(app)
        val stale = pending().single().id

        FleetPollWorker.runWidgetRefresh(app, supersedeInFlight = true)

        val fresh = pending()
        assertEquals(1, fresh.size)
        assertTrue("the superseded request must not be the one left queued", fresh.single().id != stale)
    }

    @Test
    fun supersedingLeavesExactlyOneRefreshQueued() {
        // Toggling both widget switches in a row must not stack refreshes either:
        // REPLACE displaces, it does not append.
        FleetPollWorker.runWidgetRefresh(app, supersedeInFlight = true)
        FleetPollWorker.runWidgetRefresh(app, supersedeInFlight = true)
        FleetPollWorker.runWidgetRefresh(app, supersedeInFlight = true)

        assertEquals(1, pending().size)
    }
}
