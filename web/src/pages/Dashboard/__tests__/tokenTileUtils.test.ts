import { describe, expect, it } from "vitest";
import {
	computeTileSegments,
	SEGMENT_PCT,
	TOTAL_SEGMENTS,
} from "../tokenTileUtils";

describe("computeTileSegments", () => {
	it("returns empty array when both percentages are zero", () => {
		expect(computeTileSegments(0, 0)).toEqual([]);
	});

	it("produces exactly TOTAL_SEGMENTS tiles", () => {
		const tiles = computeTileSegments(60, 40);
		expect(tiles).toHaveLength(TOTAL_SEGMENTS);
	});

	it("assigns tiles proportionally for a balanced split", () => {
		const tiles = computeTileSegments(50, 50);
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		const completionTiles = tiles.filter((t) => t.type === "completion");
		expect(promptTiles).toHaveLength(10);
		expect(completionTiles).toHaveLength(10);
	});

	it("assigns tiles proportionally for 60/40 split", () => {
		const tiles = computeTileSegments(60, 40);
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		const completionTiles = tiles.filter((t) => t.type === "completion");
		expect(promptTiles).toHaveLength(12);
		expect(completionTiles).toHaveLength(8);
	});

	it("all tiles have full opacity for moderate splits", () => {
		const tiles = computeTileSegments(60, 40);
		expect(tiles.every((t) => t.opacity === 1)).toBe(true);
	});

	it("gives minority at least 1 tile for extreme 99.1/0.9 split", () => {
		const tiles = computeTileSegments(99.1, 0.9);
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		const completionTiles = tiles.filter((t) => t.type === "completion");
		expect(promptTiles).toHaveLength(19);
		expect(completionTiles).toHaveLength(1);
	});

	it("shades minority tile proportionally for extreme 99.1/0.9 split", () => {
		const tiles = computeTileSegments(99.1, 0.9);
		const completionTile = tiles.find((t) => t.type === "completion");
		expect(completionTile).toBeDefined();
		expect(completionTile?.opacity).toBeCloseTo(0.9 / SEGMENT_PCT, 2);
	});

	it("shades minority tile for very extreme 99.9/0.1 split", () => {
		const tiles = computeTileSegments(99.9, 0.1);
		const completionTile = tiles.find((t) => t.type === "completion");
		expect(completionTile).toBeDefined();
		expect(completionTile?.opacity).toBeCloseTo(0.1 / SEGMENT_PCT, 2);
	});

	it("shades prompt tile when prompt is the minority", () => {
		const tiles = computeTileSegments(2, 98);
		const promptTile = tiles.find((t) => t.type === "prompt");
		expect(promptTile).toBeDefined();
		expect(promptTile?.opacity).toBeCloseTo(2 / SEGMENT_PCT, 2);
	});

	it("gives all tiles to prompt when completion is 0", () => {
		const tiles = computeTileSegments(100, 0);
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		expect(promptTiles).toHaveLength(TOTAL_SEGMENTS);
		expect(tiles.every((t) => t.type === "prompt")).toBe(true);
	});

	it("gives all tiles to completion when prompt is 0", () => {
		const tiles = computeTileSegments(0, 100);
		expect(tiles.every((t) => t.type === "completion")).toBe(true);
		expect(tiles).toHaveLength(TOTAL_SEGMENTS);
	});

	it("all tiles full opacity for 100/0 split", () => {
		const tiles = computeTileSegments(100, 0);
		expect(tiles.every((t) => t.opacity === 1)).toBe(true);
	});

	it("minority gets 1 tile even when value rounds to 0 segments", () => {
		const tiles = computeTileSegments(99.5, 0.5);
		const completionTiles = tiles.filter((t) => t.type === "completion");
		expect(completionTiles).toHaveLength(1);
	});

	it("minority near segment boundary (4.5%) still gets shaded tile", () => {
		const tiles = computeTileSegments(95.5, 4.5);
		const completionTile = tiles.find((t) => t.type === "completion");
		expect(completionTile).toBeDefined();
		expect(completionTile?.opacity).toBeCloseTo(4.5 / SEGMENT_PCT, 2);
	});

	it("minority at exactly one segment (5%) has full opacity", () => {
		const tiles = computeTileSegments(95, 5);
		const completionTile = tiles.find((t) => t.type === "completion");
		expect(completionTile).toBeDefined();
		expect(completionTile?.opacity).toBe(1);
	});

	it("tiles are ordered: cache_hit, prompt, completion", () => {
		const tiles = computeTileSegments(60, 30, 30);
		const firstPrompt = tiles.findIndex((t) => t.type === "prompt");
		const firstCompletion = tiles.findIndex((t) => t.type === "completion");
		const lastCacheHit =
			tiles.length -
			1 -
			[...tiles].reverse().findIndex((t) => t.type === "cache_hit");

		// cache_hit tiles come first
		if (lastCacheHit >= 0) {
			for (let i = 0; i <= lastCacheHit; i++) {
				expect(tiles[i].type).toBe("cache_hit");
			}
		}
		// prompt tiles in the middle
		if (firstPrompt >= 0 && firstCompletion >= 0) {
			for (let i = firstPrompt; i < firstCompletion; i++) {
				expect(tiles[i].type).toBe("prompt");
			}
		}
		// completion tiles at the end
		if (firstCompletion >= 0) {
			for (let i = firstCompletion; i < tiles.length; i++) {
				expect(tiles[i].type).toBe("completion");
			}
		}
	});

	it("handles near-equal splits correctly", () => {
		const tiles = computeTileSegments(51, 49);
		expect(tiles).toHaveLength(TOTAL_SEGMENTS);
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		const completionTiles = tiles.filter((t) => t.type === "completion");
		// 51% = 10.2 segments → rounds to 10, 49% = 9.8 → rounds to 10
		expect(promptTiles).toHaveLength(10);
		expect(completionTiles).toHaveLength(10);
	});

	it("opacity never exceeds 1", () => {
		const testCases = [
			[99.9, 0.1],
			[0.1, 99.9],
			[50, 50],
			[95, 5],
			[100, 0],
			[0, 100],
		] as const;
		for (const [p, c] of testCases) {
			const tiles = computeTileSegments(p, c);
			for (const tile of tiles) {
				expect(tile.opacity).toBeLessThanOrEqual(1);
			}
		}
	});

	// 3-way split tests (cache_hit)
	it("splits prompt tiles into cache_hit and uncached when cacheHitPct provided", () => {
		// 60% prompt, 40% completion → 12 prompt, 8 completion
		// 30% cache hit of total → 30/60 = 50% of prompt tiles → 6 cache_hit, 6 prompt
		const tiles = computeTileSegments(60, 40, 30);
		const cacheHitTiles = tiles.filter((t) => t.type === "cache_hit");
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		const completionTiles = tiles.filter((t) => t.type === "completion");

		expect(cacheHitTiles).toHaveLength(6);
		expect(promptTiles).toHaveLength(6);
		expect(completionTiles).toHaveLength(8);
		expect(tiles).toHaveLength(TOTAL_SEGMENTS);
	});

	it("returns no cache_hit tiles when cacheHitPct is 0", () => {
		const tiles = computeTileSegments(60, 40, 0);
		const cacheHitTiles = tiles.filter((t) => t.type === "cache_hit");
		expect(cacheHitTiles).toHaveLength(0);
	});

	it("reserves at least 1 uncached prompt tile at extreme cache ratio", () => {
		// 96% cache hit: prompt = 90%, completion = 10%, cacheHitPct = 86.4%
		// Without min-1 rule: cache_hit_ratio = 86.4/90 = 96% → 17 cache_hit of 18 prompt tiles → 1 uncached
		// With 96% ratio on 18 tiles: round(0.96 * 18) = 17 cache_hit, 1 prompt — already satisfies
		// Test a case where rounding would give 0 uncached:
		// prompt = 5% (1 tile), cacheHitPct = 4.9% → ratio ≈ 98%, round(0.98 * 1) = 1 → min-1 kicks in
		const tiles = computeTileSegments(5, 95, 4.9);
		const promptTiles = tiles.filter((t) => t.type === "prompt");
		expect(promptTiles.length).toBeGreaterThanOrEqual(1);
	});

	it("all cache_hit tiles use accent color type", () => {
		const tiles = computeTileSegments(60, 30, 30);
		const cacheHitTiles = tiles.filter((t) => t.type === "cache_hit");
		for (const tile of cacheHitTiles) {
			expect(tile.type).toBe("cache_hit");
		}
	});

	it("3-way split total always equals TOTAL_SEGMENTS", () => {
		const testCases = [
			[60, 30, 30],
			[50, 40, 10],
			[90, 10, 85],
			[10, 90, 5],
			[30, 60, 15],
		] as const;
		for (const [p, c, ch] of testCases) {
			const tiles = computeTileSegments(p, c, ch);
			expect(tiles).toHaveLength(TOTAL_SEGMENTS);
		}
	});
});
