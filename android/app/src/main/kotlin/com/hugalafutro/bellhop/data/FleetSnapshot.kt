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
    // Fleet-wide: Front Desk's auto-sync-stale flag (auto-sync off and the fleet
    // unsynced for over a day). Persisted alongside member health so the same
    // snapshot-diff that pages on health edges also notifies on drift onset.
    val autosyncStale: Boolean = false,
) {
    /** stateOf returns the stored state for a member, or null if it wasn't seen. */
    fun stateOf(id: String): MemberHealthState? =
        states[id]?.let { name -> runCatching { MemberHealthState.valueOf(name) }.getOrNull() }

    companion object {
        fun of(
            members: List<FleetMember>,
            autosyncStale: Boolean = false,
        ): FleetSnapshot =
            FleetSnapshot(
                states = members.associate { it.id to healthStateOf(it).name },
                autosyncStale = autosyncStale,
            )
    }
}

/**
 * FleetAlert is one thing worth a background notification, the unit the notifier
 * renders. It spans member health edges ([MemberTransition]) and the fleet-wide
 * auto-sync drift signal ([AutoSyncAlert]), so a single diff pass can hand both to
 * the same notify path.
 */
sealed interface FleetAlert

/**
 * MemberTransition is a health edge worth a notification. Only the two edges that
 * matter for a glance are modeled: a member that went down, and one that
 * recovered. Drain/activate is an operator action (not an alert), and moves
 * to/from UNKNOWN are noise (a reconnecting poller), so neither is a transition.
 */
sealed interface MemberTransition : FleetAlert {
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
 * AutoSyncAlert is the fleet-wide config-drift edge: [WentStale] when Front Desk
 * flips its auto-sync-stale flag on (auto-sync off and unsynced for over a day),
 * [Resumed] when it clears. It mirrors [MemberTransition]'s down/recovered shape
 * for a single boolean instead of per-member health.
 */
sealed interface AutoSyncAlert : FleetAlert {
    data object WentStale : AutoSyncAlert

    data object Resumed : AutoSyncAlert
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

/**
 * FrontDeskEvent is one Front Desk control-plane event the operator asked to be
 * told about: its type is switched on in Front Desk's alert picker (the toggles
 * Bellhop's Alerts screen edits). Unlike [MemberTransition], which Bellhop derives
 * itself from a health diff, this is Front Desk's own sentence about something
 * that happened, carried whole: a drain, a held sync, a stale backup, whatever
 * the picker has on. [memberId] is empty for fleet-wide events.
 */
data class FrontDeskEvent(
    val id: String,
    val type: String,
    val severity: String,
    val message: String,
    val memberId: String,
    val createdAt: String,
) : FleetAlert

/**
 * EventCursor marks the newest Front Desk event a poll has already seen, so the
 * next poll notifies only on what arrived after it. It is the event's own
 * created_at and id rather than a count or a clock: the event log is the truth
 * being followed, and a phone clock can drift from Front Desk's.
 */
@Serializable
data class EventCursor(
    val createdAt: String,
    val id: String,
)

/**
 * MAX_EVENT_ALERTS_PER_POLL caps how many Front Desk events one poll may turn
 * into notifications. A phone that was off for a day comes back to a page of
 * history; paging the operator once per line would bury the newest, which is
 * the one worth reading, so only the newest few are posted and the cursor still
 * advances past the rest: what Front Desk had to say six alerts ago is history
 * by then, and the event log in the app has all of it.
 */
const val MAX_EVENT_ALERTS_PER_POLL = 5

/**
 * EventDiff is what [diffEvents] hands back: the events to notify on, oldest
 * first so the newest ends up as the most recent row on the shade, and the
 * cursor the poll should persist afterwards.
 */
data class EventDiff(
    val alerts: List<FrontDeskEvent>,
    val cursor: EventCursor?,
)

/**
 * diffEvents is the pure decision for Front Desk's own alerts, the counterpart
 * of [diffFleet] for the event log: given the cursor the last poll persisted,
 * one page of the log (newest first, as GET /api/events returns it) and the set
 * of event types the operator switched on in the alert picker, return the
 * events to notify on and the cursor to persist.
 *
 * On the first ever poll ([cursor] is null) it stays silent and only records
 * the newest event, so a fresh opt-in does not replay history. A poll whose
 * page is empty keeps the cursor it had. Ordering is by created_at as an
 * instant, never as text (Front Desk writes RFC3339 with whatever fractional
 * precision the row has, so text order lies at whole seconds); at the same
 * instant the id decides, the way Front Desk's own `ORDER BY created_at, id`
 * does, so a row ordered before the cursor is never re-read as new. The cursor
 * only ever moves to a row whose time could be read, so one unreadable row
 * cannot blind the next poll to everything after it.
 */
fun diffEvents(
    cursor: EventCursor?,
    page: List<FdEvent>,
    enabled: Set<String>,
): EventDiff {
    val newest = page.firstOrNull { parseInstant(it.createdAt) != null } ?: return EventDiff(emptyList(), cursor)
    val next = EventCursor(newest.createdAt, newest.id)
    if (cursor == null) return EventDiff(emptyList(), next)
    val since = parseInstant(cursor.createdAt) ?: return EventDiff(emptyList(), next)
    val fresh =
        page.filter { ev ->
            val at = parseInstant(ev.createdAt) ?: return@filter false
            at.isAfter(since) || (at == since && ev.id > cursor.id)
        }
    val alerts =
        fresh
            .filter { it.type in enabled }
            .take(MAX_EVENT_ALERTS_PER_POLL)
            .map { FrontDeskEvent(it.id, it.type, it.severity, it.message, it.memberId, it.createdAt) }
            .asReversed()
    return EventDiff(alerts, next)
}

private fun parseInstant(raw: String): java.time.Instant? = runCatching { java.time.Instant.parse(raw) }.getOrNull()

/**
 * diffAutoSync is the fleet-wide counterpart to [diffFleet]: given the previously
 * persisted snapshot and the auto-sync-stale flag just read, return the drift edge
 * to notify on, or null if nothing crossed. Like diffFleet it stays silent on the
 * first ever poll ([previous] is null) so a phone that opens onto an
 * already-stale fleet doesn't buzz for a state it never saw change.
 */
fun diffAutoSync(
    previous: FleetSnapshot?,
    current: Boolean,
): AutoSyncAlert? {
    if (previous == null) return null
    return when {
        !previous.autosyncStale && current -> AutoSyncAlert.WentStale
        previous.autosyncStale && !current -> AutoSyncAlert.Resumed
        else -> null
    }
}
