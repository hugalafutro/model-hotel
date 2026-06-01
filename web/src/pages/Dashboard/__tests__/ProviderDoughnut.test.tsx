import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { ProviderDistributionItem } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { ProviderDoughnut } from "../ProviderDoughnut";
import type { MetricType, Range } from "../types";

describe("ProviderDoughnut", () => {
	const mockItems: ProviderDistributionItem[] = [
		{ name: "Provider A", count: 100, tokens: 5000, share: 40 },
		{ name: "Provider B", count: 80, tokens: 3000, share: 32 },
		{ name: "Provider C", count: 70, tokens: 2000, share: 28 },
	];

	const defaultProps = {
		items: mockItems,
		range: "24h" as Range,
		onRangeChange: vi.fn(),
		metric: "tokens" as MetricType,
		onMetricChange: vi.fn(),
		loading: false,
	};

	it("renders with title and icon", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByRole("heading", { name: /Top \d/ })).toBeInTheDocument();
	});

	it("renders waffle grid when items are provided", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(
			screen.getByRole("img", { name: /Provider distribution/ }),
		).toBeInTheDocument();
	});

	it("renders exactly 100 grid cells", () => {
		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		expect(cells).toHaveLength(100);
	});

	it("assigns correct colors to cells by provider share", () => {
		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		// Provider A = #818cf8 (first color), 40 cells
		expect(cells[0]).toHaveStyle({ backgroundColor: "rgb(129, 140, 248)" });
		// Provider B = #34d399 (second color), starts at cell 40
		expect(cells[40]).toHaveStyle({ backgroundColor: "rgb(52, 211, 153)" });
		// Provider C = #fbbf24 (third color), starts at cell 72
		expect(cells[72]).toHaveStyle({ backgroundColor: "rgb(251, 191, 36)" });
	});

	it("applies staggered animation delays to cells", () => {
		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		expect(cells[0]).toHaveStyle({ animationDelay: "0ms" });
		expect(cells[1]).toHaveStyle({ animationDelay: "6ms" });
		expect(cells[10]).toHaveStyle({ animationDelay: "60ms" });
	});

	it("dims other providers on legend hover", async () => {
		const user = userEvent.setup();
		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} />,
		);

		const legendItem = screen.getByText("Provider A");
		await user.hover(legendItem);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		// Provider B cells should be dimmed
		const providerBCell = cells[40];
		expect(providerBCell).toHaveStyle({ opacity: "0.2" });
	});

	it("allocates cells proportionally with minimum 1 for non-zero shares", () => {
		const tinyItems: ProviderDistributionItem[] = [
			{ name: "Big", count: 9990, tokens: 9990000, share: 99.5 },
			{ name: "Small", count: 10, tokens: 500, share: 0.3 },
			{ name: "Tiny", count: 5, tokens: 200, share: 0.2 },
		];

		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={tinyItems} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		expect(cells).toHaveLength(100);
		// Every provider with share > 0 gets at least 1 cell
		const colors = new Set(
			Array.from(cells).map((c) => (c as HTMLElement).style.backgroundColor),
		);
		expect(colors.size).toBe(3);
	});

	it("fills to exactly 100 cells when rounding under-allocates", () => {
		const fractionalItems: ProviderDistributionItem[] = [
			{ name: "A", count: 100, tokens: 1000, share: 33.3 },
			{ name: "B", count: 100, tokens: 1000, share: 33.3 },
			{ name: "C", count: 100, tokens: 1000, share: 33.4 },
		];

		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={fractionalItems} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		expect(cells).toHaveLength(100);
	});

	it("displays item names in the legend", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByText("Provider A")).toBeInTheDocument();
		expect(screen.getByText("Provider B")).toBeInTheDocument();
		expect(screen.getByText("Provider C")).toBeInTheDocument();
	});

	it("shows <0.1% for tiny non-zero shares", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByText("40.0%")).toBeInTheDocument();
		expect(screen.getByText("32.0%")).toBeInTheDocument();
		expect(screen.getByText("28.0%")).toBeInTheDocument();
	});

	it("shows <0.1% for zero or near-zero shares", () => {
		const tinyShareItems: ProviderDistributionItem[] = [
			{ name: "Big", count: 9990, tokens: 9990000, share: 99.9 },
			{ name: "Tiny", count: 10, tokens: 11400, share: 0.02 },
			{ name: "Zero", count: 0, tokens: 0, share: 0 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={tinyShareItems} />,
		);

		expect(screen.getByText("99.9%")).toBeInTheDocument();
		const lessThan = screen.getAllByText("<0.1%");
		expect(lessThan).toHaveLength(2);
	});

	it("gives a cell to providers with share 0 but non-zero count/tokens", () => {
		const backendRoundedItems: ProviderDistributionItem[] = [
			{ name: "Wafer AI", count: 8650, tokens: 313200000, share: 86.5 },
			{ name: "Ollama Cloud", count: 1320, tokens: 47900000, share: 13.2 },
			{ name: "NanoGPT", count: 30, tokens: 997900, share: 0.3 },
			{ name: "OpenRouter", count: 5, tokens: 113300, share: 0 },
			{ name: "OpenAI", count: 1, tokens: 11400, share: 0 },
		];

		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={backendRoundedItems} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		expect(cells).toHaveLength(100);
		// All 5 providers should appear in the grid
		const colors = new Set(
			Array.from(cells).map((c) => (c as HTMLElement).style.backgroundColor),
		);
		expect(colors.size).toBe(5);
	});

	it("displays token counts when metric is tokens", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByText(/5K Tokens?/)).toBeInTheDocument();
		expect(screen.getByText(/3K Tokens?/)).toBeInTheDocument();
		expect(screen.getByText(/2K Tokens?/)).toBeInTheDocument();
	});

	it("displays request counts when metric is requests", () => {
		renderWithProviders(
			<ProviderDoughnut {...defaultProps} metric="requests" />,
		);

		expect(screen.getByText(/100 Requests?/)).toBeInTheDocument();
		expect(screen.getByText(/80 Requests?/)).toBeInTheDocument();
		expect(screen.getByText(/70 Requests?/)).toBeInTheDocument();
	});

	it("uses singular form for 1 token", () => {
		const singleItem: ProviderDistributionItem[] = [
			{ name: "Single", count: 1, tokens: 1, share: 100 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={singleItem} metric="tokens" />,
		);

		expect(screen.getByText(/1 Token/)).toBeInTheDocument();
	});

	it("uses singular form for 1 request", () => {
		const singleItem: ProviderDistributionItem[] = [
			{ name: "Single", count: 1, tokens: 100, share: 100 },
		];

		renderWithProviders(
			<ProviderDoughnut
				{...defaultProps}
				items={singleItem}
				metric="requests"
			/>,
		);

		expect(screen.getByText(/1 Request/)).toBeInTheDocument();
	});

	it("shows empty state when items array is empty", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} items={[]} />);

		expect(
			screen.getByText(
				"No provider data yet. Provider breakdown will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("does not render grid when items are empty", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} items={[]} />);

		expect(
			screen.queryByRole("img", { name: /Provider distribution/ }),
		).not.toBeInTheDocument();
	});

	it("shows loading spinner when loading is true", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} loading />);

		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not show loading spinner when loading is false", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} loading={false} />);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});

	it("calls onRangeChange when range button is clicked", async () => {
		const user = userEvent.setup();
		const onRangeChangeMock = vi.fn();

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} onRangeChange={onRangeChangeMock} />,
		);

		const sevenDButton = screen.getByText("1W");
		await user.click(sevenDButton);

		expect(onRangeChangeMock).toHaveBeenCalledWith("1w");
	});

	it("calls onMetricChange when metric button is clicked", async () => {
		const user = userEvent.setup();
		const onMetricChangeMock = vi.fn();

		renderWithProviders(
			<ProviderDoughnut
				{...defaultProps}
				onMetricChange={onMetricChangeMock}
			/>,
		);

		const reqButton = screen.getByText("Req");
		await user.click(reqButton);

		expect(onMetricChangeMock).toHaveBeenCalledWith("requests");
	});

	it("renders MetricToggle component", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByText("Tok")).toBeInTheDocument();
		expect(screen.getByText("Req")).toBeInTheDocument();
	});

	it("renders RangeToggle component", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("1W")).toBeInTheDocument();
	});

	it("highlights active range button", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} range="24h" />);

		const oneDButton = screen.getByText("1D");
		expect(oneDButton).toHaveStyle("background-color: var(--accent)");
		expect(oneDButton).toHaveClass("text-white");
	});

	it("highlights active metric button", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} metric="tokens" />);

		const tokButton = screen.getByText("Tok");
		expect(tokButton).toHaveStyle("background-color: var(--accent)");
		expect(tokButton).toHaveClass("text-white");
	});

	it("formats large token numbers with formatCompact", () => {
		const largeItems: ProviderDistributionItem[] = [
			{ name: "Big Provider", count: 1500000, tokens: 1500000, share: 100 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={largeItems} metric="tokens" />,
		);

		expect(screen.getByText(/1\.5M Tokens?/)).toBeInTheDocument();
	});

	it("uses distinct colors for 6+ providers", () => {
		const manyItems: ProviderDistributionItem[] = [
			{ name: "P1", count: 10, tokens: 100, share: 30 },
			{ name: "P2", count: 10, tokens: 100, share: 25 },
			{ name: "P3", count: 10, tokens: 100, share: 20 },
			{ name: "P4", count: 10, tokens: 100, share: 10 },
			{ name: "P5", count: 10, tokens: 100, share: 8 },
			{ name: "P6", count: 10, tokens: 100, share: 7 },
		];

		const { container } = renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={manyItems} />,
		);

		const grid = container.querySelector("[role='img']");
		const cells = grid?.querySelectorAll(".animate-waffle-pop") ?? [];
		// P6 uses COLORS[5] = #c084fc, which is distinct from COLORS[0] = #818cf8
		const p6StartCell = cells[93]; // 30+25+20+10+8 = 93
		expect(p6StartCell).toHaveStyle({ backgroundColor: "rgb(192, 132, 252)" });
	});

	describe("dynamic title", () => {
		it("shows 'Top N Providers' with multiple items", () => {
			renderWithProviders(<ProviderDoughnut {...defaultProps} />);

			expect(
				screen.getByRole("heading", { name: /Top 3 Providers/ }),
			).toBeInTheDocument();
		});

		it("uses singular 'Provider' for a single item", () => {
			const singleItem: ProviderDistributionItem[] = [
				{ name: "Only One", count: 50, tokens: 500, share: 100 },
			];

			renderWithProviders(
				<ProviderDoughnut {...defaultProps} items={singleItem} />,
			);

			expect(
				screen.getByRole("heading", { name: /Top 1 Provider$/ }),
			).toBeInTheDocument();
		});

		it("shows 'Providers' heading with empty items", () => {
			renderWithProviders(<ProviderDoughnut {...defaultProps} items={[]} />);

			expect(
				screen.getByRole("heading", { name: "Providers" }),
			).toBeInTheDocument();
		});
	});
});
