import { describe, expect, it } from "vitest";
import { formatDuration, splitDuration } from "../logDetailUtils";

describe("splitDuration", () => {
	it("returns milliseconds for values under 1000", () => {
		expect(splitDuration(0)).toEqual({ value: "0", unit: "ms" });
		expect(splitDuration(500)).toEqual({ value: "500", unit: "ms" });
		expect(splitDuration(999)).toEqual({ value: "999", unit: "ms" });
	});

	it("returns seconds for values 1000 and above", () => {
		expect(splitDuration(1000)).toEqual({ value: "1.00", unit: "s" });
		expect(splitDuration(1450)).toEqual({ value: "1.45", unit: "s" });
		expect(splitDuration(1500)).toEqual({ value: "1.50", unit: "s" });
		expect(splitDuration(60000)).toEqual({ value: "60.00", unit: "s" });
	});
});

describe("formatDuration", () => {
	it("returns milliseconds for values under 1000", () => {
		expect(formatDuration(0)).toBe("0ms");
		expect(formatDuration(500)).toBe("500ms");
		expect(formatDuration(999)).toBe("999ms");
	});

	it("returns seconds for values 1000 and above", () => {
		expect(formatDuration(1000)).toBe("1.00s");
		expect(formatDuration(1450)).toBe("1.45s");
		expect(formatDuration(1500)).toBe("1.50s");
		expect(formatDuration(60000)).toBe("60.00s");
	});
});
