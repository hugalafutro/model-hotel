package com.hugalafutro.bellhop.data

import kotlinx.serialization.Serializable

/**
 * MemberHealthState is the coarse health a background poll cares about: the four
 * states a member card can be in, collapsed from [FleetMember] so the diff only
 * has to compare enums. DRAINED and UNKNOWN are deliberately distinct from DOWN
 * so the diff never mistakes a drained member (an operator choice) or an
 * unprobed one (a fresh Front Desk start) for an outage.
 */
enum class MemberHealthState {
    UP,
    DOWN,
    DRAINED,
    UNKNOWN,
}

/**
 * healthStateOf collapses a [FleetMember] to the one state the backstop diffs on.
 * Drained wins over health (a drained member isn't "down"); an unprobed member is
 * UNKNOWN, not DOWN, so a cold Front Desk doesn't look like a fleet-wide outage.
 */
fun healthStateOf(member: FleetMember): MemberHealthState =
    when {
        member.drained -> MemberHealthState.DRAINED
        !member.status.health.known -> MemberHealthState.UNKNOWN
        member.status.health.healthy -> MemberHealthState.UP
        else -> MemberHealthState.DOWN
    }

/**
 * FleetSnapshot is the last-seen health of every member, persisted between
 * background polls so a stateless worker can tell what changed. States are stored
 * as the enum name so an unknown value from a future build degrades to "no prior
 * state" (see [stateOf]) rather than crashing the diff.
 */
@Serializable
data class FleetSnapshot(
    val states: Map<String, String> = emptyMap(),
) {
    /** stateOf returns the stored state for a member, or null if it wasn't seen. */
    fun stateOf(id: String): MemberHealthState? =
        states[id]?.let { name -> runCatching { MemberHealthState.valueOf(name) }.getOrNull() }

    companion object {
        fun of(members: List<FleetMember>): FleetSnapshot =
            FleetSnapshot(members.associate { it.id to healthStateOf(it).name })
    }
}

/**
 * MemberTransition is a health edge worth a notification. Only the two edges that
 * matter for a glance are modeled: a member that went down, and one that
 * recovered. Drain/activate is an operator action (not an alert), and moves
 * to/from UNKNOWN are noise (a reconnecting poller), so neither is a transition.
 */
sealed interface MemberTransition {
    val id: String
    val name: String

    data class WentDown(
        override val id: String,
        override val name: String,
    ) : MemberTransition

    data class Recovered(
        override val id: String,
        override val name: String,
    ) : MemberTransition
}

/**
 * diffFleet is the pure backstop decision: given the previously persisted
 * snapshot and the members just fetched, return the health edges to notify on.
 *
 * It alerts only on a real UP->DOWN or DOWN->UP edge. On the first ever poll
 * ([previous] is null) it stays silent so a fresh install doesn't buzz once per
 * member; a member with no prior state (newly added) is likewise skipped until it
 * has a baseline. Edges through DRAINED/UNKNOWN never alert, so an operator
 * draining a box or a poller briefly losing sight of one produces no false page.
 */
fun diffFleet(
    previous: FleetSnapshot?,
    current: List<FleetMember>,
): List<MemberTransition> {
    if (previous == null) return emptyList()
    val transitions = mutableListOf<MemberTransition>()
    for (member in current) {
        val was = previous.stateOf(member.id) ?: continue
        val now = healthStateOf(member)
        val label = member.name.ifBlank { member.id }
        when {
            was == MemberHealthState.UP && now == MemberHealthState.DOWN ->
                transitions += MemberTransition.WentDown(member.id, label)
            was == MemberHealthState.DOWN && now == MemberHealthState.UP ->
                transitions += MemberTransition.Recovered(member.id, label)
        }
    }
    return transitions
}
