import type { OpenCodeGoUsageResponse } from "./types";

// OpenCode Go names its three windows as fields rather than listing them, so
// the selector walks them in the order the UI shows them and drops the ones the
// payload omits. Percent is clamped to [0, 100] so a badge and a bar can render
// it directly; the spent rule reads the same clamped value, and a window at or
// past 100 is spent either way. A percent that is absent or not a finite number
// reads as 0 rather than rendering NaN.

export type OpenCodeGoWindowKey = "rolling" | "weekly" | "monthly";

const WINDOW_KEYS: OpenCodeGoWindowKey[] = ["rolling", "weekly", "monthly"];

export interface OpenCodeGoWindow {
	key: OpenCodeGoWindowKey;
	/** Percent of the window consumed, clamped to [0, 100]. */
	percent: number;
	resetsAt?: string;
	status?: string;
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
		windows.push({
			key,
			percent: Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : 0,
			resetsAt: w.resetsAt,
			status: w.status,
		});
	}
	return windows;
}
