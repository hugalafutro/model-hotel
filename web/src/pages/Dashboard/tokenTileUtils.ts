export interface TileSegment {
	type: "prompt" | "completion";
	opacity: number;
}

export const TOTAL_SEGMENTS = 20;
export const SEGMENT_PCT = 100 / TOTAL_SEGMENTS; // 5%

/**
 * Compute waffle-scale tile segments from prompt/completion percentages.
 * Always produces exactly TOTAL_SEGMENTS tiles. Each non-zero type gets
 * at least 1 tile. When a type's actual percentage is less than one
 * full segment (5%), its tile is shaded proportionally.
 */
export function computeTileSegments(
	promptPct: number,
	completionPct: number,
): TileSegment[] {
	const tiles: TileSegment[] = [];
	if (promptPct + completionPct === 0) return tiles;

	let promptCount = Math.round(promptPct / SEGMENT_PCT);

	// Min 1 segment for any non-zero value
	if (promptPct > 0 && promptCount < 1) promptCount = 1;
	// Leave at least 1 segment for a non-zero other type
	if (completionPct > 0 && promptCount >= TOTAL_SEGMENTS)
		promptCount = TOTAL_SEGMENTS - 1;
	// Edge: one side is zero
	if (promptPct === 0) promptCount = 0;
	if (completionPct === 0) promptCount = TOTAL_SEGMENTS;

	const completionCount = TOTAL_SEGMENTS - promptCount;

	for (let i = 0; i < promptCount; i++) {
		tiles.push({ type: "prompt", opacity: 1 });
	}
	for (let i = 0; i < completionCount; i++) {
		tiles.push({ type: "completion", opacity: 1 });
	}

	// Shade the minority tile when a type's actual percentage is less
	// than one full segment — communicates "this tile is not really full"
	if (completionPct > 0 && completionPct < SEGMENT_PCT) {
		const idx = promptCount;
		if (idx < tiles.length) {
			tiles[idx] = {
				type: "completion",
				opacity: Math.min(1, completionPct / SEGMENT_PCT),
			};
		}
	} else if (promptPct > 0 && promptPct < SEGMENT_PCT) {
		if (tiles.length > 0) {
			tiles[0] = {
				type: "prompt",
				opacity: Math.min(1, promptPct / SEGMENT_PCT),
			};
		}
	}

	return tiles;
}
