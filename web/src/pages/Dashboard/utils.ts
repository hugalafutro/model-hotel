export function formatCompact(n: number): string {
	if (n === 0) return "0";
	const abs = Math.abs(n);
	const fmt = (v: number) => {
		const s = v.toFixed(1);
		return s.endsWith(".0") ? s.slice(0, -2) : s;
	};
	if (abs >= 1_000_000) return `${fmt(n / 1_000_000)}M`;
	if (abs >= 1_000) return `${fmt(n / 1_000)}K`;
	return fmt(n);
}

export function dropTrailingZero(v: number, decimals: number): string {
	const s = v.toFixed(decimals);
	if (decimals > 0 && s.includes(".")) {
		return s.replace(/\.?0+$/, "");
	}
	return s;
}
