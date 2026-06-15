import type { UIStyle } from "../context/ThemeContext";

// Corner radii per UI style: sharp for the terminal theme, rounded for the
// SaaS and glass themes — so the favicon echoes the same rounded/rectangular
// language as the rest of the chrome.
const RADII: Record<UIStyle, { outer: number; inner: number }> = {
	"clean-saas": { outer: 10, inner: 1 },
	"glassmorphism-lite": { outer: 12, inner: 2 },
	"cyber-terminal": { outer: 0, inner: 0 },
};

/** Dark plate the hotel mark sits on (matches the original favicon). */
const BG = "#0b0c0f";

/**
 * Builds the Model Hotel favicon (a stylized hotel) tinted with the active
 * accent color and shaped to the active UI style. Kept as a single-line SVG
 * string so it embeds cleanly in a `data:` URI.
 */
export function buildFaviconSvg(accent: string, uiStyle: UIStyle): string {
	const { outer, inner } = RADII[uiStyle] ?? RADII["clean-saas"];
	return `<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 48 48" fill="none"><rect width="48" height="48" rx="${outer}" fill="${BG}"/><path d="M24 6L8 16v4h32v-4L24 6z" fill="${accent}" opacity="0.9"/><rect x="10" y="22" width="6" height="10" rx="${inner}" fill="${accent}" opacity="0.7"/><rect x="21" y="22" width="6" height="10" rx="${inner}" fill="${accent}" opacity="0.7"/><rect x="32" y="22" width="6" height="10" rx="${inner}" fill="${accent}" opacity="0.7"/><rect x="8" y="34" width="32" height="4" rx="${inner}" fill="${accent}" opacity="0.5"/><circle cx="24" cy="12" r="2" fill="${BG}"/></svg>`;
}

/**
 * Swaps the document favicon to match the current accent + UI style. SVG
 * favicons can't read the page's CSS variables (they render in isolation), so
 * we regenerate the markup with the resolved accent and set it as a data URI.
 */
export function applyFavicon(accent: string, uiStyle: UIStyle): void {
	if (typeof document === "undefined") return;
	const link = document.querySelector<HTMLLinkElement>("link[rel='icon']");
	if (!link) return;
	link.href = `data:image/svg+xml,${encodeURIComponent(
		buildFaviconSvg(accent, uiStyle),
	)}`;
}
