import type { TFunction } from "i18next";
import { ApiError } from "../../../api/client";
import type { AlertStatus } from "../../../api/types";

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

// The fetchOK heads stripApiHead must match, re-exported from the endpoints
// that pass them so the two sides cannot drift.
export {
	ALERT_TEST_PREFIX,
	SETTINGS_UPDATE_PREFIX,
} from "../../../api/endpoints/settings";

// A reachability probe maps onto three tones: fully reachable, reachable but
// unhealthy, and not reachable at all. One classification, used for the colour,
// the wording and the badge variant alike.
export function appriseTone(
	status: AlertStatus | undefined,
): "success" | "warning" | "error" {
	if (status?.reachable && status.healthy) return "success";
	return status?.reachable ? "warning" : "error";
}

export const TONE_LABEL = {
	success: "reachable",
	warning: "issues",
	error: "unreachable",
} as const;

// safeApiMessage renders an ApiError for an operator. Only a 400 carries a
// sentence the server vouches for as safe to show; anything else (network,
// other 5xx, auth) could leak internals and falls back to the generic string.
export function safeApiMessage(
	err: unknown,
	prefix: string,
	t: TFunction,
): string {
	if (err instanceof ApiError && err.status === 400) {
		return stripApiHead(err.message, prefix);
	}
	return t("common.unknownError");
}

// reasonText renders a server reason code. Codes the catalog does not cover
// (and a failure that carried none) fall back to the caller's own wording
// rather than leaking a raw key, or a sentence about the wrong thing, into the
// dialog: a probe that could not be made is not a test that failed to deliver.
export function reasonText(
	code: string,
	t: TFunction,
	fallback: string,
): string {
	return t(`settings.alerts.reason.${code}`, { defaultValue: fallback });
}
