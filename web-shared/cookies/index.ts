// Reading one cookie out of document.cookie, shared by both frontends: each
// derives its client-visible "is logged in" signal and its CSRF double-submit
// header from a readable cookie the server sets alongside the httpOnly session.
// The cookie NAMES differ per app and stay there; only the reading is shared.

// A cookie name is a token, and several token characters (. * + $ ^ |) are also
// regex metacharacters, so the name is escaped before it becomes a pattern.
function escapeForRegex(s: string): string {
	return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * readCookie returns the percent-decoded value of `name`, or null when the
 * cookie is absent. The value is one the server wrote, so a malformed encoding
 * is a broken server response rather than something to paper over, and
 * decodeURIComponent is left to throw on it.
 */
export function readCookie(name: string): string | null {
	const m = document.cookie.match(
		new RegExp(`(?:^|;\\s*)${escapeForRegex(name)}=([^;]+)`),
	);
	return m ? decodeURIComponent(m[1]) : null;
}
