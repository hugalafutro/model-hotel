import { useEffect, useRef } from "react";
import { API_BASE, getAuthToken, onUnauthorized } from "../api/client";
import type { FdEvent } from "../api/types";

// useSSE subscribes to the Front Desk event stream (GET /api/sse) and invokes
// onEvent for each control-plane event. EventSource cannot send an
// Authorization header, so this uses fetch + a streamed ReadableStream (the same
// approach as the main dashboard) to attach the bearer token. It reconnects with
// exponential backoff and stops on a 401 (the app drops to login then).
export function useSSE(onEvent: (e: FdEvent) => void, enabled: boolean) {
	// Keep the latest handler in a ref so the long-lived stream effect (keyed
	// only on `enabled`) always calls the current callback without reconnecting.
	const handlerRef = useRef(onEvent);
	useEffect(() => {
		handlerRef.current = onEvent;
	}, [onEvent]);

	useEffect(() => {
		if (!enabled || !getAuthToken()) return;

		let unmounted = false;
		let delay = 1000;
		let abort: AbortController | null = null;
		let timer: ReturnType<typeof setTimeout> | null = null;
		const stopAuthWatch = onUnauthorized(() => {
			unmounted = true;
			abort?.abort();
		});

		const connect = () => {
			const token = getAuthToken();
			if (!token || unmounted) return;
			const ac = new AbortController();
			abort = ac;

			fetch(`${API_BASE}/api/sse`, {
				headers: { Authorization: `Bearer ${token}` },
				signal: ac.signal,
			})
				.then(async (resp) => {
					if (!resp.ok || !resp.body) return;
					delay = 1000; // connected: reset backoff
					const reader = resp.body.getReader();
					const decoder = new TextDecoder();
					let buffer = "";
					while (true) {
						const { done, value } = await reader.read();
						if (done) break;
						buffer += decoder.decode(value, { stream: true });
						// SSE frames are separated by a blank line.
						for (;;) {
							const sep = buffer.indexOf("\n\n");
							if (sep === -1) break;
							const frame = buffer.slice(0, sep);
							buffer = buffer.slice(sep + 2);
							for (const line of frame.split("\n")) {
								if (!line.startsWith("data:")) continue;
								const data = line.slice(5).trim();
								if (!data) continue;
								try {
									handlerRef.current(JSON.parse(data) as FdEvent);
								} catch {
									/* ignore malformed frame */
								}
							}
						}
					}
				})
				.catch(() => {
					/* aborted or network drop: handled by reconnect below */
				})
				.finally(() => {
					if (unmounted || ac.signal.aborted) return;
					const d = delay;
					delay = Math.min(d * 2, 30000);
					timer = setTimeout(connect, d);
				});
		};

		connect();
		return () => {
			unmounted = true;
			stopAuthWatch();
			abort?.abort();
			if (timer) clearTimeout(timer);
		};
	}, [enabled]);
}
