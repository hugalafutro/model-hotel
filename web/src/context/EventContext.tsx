import { useEffect, useRef, type ReactNode } from "react";
import { useToast } from "./ToastContext";
import { getAdminToken, API_BASE } from "../api/client";

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

    useEffect(() => {
        const token = getAdminToken();
        if (!token) return;

        let disposed = false;

        const connect = () => {
            if (disposed) return;

            const ac = new AbortController();
            abortRef.current = ac;

            fetch(`${API_BASE}/api/events`, {
                headers: { Authorization: `Bearer ${token}` },
                signal: ac.signal,
            })
                .then((response) => {
                    if (!response.ok) {
                        throw new Error(
                            `SSE connection failed: ${response.status}`,
                        );
                    }

                    const reader = response.body?.getReader();
                    if (!reader) throw new Error("No readable stream");

                    const decoder = new TextDecoder();
                    let buffer = "";

                    const processChunk = (): Promise<void> => {
                        return reader.read().then(({ done, value }) => {
                            if (done || disposed) return;

                            buffer += decoder.decode(value, { stream: true });
                            const lines = buffer.split("\n");
                            // Keep the last incomplete line in the buffer
                            buffer = lines.pop() || "";

                            for (const line of lines) {
                                if (line.startsWith("data: ")) {
                                    const jsonStr = line.slice(6).trim();
                                    if (!jsonStr) continue;
                                    try {
                                        const event: ServerEvent =
                                            JSON.parse(jsonStr);
                                        toast(event.message, event.severity);
                                    } catch {
                                        // ignore malformed JSON
                                    }
                                }
                                // SSE comments (lines starting with ":") are ignored
                            }

                            return processChunk();
                        });
                    };

                    // Connection succeeded — reset backoff
                    reconnectDelay.current = 1000;

                    return processChunk().catch(() => {
                        // Stream ended or errored
                    });
                })
                .catch(() => {
                    // Connection failed or aborted
                })
                .finally(() => {
                    if (!disposed) {
                        // Reconnect with exponential backoff (1s → 2s → 4s → ... → 30s max)
                        const delay = reconnectDelay.current;
                        reconnectDelay.current = Math.min(delay * 2, 30000);
                        setTimeout(connect, delay);
                    }
                });
        };

        connect();

        return () => {
            disposed = true;
            abortRef.current?.abort();
        };
    }, [toast]);

    return <>{children}</>;
}
