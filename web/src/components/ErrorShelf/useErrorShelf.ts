import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo } from "react";
import { api } from "../../api/client";
import type { AppLogEntry, LogEntry } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";

/** How many recent errors of each kind to fetch, and the cap on the merged
 * list the shelf renders. */
export const ERROR_SHELF_LIMIT = 15;

const ACKED_KEYS_STORAGE = "ackedErrorKeys";
/** Bound localStorage growth — keep only the most-recently acked keys. */
const ACKED_KEYS_CAP = 200;

/** Legacy single-key dismissal storage from the old two-pill design; cleared
 * once on mount so it doesn't linger. */
const LEGACY_KEYS = ["dismissedAppErrorKey", "dismissedReqErrorKey"] as const;

export type ShelfErrorKind = "request" | "app";

/** App-log sources emitted by the HA member-side config-sync receiver. An app
 * error from one of these is surfaced as a distinct "HA" category (a member
 * failing to apply config pushed by the fleet primary) rather than a generic
 * internal app error. Proxy/request errors never originate here. */
const HA_SOURCES = new Set(["configsync", "fleet"]);

/** True when an app-log source identifies a fleet/HA membership failure. */
export function isHaSource(source: string | undefined): boolean {
	return source !== undefined && HA_SOURCES.has(source);
}

export interface ShelfError {
	/** Stable id used for acknowledgement + React keys. Mirrors the old
	 * `${timestamp}:${msg[:50]}` scheme, kind-prefixed so app/request errors
	 * sharing a timestamp never collide. */
	key: string;
	kind: ShelfErrorKind;
	message: string;
	timestamp: string;
	/** Backing log row, handed to LogDetailModal on "view details". */
	entry: LogEntry | AppLogEntry;
	/** Machine-readable failure classification (request errors only). */
	errorKind?: string;
	/** App-log emitter source (app errors only); drives the HA sub-category. */
	source?: string;
}

function makeKey(kind: ShelfErrorKind, timestamp: string, message: string) {
	return `${kind}:${timestamp}:${message.slice(0, 50)}`;
}

function parseAckedKeys(stored: string, fallback: string[]): string[] {
	try {
		const parsed = JSON.parse(stored);
		return Array.isArray(parsed)
			? parsed.filter((v): v is string => typeof v === "string")
			: fallback;
	} catch {
		return fallback;
	}
}

export interface UseErrorShelf {
	/** Every recent error, newest first, capped at ERROR_SHELF_LIMIT. */
	errors: ShelfError[];
	/** The subset not yet acknowledged — drives visibility + the badge. */
	unacked: ShelfError[];
	ack: (key: string) => void;
	ackAll: () => void;
}

/**
 * Fetches the most recent request (5xx) and app (error-level) failures, merges
 * them into one newest-first feed, and tracks per-error acknowledgement in
 * localStorage. A newer error (new key) reappears even after its predecessors
 * were acked. The `dismissedErrorsReset` window event un-acks everything.
 */
export function useErrorShelf(): UseErrorShelf {
	const [ackedKeys, setAckedKeys] = useLocalStorage<string[]>(
		ACKED_KEYS_STORAGE,
		[],
		{ serialize: JSON.stringify, deserialize: parseAckedKeys },
	);

	// One-time cleanup of the superseded per-kind dismissal keys.
	useEffect(() => {
		for (const k of LEGACY_KEYS) {
			try {
				localStorage.removeItem(k);
			} catch {
				/* ignore */
			}
		}
	}, []);

	// Settings → "Reset dismissed error banners" un-acks the whole shelf.
	useEffect(() => {
		const handler = () => setAckedKeys([]);
		window.addEventListener("dismissedErrorsReset", handler);
		return () => window.removeEventListener("dismissedErrorsReset", handler);
	}, [setAckedKeys]);

	const { data: reqLogData, isLoading: reqLoading } = useQuery({
		queryKey: ["logs", "errorShelf", ERROR_SHELF_LIMIT],
		queryFn: () =>
			api.logs.list({
				page: 1,
				per_page: ERROR_SHELF_LIMIT,
				status_code: "5xx",
				sort_by: "time",
				sort_dir: "desc",
			}),
		refetchInterval: 15000,
		staleTime: 10000,
	});

	const { data: appLogData, isLoading: appLoading } = useQuery({
		queryKey: ["appLogHistory", "errorShelf", ERROR_SHELF_LIMIT],
		queryFn: () =>
			api.appLogs.history({
				page: 1,
				per_page: ERROR_SHELF_LIMIT,
				level: "error",
				sort_by: "time",
				sort_dir: "desc",
			}),
		refetchInterval: 15000,
		staleTime: 10000,
	});

	// Surface nothing until BOTH sources have settled their first fetch.
	// Otherwise whichever query resolves first flashes its rows on screen,
	// only to be re-sliced/re-filtered once the other lands — a one-render
	// blink on every page load. isLoading flips false on success OR error, so a
	// broken endpoint won't wedge the shelf shut. Background refetches don't
	// reset isLoading, so this only gates the initial load.
	const ready = !reqLoading && !appLoading;

	const errors = useMemo<ShelfError[]>(() => {
		if (!ready) return [];
		const merged: ShelfError[] = [];

		for (const entry of reqLogData?.entries ?? []) {
			if (!entry.error_message || !entry.created_at) continue;
			merged.push({
				key: makeKey("request", entry.created_at, entry.error_message),
				kind: "request",
				message: entry.error_message,
				timestamp: entry.created_at,
				entry,
				errorKind: entry.error_kind || undefined,
			});
		}

		for (const entry of appLogData?.entries ?? []) {
			if (!entry.message || !entry.timestamp) continue;
			merged.push({
				key: makeKey("app", entry.timestamp, entry.message),
				kind: "app",
				message: entry.message,
				timestamp: entry.timestamp,
				entry,
				source: entry.source || undefined,
			});
		}

		// Timestamps are fixed-width ISO 8601 UTC strings, so a direct
		// lexicographic compare is correct, deterministic, and faster than
		// the locale-sensitive localeCompare.
		merged.sort((a, b) =>
			b.timestamp < a.timestamp ? -1 : b.timestamp > a.timestamp ? 1 : 0,
		);
		return merged.slice(0, ERROR_SHELF_LIMIT);
	}, [ready, reqLogData, appLogData]);

	const ackedSet = useMemo(() => new Set(ackedKeys), [ackedKeys]);
	const unacked = useMemo(
		() => errors.filter((e) => !ackedSet.has(e.key)),
		[errors, ackedSet],
	);

	const ack = useCallback(
		(key: string) => {
			setAckedKeys((prev) =>
				prev.includes(key) ? prev : [key, ...prev].slice(0, ACKED_KEYS_CAP),
			);
		},
		[setAckedKeys],
	);

	const ackAll = useCallback(() => {
		const visibleKeys = errors.map((e) => e.key);
		setAckedKeys((prev) => {
			const next = [...visibleKeys];
			for (const k of prev) if (!next.includes(k)) next.push(k);
			return next.slice(0, ACKED_KEYS_CAP);
		});
	}, [errors, setAckedKeys]);

	return { errors, unacked, ack, ackAll };
}
