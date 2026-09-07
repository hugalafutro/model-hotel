import { formatCompact } from "../../utils/format";

// Compact Y-axis tick labels so large values (e.g. hundreds of millions) don't
// get clipped by the axis width — full-precision numbers like "100,000,000"
// overflow the default axis, worst in the monospace terminal theme. Tooltips
// keep the full value; only the axis is abbreviated. Below 1000 the raw number
// reads fine, so it keeps its locale grouping instead.
export function formatAxisTick(value: number, allowDecimals: boolean): string {
	if (Math.abs(value) >= 1_000) return formatCompact(value);
	return value.toLocaleString(undefined, {
		maximumFractionDigits: allowDecimals ? 2 : 0,
	});
}
