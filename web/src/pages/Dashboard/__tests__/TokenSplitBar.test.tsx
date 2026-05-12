import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TokenSplitBar } from "../TokenSplitBar";

describe("TokenSplitBar", () => {
	const defaultProps = {
		prompt: 600,
		completion: 400,
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
			<TokenSplitBar {...defaultProps} prompt={0} completion={0} total={0} />,
		);

		expect(
			screen.getByText(
				"No token data yet. Token mix will appear here once traffic flows.",
			),
		).toBeInTheDocument();
	});

	it("displays prompt and completion labels", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("Prompt")).toBeInTheDocument();
		expect(screen.getByText("Completion")).toBeInTheDocument();
	});

	it("displays prompt and completion values", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("600")).toBeInTheDocument();
		expect(screen.getByText("400")).toBeInTheDocument();
	});

	it("calculates and displays percentages correctly", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("60%")).toBeInTheDocument();
		expect(screen.getByText("40%")).toBeInTheDocument();
	});

	it("renders RangeToggle component", () => {
		render(<TokenSplitBar {...defaultProps} />);

		expect(screen.getByText("1H")).toBeInTheDocument();
		expect(screen.getByText("1D")).toBeInTheDocument();
		expect(screen.getByText("7D")).toBeInTheDocument();
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

		const sevenDButton = screen.getByText("7D");
		await user.click(sevenDButton);

		expect(onRangeChange).toHaveBeenCalledWith("7d");
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

	it("applies correct colors to prompt bar", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const bars = container.querySelectorAll("div[style*='background-color']");
		expect(bars).toHaveLength(2);

		expect(bars[0]).toHaveStyle("background-color: #818cf8");
		expect(bars[1]).toHaveStyle("background-color: #059669");
	});

	it("applies correct colors to legend indicators", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const indicators = container.querySelectorAll(
			"span[style*='background-color']",
		);
		expect(indicators).toHaveLength(2);

		expect(indicators[0]).toHaveStyle("background-color: #818cf8");
		expect(indicators[1]).toHaveStyle("background-color: #059669");
	});

	it("hides percentage text when bar is too narrow", () => {
		render(<TokenSplitBar {...defaultProps} prompt={10} completion={990} />);

		expect(screen.queryByText("1%")).not.toBeInTheDocument();
		expect(screen.getByText("99%")).toBeInTheDocument();
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

	it("renders bar container with correct structure", () => {
		const { container } = render(<TokenSplitBar {...defaultProps} />);

		const barContainer = container.querySelector(
			".flex.rounded-lg.overflow-hidden",
		);
		expect(barContainer).toBeInTheDocument();
		expect(barContainer).toHaveClass("h-6");
	});

	it("handles prompt-only tokens (no completion)", () => {
		render(<TokenSplitBar {...defaultProps} completion={0} total={500} />);

		expect(screen.getByText("500")).toBeInTheDocument();
		expect(screen.getByText("100%")).toBeInTheDocument();
		expect(screen.getByText("0")).toBeInTheDocument();
	});

	it("handles completion-only tokens (no prompt)", () => {
		render(<TokenSplitBar {...defaultProps} prompt={0} total={500} />);

		expect(screen.getByText("500")).toBeInTheDocument();
		expect(screen.getByText("100%")).toBeInTheDocument();
		expect(screen.getByText("0")).toBeInTheDocument();
	});
});
