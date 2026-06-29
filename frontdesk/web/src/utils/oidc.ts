// Helpers for the OIDC SSO callback hand-off. The backend callback redirects the
// browser to the SPA with the result in the URL *fragment* (never the query
// string), so the session token is not sent back to the server on the follow-up
// request (no Referer leak, nothing in request logs). It does still appear in the
// callback's 302 Location response header, so operators should redact Location on
// /api/auth/oidc/callback in proxy access logs.
//
// Ported from the main dashboard (web/src/utils/oidc.ts); the only difference is
// that Front Desk stores the token via setAuthToken (key "fdAuthToken") rather
// than writing the main app's "adminToken" slot directly.

import { setAuthToken } from "../api/client";

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
 * consumeOidcToken stores an SSO session token delivered in the URL fragment
 * (the same bearer slot the other login paths use) and scrubs the fragment.
 * Returns true when a token was consumed. Call this synchronously before the app
 * reads the stored token so an SSO redirect boots logged in.
 */
export function consumeOidcToken(): boolean {
	const hash = window.location.hash;
	if (!hash.startsWith(TOKEN_PREFIX)) return false;
	const token = safeDecode(hash.slice(TOKEN_PREFIX.length));
	scrubHash();
	if (!token) return false;
	setAuthToken(token);
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
	const code = safeDecode(hash.slice(ERROR_PREFIX.length));
	scrubHash();
	return code || "unknown";
}
