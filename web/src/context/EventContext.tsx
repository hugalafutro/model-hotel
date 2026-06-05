import { type ReactNode, useEffect, useRef } from "react";
import { API_BASE, getAdminToken } from "../api/client";
import { readSSEStream } from "../utils/sse";
import { useToast } from "./ToastContext";

interface ServerEvent {
	id: string;
	type: string;
	severity: "success" | "info" | "warning" | "error";
	message: string;
	metadata?: Record<string, unknown>;
	timestamp: string;
}

export function EventProvider({ children }: { children: ReactNode }) {
	const { toast } = useToast();
	const reconnectDelay = useRef(1000);
	const abortRef = useRef<AbortController | null>(null);
	const connectingRef = useRef(false);

	useEffect(() => {
		const token = getAdminToken();
		if (!token) return;

		const connect = () => {
			if (connectingRef.current) return;
			connectingRef.current = true;
			const ac = new AbortController();
			abortRef.current = ac;
			let authFailed = false;

			fetch(`${API_BASE}/api/events`, {
				headers: { Authorization: `Bearer ${token}` },
				signal: ac.signal,
			})
				.then((response) => {
					if (!response.ok) {
						if (response.status === 401) {
							authFailed = true;
							localStorage.removeItem("adminToken");
							window.location.reload();
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
					if (!ac.signal.aborted && !authFailed) {
						// Reconnect with exponential backoff (1s → 2s → 4s → ... → 30s max)
						const delay = reconnectDelay.current;
						reconnectDelay.current = Math.min(delay * 2, 30000);
						setTimeout(connect, delay);
					}
				});
		};

		connect();

		return () => {
			abortRef.current?.abort();
			connectingRef.current = false;
		};
	}, [toast]);

	return <>{children}</>;
}
