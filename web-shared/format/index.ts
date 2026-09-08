// The number formatters both frontends render identically: compact magnitudes
// and the units that ride on them. Pinned to en-US rather than the browser
// locale, so a figure the two dashboards show side by side reads the same
// wherever it is read from. Anything whose wording depends on the active
// language (durations, relative times, count labels) stays in the app, because
// it needs i18next and this module takes no package imports.

/** Abbreviates a number to K/M/B with at most one decimal, dropping a trailing .0. */
export function formatCompact(n: number): string {
	if (n === 0) return "0";
	const abs = Math.abs(n);
	const fmt = (v: number) => {
		const s = v.toFixed(1);
		return s.endsWith(".0") ? s.slice(0, -2) : s;
	};
	if (abs >= 1_000_000_000) return `${fmt(n / 1_000_000_000)}B`;
	if (abs >= 1_000_000) return `${fmt(n / 1_000_000)}M`;
	if (abs >= 1_000) return `${fmt(n / 1_000)}K`;
	return fmt(n);
}

/** Compact token count, or "-" when the value is absent. Zero renders as "0". */
export function formatTokens(n: number | null | undefined): string {
	if (n == null) return "-";
	return formatCompact(n);
}

/** A USD amount. Pinned to en-US so the currency symbol matches the API's units. */
export function formatDollars(v: number): string {
	return v.toLocaleString("en-US", { style: "currency", currency: "USD" });
}

/** A kWh magnitude, at most two decimals. The unit is appended by the caller. */
export function formatKwh(v: number): string {
	return v.toLocaleString("en-US", { maximumFractionDigits: 2 });
}

/**
 * A whole-item count with digit grouping, or "-" when the value is absent.
 * Unlike formatTokens this does NOT abbreviate: a daily image allowance is a
 * small number the operator reads exactly, and "1.2K/1.5K" would hide the
 * difference between 1,200 and 1,249.
 */
export function formatCount(n: number | null | undefined): string {
	if (n == null) return "-";
	return Math.round(n).toLocaleString("en-US");
}

/** Confines a value to [lo, hi]. With an inverted range (lo > hi), hi wins. */
export function clamp(v: number, lo: number, hi: number): number {
	return Math.min(Math.max(v, lo), hi);
}
