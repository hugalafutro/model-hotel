import { screen } from "@testing-library/react";
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

	it("anchors to right side by default", () => {
		const { container } = renderWithProviders(
			<DateRangePickerPopover {...defaultProps} />,
		);
		const popover = container.firstChild as HTMLElement;
		expect(popover).toHaveClass("right-0");
		expect(popover).not.toHaveClass("left-0");
	});

	it("anchors to left side when anchor='left'", () => {
		const { container } = renderWithProviders(
			<DateRangePickerPopover {...defaultProps} anchor="left" />,
		);
		const popover = container.firstChild as HTMLElement;
		expect(popover).toHaveClass("left-0");
		expect(popover).not.toHaveClass("right-0");
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
		const { container } = renderWithProviders(
			<DateRangePickerPopover {...defaultProps} />,
		);
		const popover = container.firstChild as HTMLElement;
		expect(popover).toHaveClass("absolute");
		expect(popover).toHaveClass("ui-card");
		expect(popover).toHaveClass("shadow-2xl");
		expect(popover).toHaveClass("z-50");
	});
});
