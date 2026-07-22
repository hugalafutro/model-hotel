/**
 * The backend password policy rejects a breached password with a stable,
 * English 400 body string (internal/api/passwordpolicy.go, errPasswordBreached).
 * That string is an intentional contract the dashboard maps to localized copy,
 * so we detect it by a distinctive, stable fragment rather than the whole
 * sentence — the full ApiError message is "Request failed: 400 <body>", so an
 * exact-equals check would never match.
 */
const BREACH_MARKER = "known data breach";

/**
 * isBreachedPasswordError reports whether err is the backend's "password found
 * in a breach" rejection, so callers can surface a localized message instead of
 * the raw English server text. Robust across module boundaries (duck-typed on
 * `message`, like errMessage in UserModal).
 */
export function isBreachedPasswordError(err: unknown): boolean {
	if (!err || typeof err !== "object" || !("message" in err)) return false;
	const message = (err as { message?: unknown }).message;
	return typeof message === "string" && message.includes(BREACH_MARKER);
}
