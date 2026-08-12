import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DatePickerPopover } from "../DatePickerPopover";

const base = {
	value: null as string | null,
	minDate: "2030-06-15",
	onSelect: vi.fn(),
	onApply: vi.fn(),
	onCancel: vi.fn(),
	onClose: vi.fn(),
};

describe("DatePickerPopover", () => {
	it("disables apply until a day is selected", () => {
		render(<DatePickerPopover {...base} />);
		expect(screen.getByTestId("date-picker-apply")).toBeDisabled();
	});

	it("enables apply once a value is present and fires onApply", () => {
		const onApply = vi.fn();
		render(
			<DatePickerPopover {...base} value="2030-06-20" onApply={onApply} />,
		);
		const apply = screen.getByTestId("date-picker-apply");
		expect(apply).not.toBeDisabled();
		fireEvent.click(apply);
		expect(onApply).toHaveBeenCalled();
	});

	it("cancel fires onCancel", () => {
		const onCancel = vi.fn();
		render(
			<DatePickerPopover {...base} value="2030-06-20" onCancel={onCancel} />,
		);
		fireEvent.click(screen.getByTestId("date-picker-cancel"));
		expect(onCancel).toHaveBeenCalled();
	});

	it("renders the schedule hint", () => {
		render(<DatePickerPopover {...base} />);
		expect(screen.getByTestId("date-picker-hint")).toBeInTheDocument();
	});
});
