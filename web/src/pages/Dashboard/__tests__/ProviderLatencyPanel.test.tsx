import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Timer } from "lucide-react";
import { afterEach, describe, expect, it, vi } from "vitest";
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

	afterEach(() => {
		localStorage.removeItem("dashboard.latencySortField");
		localStorage.removeItem("dashboard.latencySortDir");
	});

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

		expect(screen.getByText("8.4s")).toBeInTheDocument();
	});

	it("formats small latency values as milliseconds (e.g., 980ms → 980ms)", () => {
		renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

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

	it("colors worst (slowest) response time orange, best green", () => {
		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} />,
		);

		const responseValues = container.querySelectorAll(
			'div[title*="Avg response time"]',
		);
		expect(responseValues.length).toBe(3);

		// Slowest = orange (hue 30 = rgb(217, 128, 38))
		const slowestStyle = responseValues[0]?.getAttribute("style");
		expect(slowestStyle).toContain("rgb(217, 128, 38)");

		// Fastest = green (hue 120 = rgb(38, 217, 38))
		const fastestStyle = responseValues[2]?.getAttribute("style");
		expect(fastestStyle).toContain("rgb(38, 217, 38)");
	});

	it("colors worst (highest) overhead orange, best green", () => {
		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} />,
		);

		const overheadValues = container.querySelectorAll(
			'div[title*="Avg proxy overhead"]',
		);
		expect(overheadValues.length).toBe(3);

		// Highest overhead = orange (hue 30 = rgb(217, 128, 38))
		const highestOverheadStyle = overheadValues[0]?.getAttribute("style");
		expect(highestOverheadStyle).toContain("rgb(217, 128, 38)");

		// Lowest overhead = green (hue 120 = rgb(38, 217, 38))
		const lowestOverheadStyle = overheadValues[2]?.getAttribute("style");
		expect(lowestOverheadStyle).toContain("rgb(38, 217, 38)");
	});

	it("scales response and overhead independently", () => {
		const { container } = renderWithProviders(
			<ProviderLatencyPanel {...defaultProps} />,
		);

		const responseValues = container.querySelectorAll(
			'div[title*="Avg response time"]',
		);
		const overheadValues = container.querySelectorAll(
			'div[title*="Avg proxy overhead"]',
		);

		expect(responseValues.length).toBe(3);
		expect(overheadValues.length).toBe(3);

		// Both worst values should be orange
		const responseStyle0 = responseValues[0]?.getAttribute("style");
		const overheadStyle0 = overheadValues[0]?.getAttribute("style");

		expect(responseStyle0).toContain("rgb(217, 128, 38)");
		expect(overheadStyle0).toContain("rgb(217, 128, 38)");
	});

	it("handles single entry (neutral yellow color)", () => {
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

		expect(screen.getByText("Direct Provider")).toBeInTheDocument();
		expect(screen.getByText("1.0s")).toBeInTheDocument();
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

		const responseValues = screen.getAllByText("8.4s");
		const responseEl = responseValues[0];
		expect(responseEl).toHaveAttribute("title");
		const responseTitle = responseEl.getAttribute("title");
		expect(responseTitle).toContain("100 requests");

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

	describe("Column sorting", () => {
		it("defaults to response column sorted descending", () => {
			const { container } = renderWithProviders(
				<ProviderLatencyPanel {...defaultProps} />,
			);

			// Provider A (8420ms) should be first, Provider B (980ms) last
			const rows = container.querySelectorAll(
				'div[class*="grid-cols-3"][class*="items-center"]',
			);
			expect(rows[0]?.textContent).toContain("Provider A");
			expect(rows[2]?.textContent).toContain("Provider B");

			// Response header should show down arrow (active sort desc)
			const responseBtn = screen.getByText("Response").closest("button");
			expect(responseBtn).toBeInTheDocument();
		});

		it("sorts by response ascending when clicking response header twice", async () => {
			const user = userEvent.setup();
			const { container } = renderWithProviders(
				<ProviderLatencyPanel {...defaultProps} />,
			);

			// Click once to toggle to asc
			const responseBtn = screen.getByRole("button", { name: "Response" });
			await user.click(responseBtn);

			const rows = container.querySelectorAll(
				'div[class*="grid-cols-3"][class*="items-center"]',
			);
			// Ascending: Provider B (980ms) first, Provider A (8420ms) last
			expect(rows[0]?.textContent).toContain("Provider B");
			expect(rows[2]?.textContent).toContain("Provider A");
		});

		it("sorts by overhead descending when clicking overhead header", async () => {
			const user = userEvent.setup();
			const { container } = renderWithProviders(
				<ProviderLatencyPanel {...defaultProps} />,
			);

			const overheadBtn = screen.getByRole("button", { name: "Overhead" });
			await user.click(overheadBtn);

			const rows = container.querySelectorAll(
				'div[class*="grid-cols-3"][class*="items-center"]',
			);
			// Descending overhead: Provider A (420) first, Provider B (80) last
			expect(rows[0]?.textContent).toContain("Provider A");
			expect(rows[2]?.textContent).toContain("Provider B");
		});

		it("preserves color assignments regardless of sort order", async () => {
			const user = userEvent.setup();
			const { container } = renderWithProviders(
				<ProviderLatencyPanel {...defaultProps} />,
			);

			// Toggle to ascending
			const responseBtn = screen.getByRole("button", { name: "Response" });
			await user.click(responseBtn);

			// Colors should still be assigned by ranking, not by display order
			const responseValues = container.querySelectorAll(
				'div[title*="Avg response time"]',
			);
			// After sorting ascending, first row is fastest (green), last is slowest (orange)
			const fastestStyle = responseValues[0]?.getAttribute("style");
			const slowestStyle = responseValues[2]?.getAttribute("style");
			expect(fastestStyle).toContain("rgb(38, 217, 38)"); // green
			expect(slowestStyle).toContain("rgb(217, 128, 38)"); // orange
		});

		it("persists sort field and direction to localStorage", async () => {
			const user = userEvent.setup();
			renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

			// Click overhead header to switch sort column
			const overheadBtn = screen.getByRole("button", { name: "Overhead" });
			await user.click(overheadBtn);

			expect(localStorage.getItem("dashboard.latencySortField")).toBe(
				"overhead",
			);
			expect(localStorage.getItem("dashboard.latencySortDir")).toBe("desc");
		});

		it("persists ascending sort direction after toggling", async () => {
			const user = userEvent.setup();
			renderWithProviders(<ProviderLatencyPanel {...defaultProps} />);

			// Click response header twice: once to switch column, once to toggle dir
			const responseBtn = screen.getByRole("button", { name: "Response" });
			await user.click(responseBtn); // toggles to asc
			expect(localStorage.getItem("dashboard.latencySortDir")).toBe("asc");

			await user.click(responseBtn); // toggles to desc
			expect(localStorage.getItem("dashboard.latencySortDir")).toBe("desc");
		});

		it("restores sort state from localStorage on mount", () => {
			localStorage.setItem("dashboard.latencySortField", "overhead");
			localStorage.setItem("dashboard.latencySortDir", "asc");

			const { container } = renderWithProviders(
				<ProviderLatencyPanel {...defaultProps} />,
			);

			// Ascending overhead: Provider B (80ms) first, Provider A (420ms) last
			const rows = container.querySelectorAll(
				'div[class*="grid-cols-3"][class*="items-center"]',
			);
			expect(rows[0]?.textContent).toContain("Provider B");
			expect(rows[2]?.textContent).toContain("Provider A");
		});

		it("falls back to defaults for invalid localStorage values", () => {
			localStorage.setItem("dashboard.latencySortField", "nonsense");
			localStorage.setItem("dashboard.latencySortDir", "sideways");

			const { container } = renderWithProviders(
				<ProviderLatencyPanel {...defaultProps} />,
			);

			// Should fall back to default: response desc → Provider A (8420ms) first
			const rows = container.querySelectorAll(
				'div[class*="grid-cols-3"][class*="items-center"]',
			);
			expect(rows[0]?.textContent).toContain("Provider A");
		});
	});
});
