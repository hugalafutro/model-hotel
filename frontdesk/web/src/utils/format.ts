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

/** A USD amount. Pinned to en-US so the currency symbol matches the API's units. */
export function formatDollars(v: number): string {
	return v.toLocaleString("en-US", { style: "currency", currency: "USD" });
}

/** A kWh magnitude, at most two decimals. The unit is appended by the caller. */
export function formatKwh(v: number): string {
	return v.toLocaleString("en-US", { maximumFractionDigits: 2 });
}
