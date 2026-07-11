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
    val status: MemberStatus = MemberStatus(),
) {
    val drained: Boolean get() = state == "drained"
}

/** MemberStatus mirrors FD's poller MemberStatus (internal/frontdesk/poller.go). */
@Serializable
data class MemberStatus(
    val health: HealthStatus = HealthStatus(),
    @SerialName("traefik_status") val traefikStatus: String = "",
    val version: String = "",
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
 * AutoSyncConfig is GET /api/fleet/autosync: the auto-sync toggle plus the
 * designated primary member (empty when none is chosen). The dashboard only
 * uses primaryId, for the Primary badge.
 */
@Serializable
data class AutoSyncConfig(
    val enabled: Boolean = false,
    @SerialName("primary_id") val primaryId: String = "",
)
