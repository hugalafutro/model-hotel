import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Activity } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import type { LatencyEntry } from "../LatencyBarPanel";
import { LatencyBarPanel } from "../LatencyBarPanel";
import type { Range } from "../types";

describe("LatencyBarPanel", () => {
	const mockEntries: LatencyEntry[] = [
		{
			label: "Model A",
			totalMs: 8420,
			overheadMs: 420,
			providerMs: 8000,
			requestCount: 100,
		},
		{
			label: "Model B",
			totalMs: 980,
			overheadMs: 80,
			providerMs: 900,
			requestCount: 50,
		},
		{
			label: "Model C",
			totalMs: 1500,
			overheadMs: 150,
			providerMs: 1350,
			requestCount: 25,
		},
	];

	const defaultProps = {
		title: "Latency Overview",
		icon: Activity,
		entries: mockEntries,
		range: "24h" as Range,
		onRangeChange: vi.fn(),
		loading: false,
		overheadColor: "#f59e0b",
	};

	it("renders with title and icon", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		expect(screen.getByText("Latency Overview")).toBeInTheDocument();
	});

	it("renders entry labels", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		expect(screen.getByText("Model A")).toBeInTheDocument();
		expect(screen.getByText("Model B")).toBeInTheDocument();
		expect(screen.getByText("Model C")).toBeInTheDocument();
	});

	it("formats large latency values as seconds", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		// 8420ms should be formatted as "8.4s"
		expect(screen.getByText("8.4s")).toBeInTheDocument();
	});

	it("formats small latency values as milliseconds", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		// 980ms should remain as "980ms"
		expect(screen.getByText("980ms")).toBeInTheDocument();
	});

	it("formats 1.5s correctly", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		// 1500ms should be formatted as "1.5s"
		expect(screen.getByText("1.5s")).toBeInTheDocument();
	});

	it("shows empty state when entries array is empty", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} entries={[]} />);

		expect(
			screen.getByText(
				"No latency data yet. Latency breakdown will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("shows loading spinner when loading is true", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} loading />);

		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not show loading spinner when loading is false", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} loading={false} />);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});

	it("renders split bars with provider and overhead portions", () => {
		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} />,
		);

		// Each entry should have a bar container with two segments
		const barContainers = container.querySelectorAll(
			".h-\\[4px\\].rounded-full.overflow-hidden",
		);
		// Should have 3 bar containers (one per entry)
		expect(barContainers.length).toBe(3);
	});

	it("renders provider portion with accent color", () => {
		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} />,
		);

		// Provider bars should use var(--accent) color
		// Also matches the RangeToggle button, so we check for bars in bar containers
		const barContainers = container.querySelectorAll(
			".h-\\[4px\\].rounded-full.overflow-hidden",
		);
		expect(barContainers.length).toBe(3);

		// Each bar container should have a provider portion with var(--accent)
		const providerBars = container.querySelectorAll(
			"div[style*='background-color: var(--accent)'].rounded-l-full",
		);
		expect(providerBars.length).toBe(3);
	});

	it("renders overhead portion with custom overheadColor", () => {
		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} />,
		);

		// Overhead bars should use the overheadColor (#f59e0b)
		const overheadBars = container.querySelectorAll(
			"[style*='background-color: rgb(245, 158, 11)']",
		);
		// Each entry should have an overhead bar
		expect(overheadBars.length).toBe(3);
		expect(overheadBars[0]).toHaveClass("rounded-r-full");
	});

	it("calculates correct bar widths based on latency values", () => {
		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} />,
		);

		// Model A has the highest totalMs (8420), so its total bar should be 100% width
		// Provider portion is slightly less due to overhead split
		const providerBars = container.querySelectorAll(
			"div[style*='background-color: var(--accent)'].rounded-l-full",
		);

		// First bar (Model A) should have a width percentage
		const firstBarStyle = providerBars[0]?.getAttribute("style");
		expect(firstBarStyle).toContain("width:");
		// Width should be close to 100% (minus overhead portion)
		// For Model A: providerPct = 100% - overheadPct ≈ 95%
		expect(firstBarStyle).toMatch(/width: \d+\.\d+%/);
	});

	it("calls onRangeChange when range button is clicked", async () => {
		const user = userEvent.setup();
		const onRangeChangeMock = vi.fn();

		renderWithProviders(
			<LatencyBarPanel {...defaultProps} onRangeChange={onRangeChangeMock} />,
		);

		const sevenDButton = screen.getByText("1W");
		await user.click(sevenDButton);

		expect(onRangeChangeMock).toHaveBeenCalledWith("1w");
	});

	it("ranges large values (>= 10s) without decimals", () => {
		const largeEntries: LatencyEntry[] = [
			{
				label: "Slow Model",
				totalMs: 15000,
				overheadMs: 500,
				providerMs: 14500,
				requestCount: 10,
			},
		];

		renderWithProviders(
			<LatencyBarPanel {...defaultProps} entries={largeEntries} />,
		);

		// 15000ms = 15s, should show "15s" (no decimals for >= 10s)
		expect(screen.getByText("15s")).toBeInTheDocument();
	});

	it("renders all three range toggle options", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("1W")).toBeInTheDocument();
	});

	it("highlights active range button", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} range="24h" />);

		const oneDButton = screen.getByText("1D").closest("button");
		expect(oneDButton).toHaveStyle("background-color: var(--accent)");
		expect(oneDButton).toHaveClass("text-white");
	});

	it("renders latency tooltip with proxy overhead and provider latency", () => {
		renderWithProviders(<LatencyBarPanel {...defaultProps} />);

		// The formatted latency value should have a title attribute with tooltip data
		const latencyValue = screen.getByText("8.4s");
		expect(latencyValue).toHaveAttribute("title");
		const title = latencyValue.getAttribute("title");
		expect(title).toContain("Proxy:");
		expect(title).toContain("Provider:");
	});

	it("bar segments have explanatory tooltips", () => {
		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} />,
		);

		// Provider bar segment should have a tooltip
		const providerBar = container.querySelector(
			"div.rounded-l-full[style*='background-color: var(--accent)']",
		);
		expect(providerBar).toBeTruthy();
		expect(providerBar?.getAttribute("title")).toContain("Provider:");

		// Overhead bar segment should have a tooltip
		const overheadBar = container.querySelector(
			"div.rounded-r-full[style*='background-color']",
		);
		expect(overheadBar).toBeTruthy();
		const overheadTitle = overheadBar?.getAttribute("title");
		expect(overheadTitle).toContain("Proxy overhead:");
		expect(overheadTitle).toContain("%");
	});

	it("handles entries with zero overhead", () => {
		const zeroOverheadEntries: LatencyEntry[] = [
			{
				label: "Direct Model",
				totalMs: 1000,
				overheadMs: 0,
				providerMs: 1000,
				requestCount: 20,
			},
		];

		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} entries={zeroOverheadEntries} />,
		);

		// Should still render the bar container
		const barContainers = container.querySelectorAll(
			".h-\\[4px\\].rounded-full.overflow-hidden",
		);
		expect(barContainers.length).toBe(1);

		// Should have provider portion with var(--accent)
		const providerBars = container.querySelectorAll(
			"div[style*='background-color: var(--accent)'].rounded-l-full",
		);
		expect(providerBars.length).toBe(1);

		// No overhead bar should be rendered when overheadMs is 0
		const overheadBars = container.querySelectorAll(
			"div.rounded-r-full[style*='background-color']",
		);
		expect(overheadBars.length).toBe(0);
	});

	it("formats 1000ms as 1.0s", () => {
		const entries: LatencyEntry[] = [
			{
				label: "Threshold Model",
				totalMs: 1000,
				overheadMs: 0,
				providerMs: 1000,
				requestCount: 5,
			},
		];

		renderWithProviders(
			<LatencyBarPanel {...defaultProps} entries={entries} />,
		);

		// 1000ms should be formatted as "1.0s" (one decimal place for values < 10s)
		expect(screen.getByText("1.0s")).toBeInTheDocument();
	});

	it("renders entry label with truncated long labels", () => {
		const longLabelEntries: LatencyEntry[] = [
			{
				label: "very-long-model-name-that-should-be-truncated-in-the-ui",
				totalMs: 500,
				overheadMs: 50,
				providerMs: 450,
				requestCount: 5,
			},
		];

		renderWithProviders(
			<LatencyBarPanel {...defaultProps} entries={longLabelEntries} />,
		);

		const labelSpan = screen.getByText(
			"very-long-model-name-that-should-be-truncated-in-the-ui",
		);
		expect(labelSpan).toHaveClass("truncate");
		expect(labelSpan).toHaveClass("max-w-[70%]");
	});

	it("clamps overheadPct to totalPct when overheadMs exceeds totalMs", () => {
		const entries: LatencyEntry[] = [
			{
				label: "Jittery Model",
				totalMs: 1000,
				overheadMs: 1500,
				providerMs: 500,
				requestCount: 5,
			},
		];

		const { container } = renderWithProviders(
			<LatencyBarPanel {...defaultProps} entries={entries} />,
		);

		// The overhead bar width should be clamped to totalPct (100% for single entry)
		// so total bar width doesn't overflow
		const overheadBar = container.querySelector(
			"div.rounded-r-full[style*='background-color']",
		);
		expect(overheadBar).toBeTruthy();
		const style = overheadBar!.getAttribute("style");
		// overheadPct should be clamped to totalPct (100% for single entry)
		expect(style).toMatch(/width: 100%/);
	});
});
