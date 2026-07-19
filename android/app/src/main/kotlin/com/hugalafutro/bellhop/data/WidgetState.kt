package com.hugalafutro.bellhop.data

import kotlinx.serialization.Serializable

/**
 * WidgetMember is one row of the home-screen widget: the display name and the
 * member's [MemberHealthState] stored by enum name, so a value from a future
 * build degrades to UNKNOWN (same stance as [FleetSnapshot.stateOf]) instead of
 * crashing the render.
 */
@Serializable
data class WidgetMember(
    val name: String,
    val state: String,
) {
    val healthState: MemberHealthState
        get() = runCatching { MemberHealthState.valueOf(state) }.getOrDefault(MemberHealthState.UNKNOWN)
}

/** WidgetEvent is the fleet-wide newest event line, shown on the tall layout only. */
@Serializable
data class WidgetEvent(
    val message: String,
    val createdAt: String,
)

/**
 * WidgetState is the widget's whole persisted render model. It is written only
 * by code paths that already fetched the fleet (background poll, foreground
 * refresh, widget refresh tap) so the widget itself never needs the network;
 * [updatedAt] drives the honest "as of" stamp that makes lazy updates safe.
 */
@Serializable
data class WidgetState(
    val members: List<WidgetMember> = emptyList(),
    val autosyncStale: Boolean = false,
    val newestEvent: WidgetEvent? = null,
    val updatedAt: Long = 0L,
)

/** WidgetCounts is the collapsed fallback face for fleets too big for per-member rows. */
data class WidgetCounts(
    val up: Int,
    val down: Int,
    val drained: Int,
    val unknown: Int,
)

fun countsOf(state: WidgetState): WidgetCounts {
    val byState = state.members.groupingBy { it.healthState }.eachCount()
    return WidgetCounts(
        up = byState[MemberHealthState.UP] ?: 0,
        down = byState[MemberHealthState.DOWN] ?: 0,
        drained = byState[MemberHealthState.DRAINED] ?: 0,
        unknown = byState[MemberHealthState.UNKNOWN] ?: 0,
    )
}

/**
 * widgetStateOf collapses a fetched fleet into the widget's render model. The
 * newest event is the fleet-wide max over the inline per-member newest events;
 * Front Desk stamps created_at as RFC3339 UTC, which sorts lexicographically,
 * so a string max needs no parsing.
 */
fun widgetStateOf(
    members: List<FleetMember>,
    autosyncStale: Boolean,
    now: Long,
): WidgetState =
    WidgetState(
        members = members.map { WidgetMember(name = it.name.ifBlank { it.id }, state = healthStateOf(it).name) },
        autosyncStale = autosyncStale,
        newestEvent =
            members
                .mapNotNull { it.newestEvent }
                .maxByOrNull { it.createdAt }
                ?.let { WidgetEvent(it.message, it.createdAt) },
        updatedAt = now,
    )
