import { windowPct } from "@web-shared/quota/display";
import { describe, expect, it } from "vitest";

// A window consumed past its cap reads as 100% used / 0% remaining, never
// "105%" and "-5%".
describe("windowPct", () => {
	it("bounds an over-consumed window", () => {
		expect(windowPct(105, "used")).toBe("100%");
		expect(windowPct(105, "remaining")).toBe("0%");
		expect(windowPct(-5, "used")).toBe("0%");
		expect(windowPct(-5, "remaining")).toBe("100%");
	});
	it("passes an ordinary reading through", () => {
		expect(windowPct(40, "used")).toBe("40%");
		expect(windowPct(40, "remaining")).toBe("60%");
		expect(windowPct(null, "used")).toBe("-");
	});
});
