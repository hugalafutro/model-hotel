// One decimal, but drop a trailing ".0" (1.0M → "1M", 1.2M → "1.2M").
function trimOneDecimal(n: number): string {
	return n.toFixed(1).replace(/\.0$/, "");
}

// Compact Y-axis tick labels so large values (e.g. hundreds of millions) don't
// get clipped by the axis width — full-precision numbers like "100,000,000"
// overflow the default axis, worst in the monospace terminal theme. Tooltips
// keep the full value; only the axis is abbreviated.
export function formatAxisTick(value: number, allowDecimals: boolean): string {
	const abs = Math.abs(value);
	if (abs >= 1_000_000_000) return `${trimOneDecimal(value / 1_000_000_000)}B`;
	if (abs >= 1_000_000) return `${trimOneDecimal(value / 1_000_000)}M`;
	if (abs >= 1_000) return `${trimOneDecimal(value / 1_000)}K`;
	return value.toLocaleString(undefined, {
		maximumFractionDigits: allowDecimals ? 2 : 0,
	});
}
