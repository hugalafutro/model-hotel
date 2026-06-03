import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TokenSplitBar } from "../TokenSplitBar";

describe("TokenSplitBar", () => {
	const defaultProps = {
		prompt: 600,
		completion: 400,
		cacheHit: 0,
		total: 1000,
		range: "24h" as const,
		onRangeChange: vi.fn(),
	};

	it("renders with entries", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("Token Mix")).toBeInTheDocument();
		expect(screen.getByText("1,000")).toBeInTheDocument();
		expect(screen.getByText("Tokens")).toBeInTheDocument();
	});

	it("handles empty entries (zero tokens)", () => {
		render(
			<TokenSplitBar
				{...defaultProps}
				prompt={0}
				completion={0}
				cacheHit={0}
				total={0}
			/>,
		);

		expect(
			screen.getByText(
				"No token data yet. Token mix will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("displays prompt, completion, and cache hit labels", () => {
		render(<TokenSplitBar {...defaultProps} cacheHit={100} />);

		expect(screen.getByText("Cache hit")).toBeInTheDocument();
		expect(screen.getByText("Prompt")).toBeInTheDocument();
		expect(screen.getByText("Completion")).toBeInTheDocument();
	});

	it("displays cache hit value in legend", () => {
		render(<TokenSplitBar {...defaultProps} cacheHit={300} />);

		const legend = screen.getByTestId("legend");
		// Find the cache hit entry by looking for the label, then check its count
		const cacheHitLabel = within(legend).getByText("Cache hit");
		const cacheHitEntry = cacheHitLabel.closest("div");
		// biome-ignore lint/style/noNonNullAssertion: test assertion, label always has parent
		expect(within(cacheHitEntry!).getByText("300")).toBeInTheDocument();
	});

	it("shows uncached prompt count (prompt minus cache hit)", () => {
		render(<TokenSplitBar {...defaultProps} prompt={600} cacheHit={300} />);

		// 600 prompt - 300 cacheHit = 300 uncached prompt
		const legend = screen.getByTestId("legend");
		const promptLabel = within(legend).getByText("Prompt");
		const promptEntry = promptLabel.closest("div");
		// biome-ignore lint/style/noNonNullAssertion: test assertion, label always has parent
		expect(within(promptEntry!).getByText("300")).toBeInTheDocument();
	});

	it("displays percentages in legend", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("60.0%")).toBeInTheDocument();
		expect(screen.getByText("40.0%")).toBeInTheDocument();
	});

	it("shows all percentages in legend regardless of split ratio", () => {
		render(<TokenSplitBar {...defaultProps} prompt={10} completion={990} />);

		expect(screen.getByText("1.0%")).toBeInTheDocument();
		expect(screen.getByText("99.0%")).toBeInTheDocument();
	});

	it("renders RangeToggle component", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("1W")).toBeInTheDocument();
	});

	it("calls onRangeChange when range option is clicked", async () => {
		const onRangeChange = vi.fn();
		const user = userEvent.setup();

		render(<TokenSplitBar {...defaultProps} onRangeChange={onRangeChange} />);

		const oneHButton = screen.getByText("1H");
		await user.click(oneHButton);

		expect(onRangeChange).toHaveBeenCalledWith("1h");
		expect(onRangeChange).toHaveBeenCalledTimes(1);
	});

	it("calls onRangeChange when 7d option is clicked", async () => {
		const onRangeChange = vi.fn();
		const user = userEvent.setup();

		render(<TokenSplitBar {...defaultProps} onRangeChange={onRangeChange} />);

		const sevenDButton = screen.getByText("1W");
		await user.click(sevenDButton);

		expect(onRangeChange).toHaveBeenCalledWith("1w");
	});

	it("displays loading spinner when loading is true", () => {
		render(<TokenSplitBar {...defaultProps} loading />);

		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not display loading spinner when loading is false", () => {
		render(<TokenSplitBar {...defaultProps} loading={false} />);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});

	it("renders Target icon", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const svg = container.querySelector("svg");
		expect(svg).toBeInTheDocument();
	});

	it("renders 20 waffle tiles for a balanced split", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const promptTiles = container.querySelectorAll("[data-tile-type='prompt']");
		const completionTiles = container.querySelectorAll(
			"[data-tile-type='completion']",
		);
		expect(promptTiles).toHaveLength(12);
		expect(completionTiles).toHaveLength(8);
		expect(promptTiles.length + completionTiles.length).toBe(20);
	});

	it("renders cache_hit tiles when cacheHit > 0", () => {
		const { container } = render(
			<TokenSplitBar {...defaultProps} cacheHit={300} />,
		);

		const cacheHitTiles = container.querySelectorAll(
			"[data-tile-type='cache_hit']",
		);
		const promptTiles = container.querySelectorAll("[data-tile-type='prompt']");
		const completionTiles = container.querySelectorAll(
			"[data-tile-type='completion']",
		);
		expect(cacheHitTiles.length).toBeGreaterThan(0);
		expect(
			cacheHitTiles.length + promptTiles.length + completionTiles.length,
		).toBe(20);
	});

	it("reserves at least 1 uncached prompt tile at high cache-hit ratio", () => {
		// 96% cache hit on prompt tokens — should still show at least 1 prompt tile
		const { container } = render(
			<TokenSplitBar
				{...defaultProps}
				prompt={1000}
				cacheHit={960}
				completion={100}
				total={1100}
			/>,
		);

		const promptTiles = container.querySelectorAll("[data-tile-type='prompt']");
		expect(promptTiles.length).toBeGreaterThanOrEqual(1);
	});

	it("applies correct colors to tiles", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const promptTiles = container.querySelectorAll("[data-tile-type='prompt']");
		const completionTiles = container.querySelectorAll(
			"[data-tile-type='completion']",
		);

		promptTiles.forEach((tile) => {
			expect(tile).toHaveStyle({ backgroundColor: "#818cf8" });
		});
		completionTiles.forEach((tile) => {
			expect(tile).toHaveStyle({ backgroundColor: "#059669" });
		});
	});

	it("applies accent color to cache_hit tiles", () => {
		const { container } = render(
			<TokenSplitBar {...defaultProps} cacheHit={300} />,
		);

		const cacheHitTiles = container.querySelectorAll(
			"[data-tile-type='cache_hit']",
		);
		cacheHitTiles.forEach((tile) => {
			expect(tile).toHaveStyle({ backgroundColor: "var(--accent)" });
		});
	});

	it("applies correct colors to legend indicators", () => {
		const { container } = render(
			<TokenSplitBar {...defaultProps} cacheHit={100} />,
		);

		const indicators = container.querySelectorAll(
			"span[style*='background-color']",
		);
		expect(indicators).toHaveLength(3);
	});

	it("gives minority at least 1 tile for extreme split", () => {
		const { container } = render(
			<TokenSplitBar
				{...defaultProps}
				prompt={223784418}
				completion={1954511}
				total={225738929}
			/>,
		);

		const completionTiles = container.querySelectorAll(
			"[data-tile-type='completion']",
		);
		expect(completionTiles.length).toBeGreaterThanOrEqual(1);
	});

	it("shades minority tile with reduced opacity for extreme split", () => {
		const { container } = render(
			<TokenSplitBar
				{...defaultProps}
				prompt={223784418}
				completion={1954511}
				total={225738929}
			/>,
		);

		const completionTiles = container.querySelectorAll(
			"[data-tile-type='completion']",
		);
		const minorityTile = completionTiles[0] as HTMLElement;
		// 0.87% / 5% ≈ 0.17 opacity
		expect(minorityTile.style.opacity).not.toBe("1");
		expect(Number.parseFloat(minorityTile.style.opacity)).toBeLessThan(1);
	});

	it("all tiles have full opacity for moderate splits", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const allTiles = container.querySelectorAll("[data-tile-type]");
		allTiles.forEach((tile) => {
			expect((tile as HTMLElement).style.opacity || "1").toBe("1");
		});
	});

	it("adds tooltips to tiles", () => {
		render(<TokenSplitBar {...defaultProps} />);

		const promptTiles = screen.getAllByTitle("Prompt: 60.0% (600 tokens)");
		const completionTiles = screen.getAllByTitle(
			"Completion: 40.0% (400 tokens)",
		);
		expect(promptTiles).toHaveLength(12);
		expect(completionTiles).toHaveLength(8);
	});

	it("adds cache hit tooltips when cacheHit > 0", () => {
		render(<TokenSplitBar {...defaultProps} cacheHit={300} />);

		const cacheHitTiles = screen.getAllByTitle("Cache hit: 30.0% (300 tokens)");
		expect(cacheHitTiles.length).toBeGreaterThan(0);
	});

	it("formats large token numbers with locale separators", () => {
		render(
			<TokenSplitBar
				{...defaultProps}
				prompt={1234567}
				completion={987654}
				total={2222221}
			/>,
		);

		expect(screen.getByText("2,222,221")).toBeInTheDocument();
		expect(screen.getByText("1,234,567")).toBeInTheDocument();
		expect(screen.getByText("987,654")).toBeInTheDocument();
	});

	it("renders card container with correct classes", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const card = container.querySelector(".ui-card");
		expect(card).toBeInTheDocument();
		expect(card).toHaveClass("p-6");
	});

	it("renders tile container with accessible role", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const tileContainer = container.querySelector("[role='img']");
		expect(tileContainer).toBeInTheDocument();
		expect(tileContainer).toHaveClass("h-6");
	});

	it("handles prompt-only tokens (no completion)", () => {
		render(
			<TokenSplitBar
				{...defaultProps}
				completion={0}
				cacheHit={0}
				total={500}
			/>,
		);

		expect(screen.getByText("500")).toBeInTheDocument();
		expect(screen.getByText("100.0%")).toBeInTheDocument();
		const legend = screen.getByTestId("legend");
		// Check that 0 appears (for completion=0 and/or cacheHit=0)
		const zeros = within(legend).getAllByText("0");
		expect(zeros.length).toBeGreaterThanOrEqual(1);
	});

	it("handles completion-only tokens (no prompt)", () => {
		render(
			<TokenSplitBar {...defaultProps} prompt={0} cacheHit={0} total={500} />,
		);

		expect(screen.getByText("500")).toBeInTheDocument();
		expect(screen.getByText("100.0%")).toBeInTheDocument();
		const legend = screen.getByTestId("legend");
		// Check that 0 appears (for prompt=0 and/or cacheHit=0)
		const zeros = within(legend).getAllByText("0");
		expect(zeros.length).toBeGreaterThanOrEqual(1);
	});
});
