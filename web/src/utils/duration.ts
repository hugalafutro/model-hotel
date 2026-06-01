/**
 * Parse a Go duration string to total seconds.
 * Supports format: "30s", "5m", "1h", "1h30m", etc.
 */
export function goDurationToSeconds(d: string): number {
	if (!d) return 0;
	let total = 0;
	const hourMatch = d.match(/(\d+)h/);
	const minMatch = d.match(/(\d+)m/);
	const secMatch = /(\d+)s/.exec(d);
	if (hourMatch) total += Number(hourMatch[1]) * 3600;
	if (minMatch) total += Number(minMatch[1]) * 60;
	if (secMatch) total += Number(secMatch[1]);
	return total;
}

/**
 * Convert total seconds to a Go duration string.
 * Examples: 0 → "0s", 3600 → "1h", 90 → "1m30s".
 */
export function secondsToGoDuration(s: number): string {
	if (s <= 0) return "0s";
	const h = Math.floor(s / 3600);
	const m = Math.floor((s % 3600) / 60);
	const sec = s % 60;
	let result = "";
	if (h > 0) result += `${h}h`;
	if (m > 0) result += `${m}m`;
	if (sec > 0 || result === "") result += `${sec}s`;
	return result;
}
