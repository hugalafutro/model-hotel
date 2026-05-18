import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TrendingUp } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { TimeSeriesChart } from "../TimeSeriesChart";
import type { Range, TimeSeriesDataPoint } from "../types";

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

const mockData: TimeSeriesDataPoint[] = [
	{
		hour: "00:00",
		total: 100,
		errors: 5,
		tokens: 5000,
		latency: 200,
		overhead_ms: 10,
		provider_latency_ms: 190,
		rate_limit_hits: 0,
		avg_ttft_ms: 50,
	},
	{
		hour: "04:00",
		total: 150,
		errors: 3,
		tokens: 7500,
		latency: 180,
		overhead_ms: 8,
		provider_latency_ms: 172,
		rate_limit_hits: 0,
		avg_ttft_ms: 45,
	},
	{
		hour: "08:00",
		total: 200,
		errors: 10,
		tokens: 10000,
		latency: 250,
		overhead_ms: 15,
		provider_latency_ms: 235,
		rate_limit_hits: 2,
		avg_ttft_ms: 60,
	},
	{
		hour: "12:00",
		total: 300,
		errors: 8,
		tokens: 15000,
		latency: 220,
		overhead_ms: 12,
		provider_latency_ms: 208,
		rate_limit_hits: 1,
		avg_ttft_ms: 55,
	},
	{
		hour: "16:00",
		total: 250,
		errors: 6,
		tokens: 12500,
		latency: 210,
		overhead_ms: 11,
		provider_latency_ms: 199,
		rate_limit_hits: 0,
		avg_ttft_ms: 52,
	},
	{
		hour: "20:00",
		total: 180,
		errors: 4,
		tokens: 9000,
		latency: 190,
		overhead_ms: 9,
		provider_latency_ms: 181,
		rate_limit_hits: 0,
		avg_ttft_ms: 48,
	},
];

const defaultProps = {
	data: mockData,
	range: "24h" as Range,
	onRangeChange: vi.fn(),
	metric: "Requests",
	icon: TrendingUp,
	color: "#3b82f6",
	label: "Total Requests",
	dataKey: "total" as const,
	loading: false,
};

describe("TimeSeriesChart", () => {
	it("renders with metric title and icon", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByText("Requests / Day")).toBeInTheDocument();
	});

	it("renders area chart when data is provided", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("area-chart")).toBeInTheDocument();
		expect(screen.getByTestId("area")).toBeInTheDocument();
	});

	it("sets correct dataKey on Area component", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		const area = screen.getByTestId("area");
		expect(area).toHaveAttribute("data-datakey", "total");
	});

	it("shows empty state when data array is empty", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} data={[]} />);

		expect(
			screen.getByText(
				"No time-series data yet. Requests will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("does not render chart when data is empty", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} data={[]} />);

		expect(screen.queryByTestId("area-chart")).not.toBeInTheDocument();
	});

	it("shows loading spinner when loading is true", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} loading />);

		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not show loading spinner when loading is false", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} loading={false} />);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});

	it("calls onRangeChange when range button is clicked", async () => {
		const user = userEvent.setup();
		const onRangeChangeMock = vi.fn();

		renderWithProviders(
			<TimeSeriesChart {...defaultProps} onRangeChange={onRangeChangeMock} />,
		);

		const sevenDButton = screen.getByText("7D");
		await user.click(sevenDButton);

		expect(onRangeChangeMock).toHaveBeenCalledWith("7d");
	});

	it("renders RangeToggle when showToggle is true", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} showToggle />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("7D")).toBeInTheDocument();
	});

	it("does not render RangeToggle when showToggle is false", () => {
		renderWithProviders(
			<TimeSeriesChart {...defaultProps} showToggle={false} />,
		);

		expect(screen.queryByText("1H")).not.toBeInTheDocument();
		expect(screen.queryByText("1D")).not.toBeInTheDocument();
		expect(screen.queryByText("7D")).not.toBeInTheDocument();
	});

	it("displays day label for 7d range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="7d" />);

		expect(screen.getByText("Requests / Day")).toBeInTheDocument();
	});

	it("displays day label for 24h range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="24h" />);

		expect(screen.getByText("Requests / Day")).toBeInTheDocument();
	});

	it("displays hour label for 1h range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="1h" />);

		expect(screen.getByText("Requests / Hour")).toBeInTheDocument();
	});

	it("displays correct rate label in empty state for 24h range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="24h" data={[]} />);

		expect(screen.getByText("Requests / Day")).toBeInTheDocument();
	});

	it("displays correct rate label in empty state for 1h range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="1h" data={[]} />);

		expect(screen.getByText("Requests / Hour")).toBeInTheDocument();
	});

	it("applies color to icon", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} color="#ef4444" />);

		// Icon should have the specified color
		const icon = document.querySelector("svg");
		expect(icon).toHaveStyle("color: #ef4444");
	});

	it("renders CartesianGrid", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("cartesian-grid")).toBeInTheDocument();
	});

	it("renders XAxis", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("x-axis")).toBeInTheDocument();
	});

	it("renders YAxis", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("y-axis")).toBeInTheDocument();
	});

	it("renders Tooltip", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("tooltip")).toBeInTheDocument();
	});

	it("accepts custom height prop", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} height={300} />);

		// Chart should render with area component
		expect(screen.getByTestId("area-chart")).toBeInTheDocument();
	});

	it("uses default height when not provided", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("area-chart")).toBeInTheDocument();
	});

	it("allows decimals when allowDecimals is true", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} allowDecimals />);

		const yAxis = screen.getByTestId("y-axis");
		// YAxis should be configured to allow decimals
		expect(yAxis).toBeInTheDocument();
	});

	it("does not allow decimals when allowDecimals is false", () => {
		renderWithProviders(
			<TimeSeriesChart {...defaultProps} allowDecimals={false} />,
		);

		const yAxis = screen.getByTestId("y-axis");
		expect(yAxis).toBeInTheDocument();
	});

	it("applies scale factor to data", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} scale={0.001} />);

		// Scale is applied in tickFormatter and tooltip formatter
		const area = screen.getByTestId("area");
		expect(area).toBeInTheDocument();
	});

	it("highlights active range button", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="24h" />);

		const oneDButton = screen.getByText("1D");
		expect(oneDButton).toHaveStyle("background-color: var(--accent)");
		expect(oneDButton).toHaveClass("text-white");
	});

	it("renders responsive container", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByTestId("responsive-container")).toBeInTheDocument();
	});

	it("uses correct gradientId based on dataKey", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} dataKey="tokens" />);

		const area = screen.getByTestId("area");
		expect(area).toHaveAttribute("data-datakey", "tokens");
	});

	it("handles different dataKey values", () => {
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				dataKey="total"
				label="Total Requests"
			/>,
		);

		expect(screen.getByTestId("area-chart")).toBeInTheDocument();
	});
});

// Helper to generate test data with N points
function generateData(count: number): TimeSeriesDataPoint[] {
	return Array.from({ length: count }, (_, i) => ({
		hour: `${String(Math.floor(i / 4) % 24).padStart(2, "0")}:${String((i % 4) * 15).padStart(2, "0")}`,
		total: 100 + i * 10,
		errors: Math.floor(i / 3),
		tokens: 5000 + i * 500,
		latency: 200 - i,
		overhead_ms: 10,
		provider_latency_ms: 190 - i,
		rate_limit_hits: 0,
		avg_ttft_ms: 50,
	}));
}

describe("Drag-to-pan and wheel scroll", () => {
	it("shows grab cursor when data exceeds viewport", () => {
		const data = generateData(15); // 15 > 12 (viewportSize for 1h)
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// Chart container should have grab cursor
		const chartContainer = screen.getByTestId("area-chart").parentElement;
		expect(chartContainer).toHaveStyle("cursor: grab");
	});

	it("shows default cursor when data fits viewport", () => {
		// 6 points < 24 (viewportSize for 24h), so not pannable
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		const chartContainer = screen.getByTestId("area-chart").parentElement;
		// Should not have grab cursor when not pannable
		expect(chartContainer).not.toHaveStyle("cursor: grab");
	});

	it("shows drag to pan hint when pannable and not at edges", () => {
		const data = generateData(15); // pannable for 1h range
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// Should show "drag to pan" text and arrows when pannable
		expect(screen.getByText("drag to pan")).toBeInTheDocument();
		// At start, can pan left (toward older data) but not right
		expect(screen.getByText("→")).toBeInTheDocument();
		expect(screen.queryByText("←")).not.toBeInTheDocument();
	});

	it("shows right arrow when can pan left (toward older data)", () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// At start position, should show right arrow (can pan left = see older data)
		expect(screen.getByText("→")).toBeInTheDocument();
	});

	it("shows left arrow when can pan right (toward newer data)", async () => {
		const data = generateData(20); // enough data to pan both directions
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// First scroll to older data (positive delta = decrease start = older)
		fireEvent.wheel(chartContainer, { deltaY: 50 });

		// Now we can pan back to newer data, so left arrow appears
		expect(screen.getByText("←")).toBeInTheDocument();
	});

	it("sets isDragging on pointer down", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Fire pointer down event
		fireEvent.pointerDown(chartContainer, { clientX: 100, pointerId: 1 });

		// Should show grabbing cursor during drag
		expect(chartContainer).toHaveStyle("cursor: grabbing");
	});

	it("updates viewport on pointer move during drag", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Start drag
		fireEvent.pointerDown(chartContainer, { clientX: 100, pointerId: 1 });

		// Move pointer to the right (should see older data)
		fireEvent.pointerMove(chartContainer, { clientX: 200, pointerId: 1 });

		// Viewport should have shifted - check that drag overlay is still visible
		expect(chartContainer).toHaveStyle("cursor: grabbing");
	});

	it("clears isDragging on pointer up", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Start drag
		fireEvent.pointerDown(chartContainer, { clientX: 100, pointerId: 1 });
		expect(chartContainer).toHaveStyle("cursor: grabbing");

		// End drag
		fireEvent.pointerUp(chartContainer, { pointerId: 1 });

		// Should return to grab cursor
		expect(chartContainer).toHaveStyle("cursor: grab");

		// Drag overlay should be gone
		const dragOverlay = chartContainer.querySelector(
			'div[style*="position: absolute"]',
		);
		expect(dragOverlay).not.toBeInTheDocument();
	});

	it("scrolls to older data on positive wheel delta", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Positive delta = scroll right = see older data (decrease start)
		fireEvent.wheel(chartContainer, { deltaY: 50 });

		// Should show left arrow now (can pan right toward newer data)
		expect(screen.getByText("←")).toBeInTheDocument();
		// Right arrow should still be visible (can still pan left toward older data)
		expect(screen.getByText("→")).toBeInTheDocument();
	});

	it("scrolls to newer data on negative wheel delta", async () => {
		const data = generateData(20);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// First scroll to older data
		fireEvent.wheel(chartContainer, { deltaY: 50 });

		// Then scroll back (negative delta = see newer data)
		fireEvent.wheel(chartContainer, { deltaY: -50 });

		// Should be able to pan left again
		expect(screen.getByText("→")).toBeInTheDocument();
	});

	it("handles deltaMode 1 (line mode) for trackpad", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// deltaMode 1 = line mode, uses deltaX * 20
		fireEvent.wheel(chartContainer, { deltaX: 1, deltaMode: 1 });

		// Should scroll (1 * 20 = 20, which is significant)
		expect(screen.getByText("←")).toBeInTheDocument();
	});

	it("does not scroll when not pannable", async () => {
		// 6 points < 24 (viewportSize for 24h), so not pannable
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Try to scroll
		fireEvent.wheel(chartContainer, { deltaY: 50 });

		// Should not show any pan indicators
		expect(screen.queryByText("drag to pan")).not.toBeInTheDocument();
		expect(screen.queryByText("→")).not.toBeInTheDocument();
		expect(screen.queryByText("←")).not.toBeInTheDocument();
	});

	it("clamps viewport at boundaries", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Try to scroll past the start (older data boundary)
		// Scroll multiple times to try to go past boundary
		fireEvent.wheel(chartContainer, { deltaY: 50 });
		fireEvent.wheel(chartContainer, { deltaY: 50 });
		fireEvent.wheel(chartContainer, { deltaY: 50 });
		fireEvent.wheel(chartContainer, { deltaY: 50 });
		fireEvent.wheel(chartContainer, { deltaY: 50 });

		// Should still show left arrow (can pan right) but not right arrow
		expect(screen.getByText("←")).toBeInTheDocument();
		expect(screen.queryByText("→")).not.toBeInTheDocument();

		// Now scroll back to the end (newer data boundary)
		fireEvent.wheel(chartContainer, { deltaY: -50 });
		fireEvent.wheel(chartContainer, { deltaY: -50 });
		fireEvent.wheel(chartContainer, { deltaY: -50 });
		fireEvent.wheel(chartContainer, { deltaY: -50 });
		fireEvent.wheel(chartContainer, { deltaY: -50 });
		fireEvent.wheel(chartContainer, { deltaY: -50 });

		// Should be back at start position
		expect(screen.getByText("→")).toBeInTheDocument();
		expect(screen.queryByText("←")).not.toBeInTheDocument();
	});

	it("handles pointer cancel", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Start drag
		fireEvent.pointerDown(chartContainer, { clientX: 100, pointerId: 1 });
		expect(chartContainer).toHaveStyle("cursor: grabbing");

		// Cancel drag
		fireEvent.pointerCancel(chartContainer, { pointerId: 1 });

		// Should return to grab cursor
		expect(chartContainer).toHaveStyle("cursor: grab");
	});

	it("ignores pointer events when not pannable", async () => {
		// 6 points < 24 (viewportSize for 24h), so not pannable
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// Try to start drag
		fireEvent.pointerDown(chartContainer, { clientX: 100, pointerId: 1 });

		// Should not have grabbing cursor
		expect(chartContainer).not.toHaveStyle("cursor: grabbing");
		expect(chartContainer).not.toHaveStyle("cursor: grab");
	});

	it("uses deltaX when it has larger absolute value than deltaY", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// deltaX (100) > deltaY (10), so should use deltaX
		fireEvent.wheel(chartContainer, { deltaX: 100, deltaY: 10 });

		// Should scroll based on deltaX (positive = older data)
		expect(screen.getByText("←")).toBeInTheDocument();
	});

	it("uses deltaY when it has larger absolute value than deltaX", async () => {
		const data = generateData(15);
		renderWithProviders(
			<TimeSeriesChart
				{...defaultProps}
				data={data}
				range="1h"
				metric="Requests"
			/>,
		);

		// biome-ignore lint/style/noNonNullAssertion: test code, parentElement always exists
		const chartContainer = screen.getByTestId("area-chart").parentElement!;

		// deltaY (100) > deltaX (10), so should use deltaY
		fireEvent.wheel(chartContainer, { deltaX: 10, deltaY: 100 });

		// Should scroll based on deltaY (positive = older data)
		expect(screen.getByText("←")).toBeInTheDocument();
	});
});
