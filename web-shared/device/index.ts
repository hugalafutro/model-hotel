// Light user-agent summarization for the active-sessions list: "Firefox ·
// Linux", "Chrome · Android". Deliberately a handful of substring checks, not
// a UA-parsing library — the label only has to help an operator recognize
// their own devices, and the raw string is available in the row's tooltip.
// Returns null when nothing recognizable is found (or the UA is empty), so the
// caller can fall back to a translated "unknown device" label.

// Order matters in both lists: several engines impersonate their ancestors
// (Edge and Opera carry "Chrome", Chrome carries "Safari", Android carries
// "Linux"), so the more specific token is tested first.
const BROWSERS: Array<[string, string]> = [
	["Edg/", "Edge"],
	["OPR/", "Opera"],
	["SamsungBrowser/", "Samsung Internet"],
	["Firefox/", "Firefox"],
	["Chrome/", "Chrome"],
	["Safari/", "Safari"],
];

const SYSTEMS: Array<[string, string]> = [
	["Android", "Android"],
	["iPhone", "iOS"],
	["iPad", "iPadOS"],
	["Windows", "Windows"],
	["Mac OS X", "macOS"],
	["CrOS", "ChromeOS"],
	["Linux", "Linux"],
];

/**
 * A short human label for a session's user agent, or null when the string is
 * empty or matches nothing recognizable.
 */
export function deviceSummary(userAgent: string): string | null {
	if (!userAgent) return null;
	const browser = BROWSERS.find(([token]) => userAgent.includes(token))?.[1];
	const system = SYSTEMS.find(([token]) => userAgent.includes(token))?.[1];
	if (browser && system) return `${browser} · ${system}`;
	return browser ?? system ?? null;
}
