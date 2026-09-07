import { useEffect, useRef } from "react";

/**
 * Keeps a scroll-mode list current while the tab is in the foreground: a slow
 * poll as the fallback for a dropped SSE connection, plus one immediate fetch
 * when the tab comes back, so a list left in the background is up to date the
 * moment it is looked at again.
 */
export function useScrollLivePoll({
	enabled,
	fetchNewer,
	intervalMs,
}: {
	enabled: boolean;
	fetchNewer: () => void;
	intervalMs: number;
}): void {
	// Held in a ref so a caller passing an inline closure does not restart the
	// interval on every render.
	const fetchRef = useRef(fetchNewer);
	useEffect(() => {
		fetchRef.current = fetchNewer;
	});

	useEffect(() => {
		if (!enabled) return;
		const refresh = () => {
			if (!document.hidden) fetchRef.current();
		};
		const interval = setInterval(refresh, intervalMs);
		document.addEventListener("visibilitychange", refresh);
		return () => {
			clearInterval(interval);
			document.removeEventListener("visibilitychange", refresh);
		};
	}, [enabled, intervalMs]);
}
