import { describe, expect, it } from "vitest";
import {
	daysInMonth,
	firstDayOfMonth,
	formatDateRangeShort,
	pad,
	todayISO,
	toISODate,
} from "../../../components/AccentCalendar.utils";
import { formatMs, formatTPS } from "../utils";

describe("toISODate", () => {
	it("formats date as YYYY-MM-DD", () => {
		const date = new Date(2024, 0, 15); // January 15, 2024
		expect(toISODate(date)).toBe("2024-01-15");
	});

	it("pads single-digit months", () => {
		const date = new Date(2024, 2, 5); // March 5, 2024
		expect(toISODate(date)).toBe("2024-03-05");
	});

	it("pads single-digit days", () => {
		const date = new Date(2024, 11, 3); // December 3, 2024
		expect(toISODate(date)).toBe("2024-12-03");
	});

	it("handles double-digit months and days", () => {
		const date = new Date(2024, 9, 25); // October 25, 2024
		expect(toISODate(date)).toBe("2024-10-25");
	});

	it("handles year boundary", () => {
		const date = new Date(2023, 0, 1); // January 1, 2023
		expect(toISODate(date)).toBe("2023-01-01");
	});
});

describe("todayISO", () => {
	it("returns string in YYYY-MM-DD format", () => {
		const result = todayISO();
		expect(result).toMatch(/^\d{4}-\d{2}-\d{2}$/);
	});

	it("returns today's date", () => {
		const today = new Date();
		const expected = toISODate(today);
		expect(todayISO()).toBe(expected);
	});
});

describe("daysInMonth", () => {
	it("returns 31 for January", () => {
		expect(daysInMonth(2024, 0)).toBe(31);
	});

	it("returns 28 for February in non-leap year", () => {
		expect(daysInMonth(2023, 1)).toBe(28);
	});

	it("returns 29 for February in leap year", () => {
		expect(daysInMonth(2024, 1)).toBe(29);
		expect(daysInMonth(2000, 1)).toBe(29); // divisible by 400
		expect(daysInMonth(1900, 1)).toBe(28); // divisible by 100 but not 400
	});

	it("returns 30 for April", () => {
		expect(daysInMonth(2024, 3)).toBe(30);
	});

	it("returns 31 for December", () => {
		expect(daysInMonth(2024, 11)).toBe(31);
	});

	it("handles all months correctly", () => {
		expect(daysInMonth(2024, 0)).toBe(31); // Jan
		expect(daysInMonth(2024, 1)).toBe(29); // Feb (leap)
		expect(daysInMonth(2024, 2)).toBe(31); // Mar
		expect(daysInMonth(2024, 3)).toBe(30); // Apr
		expect(daysInMonth(2024, 4)).toBe(31); // May
		expect(daysInMonth(2024, 5)).toBe(30); // Jun
		expect(daysInMonth(2024, 6)).toBe(31); // Jul
		expect(daysInMonth(2024, 7)).toBe(31); // Aug
		expect(daysInMonth(2024, 8)).toBe(30); // Sep
		expect(daysInMonth(2024, 9)).toBe(31); // Oct
		expect(daysInMonth(2024, 10)).toBe(30); // Nov
		expect(daysInMonth(2024, 11)).toBe(31); // Dec
	});
});

describe("firstDayOfMonth", () => {
	it("returns 0-6 for day of week", () => {
		const result = firstDayOfMonth(2024, 0);
		expect(result).toBeGreaterThanOrEqual(0);
		expect(result).toBeLessThanOrEqual(6);
	});

	it("returns correct day for known dates", () => {
		// January 1, 2024 was a Monday (1)
		expect(firstDayOfMonth(2024, 0)).toBe(1);
		// January 1, 2023 was a Sunday (0)
		expect(firstDayOfMonth(2023, 0)).toBe(0);
	});

	it("handles different months", () => {
		expect(firstDayOfMonth(2024, 1)).toBe(4); // Feb 1, 2024 was Thursday
		expect(firstDayOfMonth(2024, 6)).toBe(1); // July 1, 2024 was Monday
	});
});

describe("pad", () => {
	it("pads single digit with leading zero", () => {
		expect(pad(0)).toBe("00");
		expect(pad(1)).toBe("01");
		expect(pad(5)).toBe("05");
		expect(pad(9)).toBe("09");
	});

	it("returns double digits unchanged", () => {
		expect(pad(10)).toBe("10");
		expect(pad(12)).toBe("12");
		expect(pad(59)).toBe("59");
		expect(pad(99)).toBe("99");
	});

	it("handles larger numbers", () => {
		expect(pad(100)).toBe("100");
		expect(pad(1000)).toBe("1000");
	});
});

describe("formatDateRangeShort", () => {
	it("formats same-month range with abbreviated format", () => {
		const from = "2024-03-05";
		const to = "2024-03-15";
		expect(formatDateRangeShort(from, to)).toBe("05/03-15/03/2024");
	});

	it("formats different-month range with full format", () => {
		const from = "2024-02-25";
		const to = "2024-03-05";
		expect(formatDateRangeShort(from, to)).toBe("25/02/24 - 05/03/2024");
	});

	it("handles year boundary", () => {
		const from = "2023-12-25";
		const to = "2024-01-05";
		expect(formatDateRangeShort(from, to)).toBe("25/12/23 - 05/01/2024");
	});

	it("handles same day", () => {
		const from = "2024-03-15";
		const to = "2024-03-15";
		expect(formatDateRangeShort(from, to)).toBe("15/03-15/03/2024");
	});

	it("handles ISO timestamp inputs", () => {
		// Local-time timestamps (no Z suffix) so toISODate extracts
		// the same date components regardless of the runner's timezone.
		const from = "2024-03-05T10:30:00";
		const to = "2024-03-15T18:00:00";
		expect(formatDateRangeShort(from, to)).toBe("05/03-15/03/2024");
	});

	it("handles mixed bare-date and timestamp inputs", () => {
		const from = "2024-02-25";
		const to = "2024-03-05T12:00:00";
		expect(formatDateRangeShort(from, to)).toBe("25/02/24 - 05/03/2024");
	});
});

describe("formatTPS", () => {
	it("returns '-' for null", () => {
		expect(formatTPS(null)).toBe("-");
	});

	it("returns '-' for zero", () => {
		expect(formatTPS(0)).toBe("-");
	});

	it("formats 45.5 as '45.5'", () => {
		expect(formatTPS(45.5)).toBe("45.5");
	});

	it("formats 1000.123 as '1000.1' (1 decimal)", () => {
		expect(formatTPS(1000.123)).toBe("1000.1");
	});

	it("returns '-' for undefined", () => {
		expect(formatTPS(undefined as unknown as null)).toBe("-");
	});
});

describe("formatMs", () => {
	it("returns '-' for null", () => {
		expect(formatMs(null)).toBe("-");
	});

	it("returns '-' for undefined", () => {
		expect(formatMs(undefined)).toBe("-");
	});

	it("returns '-' for zero", () => {
		expect(formatMs(0)).toBe("-");
	});

	it("formats number with 2 decimals by default", () => {
		expect(formatMs(100)).toBe("100.00ms");
		expect(formatMs(100.5)).toBe("100.50ms");
		expect(formatMs(100.123)).toBe("100.12ms");
	});

	it("respects custom decimals parameter", () => {
		expect(formatMs(100, 0)).toBe("100ms");
		expect(formatMs(100, 1)).toBe("100.0ms");
		expect(formatMs(100, 3)).toBe("100.000ms");
		expect(formatMs(100.1234, 3)).toBe("100.123ms");
	});

	it("handles small values", () => {
		expect(formatMs(0.5)).toBe("0.50ms");
		expect(formatMs(0.001)).toBe("0.00ms");
	});

	it("handles large values", () => {
		expect(formatMs(1000)).toBe("1000.00ms");
		expect(formatMs(10000.5)).toBe("10000.50ms");
	});
});
