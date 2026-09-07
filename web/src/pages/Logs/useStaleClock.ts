import { useEffect, useState } from "react";
import type { LogEntry } from "../../api/types";
import { goDurationToSeconds } from "../../utils/duration";

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
	const staleThresholdMs =
		goDurationToSeconds(staleRequestTimeout ?? "") * 1000 || DEFAULT_STALE_MS;
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

	return { nowMs, staleThresholdMs };
}
