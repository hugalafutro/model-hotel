import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo } from "react";
import {
	buildUrl,
	fetchJSONWithServerNow,
	getAuthHeaders,
} from "../../api/client";
import type { AppLogEntry, LogEntry, LogsResponse } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";

/** How many recent errors of each kind to fetch, and the cap on the merged
 * list the shelf renders. */
export const ERROR_SHELF_LIMIT = 15;

/** Only surface errors this recent. Anything older is stale noise (e.g. a
 * request error from before the last rebuild) that the user should not have to
 * keep dismissing. */
export const ERROR_SHELF_MAX_AGE_MS = 24 * 60 * 60 * 1000;

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

/** The HTTP access logger stamps every request line with source "access" and
 * carries the target path as a `path=/...` field in the message. A 5xx on a
 * `/api/fleet/*` control-plane endpoint (e.g. the Front Desk announce
 * heartbeat) is therefore an HA-plane failure that the source alone can't
 * distinguish from any other request error — this recovers it from the path. */
export function isHaAccessLog(
	source: string | undefined,
	message: string,
): boolean {
	return source === "access" && message.includes("path=/api/fleet/");
}

/** App-log sources emitted by the OIDC single-sign-on admin-login flow. An app
 * error from one of these is surfaced as a distinct "SSO" category (an admin
 * login failing against the identity provider) rather than a generic internal
 * app error. */
const SSO_SOURCES = new Set(["oidc"]);

/** True when an app-log source identifies an SSO / OIDC login failure. */
export function isSsoSource(source: string | undefined): boolean {
	return source !== undefined && SSO_SOURCES.has(source);
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

/** Matches a trailing timezone designator (`Z` or `±HH:MM`/`±HHMM`). */
const HAS_TZ = /([zZ]|[+-]\d{2}:?\d{2})$/;

/** Parse a log timestamp to epoch millis, treating a zone-less string as UTC.
 * The backend emits RFC3339Nano (always zoned), but a bare
 * `YYYY-MM-DDTHH:MM:SS` would otherwise be parsed in the viewer's local
 * timezone, shifting its apparent age by the browser's offset. Returns NaN for
 * anything unparseable, which callers treat as "keep, don't drop". */
function parseTs(timestamp: string): number {
	return Date.parse(HAS_TZ.test(timestamp) ? timestamp : `${timestamp}Z`);
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

	// Both queries capture the server's `Date` header alongside the rows so the
	// recency window can anchor on the server clock (see the errors memo) rather
	// than a possibly-skewed browser clock.
	const { data: reqLogData, isLoading: reqLoading } = useQuery({
		queryKey: ["logs", "errorShelf", ERROR_SHELF_LIMIT],
		queryFn: () =>
			fetchJSONWithServerNow<LogsResponse>(
				buildUrl("/api/logs", {
					page: 1,
					per_page: ERROR_SHELF_LIMIT,
					status_code: "5xx",
					sort_by: "time",
					sort_dir: "desc",
				}),
				{ headers: getAuthHeaders() },
			),
		refetchInterval: 15000,
		staleTime: 10000,
	});

	const { data: appLogData, isLoading: appLoading } = useQuery({
		queryKey: ["appLogHistory", "errorShelf", ERROR_SHELF_LIMIT],
		queryFn: () =>
			fetchJSONWithServerNow<{ entries: AppLogEntry[] }>(
				buildUrl("/api/logs/app", {
					history: "true",
					page: 1,
					per_page: ERROR_SHELF_LIMIT,
					level: "error",
					sort_by: "time",
					sort_dir: "desc",
				}),
				{ headers: getAuthHeaders() },
			),
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
		const reqEntries = reqLogData?.data.entries ?? [];
		const appEntries = appLogData?.data.entries ?? [];

		// Newest served timestamp. Every row is server-stamped, so this is both a
		// lower bound on the server's "now" and the freshest error we must never
		// hide.
		let newest = 0;
		for (const entry of reqEntries) {
			const t = parseTs(entry.created_at ?? "");
			if (!Number.isNaN(t) && t > newest) newest = t;
		}
		for (const entry of appEntries) {
			const t = parseTs(entry.timestamp ?? "");
			if (!Number.isNaN(t) && t > newest) newest = t;
		}

		// Anchor "now" on a trusted server clock, never the browser clock (which
		// may be skewed either way). Priority:
		//   1. the `Date` response header — authoritative server wall-clock;
		//      clamped up to `newest` in case the sample is a poll interval stale.
		//   2. otherwise the newest served timestamp — itself server-stamped, so a
		//      fast/slow browser clock still can't age a served error out of view.
		//   3. the browser clock only when there is nothing to filter (no rows),
		//      where its value cannot hide anything.
		// So a skewed client clock can never wrongly drop a row the server just
		// returned; in production the header path always wins (same-origin Go
		// responses always carry `Date`). Anything older than the window is stale
		// noise (e.g. an error from before the last rebuild); an unparseable
		// timestamp is kept rather than silently dropped.
		const serverNowMs =
			reqLogData?.serverNowMs ?? appLogData?.serverNowMs ?? null;
		const anchor =
			serverNowMs !== null
				? Math.max(serverNowMs, newest)
				: newest > 0
					? newest
					: Date.now();
		const cutoff = anchor - ERROR_SHELF_MAX_AGE_MS;
		const isRecent = (timestamp: string) => {
			const t = parseTs(timestamp);
			return Number.isNaN(t) || t >= cutoff;
		};

		for (const entry of reqEntries) {
			if (!entry.error_message || !entry.created_at) continue;
			if (!isRecent(entry.created_at)) continue;
			merged.push({
				key: makeKey("request", entry.created_at, entry.error_message),
				kind: "request",
				message: entry.error_message,
				timestamp: entry.created_at,
				entry,
				errorKind: entry.error_kind || undefined,
			});
		}

		for (const entry of appEntries) {
			if (!entry.message || !entry.timestamp) continue;
			if (!isRecent(entry.timestamp)) continue;
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
