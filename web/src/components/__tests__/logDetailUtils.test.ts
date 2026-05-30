import { describe, expect, it } from "vitest";
import { splitDuration } from "../logDetailUtils";

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
