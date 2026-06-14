import { describe, expect, it } from "vitest";
import { formatAxisTick } from "../axisFormat";

describe("formatAxisTick", () => {
	it("abbreviates billions, millions, and thousands", () => {
		expect(formatAxisTick(1_000_000_000, false)).toBe("1B");
		expect(formatAxisTick(1_200_000_000, false)).toBe("1.2B");
		expect(formatAxisTick(100_000_000, false)).toBe("100M");
		expect(formatAxisTick(1_500_000, false)).toBe("1.5M");
		expect(formatAxisTick(350_000, false)).toBe("350K");
		expect(formatAxisTick(1_200, false)).toBe("1.2K");
	});

	it("drops a trailing .0 (1.0M → 1M)", () => {
		expect(formatAxisTick(2_000_000, false)).toBe("2M");
		expect(formatAxisTick(5_000, false)).toBe("5K");
	});

	it("leaves sub-thousand values as locale strings", () => {
		expect(formatAxisTick(0, false)).toBe("0");
		expect(formatAxisTick(999, false)).toBe("999");
		// Rounded to integer when decimals are not allowed.
		expect(formatAxisTick(42.4, false)).toBe("42");
	});

	it("keeps up to two decimals for sub-thousand values when allowDecimals", () => {
		expect(formatAxisTick(42.5, true)).toBe("42.5");
		expect(formatAxisTick(0.25, true)).toBe("0.25");
	});

	it("handles negative values symmetrically", () => {
		expect(formatAxisTick(-1_500_000, false)).toBe("-1.5M");
		expect(formatAxisTick(-2_000, false)).toBe("-2K");
	});
});
