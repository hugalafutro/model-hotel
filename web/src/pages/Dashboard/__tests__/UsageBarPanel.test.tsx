import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Users } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import type { Range, UsageEntry } from "../types";
import { UsageBarPanel } from "../UsageBarPanel";

describe("UsageBarPanel", () => {
	const mockEntries: UsageEntry[] = [
		{ label: "Model A", value: 100, suffix: "requests" },
		{ label: "Model B", value: 50, suffix: "requests" },
		{ label: "Model C", value: 25, suffix: "requests" },
	];

	const defaultProps = {
		title: "Top Models",
		icon: Users,
		entries: mockEntries,
		range: "24h" as Range,
		onRangeChange: vi.fn(),
		loading: false,
	};

	it("renders with title and icon", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		expect(screen.getByText("Top Models")).toBeInTheDocument();
	});

	it("renders entry labels", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		expect(screen.getByText("Model A")).toBeInTheDocument();
		expect(screen.getByText("Model B")).toBeInTheDocument();
		expect(screen.getByText("Model C")).toBeInTheDocument();
	});

	it("renders entry values", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		expect(screen.getByText(/100/)).toBeInTheDocument();
		expect(screen.getByText(/50/)).toBeInTheDocument();
		expect(screen.getByText(/25/)).toBeInTheDocument();
	});

	it("renders suffixes", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		// Suffix text is rendered alongside the value
		const entryDiv = screen.getByText("Model A").closest("div");
		expect(entryDiv?.textContent).toContain("requests");
	});

	it("uses singular suffix when value is 1", () => {
		const singleEntry: UsageEntry[] = [
			{ label: "Single", value: 1, suffix: "requests" },
		];

		renderWithProviders(
			<UsageBarPanel {...defaultProps} entries={singleEntry} />,
		);

		// Suffix should be singular when value is 1
		const entryDiv = screen.getByText("Single").closest("div");
		expect(entryDiv?.textContent).toContain("request");
	});

	it("does not render suffix when not provided", () => {
		const noSuffixEntries: UsageEntry[] = [{ label: "No Suffix", value: 100 }];

		renderWithProviders(
			<UsageBarPanel {...defaultProps} entries={noSuffixEntries} />,
		);

		const entry = screen.getByText("No Suffix").closest("div");
		expect(entry?.textContent).not.toMatch(/request/);
	});

	it("shows empty state when entries array is empty", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} entries={[]} />);

		expect(
			screen.getByText(
				"No usage data yet. Usage breakdown will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("does not render progress bars when entries are empty", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} entries={[]} />);

		expect(screen.queryByTestId("progress-bar")).not.toBeInTheDocument();
	});

	it("renders progress bars", () => {
		const { container } = renderWithProviders(
			<UsageBarPanel {...defaultProps} />,
		);

		// Progress bars should be rendered as divs inside the card
		const progressBars = container.querySelectorAll(".h-\\[4px\\]");
		expect(progressBars.length).toBeGreaterThanOrEqual(3);
	});

	it("shows loading spinner when loading is true", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} loading />);

		expect(screen.getByTestId("spinner")).toBeInTheDocument();
	});

	it("does not show loading spinner when loading is false", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} loading={false} />);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
	});

	it("calls onRangeChange when range button is clicked", async () => {
		const user = userEvent.setup();
		const onRangeChangeMock = vi.fn();

		renderWithProviders(
			<UsageBarPanel {...defaultProps} onRangeChange={onRangeChangeMock} />,
		);

		const sevenDButton = screen.getByText("7D");
		await user.click(sevenDButton);

		expect(onRangeChangeMock).toHaveBeenCalledWith("7d");
	});

	it("renders MetricToggle when metric and onMetricChange are provided", () => {
		renderWithProviders(
			<UsageBarPanel
				{...defaultProps}
				metric="tokens"
				onMetricChange={vi.fn()}
			/>,
		);

		expect(screen.getByText("Tok")).toBeInTheDocument();
		expect(screen.getByText("Req")).toBeInTheDocument();
	});

	it("does not render MetricToggle when metric is not provided", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		expect(screen.queryByText("Tok")).not.toBeInTheDocument();
		expect(screen.queryByText("Req")).not.toBeInTheDocument();
	});

	it("calls onMetricChange when metric button is clicked", async () => {
		const user = userEvent.setup();
		const onMetricChangeMock = vi.fn();

		renderWithProviders(
			<UsageBarPanel
				{...defaultProps}
				metric="tokens"
				onMetricChange={onMetricChangeMock}
			/>,
		);

		const reqButton = screen.getByText("Req");
		await user.click(reqButton);

		expect(onMetricChangeMock).toHaveBeenCalledWith("requests");
	});

	it("renders entries as clickable buttons when onEntryClick is provided", () => {
		renderWithProviders(
			<UsageBarPanel {...defaultProps} onEntryClick={vi.fn()} />,
		);

		const buttons = screen.getAllByRole("button");
		// Should have 3 entry buttons + 3 range buttons
		expect(buttons.length).toBeGreaterThanOrEqual(3);

		expect(screen.getByText("Model A")).toHaveClass("cursor-pointer");
	});

	it("calls onEntryClick when entry is clicked", async () => {
		const user = userEvent.setup();
		const onEntryClickMock = vi.fn();

		renderWithProviders(
			<UsageBarPanel {...defaultProps} onEntryClick={onEntryClickMock} />,
		);

		await user.click(screen.getByText("Model A"));

		expect(onEntryClickMock).toHaveBeenCalledWith("Model A");
	});

	it("renders entries as spans when onEntryClick is not provided", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		// Entries should be spans, not buttons
		const modelA = screen.getByText("Model A");
		expect(modelA.tagName).toBe("SPAN");
	});

	it("applies deleted styling to deleted entries", () => {
		const deletedEntries: UsageEntry[] = [
			{ label: "Deleted Model", value: 100, deleted: true },
		];

		renderWithProviders(
			<UsageBarPanel {...defaultProps} entries={deletedEntries} />,
		);

		const deletedLabel = screen.getByText("Deleted Model");
		expect(deletedLabel).toHaveClass("text-red-400");
		expect(deletedLabel).toHaveClass("italic");
	});

	it("does not apply deleted styling to non-deleted entries", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		const modelA = screen.getByText("Model A");
		expect(modelA).not.toHaveClass("text-red-400");
		expect(modelA).not.toHaveClass("italic");
	});

	it("renders deleted entries as non-clickable spans with styling", () => {
		const deletedEntries: UsageEntry[] = [
			{ label: "Deleted Clickable", value: 100, deleted: true },
		];

		renderWithProviders(
			<UsageBarPanel
				{...defaultProps}
				entries={deletedEntries}
				onEntryClick={vi.fn()}
			/>,
		);

		const deletedLabel = screen.getByText("Deleted Clickable");
		expect(deletedLabel).toHaveClass("text-red-400");
		expect(deletedLabel).toHaveClass("italic");
		// Deleted entries are rendered as spans, not buttons
		expect(deletedLabel.tagName).toBe("SPAN");
	});

	it("renders failover group entries as non-interactive spans even when onEntryClick is provided", () => {
		const failoverEntries: UsageEntry[] = [
			{ label: "hotel/my-group", value: 100, failoverGroup: true },
		];

		renderWithProviders(
			<UsageBarPanel
				{...defaultProps}
				entries={failoverEntries}
				onEntryClick={vi.fn()}
			/>,
		);

		const label = screen.getByText("hotel/my-group");
		expect(label.tagName).toBe("SPAN");
		expect(label).not.toHaveClass("cursor-pointer");
	});

	it("does not apply hover styles to failover group entries", () => {
		const failoverEntries: UsageEntry[] = [
			{ label: "hotel/my-group", value: 100, failoverGroup: true },
		];

		renderWithProviders(
			<UsageBarPanel
				{...defaultProps}
				entries={failoverEntries}
				onEntryClick={vi.fn()}
			/>,
		);

		const label = screen.getByText("hotel/my-group");
		expect(label).not.toHaveClass("hover:drop-shadow-[var(--glow-accent)]");
	});

	it("applies glow hover style to clickable entries", () => {
		renderWithProviders(
			<UsageBarPanel {...defaultProps} onEntryClick={vi.fn()} />,
		);

		const modelA = screen.getByText("Model A");
		expect(modelA).toHaveClass("hover:drop-shadow-[var(--glow-accent)]");
	});

	it("renders multiple entries with progress bars", () => {
		const variedEntries: UsageEntry[] = [
			{ label: "Small", value: 10 },
			{ label: "Medium", value: 50 },
			{ label: "Large", value: 100 },
		];

		renderWithProviders(
			<UsageBarPanel {...defaultProps} entries={variedEntries} />,
		);

		expect(screen.getByText("Small")).toBeInTheDocument();
		expect(screen.getByText("Medium")).toBeInTheDocument();
		expect(screen.getByText("Large")).toBeInTheDocument();
	});

	it("handles zero value entries", () => {
		const zeroEntries: UsageEntry[] = [{ label: "Zero", value: 0 }];

		renderWithProviders(
			<UsageBarPanel {...defaultProps} entries={zeroEntries} />,
		);

		expect(screen.getByText("Zero")).toBeInTheDocument();
	});

	it("formats large numbers with locale formatting", () => {
		const largeEntries: UsageEntry[] = [{ label: "Large", value: 1000000 }];

		renderWithProviders(
			<UsageBarPanel {...defaultProps} entries={largeEntries} />,
		);

		expect(screen.getByText("1,000,000")).toBeInTheDocument();
	});

	it("renders RangeToggle component", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("7D")).toBeInTheDocument();
	});

	it("highlights active range button", () => {
		renderWithProviders(<UsageBarPanel {...defaultProps} range="24h" />);

		const oneDButton = screen.getByText("1D");
		expect(oneDButton).toHaveStyle("background-color: var(--accent)");
		expect(oneDButton).toHaveClass("text-white");
	});
});
