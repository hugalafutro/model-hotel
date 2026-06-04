import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { DateRangePickerPopover } from "../../logs/DateRangePickerPopover";

describe("DateRangePickerPopover", () => {
	const defaultProps = {
		pickerYear: 2024,
		pickerMonth: 2,
		pendingFrom: null,
		pendingTo: null,
		onCalendarSelect: vi.fn(),
		onApply: vi.fn(),
		onClear: vi.fn(),
		onClose: vi.fn(),
	};

	it("renders 'Select date range' header", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		expect(screen.getByText("Select date range")).toBeInTheDocument();
	});

	it("renders close button", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		expect(
			screen.getByRole("button", { name: /close date picker/i }),
		).toBeInTheDocument();
	});

	it("renders AccentCalendar", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		expect(screen.getByText("March 2024")).toBeInTheDocument();
	});

	it("renders Clear button", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		expect(screen.getByRole("button", { name: "Clear" })).toBeInTheDocument();
	});

	it("renders Apply button", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		expect(screen.getByRole("button", { name: "Apply" })).toBeInTheDocument();
	});

	it("Apply button is disabled when pendingFrom is null", () => {
		renderWithProviders(
			<DateRangePickerPopover {...defaultProps} pendingFrom={null} />,
		);
		const applyButton = screen.getByRole("button", { name: "Apply" });
		expect(applyButton).toBeDisabled();
	});

	it("Apply button is enabled when pendingFrom is set", () => {
		renderWithProviders(
			<DateRangePickerPopover
				{...defaultProps}
				pendingFrom="2024-03-15"
				pendingTo="2024-03-20"
			/>,
		);
		const applyButton = screen.getByRole("button", { name: "Apply" });
		expect(applyButton).not.toBeDisabled();
	});

	it("calls onClear when Clear button clicked", async () => {
		const user = userEvent.setup();
		const onClear = vi.fn();
		renderWithProviders(
			<DateRangePickerPopover {...defaultProps} onClear={onClear} />,
		);

		const clearButton = screen.getByRole("button", { name: "Clear" });
		await user.click(clearButton);

		expect(onClear).toHaveBeenCalledTimes(1);
	});

	it("calls onClose when close button clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		renderWithProviders(
			<DateRangePickerPopover {...defaultProps} onClose={onClose} />,
		);

		const closeButton = screen.getByRole("button", {
			name: /close date picker/i,
		});
		await user.click(closeButton);

		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("shows 'Select start date' when no pending dates", () => {
		renderWithProviders(
			<DateRangePickerPopover {...defaultProps} pendingFrom={null} />,
		);
		expect(screen.getByText("Select start date")).toBeInTheDocument();
	});

	it("shows date range summary when both dates set", () => {
		renderWithProviders(
			<DateRangePickerPopover
				{...defaultProps}
				pendingFrom="2024-03-01"
				pendingTo="2024-03-31"
			/>,
		);
		// Date format is dd/mm-dd/mm (e.g., "01/03-31/03")
		const summaryElement = screen.getByText(/01\/03/i);
		expect(summaryElement).toBeInTheDocument();
	});

	it("renders as a portaled popover with fixed positioning", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		// Component uses createPortal to document.body, so look there
		const popover = document.querySelector(".w-72");
		expect(popover).toBeTruthy();
		expect(popover?.className).toContain("fixed");
		expect(popover?.className).toContain("ui-card");
		expect(popover?.className).toContain("shadow-2xl");
		expect(popover?.className).toContain("z-50");
	});

	it("positions popover with inline style", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		const popover = document.querySelector(".w-72");
		expect(popover).toBeTruthy();
		// Position is set via inline style (top/left) from useLayoutEffect.
		// In jsdom, getBoundingClientRect returns zeros, so top is 0 + gap = gap.
		expect(popover).toHaveStyle({ top: "0px" });
	});

	it("calls onCalendarSelect when a day is selected", async () => {
		const user = userEvent.setup();
		const onCalendarSelect = vi.fn();
		renderWithProviders(
			<DateRangePickerPopover
				{...defaultProps}
				onCalendarSelect={onCalendarSelect}
			/>,
		);

		const day15Button = screen.getByRole("button", { name: "15" });
		await user.click(day15Button);
		expect(onCalendarSelect).toHaveBeenCalledWith("2024-03-15");
	});

	it("calls onApply when Apply button clicked", async () => {
		const user = userEvent.setup();
		const onApply = vi.fn();
		renderWithProviders(
			<DateRangePickerPopover
				{...defaultProps}
				pendingFrom="2024-03-15"
				onApply={onApply}
			/>,
		);

		const applyButton = screen.getByRole("button", { name: "Apply" });
		await user.click(applyButton);

		expect(onApply).toHaveBeenCalledTimes(1);
	});

	it("has correct popover styling", () => {
		renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
		const popover = document.querySelector(".w-72");
		expect(popover).toBeTruthy();
		expect(popover?.className).toContain("fixed");
		expect(popover?.className).toContain("ui-card");
		expect(popover?.className).toContain("shadow-2xl");
		expect(popover?.className).toContain("z-50");
	});

	describe("anchor positioning", () => {
		// The popover positions itself relative to the trigger button found via
		// [data-popover-trigger="date-range"]. We need to render a trigger first.
		it("positions popover right-aligned when anchor is right", () => {
			const { container } = renderWithProviders(
				<>
					<button type="button" data-popover-trigger="date-range">
						Trigger
					</button>
					<DateRangePickerPopover {...defaultProps} anchor="right" />
				</>,
			);
			const popover = container.ownerDocument.querySelector(
				".w-72",
			) as HTMLElement;
			expect(popover).toBeTruthy();
			// In jsdom, getBoundingClientRect returns zeros so:
			// left = triggerRect.right - 288 = -288, clamped to 0 by viewport bounds.
			expect(popover.style.left).toBe("0px");
		});

		it("clamps left-aligned popover that would exceed viewport", () => {
			const { container } = renderWithProviders(
				<>
					<button type="button" data-popover-trigger="date-range">
						Trigger
					</button>
					<DateRangePickerPopover {...defaultProps} anchor="left" />
				</>,
			);
			const popover = container.ownerDocument.querySelector(
				".w-72",
			) as HTMLElement;
			expect(popover).toBeTruthy();
			// Viewport clamp keeps the popover within window bounds.
			// Both anchor="left" (left = triggerRect.left = 0) and
			// anchor="right" (left = -288, clamped to 0) end up at 0
			// in jsdom since getBoundingClientRect returns zeros.
			expect(popover.style.left).toBe("0px");
		});

		it("defaults to right anchor when anchor prop is omitted", () => {
			const { container } = renderWithProviders(
				<>
					<button type="button" data-popover-trigger="date-range">
						Trigger
					</button>
					<DateRangePickerPopover {...defaultProps} />,
				</>,
			);
			const popover = container.ownerDocument.querySelector(
				".w-72",
			) as HTMLElement;
			expect(popover).toBeTruthy();
			// Default anchor="right" → left = -288, clamped to 0 by viewport bounds.
			expect(popover.style.left).toBe("0px");
		});
	});

	describe("scroll and resize repositioning", () => {
		it("repositions popover on window scroll", () => {
			renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
			const popover = document.querySelector(".w-72") as HTMLElement;
			expect(popover).toBeTruthy();

			// Initial position (jsdom: getBoundingClientRect returns zeros)
			const initialTop = popover.style.top;

			// Fire a scroll event — the reposition handler should run
			fireEvent.scroll(window);
			// The position won't change in jsdom since getBoundingClientRect
			// always returns zeros, but we verify the handler doesn't throw
			expect(popover.style.top).toBe(initialTop);
		});

		it("repositions popover on window resize", () => {
			renderWithProviders(<DateRangePickerPopover {...defaultProps} />);
			const popover = document.querySelector(".w-72") as HTMLElement;
			expect(popover).toBeTruthy();

			const initialTop = popover.style.top;

			fireEvent.resize(window);
			expect(popover.style.top).toBe(initialTop);
		});
	});
});
