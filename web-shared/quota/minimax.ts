import type {
	MiniMaxModelRemains,
	MiniMaxQuotaResponse,
	MiniMaxQuotaWindow,
} from "./types";

// MiniMax reports REMAINING percentages per model class. The active class is the
// first "general" entry with current_interval_status === 1. Used = 100 minus
// remaining. A non-zero base_resp.status_code (2062 "no active token plan", for
// example) means there is nothing to show.

/** First active "general" model-class entry, or undefined. */
export function getMiniMaxGeneralEntry(
	data: MiniMaxQuotaResponse | undefined | null,
): MiniMaxModelRemains | undefined {
	const entries =
		data?.base_resp?.status_code === 0 ? data.model_remains : null;
	if (!Array.isArray(entries)) return undefined;
	return entries.find(
		(m) => m.model_name === "general" && m.current_interval_status === 1,
	);
}

function toMiniMaxWindow(
	remainingPercent: number | undefined,
	resetMs: number | undefined,
): MiniMaxQuotaWindow | undefined {
	if (remainingPercent == null || !Number.isFinite(remainingPercent)) {
		return undefined;
	}
	return {
		percentage: 100 - remainingPercent,
		remainingPercent,
		resetMs: resetMs ?? 0,
	};
}

/** Rolling 5-hour window derived from the active general entry. */
export function getMiniMaxFiveHourLimit(
	data: MiniMaxQuotaResponse | undefined | null,
): MiniMaxQuotaWindow | undefined {
	const g = getMiniMaxGeneralEntry(data);
	if (!g) return undefined;
	return toMiniMaxWindow(g.current_interval_remaining_percent, g.remains_time);
}

/** Weekly window derived from the active general entry. */
export function getMiniMaxWeeklyLimit(
	data: MiniMaxQuotaResponse | undefined | null,
): MiniMaxQuotaWindow | undefined {
	const g = getMiniMaxGeneralEntry(data);
	if (!g) return undefined;
	return toMiniMaxWindow(
		g.current_weekly_remaining_percent,
		g.weekly_remains_time,
	);
}
