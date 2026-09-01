import type {
	CircuitBreakerProviderStatus,
	CircuitStatus,
} from "../../api/types";

/**
 * The circuit-breaker row to render on one failover entry, or undefined when the
 * breaker is not turning that entry away.
 *
 * Circuits are keyed (provider, resolved upstream model) and the status endpoint
 * reports one row per provider, built from its most degraded circuit. So a row
 * saying "open" means *some* model of that provider is dark, not necessarily
 * this one, and painting every entry of the provider from it is exactly the
 * provider-wide darkening the per-model keying exists to end.
 *
 * An entry is turned away when the derived provider verdict is open (the breaker
 * skips the provider for every model, whether from a quota pin or from enough
 * models corroborating) or when its own model id is one the breaker is blocking.
 * A row owed a probe ("half-open") names no model at all, since open_models
 * carries only circuits that are still blocking, so it stays on every entry of
 * the provider, where it reads as recovering rather than as down.
 */
export function entryCircuitStatus(
	row: CircuitBreakerProviderStatus | undefined,
	modelId: string,
): CircuitBreakerProviderStatus | undefined {
	if (!row || row.state === "closed") return undefined;
	if (row.state === "half-open" || row.provider_open) return row;
	return row.open_models?.includes(modelId) ? row : undefined;
}

/** The state chip an entry shows. */
export type EntryChip = "live" | "busy" | "open" | "probe" | "pinned";

/**
 * How recently a saturated 429 must have landed on a closed circuit for the
 * entry to read as busy rather than live. Matches the breaker's behavioural
 * window (rate_limit_recent_success_window default): a provider that said
 * "busy" a minute ago is a routing fact, one that said it an hour ago is not.
 */
export const BUSY_WINDOW_MS = 60_000;

/** What the chip and tooltip need about one entry's own circuit. */
export interface EntryCircuitView {
	chip: EntryChip;
	// The entry's own circuit when the member reports circuits[]; undefined on
	// a member from before that field, where only the row is known.
	circuit?: CircuitStatus;
	// Why (and how recently) the breaker last judged this circuit, when known.
	lastCause?: string;
	lastStatus?: number;
	lastAt?: string;
	// When the entry is next eligible for a probe, for the countdown.
	nextRetryAt?: string;
}

/**
 * Derives the entry's chip from the provider row, preferring the entry's own
 * circuit when the member reports circuits[] (the per-model truth) and falling
 * back to the row's open_models on an older member, so a mixed-version fleet
 * renders every entry either way.
 *
 * Rules, in order: the provider verdict open turns every entry of the provider
 * away (pinned when a quota pin holds it, else open); the entry's own circuit
 * open and blocking is open/pinned; owed a probe is probe; a closed circuit
 * whose last verdict was a saturated 429 inside BUSY_WINDOW_MS is busy; else
 * live.
 */
export function entryCircuitView(
	row: CircuitBreakerProviderStatus | undefined,
	modelId: string,
	now: number = Date.now(),
): EntryCircuitView {
	if (!row) return { chip: "live" };
	const circuit = row.circuits?.find((c) => c.model === modelId);
	const base: EntryCircuitView = {
		chip: "live",
		circuit,
		lastCause: circuit?.last_cause,
		lastStatus: circuit?.last_status,
		lastAt: circuit?.last_at,
	};

	// The provider-wide verdict: every entry of the provider is skipped.
	if (row.provider_open) {
		return {
			...base,
			chip: row.quota_pinned ? "pinned" : "open",
			nextRetryAt: circuit?.next_retry_at ?? row.next_retry_at,
		};
	}

	if (circuit) {
		if (circuit.state === "open") {
			return {
				...base,
				chip: circuit.quota_pinned ? "pinned" : "open",
				nextRetryAt: circuit.next_retry_at,
			};
		}
		if (circuit.state === "half-open") return { ...base, chip: "probe" };
		if (
			circuit.last_cause?.includes("(saturated)") &&
			circuit.last_at &&
			now - new Date(circuit.last_at).getTime() <= BUSY_WINDOW_MS
		) {
			return { ...base, chip: "busy" };
		}
		return base;
	}

	// Older member: only the row and open_models are known. A current member
	// that reports circuits[] without one for this model has simply never
	// routed it, and a sibling model's probe or outage says nothing about it.
	if (!row.circuits) {
		if (row.state === "half-open") return { ...base, chip: "probe" };
		if (row.state === "open" && row.open_models?.includes(modelId)) {
			return {
				...base,
				chip: row.quota_pinned ? "pinned" : "open",
				nextRetryAt: row.next_retry_at,
			};
		}
	}
	return base;
}

/**
 * The group header's summary: how many entries are live out of those enabled,
 * and, when none are, the earliest instant one of them is eligible to probe
 * again. Busy counts as live (it still serves), and so does an entry owed a
 * probe: it gets exactly one request, which overstates it, but a group whose
 * every entry is recovering is not dark, and "all entries dark" is the alarm.
 */
export function groupCircuitSummary(views: EntryCircuitView[]): {
	live: number;
	total: number;
	allDark: boolean;
	earliestRetryAt?: string;
} {
	const total = views.length;
	const live = views.filter(
		(v) => v.chip === "live" || v.chip === "busy" || v.chip === "probe",
	).length;
	let earliest: string | undefined;
	for (const v of views) {
		if (!v.nextRetryAt) continue;
		if (!earliest || new Date(v.nextRetryAt) < new Date(earliest)) {
			earliest = v.nextRetryAt;
		}
	}
	return {
		live,
		total,
		allDark: total > 0 && live === 0,
		earliestRetryAt: earliest,
	};
}
