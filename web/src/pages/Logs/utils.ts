/* =========================================================
   Request-logs formatting helpers
   ===================================================== */
export function formatTPS(t: number | null): string {
	if (t == null || t === 0) return "-";
	return t.toFixed(1);
}

export function formatMs(
	v: number | null | undefined,
	decimals: number = 2,
): string {
	if (v == null || v === 0) return "-";
	return `${v.toFixed(decimals)}ms`;
}
