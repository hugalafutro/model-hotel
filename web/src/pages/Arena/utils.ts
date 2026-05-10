/** Returns the smallest valid bracket size (2, 4, or 8) that fits `count` models. */
export function nextBracketSize(count: number): number {
	return count <= 2 ? 2 : count <= 4 ? 4 : 8;
}
