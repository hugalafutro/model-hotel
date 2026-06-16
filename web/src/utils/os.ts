// Best-effort client OS detection, used only to pick a friendlier "Follow
// System" icon. Purely cosmetic: an unknown OS falls back to a generic
// monitor, so the heuristics never need to be exhaustive.

export type ClientOS = "macos" | "windows" | "linux" | "unknown";

/**
 * Detect the host OS from the browser. Prefers the structured
 * `navigator.userAgentData.platform` (Chromium) and falls back to parsing
 * `navigator.userAgent` / `navigator.platform`. iOS/Android count as their
 * desktop kin (Apple/Linux) since this only drives logo choice.
 */
export function detectOS(): ClientOS {
	if (typeof navigator === "undefined") return "unknown";

	const uaData = (
		navigator as Navigator & { userAgentData?: { platform?: string } }
	).userAgentData;
	const platform = (uaData?.platform || navigator.platform || "").toLowerCase();
	const ua = (navigator.userAgent || "").toLowerCase();
	const hay = `${platform} ${ua}`;

	if (/mac|iphone|ipad|ipod|ios/.test(hay)) return "macos";
	if (/win/.test(hay)) return "windows";
	if (/linux|android|cros/.test(hay)) return "linux";
	return "unknown";
}
