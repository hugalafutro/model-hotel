const ELLIPSIS = "\u2026";

/**
 * Truncate a string to `maxLength` characters, appending a Unicode ellipsis (…).
 * Strips trailing whitespace before the ellipsis to avoid "word …" gaps.
 */
export function truncateWithEllipsis(text: string, maxLength: number): string {
	if (text.length <= maxLength) return text;
	// Remove trailing whitespace before adding ellipsis
	const trimmed = text.slice(0, maxLength).trimEnd();
	return trimmed + ELLIPSIS;
}
