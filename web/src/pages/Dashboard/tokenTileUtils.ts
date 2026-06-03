export interface TileSegment {
	type: "prompt" | "completion" | "cache_hit";
	opacity: number;
}

export const TOTAL_SEGMENTS = 20;
export const SEGMENT_PCT = 100 / TOTAL_SEGMENTS; // 5%

/**
 * Compute waffle-scale tile segments from prompt/completion/cache-hit percentages.
 * Always produces exactly TOTAL_SEGMENTS tiles. Each non-zero type gets
 * at least 1 tile. When a type's actual percentage is less than one
 * full segment (5%), its tile is shaded proportionally.
 *
 * Prompt tokens are split into cache_hit (accent) and prompt (uncached, purple).
 * When prompt > 0 and cache_hit > 0, at least 1 tile is reserved for uncached
 * prompt tokens to avoid the visual impression of 100% cache coverage.
 *
 * Tile order (left to right): cache_hit, prompt, completion.
 */
export function computeTileSegments(
	promptPct: number,
	completionPct: number,
	cacheHitPct: number = 0,
): TileSegment[] {
	const tiles: TileSegment[] = [];
	if (promptPct + completionPct === 0) return tiles;

	// Step 1: Split total tiles between prompt (all) and completion
	let promptTotalCount = Math.round(promptPct / SEGMENT_PCT);

	// Min 1 segment for any non-zero value
	if (promptPct > 0 && promptTotalCount < 1) promptTotalCount = 1;
	// Leave at least 1 segment for a non-zero other type
	if (completionPct > 0 && promptTotalCount >= TOTAL_SEGMENTS)
		promptTotalCount = TOTAL_SEGMENTS - 1;
	// Edge: one side is zero
	if (promptPct === 0) promptTotalCount = 0;
	if (completionPct === 0) promptTotalCount = TOTAL_SEGMENTS;

	const completionCount = TOTAL_SEGMENTS - promptTotalCount;

	// Step 2: Within prompt tiles, split between cache_hit and uncached prompt
	let cacheHitCount = 0;
	let uncachedCount = promptTotalCount;

	if (promptTotalCount > 0 && cacheHitPct > 0) {
		// cacheHitPct is the percentage of TOTAL tokens that are cache hits
		// Within prompt tiles, the ratio is cacheHitPct / promptPct
		const cacheHitRatio = promptPct > 0 ? cacheHitPct / promptPct : 0;
		cacheHitCount = Math.round(cacheHitRatio * promptTotalCount);

		// Reserve at least 1 tile for uncached prompt when there are
		// both cached and uncached prompt tokens
		const uncachedTokens = promptPct - cacheHitPct;
		if (uncachedTokens > 0 && cacheHitCount >= promptTotalCount) {
			cacheHitCount = promptTotalCount - 1;
		}
		// Clamp: no negative cache_hit tiles
		if (cacheHitCount < 0) cacheHitCount = 0;

		uncachedCount = promptTotalCount - cacheHitCount;
	}

	// Step 3: Build tile array — order: cache_hit, prompt, completion
	for (let i = 0; i < cacheHitCount; i++) {
		tiles.push({ type: "cache_hit", opacity: 1 });
	}
	for (let i = 0; i < uncachedCount; i++) {
		tiles.push({ type: "prompt", opacity: 1 });
	}
	for (let i = 0; i < completionCount; i++) {
		tiles.push({ type: "completion", opacity: 1 });
	}

	// Step 4: Shade minority tiles when a type's actual percentage is less
	// than one full segment — communicates "this tile is not really full"
	if (completionPct > 0 && completionPct < SEGMENT_PCT) {
		const idx = cacheHitCount + uncachedCount;
		if (idx < tiles.length) {
			tiles[idx] = {
				type: "completion",
				opacity: Math.min(1, completionPct / SEGMENT_PCT),
			};
		}
	} else if (promptPct > 0 && promptPct < SEGMENT_PCT) {
		const idx = cacheHitCount;
		if (idx < tiles.length) {
			tiles[idx] = {
				type: "prompt",
				opacity: Math.min(1, promptPct / SEGMENT_PCT),
			};
		}
	}

	return tiles;
}
