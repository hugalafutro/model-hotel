// stripApiHead removes the head fetchOK builds into ApiError.message. Every
// failed call is reported as `${prefix}: ${status} ${detail}` ("Failed to
// update settings: 400 apprise url must be http(s)"), and every place that
// shows the message already says which action failed, so the caller's own
// prefix and the bare status number are noise in front of the one sentence the
// operator can act on. A message that does not carry the head is left alone.
export function stripApiHead(message: string, prefix: string): string {
	const head = `${prefix}: `;
	if (!message.startsWith(head)) return message;
	const rest = message.slice(head.length);
	const status = /^\d+ /.exec(rest);
	return status === null ? message : rest.slice(status[0].length);
}

// The stable failure codes the alert endpoints report: /api/alert/test with a
// 502 and the reachability probe in AlertStatus.reason. Anything outside this
// set falls back to a generic error, so no server internals reach the screen.
export const REASON_CODES = new Set([
	"not_configured",
	"invalid_url",
	"unreachable",
	"unhealthy",
	"apprise_reject",
	"deliver_failed",
	"undecryptable",
]);
