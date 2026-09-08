import type { OpenCodeGoUsageResponse } from "./types";

// OpenCode Go names its three windows as fields rather than listing them, so
// the selector walks them in the order the UI shows them and drops the ones the
// payload omits. Percent is clamped to [0, 100] so a badge and a bar can render
// it directly; the spent rule reads the same clamped value, and a window at or
// past 100 is spent either way. A percent that is absent or not a finite number
// reads as 0 rather than rendering NaN.
//
// A refused window reports 100 regardless of the percent it carries: OpenCode
// Go serves nothing from it, and it counts as spent, so a label or a bar
// showing the 0 such a payload arrives with would contradict the spent styling
// around it.

export type OpenCodeGoWindowKey = "rolling" | "weekly" | "monthly";

const WINDOW_KEYS: OpenCodeGoWindowKey[] = ["rolling", "weekly", "monthly"];

export interface OpenCodeGoWindow {
	key: OpenCodeGoWindowKey;
	/** Percent of the window consumed, clamped to [0, 100]. */
	percent: number;
	resetsAt?: string;
	status?: string;
}

/**
 * Whether OpenCode Go is refusing this window. Only "ok" is a documented
 * status, so any other non-empty value counts as a refusal: an undocumented
 * value fails closed rather than reading as healthy. The comparison is trimmed
 * and case-insensitive so a casing drift upstream changes nothing. An absent
 * status decides nothing.
 */
export function isOpenCodeGoWindowRefused(status?: string): boolean {
	const s = status?.trim().toLowerCase();
	return !!s && s !== "ok";
}

export function getOpenCodeGoWindows(
	u: OpenCodeGoUsageResponse | undefined | null,
): OpenCodeGoWindow[] {
	const usage = u?.usage;
	if (!usage) return [];
	const windows: OpenCodeGoWindow[] = [];
	for (const key of WINDOW_KEYS) {
		const w = usage[key];
		if (!w) continue;
		const n = Number(w.percent);
		const clamped = Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : 0;
		windows.push({
			key,
			percent: isOpenCodeGoWindowRefused(w.status) ? 100 : clamped,
			resetsAt: w.resetsAt,
			status: w.status,
		});
	}
	return windows;
}
