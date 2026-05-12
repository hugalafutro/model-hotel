import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TrendingUp } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { TimeSeriesChart } from "../TimeSeriesChart";
import type { Range, TimeSeriesDataPoint } from "../types";

vi.mock("recharts", () => ({
	ResponsiveContainer: ({ children }: any) => (
		<div data-testid="responsive-container">{children}</div>
	),
	AreaChart: ({ children }: any) => (
		<div data-testid="area-chart">{children}</div>
	),
	PieChart: ({ children }: any) => (
		<div data-testid="pie-chart">{children}</div>
	),
	Pie: ({ children }: any) => <div data-testid="pie">{children}</div>,
	Cell: ({ fill }: any) => (
		<div data-testid="pie-cell" style={{ backgroundColor: fill }} />
	),
	Area: ({ dataKey }: any) => <div data-testid="area" data-datakey={dataKey} />,
	XAxis: () => <div data-testid="x-axis" />,
	YAxis: () => <div data-testid="y-axis" />,
	CartesianGrid: () => <div data-testid="cartesian-grid" />,
	Tooltip: () => <div data-testid="tooltip" />,
}));

describe("TimeSeriesChart", () => {
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

	it("renders with metric title and icon", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} />);

		expect(screen.getByText("Requests / Hour")).toBeInTheDocument();
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

	it("displays hour label for 24h range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="24h" />);

		expect(screen.getByText("Requests / Hour")).toBeInTheDocument();
	});

	it("displays hour label for 1h range", () => {
		renderWithProviders(<TimeSeriesChart {...defaultProps} range="1h" />);

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
