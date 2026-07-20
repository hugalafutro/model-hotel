// Helpers for the OIDC SSO callback hand-off. On success the backend callback
// sets the session cookie pair and redirects to a clean `/` (no token in the URL
// at all), so the app boots logged in purely from the cookie. On failure it
// redirects to `/#oidc_error=<code>`; consumeOidcError turns that into a message.

const ERROR_PREFIX = "#oidc_error=";

/** scrubHash removes the fragment without adding a history entry. */
function scrubHash() {
	window.history.replaceState(
		null,
		"",
		window.location.pathname + window.location.search,
	);
}

/** decode percent-encoding but never throw: a malformed fragment (e.g. a bare
 * `%`) must not crash app boot and strand the local-login fallback. */
function safeDecode(s: string): string {
	try {
		return decodeURIComponent(s);
	} catch {
		return "";
	}
}

/**
 * consumeOidcError returns the coarse error code from a failed SSO callback
 * (and scrubs the fragment), or null when none is present. The login screen
 * turns it into a localized message.
 */
export function consumeOidcError(): string | null {
	const hash = window.location.hash;
	if (!hash.startsWith(ERROR_PREFIX)) return null;
	const code = safeDecode(hash.slice(ERROR_PREFIX.length));
	scrubHash();
	return code || "unknown";
}
