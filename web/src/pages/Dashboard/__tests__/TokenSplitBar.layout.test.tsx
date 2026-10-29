import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TokenSplitBar } from "../TokenSplitBar";

// Once the bar has a measured width, tiles are laid out on whole pixels: each
// gap is exactly 2px and the leftover pixels widen the first tiles by one, so
// the row fills the bar exactly. jsdom measures nothing, so the width is
// supplied by stubbing the resize hook.
const BAR_WIDTH = 503;
vi.mock("../../../hooks/useResizeObserver", () => ({
	useResizeObserver: () => ({ ref: () => {}, width: BAR_WIDTH }),
}));

describe("TokenSplitBar integer layout", () => {
	it("places tiles on whole pixels with 2px gaps and fills the bar", () => {
		render(
			<TokenSplitBar
				prompt={600}
				completion={400}
				cacheHit={100}
				total={1000}
				range="24h"
				onRangeChange={vi.fn()}
			/>,
		);
		const tiles = Array.from(
			screen.getByRole("img").querySelectorAll<HTMLElement>("[data-tile-type]"),
		);
		expect(tiles.length).toBeGreaterThan(1);
		const boxes = tiles.map((el) => ({
			left: Number.parseFloat(el.style.left),
			width: Number.parseFloat(el.style.width),
		}));
		expect(boxes[0].left).toBe(0);
		for (let i = 1; i < boxes.length; i++) {
			expect(boxes[i].left).toBe(boxes[i - 1].left + boxes[i - 1].width + 2);
		}
		const last = boxes[boxes.length - 1];
		expect(last.left + last.width).toBe(BAR_WIDTH);
		// Widths differ by at most the one leftover pixel.
		const widths = new Set(boxes.map((b) => b.width));
		expect(Math.max(...widths) - Math.min(...widths)).toBeLessThanOrEqual(1);
	});
});
