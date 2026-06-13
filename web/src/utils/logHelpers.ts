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
