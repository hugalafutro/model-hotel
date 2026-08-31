package com.hugalafutro.bellhop.push

import com.hugalafutro.bellhop.data.MonitorStore
import com.hugalafutro.bellhop.notify.FleetNotifier
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
 * The push is a wake trigger, not a data source: on a message it re-runs the
 * same backstop poll Layer 2 uses ([FleetPollWorker.runNow]), which re-reads the
 * fleet AND the Front Desk event log, so the alert the push announced is rendered
 * from Front Desk's own record of it rather than from a payload that may be
 * stale, encrypted, or shaped by whatever Apprise sent (the distributor hands
 * over the message body alone, with no event type or severity). The Front Desk
 * events that get rendered are the types switched on in Front Desk's alert
 * picker, the toggles under Alerts, because those are the events Front Desk
 * pushes for; Bellhop's own health and drift alerts stay on beside them, and
 * the poll drops its own row when Front Desk's event for the same thing is
 * being posted.
 *
 * The payload is inspected for exactly one thing: Front Desk's test marker
 * ([BellhopPush.isTestPush]). A test is not in the event log, so it would
 * otherwise be invisible on the phone, leaving the operator unable to tell a
 * working pipeline from a broken one; a matching payload therefore posts a "push
 * test received" notification. The wake poll runs either way.
 *
 * The registration is thin on purpose (the testable pieces are [MonitorStore],
 * [BellhopPush.isTestPush] and [FleetPollWorker.runNow], exercised on their own);
 * this shell only wires the connector callbacks to them. runBlocking is safe here:
 * the endpoint writes are sub-millisecond DataStore edits and the callback must
 * finish before it returns.
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
        // The payload is read only for Front Desk's test marker; the poll reads
        // every real alert back from Front Desk's event log (see class doc).
        if (BellhopPush.isTestPush(message.content)) {
            FleetNotifier.notifyPushTest(applicationContext, BellhopPush.testPushSender(message.content))
        }
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
