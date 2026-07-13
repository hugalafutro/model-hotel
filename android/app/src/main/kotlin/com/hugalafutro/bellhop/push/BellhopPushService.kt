package com.hugalafutro.bellhop.push

import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.work.FleetPollWorker
import kotlinx.coroutines.runBlocking
import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

/**
 * BellhopPushService is the Layer-3 real-time wake (plan section 5.2): the
 * UnifiedPush entry point a distributor (ntfy) delivers to when Front Desk's
 * Apprise pipeline pushes to Bellhop's topic. It is opt-in and Google-free — no
 * FCM, no google-services.json — the distributor holds the persistent socket.
 *
 * The push is deliberately treated as a bare wake trigger, not a data source: on a
 * message it re-runs the same backstop poll Layer 2 uses ([FleetPollWorker.runNow])
 * so Bellhop's notification always reflects current Front Desk truth rather than a
 * payload that may be stale, encrypted, or shaped by whatever Apprise sent. That
 * also means Bellhop never becomes a second, redundant alert source: it renders the
 * same fleet state, just woken sooner than the 15-minute periodic floor.
 *
 * The registration is thin on purpose (the testable pieces are [MonitorStore] and
 * [FleetPollWorker.runNow], exercised on their own); this shell only wires the
 * connector callbacks to them. runBlocking is safe here: the endpoint writes are
 * sub-millisecond DataStore edits and the callback must finish before it returns.
 */
class BellhopPushService : PushService() {
    override fun onNewEndpoint(
        endpoint: PushEndpoint,
        instance: String,
    ) {
        // Persist the distributor's topic URL so Settings can show it for the user
        // to point Front Desk's Apprise phone-topic at. Passing the callback's
        // instance lets the store reject a late endpoint from a superseded
        // registration instead of displaying a topic that's no longer routed.
        runBlocking { MonitorStore.create(applicationContext).saveEndpoint(endpoint.url, instance) }
    }

    override fun onMessage(
        message: PushMessage,
        instance: String,
    ) {
        // Payload ignored on purpose (see class doc): the poll re-derives truth.
        FleetPollWorker.runNow(applicationContext)
    }

    override fun onRegistrationFailed(
        reason: FailedReason,
        instance: String,
    ) {
        // No usable endpoint: drop any stale one so Settings stops advertising a
        // topic that can't deliver. The user re-picks a distributor from Settings.
        // Gated on the callback's instance so a failure for a superseded
        // registration can't wipe a newer registration's live endpoint.
        runBlocking { MonitorStore.create(applicationContext).clearEndpoint(instance) }
    }

    override fun onUnregistered(instance: String) {
        runBlocking { MonitorStore.create(applicationContext).clearEndpoint(instance) }
    }
}
