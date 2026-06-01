/**
 * Parse a Go duration string to total seconds.
 * Supports format: "30s", "5m", "1h", "1h30m", "7d", etc.
 */
export function goDurationToSeconds(d: string): number {
	if (!d) return 0;
	let total = 0;
	const dayMatch = d.match(/(\d+)d/);
	const hourMatch = d.match(/(\d+)h/);
	const minMatch = d.match(/(\d+)m(?!s)/);
	const secMatch = /(\d+)s/.exec(d);
	if (dayMatch) total += Number(dayMatch[1]) * 86400;
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

/**
 * Parse a Go duration string to total hours (rounded to nearest 0.5).
 * Used by discovery interval and log retention sliders.
 */
export function goDurationToHours(d: string): number {
	if (!d || d === "0") return 0;
	let total = 0;
	const dayMatch = d.match(/(\d+)d/);
	const hourMatch = d.match(/(\d+)h/);
	const minMatch = d.match(/(\d+)m(?!s)/);
	const secMatch = /(\d+)s/.exec(d);
	if (dayMatch) total += Number(dayMatch[1]) * 24;
	if (hourMatch) total += Number(hourMatch[1]);
	if (minMatch) total += Number(minMatch[1]) / 60;
	if (secMatch) total += Number(secMatch[1]) / 3600;
	return Math.round(total * 2) / 2;
}

/**
 * Convert hours to a Go duration string.
 * Supports half-hour precision (0.5 → "30m", 1.5 → "1h30m").
 */
export function hoursToGoDuration(h: number): string {
	if (h <= 0) return "0";
	const wholeHours = Math.floor(h);
	const halfHour = h - wholeHours;
	let result = "";
	if (wholeHours > 0) result += `${wholeHours}h`;
	if (halfHour === 0.5) result += "30m";
	if (result === "") result = "0";
	return result;
}

/**
 * Parse a Go duration string to total minutes (rounded).
 * Used by stale request timeout slider.
 */
export function goDurationToMinutes(d: string): number {
	if (!d || d === "0") return 0;
	let total = 0;
	const dayMatch = d.match(/(\d+)d/);
	const hourMatch = d.match(/(\d+)h/);
	const minMatch = d.match(/(\d+)m(?!s)/);
	const secMatch = /(\d+)s/.exec(d);
	if (dayMatch) total += Number(dayMatch[1]) * 1440;
	if (hourMatch) total += Number(hourMatch[1]) * 60;
	if (minMatch) total += Number(minMatch[1]);
	if (secMatch) total += Number(secMatch[1]) / 60;
	return Math.round(total);
}

/**
 * Convert minutes to a Go duration string.
 * Examples: 90 → "1h30m", 5 → "5m".
 */
export function minutesToGoDuration(m: number): string {
	if (m <= 0) return "0";
	const h = Math.floor(m / 60);
	const mins = m % 60;
	let result = "";
	if (h > 0) result += `${h}h`;
	if (mins > 0) result += `${mins}m`;
	if (result === "") result = "0";
	return result;
}
