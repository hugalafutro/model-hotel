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
		expect(tiles.every((t) => t.type === "prompt")).toBe(true);
		expect(tiles).toHaveLength(TOTAL_SEGMENTS);
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
		// 0.5% rounds to 0.1 segments → should still get 1 tile
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

	it("prompt tiles come before completion tiles", () => {
		const tiles = computeTileSegments(60, 40);
		const firstCompletion = tiles.findIndex((t) => t.type === "completion");
		// All tiles before firstCompletion should be prompt
		for (let i = 0; i < firstCompletion; i++) {
			expect(tiles[i].type).toBe("prompt");
		}
		// All tiles from firstCompletion should be completion
		for (let i = firstCompletion; i < tiles.length; i++) {
			expect(tiles[i].type).toBe("completion");
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
});
