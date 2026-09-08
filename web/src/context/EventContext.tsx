import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useEffect, useRef } from "react";
import { API_BASE, isAuthenticated, resetToLogin } from "../api/client";
import { readSSEStream } from "../utils/sse";
import { useToast } from "./ToastContext";

/** One server-sent event, as it rides on the "server-event" window event. */
export interface ServerEvent {
	id: string;
	type: string;
	severity: "success" | "info" | "warning" | "error";
	message: string;
	metadata?: Record<string, unknown>;
	timestamp: string;
}

/**
 * Subscribes to the server events this provider dispatches. The handler is held
 * in a ref, so an inline closure does not re-attach the listener on every
 * render and the subscription lasts as long as the component.
 */
// eslint-disable-next-line react-refresh/only-export-components -- the consumer hook lives beside its provider
export function useServerEvent(handler: (event: ServerEvent) => void): void {
	const handlerRef = useRef(handler);
	useEffect(() => {
		handlerRef.current = handler;
	});

	useEffect(() => {
		const listener = (e: Event) => {
			handlerRef.current((e as CustomEvent<ServerEvent>).detail);
		};
		window.addEventListener("server-event", listener);
		return () => window.removeEventListener("server-event", listener);
	}, []);
}

export function EventProvider({ children }: { children: ReactNode }) {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const reconnectDelay = useRef(1000);
	const abortRef = useRef<AbortController | null>(null);
	const connectingRef = useRef(false);
	const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

	useEffect(() => {
		if (!isAuthenticated()) return;

		// Set once the effect's cleanup has run so any in-flight fetch that
		// settles afterwards won't schedule a fresh reconnect.
		let unmounted = false;

		const connect = () => {
			if (connectingRef.current) return;
			// Re-check auth on every (re)connect so a session dropped mid-run stops
			// us from reconnecting in a loop. The session cookie attaches
			// automatically on this same-origin request.
			if (!isAuthenticated()) return;
			connectingRef.current = true;
			const ac = new AbortController();
			abortRef.current = ac;
			let authFailed = false;

			fetch(`${API_BASE}/api/events`, {
				credentials: "same-origin",
				signal: ac.signal,
			})
				.then((response) => {
					if (!response.ok) {
						if (response.status === 401) {
							authFailed = true;
							// The same teardown the logout button runs, so an expired
							// session cannot leave a query settling against a page that is
							// already reloading.
							resetToLogin(queryClient);
							return;
						}
						throw new Error(`SSE connection failed: ${response.status}`);
					}

					const reader = response.body?.getReader();
					if (!reader) throw new Error("No readable stream");

					// Connection succeeded - reset backoff
					reconnectDelay.current = 1000;

					return readSSEStream<ServerEvent>({
						reader,
						signal: ac.signal,
						doneSentinel: null,
						idleTimeoutMs: 0,
						onChunk(event) {
							// Dispatch custom event for programmatic consumers (e.g., logs page)
							window.dispatchEvent(
								new CustomEvent("server-event", { detail: event }),
							);
							// Only show toast for user-facing events, not request lifecycle
							if (!event.type.startsWith("request.")) {
								toast(event.message, event.severity);
							}
							// Discovery also runs with no dashboard action behind it: a
							// provider save that changed its address or key scans in the
							// background, so the lists follow the completion event. Only
							// that one: the fetched/enriched events fire before the scan's
							// rows are written. exact on the badge key, for the reason
							// useRefreshDiscoveryBadge gives.
							if (event.type === "request.discovery.provider_completed") {
								queryClient.invalidateQueries({ queryKey: ["providers"] });
								queryClient.invalidateQueries({ queryKey: ["models"] });
								queryClient.invalidateQueries({
									queryKey: ["discovery-status"],
									exact: true,
								});
							}
						},
					}).catch(() => {
						// Stream ended or errored
					});
				})
				.catch(() => {
					// Connection failed or aborted
				})
				.finally(() => {
					connectingRef.current = false;
					if (!ac.signal.aborted && !authFailed && !unmounted) {
						// Reconnect with exponential backoff (1s → 2s → 4s → ... → 30s max)
						const delay = reconnectDelay.current;
						reconnectDelay.current = Math.min(delay * 2, 30000);
						reconnectTimerRef.current = setTimeout(connect, delay);
					}
				});
		};

		connect();

		return () => {
			unmounted = true;
			abortRef.current?.abort();
			connectingRef.current = false;
			// Clear any reconnect already queued from a prior finally(); aborting
			// the fetch alone doesn't cancel a pending setTimeout, so without this
			// the provider keeps reconnecting after unmount.
			if (reconnectTimerRef.current) {
				clearTimeout(reconnectTimerRef.current);
				reconnectTimerRef.current = null;
			}
		};
	}, [toast, queryClient]);

	return <>{children}</>;
}
