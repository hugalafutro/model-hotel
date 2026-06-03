/** Returns a Tailwind bg-[color] class based on remaining percentage. */
export function remainingBarColor(remainingPct: number): string {
	if (remainingPct < 20) return "bg-red-500";
	if (remainingPct < 60) return "bg-amber-500";
	return "bg-[#6366F1]";
}

/** Returns a Tailwind bg-[color] class based on used percentage. */
export function usedBarColor(usedPct: number): string {
	if (usedPct < 50) return "bg-amber-500";
	if (usedPct < 80) return "bg-orange-500";
	return "bg-red-500";
}
