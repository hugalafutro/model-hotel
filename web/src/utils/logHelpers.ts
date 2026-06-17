/**
 * Error kinds (request_logs.error_kind) that represent an interruption rather
 * than a provider failure: the client went away, or a gateway/retry deadline
 * expired. Kept in sync with the ErrorKind constants in
 * internal/proxy/reqerror.go.
 */
export const CANCELLED_KINDS = new Set<string>([
	"client_disconnect",
	"failover_timeout",
	"retry_timeout",
]);

/**
 * Legacy substring matcher for rows with no error_kind (logs written before the
 * error_kind column existed). New rows always carry error_kind, so this is only
 * a fallback for historical data — remove once retention ages out all
 * pre-error_kind rows.
 */
const isCancelledMessage = (errorMessage?: string): boolean => {
	if (!errorMessage) return false;
	const msg = errorMessage.toLowerCase();
	return (
		msg.includes("cancel") ||
		msg.includes("disconnect") ||
		msg.includes("upstream request timed out") ||
		msg.includes("param-strip retry timed out")
	);
};

/**
 * Checks whether a request log was interrupted (client disconnect, gateway or
 * retry timeout) rather than failing due to an upstream provider error.
 *
 * Prefers the machine-readable error_kind; only falls back to substring
 * matching of the English error_message for legacy rows that have no kind.
 * Accepts either a log-like object ({ error_kind, error_message }) or a raw
 * error message string (legacy callers and unit tests).
 */
export const isCancelled = (
	log?: string | { error_kind?: string; error_message?: string },
): boolean => {
	if (!log) return false;
	if (typeof log === "string") return isCancelledMessage(log);
	if (log.error_kind) return CANCELLED_KINDS.has(log.error_kind);
	return isCancelledMessage(log.error_message);
};

export type StatusBadgeVariant =
	| "error"
	| "warning"
	| "info"
	| "success"
	| "orange"
	| "muted";

/**
 * Maps a request's status code (plus error context) to a Badge variant.
 * Shared by both the paginated and virtualized log tables so the two views
 * never drift apart.
 */
export const getStatusBadgeVariant = (
	statusCode: number,
	log?: { error_kind?: string; error_message?: string },
): StatusBadgeVariant => {
	if (isCancelled(log)) return "warning";
	if (statusCode === 0) return "error";
	if (statusCode >= 200 && statusCode < 300) return "success";
	if (statusCode >= 400 && statusCode < 500) return "orange";
	if (statusCode >= 500) return "error";
	return "muted";
};

type InProgressLike = { state?: string; created_at: string };

/**
 * A request still in pending/streaming state but older than the configured
 * timeout is almost certainly dead (server crash, unhandled error, etc.) -
 * treat it as stale rather than showing a permanently pulsing row.
 *
 * `nowMs` is the caller's ticking "now" (so rows re-evaluate on an interval)
 * and `staleThresholdMs` the configured stale-request timeout.
 */
export const isStale = (
	log: InProgressLike,
	nowMs: number,
	staleThresholdMs: number,
): boolean => {
	if (log.state !== "pending" && log.state !== "streaming") return false;
	const age = nowMs - new Date(log.created_at).getTime();
	return age > staleThresholdMs;
};

/**
 * A request that is actively pending/streaming and not yet stale. These rows
 * render the blue "…"/"Live" in-progress indicator instead of a status code.
 */
export const isInProgress = (
	log: InProgressLike,
	nowMs: number,
	staleThresholdMs: number,
): boolean =>
	!isStale(log, nowMs, staleThresholdMs) &&
	(log.state === "pending" || log.state === "streaming");

/**
 * Badge variant for a request-log row's status pill, accounting for
 * in-progress and stale states. An in-progress request carries status_code 0,
 * so the raw getStatusBadgeVariant would paint the pill red (error) even though
 * the cell now reads "…"/"Live" - return an "info" (blue) pill for in-progress
 * and a "warning" (amber) pill for stale so the pill colour matches its text.
 */
export const getRowStatusVariant = (
	log: InProgressLike & {
		status_code: number;
		error_kind?: string;
		error_message?: string;
	},
	nowMs: number,
	staleThresholdMs: number,
): StatusBadgeVariant => {
	if (isStale(log, nowMs, staleThresholdMs)) return "warning";
	if (isInProgress(log, nowMs, staleThresholdMs)) return "info";
	return getStatusBadgeVariant(log.status_code, log);
};
