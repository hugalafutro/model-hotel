import { useEffect, useState } from "react";
import type { LogEntry } from "../../api/types";
import {
	isInProgress as isInProgressShared,
	isStale as isStaleShared,
} from "../../utils/logHelpers";

/**
 * Milliseconds in a Go duration string (e.g. "30m0s", "1h0m0s"), which is
 * how the stale-request timeout setting is stored. Unparseable input yields 0.
 */
export function parseGoDuration(d: string): number {
	let ms = 0;
	const h = d.match(/(\d+)h/);
	const m = d.match(/(\d+)m(?!s)/);
	const s = d.match(/(\d+)s/);
	if (h) ms += parseInt(h[1], 10) * 3600000;
	if (m) ms += parseInt(m[1], 10) * 60000;
	if (s) ms += parseInt(s[1], 10) * 1000;
	return ms;
}

const DEFAULT_STALE_MS = 30 * 60 * 1000;

/**
 * The clock the request rows read their age from, plus the stale threshold.
 *
 * A request stuck in pending/streaming longer than the configured timeout is
 * almost certainly dead (server crash, unhandled error, etc.), so it renders as
 * stale rather than as a permanently pulsing "Resolving…" / "Live" row. The
 * default of 30m accommodates providers with long time-to-first-token.
 *
 * In-progress rows show a live-ticking duration, so while any are present the
 * clock ticks every second; otherwise a coarse 60s tick is enough to age rows
 * into the stale state without re-rendering the table needlessly.
 */
export function useStaleClock(
	staleRequestTimeout: string | undefined,
	entries: LogEntry[],
) {
	const staleMs = parseGoDuration(staleRequestTimeout || "30m0s");
	const staleThresholdMs = staleMs > 0 ? staleMs : DEFAULT_STALE_MS;
	const [nowMs, setNowMs] = useState(() => Date.now());
	const hasLiveEntries = entries.some(
		(log) => log.state === "pending" || log.state === "streaming",
	);
	useEffect(() => {
		const id = setInterval(
			() => {
				setNowMs(Date.now());
			},
			hasLiveEntries ? 1_000 : 60_000,
		);
		return () => clearInterval(id);
	}, [hasLiveEntries]);

	return {
		nowMs,
		staleThresholdMs,
		isStale: (log: LogEntry) => isStaleShared(log, nowMs, staleThresholdMs),
		isInProgress: (log: LogEntry) =>
			isInProgressShared(log, nowMs, staleThresholdMs),
	};
}
