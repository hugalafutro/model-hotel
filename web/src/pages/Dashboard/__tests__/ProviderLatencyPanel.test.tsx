import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Timer } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import type { ProviderLatencyEntry } from "../ProviderLatencyPanel";
import { ProviderLatencyPanel } from "../ProviderLatencyPanel";
import type { Range } from "../types";

describe("ProviderLatencyPanel", () => {
	const mockEntries: ProviderLatencyEntry[] = [
		{
			label: "Provider A",
			totalMs: 8420,
			overheadMs: 420,
			providerMs: 8000,
			requestCount: 100,
		},
		{
			label: "Provider B",
			totalMs: 980,
			overheadMs: 80,
			providerMs: 900,
			requestCount: 50,
		},
		{
			label: "Provider C",
			totalMs: 1500,
			overheadMs: 150,
			providerMs: 1350,
			requestCount: 25,
		},
	];

	const defaultProps = {
		title: "Provider Latency",
		icon: Timer,
		entries: mockEntries,
		range: "24h" as Range,
		onRangeChange: vi.fn(),
		loading: false,
	};

	it("renders with title and icon", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		expect(screen.getByText("Provider Latency")).toBeInTheDocument();
	});

	it("renders provider names", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		expect(screen.getByText("Provider A")).toBeInTheDocument();
		expect(screen.getByText("Provider B")).toBeInTheDocument();
		expect(screen.getByText("Provider C")).toBeInTheDocument();
	});

	it("formats large latency values as seconds (e.g., 8420ms → 8.4s)", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		// 8420ms should be formatted as "8.4s"
		expect(screen.getByText("8.4s")).toBeInTheDocument();
	});

	it("formats small latency values as milliseconds (e.g., 980ms → 980ms)", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		// 980ms should remain as "980ms"
		expect(screen.getByText("980ms")).toBeInTheDocument();
	});

	it("shows empty state when entries array is empty", () => {
		renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} entries={[]} />,
		);

		expect(
			screen.getByText(
				"No latency data yet. Provider latency will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("shows loading spinner when loading is true", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} loading />);

		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not show loading spinner when loading is false", () => {
		renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} loading={false} />,
		);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});

	it("colors worst (slowest) response time red, best green", () => {
		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} />,
		);

		// Provider A has the slowest totalMs (8420), should be red (hue 0)
		const responseValues = container.querySelectorAll(
			'div[title*="Avg response time"]',
		);
		expect(responseValues.length).toBe(3);

		// First value (slowest) should have red color (hsl(0, 70%, 50%) = rgb(217, 38, 38))
		const slowestStyle = responseValues[0]?.getAttribute("style");
		expect(slowestStyle).toContain("rgb(217, 38, 38)");

		// Last value (fastest) should have green color (hsl(120, 70%, 50%) = rgb(38, 217, 38))
		const fastestStyle = responseValues[2]?.getAttribute("style");
		expect(fastestStyle).toContain("rgb(38, 217, 38)");
	});

	it("colors worst (highest) overhead red, best green", () => {
		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} />,
		);

		// Overhead values should be colored independently
		const overheadValues = container.querySelectorAll(
			'div[title*="Avg proxy overhead"]',
		);
		expect(overheadValues.length).toBe(3);

		// Highest overhead should be red (hsl(0, 70%, 50%) = rgb(217, 38, 38))
		const highestOverheadStyle = overheadValues[0]?.getAttribute("style");
		expect(highestOverheadStyle).toContain("rgb(217, 38, 38)");

		// Lowest overhead should be green (hsl(120, 70%, 50%) = rgb(38, 217, 38))
		const lowestOverheadStyle = overheadValues[2]?.getAttribute("style");
		expect(lowestOverheadStyle).toContain("rgb(38, 217, 38)");
	});

	it("scales response and overhead independently", () => {
		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} />,
		);

		// Get both response and overhead values
		const responseValues = container.querySelectorAll(
			'div[title*="Avg response time"]',
		);
		const overheadValues = container.querySelectorAll(
			'div[title*="Avg proxy overhead"]',
		);

		// Each should have independent color assignments
		expect(responseValues.length).toBe(3);
		expect(overheadValues.length).toBe(3);

		// Colors should be assigned based on relative ranking within each column
		const responseStyle0 = responseValues[0]?.getAttribute("style");
		const overheadStyle0 = overheadValues[0]?.getAttribute("style");

		// Both slowest values should be red (rgb(217, 38, 38))
		expect(responseStyle0).toContain("rgb(217, 38, 38)");
		expect(overheadStyle0).toContain("rgb(217, 38, 38)");
	});

	it("handles single entry (all same color)", () => {
		const singleEntry: ProviderLatencyEntry[] = [
			{
				label: "Single Provider",
				totalMs: 1000,
				overheadMs: 100,
				providerMs: 900,
				requestCount: 10,
			},
		];

		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} entries={singleEntry} />,
		);

		const responseValue = container.querySelector(
			'div[title*="Avg response time"]',
		);
		expect(responseValue).toBeTruthy();
		const style = responseValue?.getAttribute("style");
		// Single entry should get middle color (yellow, hue 60 = rgb(217, 217, 38))
		expect(style).toContain("rgb(217, 217, 38)");
	});

	it("handles entries with zero overhead", () => {
		const zeroOverheadEntries: ProviderLatencyEntry[] = [
			{
				label: "Direct Provider",
				totalMs: 1000,
				overheadMs: 0,
				providerMs: 1000,
				requestCount: 20,
			},
		];

		renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} entries={zeroOverheadEntries} />,
		);

		// Should still render the provider name and values
		expect(screen.getByText("Direct Provider")).toBeInTheDocument();
		// 1000ms should be formatted as "1.0s"
		expect(screen.getByText("1.0s")).toBeInTheDocument();
		// 0ms should be formatted as "0ms"
		expect(screen.getByText("0ms")).toBeInTheDocument();
	});

	it("calls onRangeChange when range button is clicked", async () => {
		const user = userEvent.setup();
		const onRangeChangeMock = vi.fn();

		renderWithProviders(
			<ProviderLatencyPanel
				{...defaultProps}
				onRangeChange={onRangeChangeMock}
			/>,
		);

		const sevenDButton = screen.getByText("1W");
		await user.click(sevenDButton);

		expect(onRangeChangeMock).toHaveBeenCalledWith("1w");
	});

	it("renders column headers Response and Overhead", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		expect(screen.getByText("Response")).toBeInTheDocument();
		expect(screen.getByText("Overhead")).toBeInTheDocument();
	});

	it("includes request count in response and overhead tooltips", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		// Response time values should have tooltips with request count
		const responseValues = screen.getAllByText("8.4s");
		const responseEl = responseValues[0];
		expect(responseEl).toHaveAttribute("title");
		const responseTitle = responseEl.getAttribute("title");
		expect(responseTitle).toContain("100 requests");

		// Overhead values should also have tooltips with request count
		const overheadValues = screen.getAllByText("420ms");
		const overheadEl = overheadValues[0];
		expect(overheadEl).toHaveAttribute("title");
		const overheadTitle = overheadEl.getAttribute("title");
		expect(overheadTitle).toContain("100 requests");
	});

	it("renders all three range toggle options", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("1W")).toBeInTheDocument();
	});

	it("highlights active range button", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} range="24h" />);

		const oneDButton = screen.getByText("1D").closest("button");
		expect(oneDButton).toHaveStyle("background-color: var(--accent)");
		expect(oneDButton).toHaveClass("text-white");
	});
});
