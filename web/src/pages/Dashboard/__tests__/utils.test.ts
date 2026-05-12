import { describe, expect, it } from "vitest";
import { formatCompact } from "../utils";

describe("formatCompact", () => {
	it("formats zero as '0'", () => {
		expect(formatCompact(0)).toBe("0");
	});

	it("formats single digits without suffix", () => {
		expect(formatCompact(1)).toBe("1");
		expect(formatCompact(5)).toBe("5");
		expect(formatCompact(9)).toBe("9");
	});

	it("formats numbers below 1000 without suffix", () => {
		expect(formatCompact(999)).toBe("999");
		expect(formatCompact(100)).toBe("100");
		expect(formatCompact(50)).toBe("50");
	});

	it("formats thousands with K suffix", () => {
		expect(formatCompact(1000)).toBe("1K");
		expect(formatCompact(1500)).toBe("1.5K");
		expect(formatCompact(2000)).toBe("2K");
		expect(formatCompact(2500)).toBe("2.5K");
	});

	it("formats large thousands correctly", () => {
		expect(formatCompact(999500)).toBe("999.5K");
		expect(formatCompact(999900)).toBe("999.9K");
	});

	it("formats millions with M suffix", () => {
		expect(formatCompact(1000000)).toBe("1M");
		expect(formatCompact(2500000)).toBe("2.5M");
		expect(formatCompact(3000000)).toBe("3M");
		expect(formatCompact(10000000)).toBe("10M");
	});

	it("formats negative numbers correctly", () => {
		expect(formatCompact(-1000)).toBe("-1K");
		expect(formatCompact(-1500)).toBe("-1.5K");
		expect(formatCompact(-2500000)).toBe("-2.5M");
	});

	it("formats decimal values correctly", () => {
		expect(formatCompact(0.5)).toBe("0.5");
		expect(formatCompact(0.1)).toBe("0.1");
		expect(formatCompact(99.9)).toBe("99.9");
	});

	it("handles exact boundary at 1000", () => {
		expect(formatCompact(999)).toBe("999");
		expect(formatCompact(1000)).toBe("1K");
	});

	it("handles exact boundary at 1000000", () => {
		expect(formatCompact(999999)).toBe("1000K");
		expect(formatCompact(1000000)).toBe("1M");
	});

	it("removes trailing .0 from formatted numbers", () => {
		expect(formatCompact(1000)).toBe("1K");
		expect(formatCompact(2000)).toBe("2K");
		expect(formatCompact(1000000)).toBe("1M");
		expect(formatCompact(5000000)).toBe("5M");
	});
});
