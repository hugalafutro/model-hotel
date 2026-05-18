import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useDateRangePicker } from "../useDateRangePicker";

// Mock todayISO so tests are deterministic
vi.mock("../../components/AccentCalendar.utils", () => ({
	todayISO: vi.fn(() => "2024-06-15"),
	toISODate: vi.fn((d: Date) => {
		const y = d.getFullYear();
		const m = String(d.getMonth() + 1).padStart(2, "0");
		const day = String(d.getDate()).padStart(2, "0");
		return `${y}-${m}-${day}`;
	}),
}));

describe("useDateRangePicker", () => {
	it("initializes with empty date state", () => {
		const { result } = renderHook(() => useDateRangePicker());
		expect(result.current.dateFrom).toBe("");
		expect(result.current.dateTo).toBe("");
		expect(result.current.hasDateFilter).toBe(false);
		expect(result.current.showDatePicker).toBe(false);
		expect(result.current.pendingFrom).toBe("");
		expect(result.current.pendingTo).toBe("");
	});

	it("handleCalendarSelect sets start date when nothing selected", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		expect(result.current.pendingFrom).toBe("2024-06-10");
		expect(result.current.pendingTo).toBe("");
	});

	it("handleCalendarSelect sets end date when start already selected", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.handleCalendarSelect("2024-06-15");
		});
		expect(result.current.pendingFrom).toBe("2024-06-10");
		expect(result.current.pendingTo).toBe("2024-06-15");
	});

	it("handleCalendarSelect auto-swaps when end < start", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-15");
		});
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		expect(result.current.pendingFrom).toBe("2024-06-10");
		expect(result.current.pendingTo).toBe("2024-06-15");
	});

	it("handleCalendarSelect resets range when both already selected", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.handleCalendarSelect("2024-06-15");
		});
		// Both selected, click again should start fresh
		act(() => {
			result.current.handleCalendarSelect("2024-06-20");
		});
		expect(result.current.pendingFrom).toBe("2024-06-20");
		expect(result.current.pendingTo).toBe("");
	});

	it("applyDateFilter sets dateFrom/dateTo from pending", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.handleCalendarSelect("2024-06-15");
		});
		act(() => {
			result.current.applyDateFilter();
		});
		expect(result.current.dateFrom).toBeTruthy();
		expect(result.current.dateTo).toBeTruthy();
		expect(result.current.hasDateFilter).toBe(true);
		expect(result.current.showDatePicker).toBe(false);
	});

	it("applyDateFilter handles single day (no end date)", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.applyDateFilter();
		});
		expect(result.current.dateFrom).toBeTruthy();
		expect(result.current.dateTo).toBeTruthy();
		expect(result.current.hasDateFilter).toBe(true);
		// dateTo should be near end of the same day (time varies by timezone)
		expect(result.current.dateTo).toContain("59:59");
	});

	it("clearDateFilter resets all state", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.handleCalendarSelect("2024-06-15");
		});
		act(() => {
			result.current.applyDateFilter();
		});
		expect(result.current.hasDateFilter).toBe(true);
		act(() => {
			result.current.clearDateFilter();
		});
		expect(result.current.dateFrom).toBe("");
		expect(result.current.dateTo).toBe("");
		expect(result.current.pendingFrom).toBe("");
		expect(result.current.pendingTo).toBe("");
		expect(result.current.hasDateFilter).toBe(false);
		expect(result.current.showDatePicker).toBe(false);
	});

	it("toggleDatePicker opens and closes", () => {
		const { result } = renderHook(() => useDateRangePicker());
		expect(result.current.showDatePicker).toBe(false);
		act(() => {
			result.current.toggleDatePicker();
		});
		expect(result.current.showDatePicker).toBe(true);
		act(() => {
			result.current.toggleDatePicker();
		});
		expect(result.current.showDatePicker).toBe(false);
	});

	it("toggleDatePicker restores pending from applied filter", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.handleCalendarSelect("2024-06-15");
		});
		act(() => {
			result.current.applyDateFilter();
		});
		// Picker is already closed by applyDateFilter
		expect(result.current.showDatePicker).toBe(false);
		// Re-open — should restore pending from applied filter
		act(() => {
			result.current.toggleDatePicker();
		});
		expect(result.current.pendingFrom).toBe("2024-06-10");
		expect(result.current.pendingTo).toBe("2024-06-15");
	});

	it("closeDatePicker closes without toggling", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.toggleDatePicker();
		});
		expect(result.current.showDatePicker).toBe(true);
		act(() => {
			result.current.closeDatePicker();
		});
		expect(result.current.showDatePicker).toBe(false);
		// Closing again should be a no-op (already closed)
		act(() => {
			result.current.closeDatePicker();
		});
		expect(result.current.showDatePicker).toBe(false);
	});

	it("calls onFilterChange callback on apply", () => {
		const onFilterChange = vi.fn();
		const { result } = renderHook(() => useDateRangePicker(onFilterChange));
		act(() => {
			result.current.handleCalendarSelect("2024-06-10");
		});
		act(() => {
			result.current.applyDateFilter();
		});
		expect(onFilterChange).toHaveBeenCalledTimes(1);
	});

	it("calls onFilterChange callback on clear", () => {
		const onFilterChange = vi.fn();
		const { result } = renderHook(() => useDateRangePicker(onFilterChange));
		act(() => {
			result.current.clearDateFilter();
		});
		expect(onFilterChange).toHaveBeenCalledTimes(1);
	});

	it("pickerYear/pickerMonth use pendingFrom when picker is open", () => {
		const { result } = renderHook(() => useDateRangePicker());
		act(() => {
			result.current.toggleDatePicker();
		});
		act(() => {
			result.current.handleCalendarSelect("2024-03-10");
		});
		expect(result.current.pickerYear).toBe(2024);
		expect(result.current.pickerMonth).toBe(2); // March = 2
	});
});
