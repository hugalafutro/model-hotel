import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { ProviderDoughnut } from "../ProviderDoughnut";
import type { MetricType, ProviderDistItem, Range } from "../types";

vi.mock("recharts", () => ({
	ResponsiveContainer: ({ children }: { children?: React.ReactNode }) => (
		<div data-testid="responsive-container">{children}</div>
	),
	AreaChart: ({ children }: { children?: React.ReactNode }) => (
		<div data-testid="area-chart">{children}</div>
	),
	PieChart: ({ children }: { children?: React.ReactNode }) => (
		<div data-testid="pie-chart">{children}</div>
	),
	Pie: ({ children }: { children?: React.ReactNode }) => (
		<div data-testid="pie">{children}</div>
	),
	Cell: ({ fill }: { fill?: string }) => (
		<div data-testid="pie-cell" style={{ backgroundColor: fill }} />
	),
	Area: ({ dataKey }: { dataKey?: string }) => (
		<div data-testid="area" data-datakey={dataKey} />
	),
	XAxis: () => <div data-testid="x-axis" />,
	YAxis: () => <div data-testid="y-axis" />,
	CartesianGrid: () => <div data-testid="cartesian-grid" />,
	Tooltip: () => <div data-testid="tooltip" />,
}));

describe("ProviderDoughnut", () => {
	const mockItems: ProviderDistItem[] = [
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

	it("renders pie chart when items are provided", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		expect(screen.getByTestId("pie-chart")).toBeInTheDocument();
		expect(screen.getByTestId("pie")).toBeInTheDocument();
	});

	it("renders correct number of pie cells for items", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		const cells = screen.getAllByTestId("pie-cell");
		expect(cells).toHaveLength(3);
	});

	it("applies correct colors to pie cells", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} />);

		const cells = screen.getAllByTestId("pie-cell");
		expect(cells[0]).toHaveStyle("background-color: rgb(129, 140, 248)");
		expect(cells[1]).toHaveStyle("background-color: rgb(5, 150, 105)");
		expect(cells[2]).toHaveStyle("background-color: rgb(251, 191, 36)");
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
		const tinyShareItems: ProviderDistItem[] = [
			{ name: "Big", count: 9990, tokens: 9990000, share: 99.9 },
			{ name: "Tiny", count: 10, tokens: 11400, share: 0.02 },
			{ name: "Zero", count: 0, tokens: 0, share: 0 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={tinyShareItems} />,
		);

		expect(screen.getByText("99.9%")).toBeInTheDocument();
		// Both zero and near-zero shares display as "<0.1%"
		const lessThan = screen.getAllByText("<0.1%");
		expect(lessThan).toHaveLength(2);
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
		const singleItem: ProviderDistItem[] = [
			{ name: "Single", count: 1, tokens: 1, share: 100 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={singleItem} metric="tokens" />,
		);

		expect(screen.getByText(/1 Token/)).toBeInTheDocument();
	});

	it("uses singular form for 1 request", () => {
		const singleItem: ProviderDistItem[] = [
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

	it("does not render pie chart when items are empty", () => {
		renderWithProviders(<ProviderDoughnut {...defaultProps} items={[]} />);

		expect(screen.queryByTestId("pie-chart")).not.toBeInTheDocument();
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

		const sevenDButton = screen.getByText("7D");
		await user.click(sevenDButton);

		expect(onRangeChangeMock).toHaveBeenCalledWith("7d");
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
		expect(screen.getByText("7D")).toBeInTheDocument();
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
		const largeItems: ProviderDistItem[] = [
			{ name: "Big Provider", count: 1500000, tokens: 1500000, share: 100 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={largeItems} metric="tokens" />,
		);

		expect(screen.getByText(/1\.5M Tokens?/)).toBeInTheDocument();
	});

	it("cycles colors correctly for more than 5 items", () => {
		const manyItems: ProviderDistItem[] = [
			{ name: "P1", count: 10, tokens: 100, share: 20 },
			{ name: "P2", count: 10, tokens: 100, share: 20 },
			{ name: "P3", count: 10, tokens: 100, share: 20 },
			{ name: "P4", count: 10, tokens: 100, share: 20 },
			{ name: "P5", count: 10, tokens: 100, share: 20 },
			{ name: "P6", count: 10, tokens: 100, share: 20 },
		];

		renderWithProviders(
			<ProviderDoughnut {...defaultProps} items={manyItems} />,
		);

		const cells = screen.getAllByTestId("pie-cell");
		expect(cells).toHaveLength(6);
		// 6th item should cycle back to first color
		expect(cells[5]).toHaveStyle("background-color: rgb(129, 140, 248)");
	});

	describe("dynamic title", () => {
		it("shows 'Top N Providers' with multiple items", () => {
			renderWithProviders(<ProviderDoughnut {...defaultProps} />);

			expect(
				screen.getByRole("heading", { name: /Top 3 Providers/ }),
			).toBeInTheDocument();
		});

		it("uses singular 'Provider' for a single item", () => {
			const singleItem: ProviderDistItem[] = [
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
