import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { DateFilterButton } from "./DateFilterButton";

describe("DateFilterButton", () => {
	it("renders calendar button", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={false}
				dateFrom={null}
				dateTo={null}
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		expect(
			screen.getByRole("button", { name: /filter by date range/i }),
		).toBeInTheDocument();
	});

	it("shows clear button when hasDateFilter is true", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={true}
				dateFrom="2024-01-01"
				dateTo="2024-01-31"
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		expect(
			screen.getByRole("button", { name: /clear date filter/i }),
		).toBeInTheDocument();
	});

	it("hides clear button when hasDateFilter is false", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={false}
				dateFrom={null}
				dateTo={null}
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		expect(
			screen.queryByRole("button", { name: /clear date filter/i }),
		).not.toBeInTheDocument();
	});

	it("calls onToggleDatePicker when calendar button clicked", async () => {
		const user = userEvent.setup();
		const onToggleDatePicker = vi.fn();
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={false}
				dateFrom={null}
				dateTo={null}
				onToggleDatePicker={onToggleDatePicker}
				onClearDateFilter={vi.fn()}
			/>,
		);

		const calendarButton = screen.getByRole("button", {
			name: /filter by date range/i,
		});
		await user.click(calendarButton);

		expect(onToggleDatePicker).toHaveBeenCalledTimes(1);
	});

	it("calls onClearDateFilter when clear button clicked", async () => {
		const user = userEvent.setup();
		const onClearDateFilter = vi.fn();
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={true}
				dateFrom="2024-01-01"
				dateTo="2024-01-31"
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={onClearDateFilter}
			/>,
		);

		const clearButton = screen.getByRole("button", {
			name: /clear date filter/i,
		});
		await user.click(clearButton);

		expect(onClearDateFilter).toHaveBeenCalledTimes(1);
	});

	it("has aria-label with date range when filter is active", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={true}
				dateFrom="2024-01-01"
				dateTo="2024-01-31"
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		const calendarButton = screen.getByRole("button", {
			name: /date filter:/i,
		});
		expect(calendarButton).toBeInTheDocument();
		expect(calendarButton).toHaveAttribute(
			"aria-label",
			expect.stringContaining("Date filter:"),
		);
	});

	it("has default aria-label when no filter", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={false}
				dateFrom={null}
				dateTo={null}
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		const calendarButton = screen.getByRole("button", {
			name: /filter by date range/i,
		});
		expect(calendarButton).toHaveAttribute(
			"aria-label",
			"Filter by date range",
		);
	});

	it("calendar button has correct styling when no filter", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={false}
				dateFrom={null}
				dateTo={null}
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		const calendarButton = screen.getByRole("button", {
			name: /filter by date range/i,
		});
		expect(calendarButton).toHaveClass("bg-gray-900/40");
		expect(calendarButton).toHaveClass("text-gray-400");
		expect(calendarButton).toHaveClass("border-gray-700/50");
	});

	it("calendar button has accent styling when filter active", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={true}
				dateFrom="2024-01-01"
				dateTo="2024-01-31"
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		const calendarButton = screen.getByRole("button", {
			name: /date filter:/i,
		});
		expect(calendarButton).toHaveClass("bg-(--accent)/15");
		expect(calendarButton).toHaveClass("text-(--accent)");
		expect(calendarButton).toHaveClass("border-(--accent)/40");
	});

	it("clear button has accent styling", () => {
		renderWithProviders(
			<DateFilterButton
				hasDateFilter={true}
				dateFrom="2024-01-01"
				dateTo="2024-01-31"
				onToggleDatePicker={vi.fn()}
				onClearDateFilter={vi.fn()}
			/>,
		);
		const clearButton = screen.getByRole("button", {
			name: /clear date filter/i,
		});
		expect(clearButton).toHaveClass("bg-(--accent)/30");
		expect(clearButton).toHaveClass("text-(--accent)");
	});
});
