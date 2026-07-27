// Number formatting for quota badges and modals. Ported from the Model Hotel
// dashboard (web/src/utils/format.ts) so both frontends render the same numbers.
// Date and relative-time formatting lives in ./time.ts; do not duplicate it here.

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

/**
 * A whole-item count with digit grouping, or "-" when the value is absent.
 * Unlike formatTokens this does NOT abbreviate: a daily image allowance is a
 * small number the operator reads exactly, and "1.2K/1.5K" would hide the
 * difference between 1,200 and 1,249. Pinned to en-US like the formatters
 * below so every locale renders the same figure.
 */
export function formatCount(n: number | null | undefined): string {
	if (n == null) return "-";
	return Math.round(n).toLocaleString("en-US");
}

/** A USD amount. Pinned to en-US so the currency symbol matches the API's units. */
export function formatDollars(v: number): string {
	return v.toLocaleString("en-US", { style: "currency", currency: "USD" });
}

/** A kWh magnitude, at most two decimals. The unit is appended by the caller. */
export function formatKwh(v: number): string {
	return v.toLocaleString("en-US", { maximumFractionDigits: 2 });
}
