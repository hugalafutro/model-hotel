// Helpers for the OIDC SSO callback hand-off. The backend callback redirects
// the browser to the SPA with the result in the URL *fragment* (never the
// query string) so the freshly minted session token is never sent back to the
// server and so it cannot land in any access log or reverse-proxy trace.

const TOKEN_PREFIX = "#oidc_token=";
const ERROR_PREFIX = "#oidc_error=";

/** scrubHash removes the fragment without adding a history entry. */
function scrubHash() {
	window.history.replaceState(
		null,
		"",
		window.location.pathname + window.location.search,
	);
}

/**
 * consumeOidcToken stores an SSO session token delivered in the URL fragment
 * (the same `adminToken` slot the other login paths use) and scrubs the
 * fragment. Returns true when a token was consumed. Call this synchronously
 * before the app reads the stored token so an SSO redirect boots logged in.
 */
export function consumeOidcToken(): boolean {
	const hash = window.location.hash;
	if (!hash.startsWith(TOKEN_PREFIX)) return false;
	const token = decodeURIComponent(hash.slice(TOKEN_PREFIX.length));
	scrubHash();
	if (!token) return false;
	localStorage.setItem("adminToken", token);
	return true;
}

/**
 * consumeOidcError returns the coarse error code from a failed SSO callback
 * (and scrubs the fragment), or null when none is present. The login screen
 * turns it into a localized message.
 */
export function consumeOidcError(): string | null {
	const hash = window.location.hash;
	if (!hash.startsWith(ERROR_PREFIX)) return null;
	const code = decodeURIComponent(hash.slice(ERROR_PREFIX.length));
	scrubHash();
	return code || "unknown";
}
