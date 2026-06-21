import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { AccentCalendar } from "../AccentCalendar";

// Mock todayISO to control "today" for consistent tests
vi.mock("../AccentCalendar.utils", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../AccentCalendar.utils")>();
	return {
		...actual,
		todayISO: vi.fn(() => "2024-03-15"),
	};
});

describe("AccentCalendar", () => {
	const mockOnSelect = vi.fn();
	const defaultProps = {
		initialYear: 2024,
		initialMonth: 2, // March (0-indexed)
		from: "2024-03-10",
		to: "2024-03-20",
		onSelect: mockOnSelect,
	};

	beforeEach(() => {
		mockOnSelect.mockClear();
	});

	it("renders with initial month and year", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		expect(screen.getByText("March 2024")).toBeInTheDocument();
	});

	it("renders day headers (Su-Sa)", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Day headers use locale-aware Intl.DateTimeFormat narrow weekday names.
		// The weekday header row is the first grid-cols-7 element inside the calendar.
		// Count the direct <div> children — there should be exactly 7 for Su-Sa.
		const weekdayRow = document.querySelector(".grid.grid-cols-7.gap-0\\.5");
		const headerDivs = weekdayRow?.querySelectorAll(":scope > div") ?? [];
		expect(headerDivs.length).toBe(7);
	});

	it("renders day buttons for all days in month", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// March has 31 days
		for (let day = 1; day <= 31; day++) {
			expect(screen.getByText(day.toString())).toBeInTheDocument();
		}
	});

	it("renders blank cells for days before first day of month", () => {
		// March 1, 2024 was a Friday (day 5), so we expect 5 blank cells
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		const blanks = document.querySelectorAll("div:empty");
		expect(blanks.length).toBeGreaterThanOrEqual(5);
	});

	it("navigates to previous month when prev button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Find prev button by its position (first button before month name)
		const allButtons = screen.getAllByRole("button");
		// Navigation buttons are first two, day buttons follow
		await user.click(allButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("February 2024")).toBeInTheDocument();
		});
	});

	it("navigates to next month when next button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Find next button (second button, after prev)
		const allButtons = screen.getAllByRole("button");
		await user.click(allButtons[1]);
		await waitFor(() => {
			expect(screen.getByText("April 2024")).toBeInTheDocument();
		});
	});

	it("wraps year when going prev from January", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AccentCalendar {...defaultProps} initialMonth={0} initialYear={2024} />,
		);
		expect(screen.getByText("January 2024")).toBeInTheDocument();
		const allButtons = screen.getAllByRole("button");
		await user.click(allButtons[0]);
		await waitFor(() => {
			expect(screen.getByText("December 2023")).toBeInTheDocument();
		});
	});

	it("wraps year when going next from December", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<AccentCalendar {...defaultProps} initialMonth={11} initialYear={2024} />,
		);
		expect(screen.getByText("December 2024")).toBeInTheDocument();
		const allButtons = screen.getAllByRole("button");
		await user.click(allButtons[1]);
		await waitFor(() => {
			expect(screen.getByText("January 2025")).toBeInTheDocument();
		});
	});

	it("calls onSelect with correct date string when day clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		const day15Button = screen.getByText("15").closest("button");
		if (day15Button) {
			await user.click(day15Button);
			expect(mockOnSelect).toHaveBeenCalledWith("2024-03-15");
		}
	});

	it("applies isInRange styling for dates within range", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Day 15 should be in range (2024-03-10 to 2024-03-20)
		const day15Button = screen.getByText("15").closest("button");
		expect(day15Button).toHaveClass("bg-(--accent)/20");
		expect(day15Button).toHaveClass("text-(--accent)");
	});

	it("applies isStart styling for from date", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Day 10 is the from date
		const day10Button = screen.getByText("10").closest("button");
		expect(day10Button).toHaveClass("bg-(--accent)");
		expect(day10Button).toHaveClass("text-white");
		expect(day10Button).toHaveClass("font-semibold");
	});

	it("applies isEnd styling for to date", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Day 20 is the to date
		const day20Button = screen.getByText("20").closest("button");
		expect(day20Button).toHaveClass("bg-(--accent)");
		expect(day20Button).toHaveClass("text-white");
		expect(day20Button).toHaveClass("font-semibold");
	});

	it("applies isSelected styling for start and end dates", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		const day10Button = screen.getByText("10").closest("button");
		const day20Button = screen.getByText("20").closest("button");
		// isSelected = isStart || isEnd, both should have accent styling
		expect(day10Button).toHaveClass("bg-(--accent)");
		expect(day20Button).toHaveClass("bg-(--accent)");
	});

	it("applies today border styling when today is outside the selected range", () => {
		// todayISO is mocked to 2024-03-15. The in-range fill outranks the today
		// border, so pick a range that excludes the 15th to see the today styling.
		renderWithProviders(
			<AccentCalendar {...defaultProps} from="2024-03-01" to="2024-03-10" />,
		);
		const todayCell = screen.getByText("15").closest("button");
		expect(todayCell?.className).toContain("border-(--accent)/50");
		// A non-today, out-of-range day keeps plain styling, not the today border.
		const otherCell = screen.getByText("25").closest("button");
		expect(otherCell?.className).not.toContain("border-(--accent)/50");
	});

	it("in-range fill overrides today border when today is within the range", () => {
		// Default range 10-20 includes the mocked today (15), so it gets the
		// in-range fill instead of the today border.
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		const todayCell = screen.getByText("15").closest("button");
		expect(todayCell?.className).toContain("bg-(--accent)/20");
		expect(todayCell?.className).not.toContain("border-(--accent)/50");
	});

	it("applies default styling for dates outside range", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Day 5 is outside the range (2024-03-10 to 2024-03-20)
		const day5Button = screen.getByText("5").closest("button");
		expect(day5Button).toHaveClass("text-gray-300");
		expect(day5Button).toHaveClass("hover:bg-gray-700");
	});

	it("renders prev navigation button", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// First button is prev navigation
		const buttons = screen.getAllByRole("button");
		expect(buttons[0]).toBeInTheDocument();
	});

	it("renders next navigation button", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		// Last button is next navigation
		const buttons = screen.getAllByRole("button");
		expect(buttons[buttons.length - 1]).toBeInTheDocument();
	});

	it("has clickable prev button with hover styling", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		const buttons = screen.getAllByRole("button");
		expect(buttons[0]).toHaveClass("hover:bg-gray-700");
		expect(buttons[0]).toHaveClass("rounded-(--radius-button)");
	});

	it("has clickable next button with hover styling", () => {
		renderWithProviders(<AccentCalendar {...defaultProps} />);
		const buttons = screen.getAllByRole("button");
		expect(buttons[buttons.length - 1]).toHaveClass("hover:bg-gray-700");
		expect(buttons[buttons.length - 1]).toHaveClass(
			"rounded-(--radius-button)",
		);
	});
});
