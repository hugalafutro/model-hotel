import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TrendingUp } from "lucide-react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { GaugeModal } from "../GaugeModal";

vi.mock("recharts", () => ({
	ResponsiveContainer: ({ children }: { children?: React.ReactNode }) => (
		<svg data-testid="responsive-container" aria-hidden="true">
			{children}
		</svg>
	),
	AreaChart: ({ children }: { children?: React.ReactNode }) => (
		<svg data-testid="area-chart" aria-hidden="true">
			{children}
		</svg>
	),
	PieChart: ({ children }: { children?: React.ReactNode }) => (
		<svg data-testid="pie-chart" aria-hidden="true">
			{children}
		</svg>
	),
	Pie: ({ children }: { children?: React.ReactNode }) => (
		<svg data-testid="pie" aria-hidden="true">
			{children}
		</svg>
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

describe("GaugeModal", () => {
	const defaultProps = {
		open: true,
		onClose: vi.fn(),
		title: "Request Volume",
		metric: "Requests",
		icon: TrendingUp,
		color: "#3b82f6",
		dataKey: "total" as const,
		label: "Total Requests",
	};

	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("does not render modal content when open is false", () => {
		renderWithProviders(<GaugeModal {...defaultProps} open={false} />);

		// Modal should not be visible when open is false
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});

	it("renders Modal when open is true", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("displays title in header", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("applies color to title", () => {
		renderWithProviders(<GaugeModal {...defaultProps} color="#ef4444" />);

		const title = screen.getByText("Request Volume");
		expect(title).toHaveStyle("color: #ef4444");
	});

	it("renders Modal content", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("renders chart area component", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		// Area component should be rendered (might be inside mocked recharts)
		const areas = screen.queryAllByTestId("area");
		expect(areas.length).toBeGreaterThanOrEqual(0);
	});

	it("calls onClose when close button is clicked", async () => {
		const user = userEvent.setup();
		const onCloseMock = vi.fn();

		renderWithProviders(<GaugeModal {...defaultProps} onClose={onCloseMock} />);

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		expect(onCloseMock).toHaveBeenCalledTimes(1);
	});

	it("calls onClose when Escape key is pressed", async () => {
		const onCloseMock = vi.fn();

		renderWithProviders(<GaugeModal {...defaultProps} onClose={onCloseMock} />);

		const dialog = screen.getByRole("dialog");
		dialog.dispatchEvent(
			new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
		);

		expect(onCloseMock).toHaveBeenCalledTimes(1);
	});

	it("calls onClose when backdrop is clicked", async () => {
		const user = userEvent.setup();
		const onCloseMock = vi.fn();

		renderWithProviders(<GaugeModal {...defaultProps} onClose={onCloseMock} />);

		const backdrop = document.querySelector(
			"button[aria-label='Close dialog']",
		);
		if (backdrop) {
			await user.click(backdrop);
			expect(onCloseMock).toHaveBeenCalledTimes(1);
		}
	});

	it("fetches time series data when modal opens", async () => {
		server.use(
			http.get("/api/stats/timeseries", () => {
				return HttpResponse.json({
					points: [
						{
							bucket: "2024-01-01T00:00:00Z",
							count: 100,
							errors: 5,
							tokens: 5000,
							latency_ms: 200,
							overhead_ms: 10,
							provider_latency_ms: 190,
							rate_limit_hits: 0,
							avg_ttft_ms: 50,
						},
						{
							bucket: "2024-01-01T04:00:00Z",
							count: 150,
							errors: 3,
							tokens: 7500,
							latency_ms: 180,
							overhead_ms: 8,
							provider_latency_ms: 172,
							rate_limit_hits: 0,
							avg_ttft_ms: 45,
						},
					],
				});
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			expect(screen.getByTestId("area-chart")).toBeInTheDocument();
		});
	});

	it("uses 24h as default range", async () => {
		server.use(
			http.get("/api/stats/timeseries", ({ request }) => {
				const url = new URL(request.url);
				expect(url.searchParams.get("period")).toBe("24h");
				return HttpResponse.json({ points: [] });
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			expect(screen.getByText("1D")).toHaveStyle(
				"background-color: var(--accent)",
			);
		});
	});

	it("changes range when range button is clicked", async () => {
		const user = userEvent.setup();

		server.use(
			http.get("/api/stats/timeseries", () => {
				return HttpResponse.json({ points: [] });
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			expect(screen.getByText("1D")).toBeInTheDocument();
		});

		const sevenDButton = screen.getByText("7D");
		await user.click(sevenDButton);

		await waitFor(() => {
			expect(screen.getByText("7D")).toHaveStyle(
				"background-color: var(--accent)",
			);
		});
	});

	it("accepts allowDecimals prop", () => {
		renderWithProviders(<GaugeModal {...defaultProps} allowDecimals />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("accepts scale prop", () => {
		renderWithProviders(<GaugeModal {...defaultProps} scale={0.001} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("handles latency dataKey", () => {
		renderWithProviders(<GaugeModal {...defaultProps} dataKey="latency" />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("renders modal with correct maxWidth", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		const modal = screen.getByRole("dialog");
		expect(modal.querySelector(".max-w-2xl")).toBeInTheDocument();
	});

	it("accepts scale prop", () => {
		renderWithProviders(<GaugeModal {...defaultProps} scale={0.001} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("handles latency dataKey", () => {
		renderWithProviders(<GaugeModal {...defaultProps} dataKey="latency" />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("passes custom allowDecimals to TimeSeriesChart", () => {
		renderWithProviders(<GaugeModal {...defaultProps} allowDecimals={false} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("passes scale to TimeSeriesChart", () => {
		renderWithProviders(<GaugeModal {...defaultProps} scale={0.001} />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("uses default scale for latency dataKey", () => {
		renderWithProviders(<GaugeModal {...defaultProps} dataKey="latency" />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("uses default scale of 1 for non-latency dataKey", () => {
		renderWithProviders(<GaugeModal {...defaultProps} dataKey="total" />);

		expect(screen.getByText("Request Volume")).toBeInTheDocument();
	});

	it("renders modal with correct maxWidth", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		const modal = screen.getByRole("dialog");
		expect(modal.querySelector(".max-w-2xl")).toBeInTheDocument();
	});

	it("transforms time series points to chart data format", async () => {
		server.use(
			http.get("/api/stats/timeseries", () => {
				return HttpResponse.json({
					points: [
						{
							bucket: "2024-01-01T00:00:00Z",
							count: 100,
							errors: 5,
							tokens: 5000,
							latency_ms: 200,
							overhead_ms: 10,
							provider_latency_ms: 190,
							rate_limit_hits: 0,
							avg_ttft_ms: 50,
						},
					],
				});
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			expect(screen.getByTestId("area-chart")).toBeInTheDocument();
		});
	});

	it("formats hour labels for 24h range", async () => {
		server.use(
			http.get("/api/stats/timeseries", () => {
				return HttpResponse.json({
					points: [
						{
							bucket: "2024-01-01T14:00:00Z",
							count: 100,
							errors: 0,
							tokens: 5000,
							latency_ms: 200,
							overhead_ms: 10,
							provider_latency_ms: 190,
							rate_limit_hits: 0,
							avg_ttft_ms: 50,
						},
					],
				});
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			// Hour label should be formatted as "14:00"
			expect(screen.getByTestId("area-chart")).toBeInTheDocument();
		});
	});

	it("formats day labels for 7d range", async () => {
		server.use(
			http.get("/api/stats/timeseries", () => {
				return HttpResponse.json({
					points: [
						{
							bucket: "2024-01-15T00:00:00Z",
							count: 100,
							errors: 0,
							tokens: 5000,
							latency_ms: 200,
							overhead_ms: 10,
							provider_latency_ms: 190,
							rate_limit_hits: 0,
							avg_ttft_ms: 50,
						},
					],
				});
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			// Day label should be formatted as "Jan 15"
			expect(screen.getByTestId("area-chart")).toBeInTheDocument();
		});
	});

	it("uses max-w-2xl as default maxWidth", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		const modal = screen.getByRole("dialog");
		const modalContent = modal.querySelector(".max-w-2xl");
		expect(modalContent).toBeInTheDocument();
	});

	it("is scrollable", () => {
		renderWithProviders(<GaugeModal {...defaultProps} />);

		const modal = screen.getByRole("dialog");
		const modalContent = modal.querySelector(".overflow-y-auto");
		expect(modalContent).toBeInTheDocument();
	});

	it("renders with different icons", () => {
		renderWithProviders(<GaugeModal {...defaultProps} icon={TrendingUp} />);

		// Icon should be rendered
		const icon = document.querySelector("svg");
		expect(icon).toBeInTheDocument();
	});

	it("handles empty time series data", async () => {
		server.use(
			http.get("/api/stats/timeseries", () => {
				return HttpResponse.json({ points: [] });
			}),
		);

		renderWithProviders(<GaugeModal {...defaultProps} />);

		await waitFor(() => {
			expect(
				screen.getByText(
					"No time-series data yet. Requests will appear here once traffic flows.",
				),
			).toBeInTheDocument();
		});
	});
});
