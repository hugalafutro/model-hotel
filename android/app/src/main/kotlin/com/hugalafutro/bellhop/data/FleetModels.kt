package com.hugalafutro.bellhop.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Wire models for the Front Desk read tier a device token can reach:
// GET /api/members (internal/frontdesk/server_members.go memberView) and
// GET /api/fleet/autosync. These mirror the FD contract exactly; do not rename
// JSON fields. Unknown keys are ignored, so FD may grow fields freely.

/**
 * FleetMember is one row of GET /api/members: the stored member plus the
 * poller's live view of it. Only the fields the dashboard renders are modeled.
 */
@Serializable
data class FleetMember(
    val id: String,
    val name: String = "",
    val url: String = "",
    val state: String = "active",
    @SerialName("has_token") val hasToken: Boolean = false,
    @SerialName("created_at") val createdAt: String = "",
    // Set only on a non-primary member Front Desk has actually synced; empty
    // otherwise. The detail screen surfaces both (when and why config was pushed).
    @SerialName("last_config_sync_at") val lastConfigSyncAt: String = "",
    @SerialName("last_config_sync_reason") val lastConfigSyncReason: String = "",
    val status: MemberStatus = MemberStatus(),
    // This member's newest event, attached inline by Front Desk so the card's
    // latest-event pill needs no per-member events fetch. Null when the member has
    // no events, and also when Front Desk predates the field (an older FD omits
    // it) — the dashboard falls back to a per-member fetch in that case.
    @SerialName("newest_event") val newestEvent: FdEvent? = null,
) {
    val drained: Boolean get() = state == "drained"
}

/** MemberStatus mirrors FD's poller MemberStatus (internal/frontdesk/poller.go). */
@Serializable
data class MemberStatus(
    val health: HealthStatus = HealthStatus(),
    @SerialName("traefik_status") val traefikStatus: String = "",
    val version: String = "",
    // The auto-syncer's "still in sync with the primary" heartbeat: advances
    // ~every tick while the member is reachable, distinct from lastConfigSyncAt
    // (which only moves on a real config write). Empty until first verified.
    @SerialName("auto_sync_verified_at") val autoSyncVerifiedAt: String = "",
)

/**
 * HealthStatus is the poller's view of a member's /health endpoint. known=false
 * means the member has not been probed yet (a fresh FD start), which the UI
 * renders as "not checked yet", not as down.
 */
@Serializable
data class HealthStatus(
    val known: Boolean = false,
    val healthy: Boolean = false,
    @SerialName("latency_ms") val latencyMs: Long = 0,
    @SerialName("checked_at") val checkedAt: String = "",
    val error: String = "",
)

/**
 * AutoSyncConfig is GET/PUT /api/fleet/autosync: the auto-sync toggle, the
 * designated primary member (empty when none is chosen), and Front Desk's
 * computed [stale] flag (auto-sync off and the fleet unsynced for over a day, so
 * the replicas may be drifting). The dashboard uses primaryId for the Primary
 * badge and enabled for the pause/unpause control; the background monitor reads
 * stale to raise a drift notification.
 */
@Serializable
data class AutoSyncConfig(
    val enabled: Boolean = false,
    @SerialName("primary_id") val primaryId: String = "",
    val stale: Boolean = false,
    // When a sync (manual or automatic) last actually wrote config to any
    // member; empty until one has. Member detail shows it under the fleet-sync
    // action so the operator sees when the fleet truly last synced.
    @SerialName("last_sync_at") val lastSyncAt: String = "",
)

/**
 * AutoSyncRequest is the PUT /api/fleet/autosync body (operator tier). Bellhop
 * only ever toggles [enabled] on the already-designated [primaryId] and so sends
 * an empty [confirmToken]: repointing or clearing the primary needs the raw Front
 * Desk admin token (which a phone never holds) and stays a web-only action, but
 * toggling an unchanged primary is applied without one. Choosing a primary is
 * deliberately not a phone capability.
 */
@Serializable
data class AutoSyncRequest(
    val enabled: Boolean,
    @SerialName("primary_id") val primaryId: String,
    @SerialName("confirm_token") val confirmToken: String = "",
)

/**
 * MemberTraffic is GET /api/members/{id}/traffic (internal/frontdesk/
 * membertraffic.go memberTrafficResponse): the member's last-hour request and
 * error series in 5-minute buckets, proxied by Front Desk from the member's
 * admin stats API. reachable=false is a normal state (FD has no admin token
 * for the member, or the member didn't answer), not an error.
 */
@Serializable
data class MemberTraffic(
    @SerialName("member_id") val memberId: String = "",
    val reachable: Boolean = false,
    @SerialName("window_minutes") val windowMinutes: Int = 60,
    @SerialName("total_requests") val totalRequests: Int = 0,
    @SerialName("total_errors") val totalErrors: Int = 0,
    val points: List<TrafficPoint> = emptyList(),
)

/** TrafficPoint is one time bucket: total requests and the error subset. */
@Serializable
data class TrafficPoint(
    val bucket: String = "",
    val requests: Int = 0,
    val errors: Int = 0,
)

/**
 * FdEvent is one stored control-plane event row of GET /api/events
 * (internal/frontdesk/store.go Event). Distinct from [FleetEvent]: the stored
 * row spells its time as created_at where the SSE envelope says timestamp.
 * Metadata is deliberately not modeled (same reasoning as [FleetEvent]).
 */
@Serializable
data class FdEvent(
    val id: String = "",
    val type: String = "",
    val severity: String = "",
    val source: String = "",
    val message: String = "",
    @SerialName("member_id") val memberId: String = "",
    @SerialName("created_at") val createdAt: String = "",
)

/**
 * EventsResponse is the GET /api/events envelope: one page of matching events
 * (newest first) plus the total match count for pagination. The list is
 * nullable because Go marshals an empty result as `"events": null`.
 */
@Serializable
data class EventsResponse(
    val events: List<FdEvent>? = null,
    val total: Int = 0,
)

/**
 * EventQuery mirrors the GET /api/events filter params (internal/frontdesk/
 * server_status.go listEvents). Empty/zero fields are omitted from the query
 * string and mean "no constraint"; the server clamps limit into [1, 500] and
 * defaults it to 100 when absent.
 */
data class EventQuery(
    val memberId: String = "",
    val type: String = "",
    val severity: String = "",
    // RFC3339; empty = no lower bound.
    val since: String = "",
    // RFC3339; empty = no upper bound. With [since] this carries the calendar
    // date-range filter.
    val until: String = "",
    val limit: Int = 0,
    val offset: Int = 0,
)

/**
 * FleetEvent is one control-plane event off the GET /api/sse stream, mirroring
 * the backend's events.Event envelope (internal/events/bus.go). The dashboard
 * only reads [type] (to decide whether the change warrants a member refetch);
 * the other fields are carried for the Events/Alerts screens in later slices.
 * Metadata is deliberately not modeled: kotlinx.serialization has no natural
 * map<String, Any> and nothing on screen needs it yet.
 */
@Serializable
data class FleetEvent(
    val id: String = "",
    val type: String = "",
    val severity: String = "",
    val source: String = "",
    val message: String = "",
    val timestamp: String = "",
)

/**
 * AlertStatus is the reachability of Front Desk's outbound notifier (GET
 * /api/alert/status), mirroring the backend's alert.Status. [configured] is
 * false when no apprise-api URL is set (nothing can deliver); when configured,
 * [reachable] then [healthy] narrow down where a green pill turns amber or red,
 * and [detail] carries the human reason ("no notification target configured",
 * "master key rotated?") so Bellhop shows a cause, not just a colour.
 */
@Serializable
data class AlertStatus(
    val configured: Boolean = false,
    val reachable: Boolean = false,
    val healthy: Boolean = false,
    val detail: String = "",
)

/**
 * AlertEventDef is one row of Front Desk's alertable-event catalog, mirroring
 * alert.EventDef. Bellhop reads it from GET /api/alert/selection, which enriches
 * the catalog with [enabled] — whether Front Desk currently alerts on this event.
 * An operator device flips [enabled] via POST /api/alert/selection; a monitor
 * sees it read-only. [defaultOn] is the first-run seed (a reference, not the live
 * state); [severity] is the display dot; [category] groups the list.
 */
@Serializable
data class AlertEventDef(
    val type: String = "",
    val category: String = "",
    val severity: String = "",
    val defaultOn: Boolean = false,
    val enabled: Boolean = false,
)

/**
 * AlertSelectionResponse is the GET/POST /api/alert/selection envelope: the
 * catalog enriched with each event's current [AlertEventDef.enabled] state.
 * Mirrors the Front Desk handler, which returns {"events": [...]}.
 */
@Serializable
data class AlertSelectionResponse(
    val events: List<AlertEventDef> = emptyList(),
)

/**
 * AlertSelectionRequest is the POST /api/alert/selection body: flip one event
 * [type] on or off. A per-event toggle (not a full-set replace) is atomic on
 * Front Desk and version-skew safe, so a dropped request never leaves the
 * selection half-applied. Do not rename JSON fields.
 */
@Serializable
data class AlertSelectionRequest(
    val type: String,
    val enabled: Boolean,
)

// Wire models for the Front Desk operator tier a device token with the operator
// role can reach: POST /api/members/{id}/state (drain/activate) and POST
// /api/config/sync (propagate the primary's config). These mirror the FD
// contract (internal/frontdesk/server_members.go setMemberState and
// internal/frontdesk/configsync.go configSync); do not rename JSON fields.

/**
 * MemberStateRequest is the POST /api/members/{id}/state body. [state] is
 * "active" or "drained" (see [MemberState]); Front Desk records it and returns
 * the updated member. Set-state, not toggle, so a retry or double-tap is a safe
 * no-op.
 */
@Serializable
data class MemberStateRequest(
    val state: String,
)

/**
 * MemberState is the two states a member can be set to. Drained excludes a
 * member from new traffic while in-flight streams finish (the physical drain is
 * asynchronous; Front Desk records the intent immediately). Kept as the exact
 * strings Front Desk stores so they round-trip through the wire untouched.
 */
object MemberState {
    const val ACTIVE = "active"
    const val DRAINED = "drained"
}

/**
 * ConfigSyncRequest is the POST /api/config/sync body: the id of the primary
 * whose config is propagated to the rest of the fleet. Bellhop only ever sends
 * the already-designated auto-sync primary (choosing a primary is a separate,
 * later slice), so this is a "sync now from primary" trigger, not a wizard.
 */
@Serializable
data class ConfigSyncRequest(
    @SerialName("primary_id") val primaryId: String,
)

/**
 * SyncResponse is the POST /api/config/sync result: the source primary and the
 * per-member outcomes. Front Desk runs the whole sync before answering 200, so a
 * success here means the run finished; the phone summarizes [results] rather than
 * blocking on physical convergence, which the dashboard reconciles afterwards.
 */
@Serializable
data class SyncResponse(
    @SerialName("primary_id") val primaryId: String = "",
    val results: List<SyncResultItem> = emptyList(),
)

/** SyncResultItem is one member's config-sync outcome (frontdesk syncResultItem). */
@Serializable
data class SyncResultItem(
    @SerialName("member_id") val memberId: String = "",
    val name: String = "",
    val ok: Boolean = false,
    val error: String = "",
)
